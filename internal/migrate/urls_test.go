package migrate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func depWithArgs(args ...string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Args: args}}},
			},
		},
	}
}

func rejectTLS(dep *appsv1.Deployment) error {
	return rejectTLSEnabled(dep.Namespace, dep.Name, dep.Spec.Template.Spec.Containers)
}

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
	url, err := serviceBaseURL(svc, "http", "http")
	require.NoError(t, err)
	assert.Equal(t, "http://vmsingle-myrelease.default.svc:8428", url)
}

func TestServiceBaseURL_NoHTTPPort(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "grpc", Port: 9000}}},
	}
	_, err := serviceBaseURL(svc, "http", "http")
	assert.Error(t, err)
}

func TestForceMergeURL(t *testing.T) {
	svc := svcWithHTTPPort("vmsingle-myrelease", "default", 8428)
	url, err := forceMergeURL(svc, "")
	require.NoError(t, err)
	assert.Equal(t, "http://vmsingle-myrelease.default.svc:8428/internal/force_merge", url)
}

func TestForceMergeURL_PathPrefix(t *testing.T) {
	svc := svcWithHTTPPort("vmsingle-myrelease", "default", 8428)
	url, err := forceMergeURL(svc, "/prefix")
	require.NoError(t, err)
	assert.Equal(t, "http://vmsingle-myrelease.default.svc:8428/prefix/internal/force_merge", url)
}

func TestRejectTLSEnabled(t *testing.T) {
	for _, args := range [][]string{
		{"-tls"},
		{"-tls=true"},
		{"-tls=1"},
		{"-tls=t"},
		{"-tls=TRUE"},
	} {
		assert.Error(t, rejectTLS(depWithArgs(args...)), "args=%v", args)
	}
}

func TestRejectTLSEnabled_NotEnabled(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"-tls=false"},
		{"-tls=0"},
		{"-httpListenAddr=:8428"},
	} {
		assert.NoError(t, rejectTLS(depWithArgs(args...)), "args=%v", args)
	}
}

func TestHTTPPathPrefix(t *testing.T) {
	assert.Equal(t, "/prefix", httpPathPrefix(depWithArgs("-http.pathPrefix=/prefix").Spec.Template.Spec.Containers))
	assert.Equal(t, "/prefix", httpPathPrefix(depWithArgs("-http.pathPrefix=/prefix/").Spec.Template.Spec.Containers))
	assert.Equal(t, "", httpPathPrefix(depWithArgs().Spec.Template.Spec.Containers))
}

func depWithEnv(args []string, env ...corev1.EnvVar) *appsv1.Deployment {
	dep := depWithArgs(args...)
	dep.Spec.Template.Spec.Containers[0].Env = env
	return dep
}

func TestRejectTLSEnabled_EnvFlag(t *testing.T) {
	assert.Error(t, rejectTLS(depWithEnv([]string{"-envflag.enable=true"}, corev1.EnvVar{Name: "tls", Value: "true"})))
	assert.NoError(t, rejectTLS(depWithEnv([]string{"-envflag.enable=true"}, corev1.EnvVar{Name: "tls", Value: "false"})))
	// without -envflag.enable, the TLS env var isn't actually consulted by the binary.
	assert.NoError(t, rejectTLS(depWithEnv(nil, corev1.EnvVar{Name: "tls", Value: "true"})))
}

func TestHTTPPathPrefix_EnvFlag(t *testing.T) {
	assert.Equal(t, "/prefix", httpPathPrefix(depWithEnv([]string{"-envflag.enable=true"}, corev1.EnvVar{Name: "http_pathPrefix", Value: "/prefix"}).Spec.Template.Spec.Containers))
	assert.Equal(t, "", httpPathPrefix(depWithEnv(nil, corev1.EnvVar{Name: "http_pathPrefix", Value: "/prefix"}).Spec.Template.Spec.Containers))
}
