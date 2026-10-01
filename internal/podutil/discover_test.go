package podutil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDiscoverEndpointAddrs_NilReadyIsTreatedAsReady(t *testing.T) {
	// Per the discoveryv1.EndpointConditions.Ready doc comment: "A nil value indicates an
	// unknown state. In most cases consumers should interpret this unknown state as ready."
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	es := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-endpoints",
			Namespace: "default",
			Labels:    map[string]string{discoveryv1.LabelServiceName: "svc"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{"10.0.0.1"}}, // Ready unset entirely.
			{Addresses: []string{"10.0.0.2"}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}},
			{Addresses: []string{"10.0.0.3"}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(false)}},
		},
		Ports: []discoveryv1.EndpointPort{
			{Name: ptr.To("http"), Port: ptr.To(int32(8080))},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(es).Build()

	addrs, err := DiscoverEndpointAddrs(context.Background(), c, "default", "svc", "http", "http", "/metrics")
	require.NoError(t, err)

	assert.True(t, addrs.Has("http://10.0.0.1:8080/metrics"), "endpoint with unset Ready must be treated as ready")
	assert.True(t, addrs.Has("http://10.0.0.2:8080/metrics"))
	assert.False(t, addrs.Has("http://10.0.0.3:8080/metrics"), "endpoint explicitly not ready must be excluded")
	assert.Len(t, addrs, 2)
}
