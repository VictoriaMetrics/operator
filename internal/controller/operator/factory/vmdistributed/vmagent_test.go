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

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestWaitForEmptyPQ(t *testing.T) {
	type opts struct {
		handler  http.HandlerFunc
		timeout  time.Duration
		validate func()
		errMsg   string
	}

	f := func(o opts) {
		t.Helper()
		reqVMAgent := &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}

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
		vmAgent := vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      reqVMAgent.GetName(),
				Namespace: reqVMAgent.GetNamespace(),
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
				Namespace: reqVMAgent.GetNamespace(),
				Labels:    map[string]string{"kubernetes.io/service-name": vmAgent.PrefixedName()},
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
			&vmAgent,
			&endpointSlice,
		})

		ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
		defer cancel()

		err = waitForEmptyPQ(ctx, rclient, reqVMAgent, 1*time.Second)
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
			fmt.Fprintf(w, `%s{path="/tmp"} %d`, vmagentQueueMetricName, value)
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
			fmt.Fprintf(w, `%s{path="/tmp"} 0`, vmagentQueueMetricName)
		},
		timeout: 500 * time.Millisecond,
		errMsg:  "failed to wait for VMAgent metrics",
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
