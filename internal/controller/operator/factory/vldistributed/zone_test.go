package vldistributed

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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestGetZones(t *testing.T) {
	type opts struct {
		cr                *vmv1alpha1.VLDistributed
		validate          func(*vmv1alpha1.VLDistributed, *zones)
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		rclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.Background()
		got, err := getZones(ctx, rclient, o.cr)
		assert.NoError(t, err)
		assert.Len(t, got.backends, len(o.cr.Spec.Zones))
		assert.Len(t, got.vlagents, len(o.cr.Spec.Zones))
		o.validate(o.cr, got)
	}

	// override cluster version
	f(opts{
		cr: &vmv1alpha1.VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns",
			},
			Spec: vmv1alpha1.VLDistributedSpec{
				ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
					VLCluster: vmv1alpha1.VLDistributedZoneCluster{
						Spec: vmv1.VLClusterSpec{
							ClusterVersion: "v0.2.0",
						},
					},
				},
				Zones: []vmv1alpha1.VLDistributedZone{
					{
						VLCluster: vmv1alpha1.VLDistributedZoneCluster{
							Name: "external",
						},
					},
					{
						VLCluster: vmv1alpha1.VLDistributedZoneCluster{
							Name: "inline",
							Spec: vmv1.VLClusterSpec{
								ClusterVersion: "v0.1.0",
							},
						},
					},
				},
			},
		},
		validate: func(cr *vmv1alpha1.VLDistributed, zs *zones) {
			clusters := zs.clusterObjects()
			assert.Equal(t, "external", clusters[0].Name)
			assert.Equal(t, "inline", clusters[1].Name)
			assert.Equal(t, cr.Spec.Zones[1].VLCluster.Spec.ClusterVersion, clusters[1].Spec.ClusterVersion)
			assert.Equal(t, cr.Spec.ZoneCommon.VLCluster.Spec.ClusterVersion, clusters[0].Spec.ClusterVersion)
		},
	})

	// add component specs from common
	f(opts{
		cr: &vmv1alpha1.VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns",
			},
			Spec: vmv1alpha1.VLDistributedSpec{
				ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
					VLCluster: vmv1alpha1.VLDistributedZoneCluster{
						Spec: vmv1.VLClusterSpec{
							ClusterVersion: "v0.2.0",
							VLStorage: &vmv1.VLStorage{
								RetentionPeriod: "30d",
							},
						},
					},
				},
				Zones: []vmv1alpha1.VLDistributedZone{
					{
						VLCluster: vmv1alpha1.VLDistributedZoneCluster{
							Name: "external",
						},
					},
					{
						VLCluster: vmv1alpha1.VLDistributedZoneCluster{
							Name: "inline",
							Spec: vmv1.VLClusterSpec{
								ClusterVersion: "v0.1.0",
							},
						},
					},
				},
			},
		},
		validate: func(cr *vmv1alpha1.VLDistributed, zs *zones) {
			clusters := zs.clusterObjects()
			assert.NotNil(t, clusters[0].Spec.VLStorage)
			assert.Equal(t, cr.Spec.ZoneCommon.VLCluster.Spec.VLStorage.RetentionPeriod, clusters[0].Spec.VLStorage.RetentionPeriod)
		},
	})

	// prevAcceptsWrites for VLSingle is based on CreationTimestamp, not desired traffic mode.
	// A zone already in maintenance/read-only mode should still have prevAcceptsWrites=true if
	// the VLSingle already existed, so that the VLAgent PQ is drained before LB exclusion.
	f(opts{
		cr: &vmv1alpha1.VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dist",
				Namespace: "ns",
			},
			Spec: vmv1alpha1.VLDistributedSpec{
				BackendType: vmv1alpha1.VLDistributedBackendTypeVLSingle,
				Zones: []vmv1alpha1.VLDistributedZone{
					{
						Name:        "zone-a",
						TrafficMode: vmv1alpha1.VLDistributedTrafficModeMaintenance,
					},
					{
						Name:        "zone-b",
						TrafficMode: vmv1alpha1.VLDistributedTrafficModeReadWrite,
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			// zone-a VLSingle already exists (non-zero CreationTimestamp)
			&vmv1.VLSingle{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "dist-zone-a",
					Namespace:         "ns",
					CreationTimestamp: metav1.Now(),
				},
			},
			// zone-b VLSingle does not exist yet (will be created by operator)
		},
		validate: func(cr *vmv1alpha1.VLDistributed, zs *zones) {
			// After sorting: zone-b (zero TS, new) sorts before zone-a (existing).
			// Index 0 = zone-b (new): prevAcceptsWrites must be false.
			// Index 1 = zone-a (existing, maintenance): prevAcceptsWrites must be true —
			// traffic mode must NOT suppress PQ drain for an already-existing VLSingle.
			assert.False(t, zs.backends[0].prevAccepts, "new VLSingle must not trigger PQ drain")
			assert.True(t, zs.backends[1].prevAccepts, "existing VLSingle in maintenance must still drain PQ")
		},
	})

	// use VLSingle as zone backend (backendType=VLSingle applies to all zones)
	f(opts{
		cr: &vmv1alpha1.VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dist",
				Namespace: "ns",
			},
			Spec: vmv1alpha1.VLDistributedSpec{
				BackendType: vmv1alpha1.VLDistributedBackendTypeVLSingle,
				Zones: []vmv1alpha1.VLDistributedZone{
					{
						Name: "zone-a",
					},
					{
						Name: "zone-b",
						VLSingle: &vmv1alpha1.VLDistributedZoneSingle{
							Spec: &vmv1.VLSingleSpec{
								RetentionPeriod: "14d",
							},
						},
					},
				},
			},
		},
		validate: func(cr *vmv1alpha1.VLDistributed, zs *zones) {
			singles := zs.singleObjects()
			assert.Equal(t, "dist-zone-a", singles[0].Name)
			assert.Equal(t, "dist-zone-b", singles[1].Name)
			assert.Equal(t, "14d", singles[1].Spec.RetentionPeriod)
			assert.Equal(t, singles[1].GetRemoteWriteURL(), zs.vlagents[0].Spec.RemoteWrite[1].URL)
		},
	})
}

func TestWaitForEmptyPQ(t *testing.T) {
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
		vlAgent := &vmv1.VLAgent{
			ObjectMeta: objMeta,
			Status: vmv1.VLAgentStatus{
				StatusMetadata: vmv1beta1.StatusMetadata{
					UpdateStatus: vmv1beta1.UpdateStatusOperational,
				},
			},
		}
		endpointSlice := discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "random-endpoint-name",
				Namespace: vlAgent.GetNamespace(),
				Labels:    map[string]string{discoveryv1.LabelServiceName: vlAgent.PrefixedName()},
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
					Port: ptr.To(int32(tsPort)),
				},
			},
		}

		rclient := k8stools.GetTestClientWithObjects([]runtime.Object{
			&endpointSlice,
		})

		// VLCluster backend with name "test" in "default" namespace.
		// VLInsert must be non-nil so GetRemoteWriteURL() returns a non-empty URL.
		// Empty URL causes unmarshalMetric to skip the metric (empty dimValue),
		// making waitForEmptyPQ return done=true on the first poll.
		vlCluster := &vmv1.VLCluster{
			ObjectMeta: objMeta,
			Spec:       vmv1.VLClusterSpec{VLInsert: &vmv1.VLInsert{}},
		}
		backendURL := vlCluster.GetRemoteWriteURL()

		zs := &zones{
			httpClient: &http.Client{
				Timeout: httpTimeout,
			},
			vlagents: []*vmv1.VLAgent{vlAgent},
			backends: []vlBackend{{obj: vlCluster}},
		}

		ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
		defer cancel()

		zs.waitForEmptyPQ(ctx, rclient, 1*time.Second, 0)
		err = ctx.Err()
		if len(o.errMsg) > 0 {
			assert.Error(t, err)
			assert.Contains(t, err.Error(), o.errMsg)
		} else {
			assert.NoError(t, err)
		}
		if o.validate != nil {
			o.validate()
		}
		_ = backendURL
	}

	// vlCluster helper with non-nil VLInsert so GetRemoteWriteURL() returns a non-empty URL.
	newVLClusterWithInsert := func() *vmv1.VLCluster {
		return &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       vmv1.VLClusterSpec{VLInsert: &vmv1.VLInsert{}},
		}
	}

	// VLAgent metrics return zero
	f(opts{
		handler: func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `%s{url="%s"} 0`, vlAgentQueueMetricName, newVLClusterWithInsert().GetRemoteWriteURL())
		},
		timeout: time.Second,
	})

	// VLAgent metrics return non-zero then zero
	var callCount int
	var mu sync.Mutex
	value := 100
	f(opts{
		handler: func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			fmt.Fprintf(w, `%s{url="%s"} %d`, vlAgentQueueMetricName, newVLClusterWithInsert().GetRemoteWriteURL(), value)
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

	// VLAgent metrics timeout
	f(opts{
		handler: func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second)
			fmt.Fprintf(w, `%s{url="%s"} 0`, vlAgentQueueMetricName, newVLClusterWithInsert().GetRemoteWriteURL())
		},
		timeout: 500 * time.Millisecond,
		errMsg:  "context deadline exceeded",
	})
}

func TestZonesSorting(t *testing.T) {
	now := metav1.Now()

	type opts struct {
		clusters   []*vmv1.VLCluster
		hasChanges []bool
		wantNames  []string
		validate   func(*zones)
	}

	f := func(o opts) {
		t.Helper()
		zs := &zones{
			backends:     make([]vlBackend, len(o.clusters)),
			vlagents:     make([]*vmv1.VLAgent, len(o.clusters)),
			hasChanges:   make([]bool, len(o.clusters)),
			trafficModes: make([]vmv1alpha1.VLDistributedTrafficMode, len(o.clusters)),
		}
		for i, c := range o.clusters {
			zs.backends[i] = vlBackend{obj: c}
			zs.vlagents[i] = &vmv1.VLAgent{
				ObjectMeta: metav1.ObjectMeta{Name: c.Name},
			}
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
		clusters: []*vmv1.VLCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-a", CreationTimestamp: now},
				Status:     vmv1.VLClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-b", CreationTimestamp: now},
				Status:     vmv1.VLClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusFailed}},
			},
		},
		wantNames: []string{"zone-b", "zone-a"},
	})

	// failed and expanding zones are both non-operational: fall through to next criteria (name)
	f(opts{
		clusters: []*vmv1.VLCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-b", CreationTimestamp: now},
				Status:     vmv1.VLClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusFailed}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-a", CreationTimestamp: now},
				Status:     vmv1.VLClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusExpanding}},
			},
		},
		wantNames: []string{"zone-a", "zone-b"},
	})

	// expanding zones sort before operational
	f(opts{
		clusters: []*vmv1.VLCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-a", CreationTimestamp: now},
				Status:     vmv1.VLClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-b", CreationTimestamp: now},
				Status:     vmv1.VLClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusExpanding}},
			},
		},
		wantNames: []string{"zone-b", "zone-a"},
	})

	// new zones (zero CreationTimestamp) sort before existing
	f(opts{
		clusters: []*vmv1.VLCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-existing", CreationTimestamp: now},
				Status:     vmv1.VLClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-new"},
				Status:     vmv1.VLClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
		},
		wantNames: []string{"zone-new", "zone-existing"},
	})

	// higher observedGeneration sorts first
	f(opts{
		clusters: []*vmv1.VLCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-low-gen", CreationTimestamp: now},
				Status: vmv1.VLClusterStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational, ObservedGeneration: 1},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-high-gen", CreationTimestamp: now},
				Status: vmv1.VLClusterStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational, ObservedGeneration: 5},
				},
			},
		},
		wantNames: []string{"zone-high-gen", "zone-low-gen"},
	})

	// equal status and generation sorts by name ascending
	f(opts{
		clusters: []*vmv1.VLCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-b", CreationTimestamp: now},
				Status: vmv1.VLClusterStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational, ObservedGeneration: 3},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-a", CreationTimestamp: now},
				Status: vmv1.VLClusterStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational, ObservedGeneration: 3},
				},
			},
		},
		wantNames: []string{"zone-a", "zone-b"},
	})

	// priority order: status > zero-timestamp > generation > name
	f(opts{
		clusters: []*vmv1.VLCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-operational-gen1", CreationTimestamp: now},
				Status: vmv1.VLClusterStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational, ObservedGeneration: 1},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-operational-gen5", CreationTimestamp: now},
				Status: vmv1.VLClusterStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational, ObservedGeneration: 5},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-new"},
				Status:     vmv1.VLClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-failed", CreationTimestamp: now},
				Status:     vmv1.VLClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusFailed}},
			},
		},
		wantNames: []string{"zone-failed", "zone-new", "zone-operational-gen5", "zone-operational-gen1"},
	})

	// swap keeps vlagents and hasChanges in sync
	f(opts{
		clusters: []*vmv1.VLCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-b", CreationTimestamp: now},
				Status:     vmv1.VLClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "zone-a", CreationTimestamp: now},
				Status:     vmv1.VLClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational}},
			},
		},
		hasChanges: []bool{true, false},
		wantNames:  []string{"zone-a", "zone-b"},
		validate: func(zs *zones) {
			assert.Equal(t, zs.backends[0].obj.GetName(), zs.vlagents[0].Name)
			assert.Equal(t, zs.backends[1].obj.GetName(), zs.vlagents[1].Name)
			assert.False(t, zs.hasChanges[0])
			assert.True(t, zs.hasChanges[1])
		},
	})
}
