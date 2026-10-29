package vmdistributed

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"github.com/stretchr/testify/assert"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

func TestGetZones(t *testing.T) {
	type opts struct {
		cr                *vmv1alpha1.VMDistributed
		validate          func(*vmv1alpha1.VMDistributed, *zones)
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		rclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.Background()
		got, err := getZones(ctx, rclient, o.cr)
		assert.NoError(t, err)
		assert.Len(t, got.backends, len(o.cr.Spec.Zones))
		assert.Len(t, got.vmagents, len(o.cr.Spec.Zones))
		o.validate(o.cr, got)
	}

	// override cluster version
	f(opts{
		cr: &vmv1alpha1.VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns",
			},
			Spec: vmv1alpha1.VMDistributedSpec{
				ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
					VMCluster: vmv1alpha1.VMDistributedZoneCluster{
						Spec: vmv1beta1.VMClusterSpec{
							ClusterVersion: "v0.2.0",
						},
					},
				},
				Zones: []vmv1alpha1.VMDistributedZone{
					{
						VMCluster: vmv1alpha1.VMDistributedZoneCluster{
							Name: "external",
						},
					},
					{
						VMCluster: vmv1alpha1.VMDistributedZoneCluster{
							Name: "inline",
							Spec: vmv1beta1.VMClusterSpec{
								ClusterVersion: "v0.1.0",
							},
						},
					},
				},
			},
		},
		validate: func(cr *vmv1alpha1.VMDistributed, zs *zones) {
			clusters := zs.clusterObjects()
			assert.Equal(t, "external", clusters[0].Name)
			assert.Equal(t, "inline", clusters[1].Name)
			assert.Equal(t, cr.Spec.Zones[1].VMCluster.Spec.ClusterVersion, clusters[1].Spec.ClusterVersion)
			assert.Equal(t, cr.Spec.ZoneCommon.VMCluster.Spec.ClusterVersion, clusters[0].Spec.ClusterVersion)
		},
	})

	// add component specs from common
	f(opts{
		cr: &vmv1alpha1.VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns",
			},
			Spec: vmv1alpha1.VMDistributedSpec{
				ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
					VMCluster: vmv1alpha1.VMDistributedZoneCluster{
						Spec: vmv1beta1.VMClusterSpec{
							ClusterVersion: "v0.2.0",
							VMStorage: &vmv1beta1.VMStorage{
								VMInsertPort: "9999",
							},
						},
					},
				},
				Zones: []vmv1alpha1.VMDistributedZone{
					{
						VMCluster: vmv1alpha1.VMDistributedZoneCluster{
							Name: "external",
						},
					},
					{
						VMCluster: vmv1alpha1.VMDistributedZoneCluster{
							Name: "inline",
							Spec: vmv1beta1.VMClusterSpec{
								ClusterVersion: "v0.1.0",
							},
						},
					},
				},
			},
		},
		validate: func(cr *vmv1alpha1.VMDistributed, zs *zones) {
			clusters := zs.clusterObjects()
			assert.NotNil(t, clusters[0].Spec.VMStorage)
			assert.Equal(t, cr.Spec.ZoneCommon.VMCluster.Spec.VMStorage.VMInsertPort, clusters[0].Spec.VMStorage.VMInsertPort)
		},
	})

	// prevAcceptsWrites for VMSingle is based on CreationTimestamp, not desired traffic mode.
	// A zone already in maintenance/read-only mode should still have prevAcceptsWrites=true if
	// the VMSingle already existed, so that the VMAgent PQ is drained before LB exclusion.
	f(opts{
		cr: &vmv1alpha1.VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dist",
				Namespace: "ns",
			},
			Spec: vmv1alpha1.VMDistributedSpec{
				BackendType: vmv1alpha1.VMDistributedBackendTypeVMSingle,
				Zones: []vmv1alpha1.VMDistributedZone{
					{
						Name:        "zone-a",
						TrafficMode: vmv1alpha1.VMDistributedTrafficModeMaintenance,
					},
					{
						Name:        "zone-b",
						TrafficMode: vmv1alpha1.VMDistributedTrafficModeReadWrite,
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			// zone-a VMSingle already exists (non-zero CreationTimestamp)
			&vmv1beta1.VMSingle{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "dist-zone-a",
					Namespace:         "ns",
					CreationTimestamp: metav1.Now(),
				},
			},
			// zone-b VMSingle does not exist yet (will be created by operator)
		},
		validate: func(cr *vmv1alpha1.VMDistributed, zs *zones) {
			// After sorting: zone-b (zero TS, new) sorts before zone-a (existing).
			// Index 0 = zone-b (new): prevAcceptsWrites must be false.
			// Index 1 = zone-a (existing, maintenance): prevAcceptsWrites must be true —
			// traffic mode must NOT suppress PQ drain for an already-existing VMSingle.
			assert.False(t, zs.backends[0].prevAccepts, "new VMSingle must not trigger PQ drain")
			assert.True(t, zs.backends[1].prevAccepts, "existing VMSingle in maintenance must still drain PQ")
		},
	})

	// use VMSingle as zone backend (backendType=VMSingle applies to all zones)
	f(opts{
		cr: &vmv1alpha1.VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dist",
				Namespace: "ns",
			},
			Spec: vmv1alpha1.VMDistributedSpec{
				BackendType: vmv1alpha1.VMDistributedBackendTypeVMSingle,
				Zones: []vmv1alpha1.VMDistributedZone{
					{
						Name: "zone-a",
					},
					{
						Name: "zone-b",
						VMSingle: &vmv1alpha1.VMDistributedZoneSingle{
							Spec: &vmv1beta1.VMSingleSpec{
								RetentionPeriod: "14d",
							},
						},
					},
				},
			},
		},
		validate: func(cr *vmv1alpha1.VMDistributed, zs *zones) {
			singles := zs.singleObjects()
			assert.Equal(t, "dist-zone-a", singles[0].Name)
			assert.Equal(t, "dist-zone-b", singles[1].Name)
			assert.Equal(t, "14d", singles[1].Spec.RetentionPeriod)
			assert.Equal(t, singles[1].GetRemoteWriteURL(), zs.vmagents[0].obj.Spec.RemoteWrite[1].URL)
		},
	})

	// zone with VMAgent disabled has no VMAgent built, but its backend still
	// receives replicated writes from the other zone's VMAgent
	f(opts{
		cr: &vmv1alpha1.VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dist",
				Namespace: "ns",
			},
			Spec: vmv1alpha1.VMDistributedSpec{
				BackendType: vmv1alpha1.VMDistributedBackendTypeVMSingle,
				Zones: []vmv1alpha1.VMDistributedZone{
					{
						Name: "zone-a",
						VMAgent: vmv1alpha1.VMDistributedZoneAgent{Spec: vmv1alpha1.VMDistributedZoneAgentSpec{
							CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(0))},
						}},
					},
					{
						Name: "zone-b",
					},
				},
			},
		},
		validate: func(cr *vmv1alpha1.VMDistributed, zs *zones) {
			singles := zs.singleObjects()
			var zoneAIdx, zoneBIdx int
			if singles[0].Name == "dist-zone-a" {
				zoneAIdx, zoneBIdx = 0, 1
			} else {
				zoneAIdx, zoneBIdx = 1, 0
			}
			assert.Nil(t, zs.vmagents[zoneAIdx].obj, "disabled zone must not have a VMAgent built")
			assert.False(t, zs.vmagents[zoneAIdx].delete, "no stale VMAgent to clean up when none existed before")
			assert.NotNil(t, zs.vmagents[zoneBIdx].obj, "enabled zone must have a VMAgent built")
			assert.Contains(t, remoteWriteURLs(zs.vmagents[zoneBIdx].obj), singles[zoneAIdx].GetRemoteWriteURL(),
				"other zone's VMAgent must still replicate writes into the disabled zone's backend")
		},
	})

	// zone's VMAgent disabled after it already existed: getZones must pick up the
	// leftover CR so upgrade can delete it instead of leaving it running.
	f(opts{
		cr: &vmv1alpha1.VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dist",
				Namespace: "ns",
				UID:       "dist-uid",
			},
			Spec: vmv1alpha1.VMDistributedSpec{
				BackendType: vmv1alpha1.VMDistributedBackendTypeVMSingle,
				Zones: []vmv1alpha1.VMDistributedZone{
					{
						Name: "zone-a",
						VMAgent: vmv1alpha1.VMDistributedZoneAgent{Spec: vmv1alpha1.VMDistributedZoneAgentSpec{
							CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(0))},
						}},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "dist-zone-a",
					Namespace:         "ns",
					CreationTimestamp: metav1.Now(),
					OwnerReferences:   []metav1.OwnerReference{{UID: "dist-uid", Controller: ptr.To(true)}},
				},
				Spec: vmv1beta1.VMAgentSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
				},
			},
		},
		validate: func(cr *vmv1alpha1.VMDistributed, zs *zones) {
			assert.True(t, zs.vmagents[0].delete, "leftover VMAgent CR must be marked for deletion")
			if assert.NotNil(t, zs.vmagents[0].obj, "leftover VMAgent CR must be picked up for cleanup") {
				assert.Equal(t, "dist-zone-a", zs.vmagents[0].obj.Name)
			}
			assert.True(t, zs.hasChanges[0], "stale VMAgent cleanup must trigger an upgrade pass")
		},
	})
}

func remoteWriteURLs(vmAgent *vmv1beta1.VMAgent) []string {
	urls := make([]string, len(vmAgent.Spec.RemoteWrite))
	for i, rw := range vmAgent.Spec.RemoteWrite {
		urls[i] = rw.URL
	}
	return urls
}

// A VMAgent whose shardCount was server-defaulted must not be perpetually reported as changed.
func TestGetZones_PreservesExistingShardCount(t *testing.T) {
	ctx := context.Background()
	cr := &vmv1alpha1.VMDistributed{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dist",
			Namespace: "ns",
		},
		Spec: vmv1alpha1.VMDistributedSpec{
			BackendType: vmv1alpha1.VMDistributedBackendTypeVMSingle,
			Zones: []vmv1alpha1.VMDistributedZone{
				{Name: "zone-a"},
			},
		},
	}

	rclient := k8stools.GetTestClientWithObjects(nil)
	zs1, err := getZones(ctx, rclient, cr)
	assert.NoError(t, err)

	// persist zs1's already-defaulted backend and agent so a second pass sees no changes
	// there either, isolating the check below to the VMAgent shardCount handling.
	persistedBackend := zs1.backends[0].obj.(*vmv1beta1.VMSingle).DeepCopy()
	persistedBackend.ResourceVersion = ""
	persistedAgent := zs1.vmagents[0].obj.DeepCopy()
	persistedAgent.Spec.ShardCount = ptr.To[int32](1)
	persistedAgent.ResourceVersion = ""

	rclient2 := k8stools.GetTestClientWithObjects([]runtime.Object{persistedBackend, persistedAgent})
	zs2, err := getZones(ctx, rclient2, cr)
	assert.NoError(t, err)

	assert.False(t, zs2.hasChanges[0], "defaulted shardCount must not be reported as a spec change")
	assert.Equal(t, ptr.To[int32](1), zs2.vmagents[0].obj.Spec.ShardCount, "existing shardCount must be preserved, not reset to nil")
}

func TestGetZonesDisabledVMAgentOwnership(t *testing.T) {
	cr := &vmv1alpha1.VMDistributed{
		TypeMeta:   metav1.TypeMeta{APIVersion: vmv1alpha1.SchemeGroupVersion.String(), Kind: "VMDistributed"},
		ObjectMeta: metav1.ObjectMeta{Name: "dist", Namespace: "ns", UID: "dist-uid"},
		Spec: vmv1alpha1.VMDistributedSpec{
			BackendType: vmv1alpha1.VMDistributedBackendTypeVMSingle,
			Zones: []vmv1alpha1.VMDistributedZone{{
				Name: "zone-a",
				VMAgent: vmv1alpha1.VMDistributedZoneAgent{Spec: vmv1alpha1.VMDistributedZoneAgentSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(0))},
				}},
			}},
		},
	}

	for _, tc := range []struct {
		name  string
		owner metav1.OwnerReference
		owned bool
	}{
		{name: "unowned"},
		{name: "other owner", owner: metav1.OwnerReference{UID: "other-uid"}},
		{name: "owned", owner: cr.AsOwner(), owned: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := &vmv1beta1.VMAgent{ObjectMeta: metav1.ObjectMeta{
				Name: "dist-zone-a", Namespace: "ns",
			}}
			if tc.owner.UID != "" {
				agent.OwnerReferences = []metav1.OwnerReference{tc.owner}
			}
			rclient := k8stools.GetTestClientWithObjects([]runtime.Object{agent})

			zs, err := getZones(context.Background(), rclient, cr)
			if assert.NoError(t, err) {
				if tc.owned {
					assert.True(t, zs.vmagents[0].delete)
					assert.True(t, zs.hasChanges[0])
				} else {
					assert.Nil(t, zs.vmagents[0].obj)
					assert.False(t, zs.vmagents[0].delete)
				}
			}
		})
	}
}

func TestUpgradeDisabledAgentWaitsForPQ(t *testing.T) {
	backend := &vmv1beta1.VMSingle{ObjectMeta: metav1.ObjectMeta{
		Name: "target", Namespace: "default", CreationTimestamp: metav1.Now(),
	}, Status: vmv1beta1.VMSingleStatus{
		StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational},
	}}
	agent := &vmv1beta1.VMAgent{ObjectMeta: metav1.ObjectMeta{
		Name: "source", Namespace: "default", CreationTimestamp: metav1.Now(),
	}}
	zs := &zones{
		httpClient:   &http.Client{Timeout: httpTimeout},
		vmagents:     []vmAgentRef{{}, {obj: agent}},
		backends:     []vmBackend{{obj: backend}, {obj: &vmv1beta1.VMSingle{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "default"}}}},
		hasChanges:   []bool{false, false},
		trafficModes: []vmv1alpha1.VMDistributedTrafficMode{"", ""},
	}
	cr := &vmv1alpha1.VMDistributed{
		Spec: vmv1alpha1.VMDistributedSpec{
			ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{ReadyTimeout: &metav1.Duration{Duration: 200 * time.Millisecond}},
			VMAuth:     vmv1alpha1.VMDistributedAuth{Enabled: ptr.To(false)},
			Zones:      []vmv1alpha1.VMDistributedZone{{Name: "target"}, {Name: "other"}},
		},
	}
	rclient := k8stools.GetTestClientWithObjects([]runtime.Object{backend})

	// Existing agent can have a persisted queue even with no running replicas.
	err := zs.upgrade(context.Background(), rclient, cr, 0)
	assert.ErrorContains(t, err, "queue for backend=target is drained")

	// Test discovery error
	var discoveryCalls atomic.Int32
	unavailable := k8stools.GetTestClientWithObjectsAndInterceptors([]runtime.Object{backend}, interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, obj client.ObjectList, opts ...client.ListOption) error {
			if _, ok := obj.(*discoveryv1.EndpointSliceList); ok {
				discoveryCalls.Add(1)
				return fmt.Errorf("discovery unavailable")
			}
			return cl.List(ctx, obj, opts...)
		},
	})
	err = zs.upgrade(context.Background(), unavailable, cr, 0)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorContains(t, err, "queue for backend=target is drained")
	assert.Greater(t, discoveryCalls.Load(), int32(0))

	// An agent that never existed has no queue to drain.
	zs.vmagents[1] = vmAgentRef{}
	assert.NoError(t, zs.upgrade(context.Background(), rclient, cr, 0))
	zs.vmagents[1] = vmAgentRef{obj: &vmv1beta1.VMAgent{ObjectMeta: metav1.ObjectMeta{Name: "new", Namespace: "default"}}}
	assert.NoError(t, zs.upgrade(context.Background(), rclient, cr, 0))
}

func TestWaitForAgentPQNoEndpoints(t *testing.T) {
	agent := &vmv1beta1.VMAgent{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "default"}}
	zs := &zones{httpClient: &http.Client{Timeout: httpTimeout}}
	var calls atomic.Int32
	rclient := k8stools.GetTestClientWithObjectsAndInterceptors(nil, interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, obj client.ObjectList, opts ...client.ListOption) error {
			if _, ok := obj.(*discoveryv1.EndpointSliceList); ok && calls.Add(1) == 1 {
				return fmt.Errorf("temporary discovery failure")
			}
			return cl.List(ctx, obj, opts...)
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := zs.waitForAgentPQ(ctx, rclient, time.Millisecond, agent)
	assert.ErrorContains(t, err, "no ready metrics endpoints")
	assert.Greater(t, calls.Load(), int32(1))
}

func TestUpgradeDrainsDisabledAgent(t *testing.T) {
	var pending atomic.Bool
	pending.Store(true)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		value := 0
		if pending.Load() {
			value = 100
		}
		fmt.Fprintf(w, "%s{url=%q} %d\n", vmAgentQueueMetricName, "http://other-backend/write", value)
	}))
	defer ts.Close()
	u, err := url.Parse(ts.URL)
	assert.NoError(t, err)
	host, portStr, err := net.SplitHostPort(u.Host)
	assert.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	assert.NoError(t, err)

	backend := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: "default", CreationTimestamp: metav1.Now()},
		Status: vmv1beta1.VMSingleStatus{
			StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational},
		},
	}
	agent := &vmv1beta1.VMAgent{ObjectMeta: metav1.ObjectMeta{
		Name: "source", Namespace: "default", CreationTimestamp: metav1.Now(),
	}}
	endpoints := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "source-endpoints", Namespace: "default",
			Labels: map[string]string{discoveryv1.LabelServiceName: agent.PrefixedName()},
		},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses: []string{host}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
		}},
		Ports: []discoveryv1.EndpointPort{{Name: ptr.To("http"), Port: ptr.To(int32(port))}},
	}
	rclient := k8stools.GetTestClientWithObjects([]runtime.Object{backend, agent, endpoints})
	zs := &zones{
		httpClient:   &http.Client{Timeout: httpTimeout},
		vmagents:     []vmAgentRef{{obj: agent, delete: true}},
		backends:     []vmBackend{{obj: backend}},
		hasChanges:   []bool{true},
		trafficModes: []vmv1alpha1.VMDistributedTrafficMode{vmv1alpha1.VMDistributedTrafficModeMaintenance},
	}
	cr := &vmv1alpha1.VMDistributed{Spec: vmv1alpha1.VMDistributedSpec{
		ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{ReadyTimeout: &metav1.Duration{Duration: 200 * time.Millisecond}},
		VMAuth:     vmv1alpha1.VMDistributedAuth{Enabled: ptr.To(false)},
		Zones:      []vmv1alpha1.VMDistributedZone{{Name: "target"}},
	}}

	// Keep a write-capable backend unchanged until its stale agent drains.
	zs.backends[0].prevAccepts = true
	backend.Spec.RetentionPeriod = "29d"
	err = zs.upgrade(context.Background(), rclient, cr, 0)
	assert.ErrorContains(t, err, "failed to drain VMAgent=default/source queues")
	var persisted vmv1beta1.VMSingle
	assert.NoError(t, rclient.Get(context.Background(), client.ObjectKeyFromObject(backend), &persisted))
	assert.NotEqual(t, "29d", persisted.Spec.RetentionPeriod)
	zs.backends[0].prevAccepts = false
	backend.Spec.RetentionPeriod = ""

	endpoints.Endpoints[0].Conditions.Ready = ptr.To(false)
	assert.NoError(t, rclient.Update(context.Background(), endpoints))
	err = zs.upgrade(context.Background(), rclient, cr, 0)
	assert.ErrorContains(t, err, "no ready metrics endpoints")
	assert.NoError(t, rclient.Get(context.Background(), client.ObjectKeyFromObject(agent), &vmv1beta1.VMAgent{}))

	endpoints.Endpoints[0].Conditions.Ready = ptr.To(true)
	assert.NoError(t, rclient.Update(context.Background(), endpoints))
	err = zs.upgrade(context.Background(), rclient, cr, 0)
	assert.ErrorContains(t, err, "failed to drain VMAgent=default/source queues")
	assert.NoError(t, rclient.Get(context.Background(), client.ObjectKeyFromObject(agent), &vmv1beta1.VMAgent{}))

	pending.Store(false)
	assert.NoError(t, zs.upgrade(context.Background(), rclient, cr, 0))
	assert.True(t, k8serrors.IsNotFound(rclient.Get(context.Background(), client.ObjectKeyFromObject(agent), &vmv1beta1.VMAgent{})))

	// An agent already scaled to zero has no running queue to poll.
	agent.Spec.ReplicaCount = ptr.To(int32(0))
	agent.ResourceVersion = ""
	assert.NoError(t, rclient.Create(context.Background(), agent))
	pending.Store(true)
	assert.NoError(t, zs.upgrade(context.Background(), rclient, cr, 0))
	assert.True(t, k8serrors.IsNotFound(rclient.Get(context.Background(), client.ObjectKeyFromObject(agent), &vmv1beta1.VMAgent{})))
}

func TestWaitForPQLogsTarget(t *testing.T) {
	backend := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: "default"},
		Spec:       vmv1beta1.VMClusterSpec{VMInsert: &vmv1beta1.VMInsert{}},
	}
	zs := &zones{backends: []vmBackend{{obj: backend}}}
	var lines []string
	ctx := logger.AddToContext(context.Background(), funcr.New(func(prefix, args string) {
		lines = append(lines, prefix+args)
	}, funcr.Options{}))

	assert.NoError(t, zs.waitForEmptyPQ(ctx, k8stools.GetTestClientWithObjects(nil), time.Millisecond, 0))
	if assert.Len(t, lines, 2) {
		for _, line := range lines {
			assert.Contains(t, line, backend.GetRemoteWriteURL())
		}
	}
}

func TestWaitForEmptyPQ(t *testing.T) {
	// Pre-compute the expected backend URL for VMCluster named "test" in ns "default"
	backendURL := (&vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       vmv1beta1.VMClusterSpec{VMInsert: &vmv1beta1.VMInsert{}},
	}).GetRemoteWriteURL()

	type opts struct {
		handler  http.HandlerFunc
		timeout  time.Duration
		validate func()
		errMsg   string
	}

	f := func(o opts) {
		t.Helper()
		mux := http.NewServeMux()
		mux.HandleFunc("/metrics", o.handler)
		ts := httptest.NewServer(mux)
		defer ts.Close()
		tsURL, err := url.Parse(ts.URL)
		assert.NoError(t, err)
		tsHost, tsPortStr, err := net.SplitHostPort(tsURL.Host)
		if err != nil {
			assert.NoError(t, err)
		}
		tsPort, err := strconv.ParseInt(tsPortStr, 10, 32)
		if err != nil {
			assert.NoError(t, err)
		}
		objMeta := metav1.ObjectMeta{
			Name:              "test",
			Namespace:         "default",
			CreationTimestamp: metav1.Now(),
		}
		vmAgent := &vmv1beta1.VMAgent{
			ObjectMeta: objMeta,
			Status: vmv1beta1.VMAgentStatus{
				StatusMetadata: vmv1beta1.StatusMetadata{
					UpdateStatus: vmv1beta1.UpdateStatusOperational,
				},
			},
		}
		endpointSlice := discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "random-endpoint-name",
				Namespace: vmAgent.GetNamespace(),
				Labels:    map[string]string{discoveryv1.LabelServiceName: vmAgent.PrefixedName()},
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses: []string{tsHost},
					Conditions: discoveryv1.EndpointConditions{
						Ready: ptr.To(true),
					},
				},
			},
			Ports: []discoveryv1.EndpointPort{
				{
					Name: ptr.To("http"),
					Port: ptr.To[int32](int32(tsPort)),
				},
			},
		}

		rclient := k8stools.GetTestClientWithObjects([]runtime.Object{
			&endpointSlice,
		})

		backend := &vmv1beta1.VMCluster{
			ObjectMeta: objMeta,
			Spec:       vmv1beta1.VMClusterSpec{VMInsert: &vmv1beta1.VMInsert{}},
		}
		zs := &zones{
			httpClient: &http.Client{
				Timeout: httpTimeout,
			},
			vmagents: []vmAgentRef{{obj: vmAgent}},
			backends: []vmBackend{{obj: backend}},
		}

		ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
		defer cancel()

		err = zs.waitForEmptyPQ(ctx, rclient, 1*time.Second, 0)
		if len(o.errMsg) > 0 {
			assert.Error(t, err)
			assert.Contains(t, err.Error(), o.errMsg)
		} else {
			assert.NoError(t, err)
		}
		if o.validate != nil {
			o.validate()
		}
	}

	// VMAgent metrics return zero
	f(opts{
		handler: func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "%s{url=%q} 0", vmAgentQueueMetricName, backendURL)
		},
		timeout: time.Second,
	})

	// VMAgent metrics return non-zero then zero
	var callCount int
	var mu sync.Mutex
	value := 100
	f(opts{
		handler: func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			fmt.Fprintf(w, "%s{url=%q} %d", vmAgentQueueMetricName, backendURL, value)
			callCount++
			value = 0
		},
		timeout: 4 * time.Second,
		validate: func() {
			mu.Lock()
			defer mu.Unlock()
			assert.Greater(t, callCount, 1)
		},
	})

	// VMAgent metrics timeout
	f(opts{
		handler: func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second) // Simulate a long response
			fmt.Fprintf(w, "%s{url=%q} 0", vmAgentQueueMetricName, backendURL)
		},
		timeout: 500 * time.Millisecond,
		errMsg:  "context deadline exceeded",
	})
}

func TestZonesSorting(t *testing.T) {
	now := metav1.Now()

	type opts struct {
		clusters   []*vmv1beta1.VMCluster
		hasChanges []bool
		wantNames  []string
		validate   func(*zones)
	}

	f := func(o opts) {
		t.Helper()
		zs := &zones{
			backends:     make([]vmBackend, len(o.clusters)),
			vmagents:     make([]vmAgentRef, len(o.clusters)),
			hasChanges:   make([]bool, len(o.clusters)),
			trafficModes: make([]vmv1alpha1.VMDistributedTrafficMode, len(o.clusters)),
		}
		for i, c := range o.clusters {
			zs.backends[i] = vmBackend{obj: c}
			zs.vmagents[i] = vmAgentRef{obj: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{Name: c.Name},
			}}
		}
		if o.hasChanges != nil {
			zs.hasChanges = o.hasChanges
		}
		sort.Sort(zs)
		names := make([]string, len(zs.backends))
		for i, b := range zs.backends {
			names[i] = b.obj.GetName()
		}
		assert.Equal(t, o.wantNames, names)
		if o.validate != nil {
			o.validate(zs)
		}
	}

	// failed zones sort before operational
	f(opts{
		clusters: []*vmv1beta1.VMCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-a", CreationTimestamp: now},
				Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-b", CreationTimestamp: now},
				Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusFailed}},
			},
		},
		wantNames: []string{"zone-b", "zone-a"},
	})

	// failed and expanding zones are both non-operational: fall through to next criteria (name)
	f(opts{
		clusters: []*vmv1beta1.VMCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-b", CreationTimestamp: now},
				Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusFailed}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-a", CreationTimestamp: now},
				Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusExpanding}},
			},
		},
		wantNames: []string{"zone-a", "zone-b"},
	})

	// expanding zones sort before operational
	f(opts{
		clusters: []*vmv1beta1.VMCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-a", CreationTimestamp: now},
				Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-b", CreationTimestamp: now},
				Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusExpanding}},
			},
		},
		wantNames: []string{"zone-b", "zone-a"},
	})

	// new zones (zero CreationTimestamp) sort before existing
	f(opts{
		clusters: []*vmv1beta1.VMCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-existing", CreationTimestamp: now},
				Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-new"},
				Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
		},
		wantNames: []string{"zone-new", "zone-existing"},
	})

	// higher observedGeneration sorts first
	f(opts{
		clusters: []*vmv1beta1.VMCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-low-gen", CreationTimestamp: now},
				Status: vmv1beta1.VMClusterStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational, ObservedGeneration: 1},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-high-gen", CreationTimestamp: now},
				Status: vmv1beta1.VMClusterStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational, ObservedGeneration: 5},
				},
			},
		},
		wantNames: []string{"zone-high-gen", "zone-low-gen"},
	})

	// equal status and generation sorts by name ascending
	f(opts{
		clusters: []*vmv1beta1.VMCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-b", CreationTimestamp: now},
				Status: vmv1beta1.VMClusterStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational, ObservedGeneration: 3},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-a", CreationTimestamp: now},
				Status: vmv1beta1.VMClusterStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational, ObservedGeneration: 3},
				},
			},
		},
		wantNames: []string{"zone-a", "zone-b"},
	})

	// priority order: status > zero-timestamp > generation > name
	f(opts{
		clusters: []*vmv1beta1.VMCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-operational-gen1", CreationTimestamp: now},
				Status: vmv1beta1.VMClusterStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational, ObservedGeneration: 1},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-operational-gen5", CreationTimestamp: now},
				Status: vmv1beta1.VMClusterStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational, ObservedGeneration: 5},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-new"},
				Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-failed", CreationTimestamp: now},
				Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusFailed}},
			},
		},
		wantNames: []string{"zone-failed", "zone-new", "zone-operational-gen5", "zone-operational-gen1"},
	})

	// swap keeps vmagents and hasChanges in sync
	f(opts{
		clusters: []*vmv1beta1.VMCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-b", CreationTimestamp: now},
				Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-a", CreationTimestamp: now},
				Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
		},
		hasChanges: []bool{true, false},
		wantNames:  []string{"zone-a", "zone-b"},
		validate: func(zs *zones) {
			assert.Equal(t, zs.backends[0].obj.GetName(), zs.vmagents[0].obj.Name)
			assert.Equal(t, zs.backends[1].obj.GetName(), zs.vmagents[1].obj.Name)
			assert.False(t, zs.hasChanges[0])
			assert.True(t, zs.hasChanges[1])
		},
	})

}
