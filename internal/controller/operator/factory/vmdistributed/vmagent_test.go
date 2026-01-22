package vmdistributed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// mockVMAgent is a small test double implementing the minimal interface expected by
// waitForVMClusterVMAgentMetrics (VMAgentWithStatus/VMAgentMetrics).
type mockVMAgent struct {
	url      string
	replicas int32
}

func (m *mockVMAgent) AsURL() string {
	return m.url
}
func (m *mockVMAgent) GetMetricPath() string {
	return ""
}
func (m *mockVMAgent) GetReplicas() int32 {
	return m.replicas
}
func (m *mockVMAgent) GetNamespace() string {
	return "default"
}
func (m *mockVMAgent) GetName() string {
	return "test-vmagent"
}
func (m *mockVMAgent) PrefixedName() string {
	// Mirror concrete VMAgent.PrefixedName behaviour for tests:
	// concrete uses the format "vmagent-%s".
	return "vmagent-test-vmagent"
}

// newVMAgentMetricsHandler creates an httptest.Server that serves the provided handler
// and a fake client seeded with a VMAgent and an EndpointSlice that points to the test server.
// It returns the server, the mock VMAgent and the client.
func newVMAgentMetricsHandler(t *testing.T, handler http.Handler) (*httptest.Server, *mockVMAgent, client.Client) {
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	ts := httptest.NewServer(handler)

	// ts.URL contains the URL of the test server
	// we need to ensure endpoint will be set to its host and vmAgent URL will use the same port
	mockVMAgent := &mockVMAgent{url: ts.URL, replicas: 1}
	tsURL, err := url.Parse(ts.URL)
	assert.NoError(t, err)

	vmAgent := vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mockVMAgent.GetName(),
			Namespace: mockVMAgent.GetNamespace(),
		},
		Status: vmv1beta1.VMAgentStatus{
			StatusMetadata: vmv1beta1.StatusMetadata{
				UpdateStatus: vmv1beta1.UpdateStatusOperational,
			},
		},
	}
	endpointSlice := discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "random-endpoint-name",
			Namespace: mockVMAgent.GetNamespace(),
			Labels:    map[string]string{"kubernetes.io/service-name": vmAgent.PrefixedName()},
		},
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: []string{tsURL.Hostname()},
			},
		},
	}

	initialObjects := []client.Object{}
	initialObjects = append(initialObjects, &vmAgent)
	initialObjects = append(initialObjects, &endpointSlice)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(initialObjects...).Build()

	return ts, mockVMAgent, fakeClient
}

func TestWaitForVMClusterVMAgentMetrics(t *testing.T) {
	t.Run("VMAgent metrics return zero", func(t *testing.T) {
		ts, mockVMAgent, trClient := newVMAgentMetricsHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "vm_persistentqueue_bytes_pending{path=\"/tmp\"} 0")
		}))
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		err := waitForVMClusterVMAgentMetrics(ctx, ts.Client(), mockVMAgent, time.Second, 1*time.Second, trClient)
		assert.NoError(t, err)
	})

	t.Run("VMAgent metrics return non-zero then zero", func(t *testing.T) {
		var callCount atomic.Int32
		ts, mockVMAgent, trClient := newVMAgentMetricsHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount.Add(1)
			if callCount.Load() == 1 {
				fmt.Fprintln(w, "vm_persistentqueue_bytes_pending{path=\"/tmp\"} 100")
			} else {
				fmt.Fprintln(w, "vm_persistentqueue_bytes_pending{path=\"/tmp\"} 0")
			}
		}))
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := waitForVMClusterVMAgentMetrics(ctx, ts.Client(), mockVMAgent, 2*time.Second, 1*time.Second, trClient)
		assert.NoError(t, err)
		assert.True(t, callCount.Load() > 1) // Ensure it polled multiple times
	})

	t.Run("VMAgent metrics timeout", func(t *testing.T) {
		ts, mockVMAgent, trClient := newVMAgentMetricsHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second) // Simulate a long response
			fmt.Fprintln(w, "vm_persistentqueue_bytes_pending{path=\"/tmp\"} 0")
		}))
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		err := waitForVMClusterVMAgentMetrics(ctx, ts.Client(), mockVMAgent, 500*time.Millisecond, 1*time.Second, trClient) // Shorter deadline
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to wait for VMAgent metrics")
	})
}

// Unit tests for helper functions and adapter behavior
func TestVMAgentURLHelpers(t *testing.T) {
	t.Run("buildPerIPMetricURL builds proper URL with scheme and port", func(t *testing.T) {
		baseURL := "http://my-svc.default.svc:1234"
		metricPath := "/metrics"
		ip := "10.0.0.1"
		got := buildPerIPMetricURL(baseURL, metricPath, ip)
		expected := "http://10.0.0.1:1234/metrics"
		assert.Equal(t, expected, got)
	})

	t.Run("buildPerIPMetricURL defaults port when not present", func(t *testing.T) {
		baseURL := "http://my-svc.default.svc"
		metricPath := "/metrics"
		ip := "10.0.0.2"
		got := buildPerIPMetricURL(baseURL, metricPath, ip)
		expected := "http://10.0.0.2:8429/metrics"
		assert.Equal(t, expected, got)
	})
}

func TestListVMAgents(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	vmAgent1 := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent1",
			Namespace: "default",
			Labels:    map[string]string{"app": "vmagent", "env": "prod"},
		},
	}
	vmAgent2 := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent2",
			Namespace: "default",
			Labels:    map[string]string{"app": "vmagent", "env": "dev"},
		},
	}
	vmAgent3 := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent3",
			Namespace: "other",
			Labels:    map[string]string{"app": "vmagent", "env": "prod"},
		},
	}

	fc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vmAgent1, vmAgent2, vmAgent3).Build()

	t.Run("match label selector", func(t *testing.T) {
		selector := &metav1.LabelSelector{
			MatchLabels: map[string]string{"env": "prod"},
		}
		agents, err := listVMAgents(context.Background(), fc, "default", selector)
		assert.NoError(t, err)
		assert.Len(t, agents, 1)
		assert.Equal(t, "agent1", agents[0].Name)
	})

	t.Run("match all in namespace", func(t *testing.T) {
		selector := &metav1.LabelSelector{}
		agents, err := listVMAgents(context.Background(), fc, "default", selector)
		assert.NoError(t, err)
		assert.Len(t, agents, 2)
	})

	t.Run("match none", func(t *testing.T) {
		selector := &metav1.LabelSelector{
			MatchLabels: map[string]string{"env": "staging"},
		}
		agents, err := listVMAgents(context.Background(), fc, "default", selector)
		assert.NoError(t, err)
		assert.Len(t, agents, 0)
	})
}
