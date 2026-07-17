package migrate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func svcWithHTTPPort(name, namespace string, port int32) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Name: "http", Port: port}},
		},
	}
}

func TestServiceBaseURL(t *testing.T) {
	svc := svcWithHTTPPort("vmsingle-myrelease", "default", 8428)
	url, err := ServiceBaseURL(svc, "http", "http")
	require.NoError(t, err)
	assert.Equal(t, "http://vmsingle-myrelease.default.svc:8428", url)
}

func TestServiceBaseURL_NoHTTPPort(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "grpc", Port: 9000}}},
	}
	_, err := ServiceBaseURL(svc, "http", "http")
	assert.Error(t, err)
}

func TestForceMergeURL(t *testing.T) {
	svc := svcWithHTTPPort("vmsingle-myrelease", "default", 8428)
	url, err := ForceMergeURL(svc)
	require.NoError(t, err)
	assert.Equal(t, "http://vmsingle-myrelease.default.svc:8428/internal/force_merge", url)
}
