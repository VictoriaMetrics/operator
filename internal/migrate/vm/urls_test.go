package vm

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

func TestRemoteWriteURL(t *testing.T) {
	svc := svcWithHTTPPort("vmsingle-myrelease", "default", 8428)
	url, err := remoteWriteURL(svc)
	require.NoError(t, err)
	assert.Equal(t, "http://vmsingle-myrelease.default.svc:8428/api/v1/write", url)
}

func TestRemoteWriteURL_NoHTTPPort(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "grpc", Port: 9000}}},
	}
	_, err := remoteWriteURL(svc)
	assert.Error(t, err)
}
