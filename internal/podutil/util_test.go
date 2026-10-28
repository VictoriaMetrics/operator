package podutil

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestGetMetricsAddrs(t *testing.T) {
	f := func(ready *bool, wantAddrs []string) {
		t.Helper()
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-eps",
				Namespace: "default",
				Labels:    map[string]string{discoveryv1.LabelServiceName: "vmagent-test"},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses:  []string{"10.0.0.1"},
					Conditions: discoveryv1.EndpointConditions{Ready: ready},
				},
			},
			Ports: []discoveryv1.EndpointPort{
				{Name: ptr.To("http"), Port: ptr.To[int32](8080)},
			},
		}
		rclient := k8stools.GetTestClientWithObjects([]runtime.Object{es})
		cr := &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}
		addrs, err := GetMetricsAddrs(context.Background(), rclient, cr)
		assert.NoError(t, err)
		assert.ElementsMatch(t, wantAddrs, addrs.UnsortedList())
	}

	// ready=true: included
	f(ptr.To(true), []string{"http://10.0.0.1:8080/metrics"})

	// ready=false: excluded
	f(ptr.To(false), nil)

	// ready=nil (unknown): EndpointSlice semantics treat this as ready, so it must be included
	f(nil, []string{"http://10.0.0.1:8080/metrics"})
}

// TestGetMetricsAddrs_IPv6 guards the AddressTypeIPv6 bracketing: an unbracketed IPv6 host
// in a URL is ambiguous with the port separator.
func TestGetMetricsAddrs_IPv6(t *testing.T) {
	es := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-eps-v6",
			Namespace: "default",
			Labels:    map[string]string{discoveryv1.LabelServiceName: "vmagent-test"},
		},
		AddressType: discoveryv1.AddressTypeIPv6,
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses:  []string{"2001:db8::1"},
				Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
			},
		},
		Ports: []discoveryv1.EndpointPort{
			{Name: ptr.To("http"), Port: ptr.To[int32](8080)},
		},
	}
	rclient := k8stools.GetTestClientWithObjects([]runtime.Object{es})
	cr := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
	}
	addrs, err := GetMetricsAddrs(context.Background(), rclient, cr)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"http://[2001:db8::1]:8080/metrics"}, addrs.UnsortedList())
}

func TestGetMetricsAddrsListError(t *testing.T) {
	wantErr := errors.New("endpoint discovery failed")
	rclient := k8stools.GetTestClientWithObjectsAndInterceptors(nil, interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return wantErr
		},
	})

	addrs, err := GetMetricsAddrs(context.Background(), rclient, &vmv1beta1.VMAgent{})
	assert.ErrorIs(t, err, wantErr)
	assert.Empty(t, addrs)
}
