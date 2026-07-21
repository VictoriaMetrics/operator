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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
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
			assert.Equal(t, singles[1].GetRemoteWriteURL(), zs.vmagents[0].Spec.RemoteWrite[1].URL)
		},
	})
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
			vmagents: []*vmv1beta1.VMAgent{vmAgent},
			backends: []vmBackend{{obj: backend}},
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
			vmagents:     make([]*vmv1beta1.VMAgent, len(o.clusters)),
			hasChanges:   make([]bool, len(o.clusters)),
			trafficModes: make([]vmv1alpha1.VMDistributedTrafficMode, len(o.clusters)),
		}
		for i, c := range o.clusters {
			zs.backends[i] = vmBackend{obj: c}
			zs.vmagents[i] = &vmv1beta1.VMAgent{
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
			assert.Equal(t, zs.backends[0].obj.GetName(), zs.vmagents[0].Name)
			assert.Equal(t, zs.backends[1].obj.GetName(), zs.vmagents[1].Name)
			assert.False(t, zs.hasChanges[0])
			assert.True(t, zs.hasChanges[1])
		},
	})

}
