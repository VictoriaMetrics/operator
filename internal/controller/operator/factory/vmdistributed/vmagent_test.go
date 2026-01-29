package vmdistributed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
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
func (m *mockVMAgent) GetMetricsPath() string {
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

func TestWaitForVMClusterVMAgentMetrics(t *testing.T) {
	type opts struct {
		handler  http.HandlerFunc
		deadline time.Duration
		validate func()
		errMsg   string
	}

	f := func(o opts) {
		t.Helper()
		ts := httptest.NewServer(o.handler)
		defer ts.Close()
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

		rclient := k8stools.GetTestClientWithObjects([]runtime.Object{
			&vmAgent,
			&endpointSlice,
		})

		timeout := time.Second
		ctx, cancel := context.WithTimeout(context.Background(), 2*timeout)
		defer cancel()

		err = waitForVMClusterVMAgentMetrics(ctx, ts.Client(), mockVMAgent, o.deadline, timeout, rclient)
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
			fmt.Fprintf(w, `%s{path="/tmp"} 0`, vmagentQueueMetricName)
		},
		deadline: time.Second,
	})

	// VMAgent metrics return non-zero then zero
	var callCount int
	var mu sync.Mutex
	value := 100
	f(opts{
		handler: func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			fmt.Fprintf(w, `%s{path="/tmp"} %d`, vmagentQueueMetricName, value)
			callCount++
			value = 0
		},
		deadline: 4 * time.Second,
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
			fmt.Fprintf(w, `%s{path="/tmp"} 0`, vmagentQueueMetricName)
		},
		deadline: 500 * time.Millisecond,
		errMsg:   "failed to wait for VMAgent metrics",
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
	type opts struct {
		selector *metav1.LabelSelector
		validate func([]*vmv1beta1.VMAgent, error)
	}

	f := func(o opts) {
		t.Helper()
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
		rclient := k8stools.GetTestClientWithObjects([]runtime.Object{vmAgent1, vmAgent2, vmAgent3})
		agents, err := listVMAgents(context.Background(), rclient, "default", o.selector)
		o.validate(agents, err)
	}

	// match label selector
	f(opts{
		selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"env": "prod"},
		},
		validate: func(agents []*vmv1beta1.VMAgent, err error) {
			assert.NoError(t, err)
			assert.Len(t, agents, 1)
			assert.Equal(t, "agent1", agents[0].Name)
		},
	})

	// match all in namespace
	f(opts{
		selector: &metav1.LabelSelector{},
		validate: func(agents []*vmv1beta1.VMAgent, err error) {
			assert.NoError(t, err)
			assert.Len(t, agents, 2)
		},
	})

	// match none
	f(opts{
		selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"env": "staging"},
		},
		validate: func(agents []*vmv1beta1.VMAgent, err error) {
			assert.NoError(t, err)
			assert.Len(t, agents, 0)
		},
	})
}
