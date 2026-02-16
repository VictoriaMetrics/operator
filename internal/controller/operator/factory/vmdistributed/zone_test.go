package vmdistributed

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		assert.Len(t, got.vmclusters, len(o.cr.Spec.Zones))
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
			assert.Equal(t, "external", zs.vmclusters[0].Name)
			assert.Equal(t, "inline", zs.vmclusters[1].Name)
			assert.Equal(t, cr.Spec.Zones[1].VMCluster.Spec.ClusterVersion, zs.vmclusters[1].Spec.ClusterVersion)
			assert.Equal(t, cr.Spec.ZoneCommon.VMCluster.Spec.ClusterVersion, zs.vmclusters[0].Spec.ClusterVersion)
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
			assert.NotNil(t, zs.vmclusters[0].Spec.VMStorage)
			assert.Equal(t, cr.Spec.ZoneCommon.VMCluster.Spec.VMStorage.VMInsertPort, zs.vmclusters[0].Spec.VMStorage.VMInsertPort)
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

		zs := &zones{
			httpClient: &http.Client{
				Timeout: httpTimeout,
			},
			vmagents:   []*vmv1beta1.VMAgent{vmAgent},
			vmclusters: []*vmv1beta1.VMCluster{{ObjectMeta: objMeta}},
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
			fmt.Fprintf(w, `%s{path="/tmp/1_EF46DB3751D8E999"} 0`, vmAgentQueueMetricName)
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
			fmt.Fprintf(w, `%s{path="/tmp/1_EF46DB3751D8E999"} %d`, vmAgentQueueMetricName, value)
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
			fmt.Fprintf(w, `%s{path="/tmp/1_EF46DB3751D8E999"} 0`, vmAgentQueueMetricName)
		},
		timeout: 500 * time.Millisecond,
		errMsg:  "failed to wait for VMAgent metrics",
	})
}
