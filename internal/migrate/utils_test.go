package migrate

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestWaitForOperational_FailsFastOnFailedStatus(t *testing.T) {
	synctest.Test(t, testWaitForOperational_FailsFastOnFailedStatus)
}

func testWaitForOperational_FailsFastOnFailedStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vmv1beta1.AddToScheme(scheme))

	nsn := types.NamespacedName{Name: "myrelease", Namespace: "default"}
	target := &vmv1beta1.VMSingle{ObjectMeta: metav1.ObjectMeta{Name: nsn.Name, Namespace: nsn.Namespace}}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&vmv1beta1.VMSingle{}).
		WithObjects(target).
		Build()

	ctx := context.Background()
	var created vmv1beta1.VMSingle
	require.NoError(t, c.Get(ctx, nsn, &created))
	created.Status.UpdateStatus = vmv1beta1.UpdateStatusFailed
	created.Status.ObservedGeneration = created.Generation
	created.Status.Reason = "boom: cannot pull image"
	require.NoError(t, c.Status().Update(ctx, &created))

	err := waitForOperational(ctx, c, &vmv1beta1.VMSingle{ObjectMeta: metav1.ObjectMeta{Name: nsn.Name, Namespace: nsn.Namespace}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom: cannot pull image")
}

func newTestAgentWithEndpoint(t *testing.T, c client.Client, agentPort int32) *vmv1beta1.VMAgent {
	t.Helper()
	agent := &vmv1beta1.VMAgent{ObjectMeta: metav1.ObjectMeta{Name: "myagent", Namespace: "default"}}
	es := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.PrefixedName() + "-endpoints",
			Namespace: "default",
			Labels:    map[string]string{discoveryv1.LabelServiceName: agent.PrefixedName()},
		},
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}},
		},
		Ports: []discoveryv1.EndpointPort{
			{Name: ptr.To("http"), Port: ptr.To(agentPort)},
		},
	}
	require.NoError(t, c.Create(context.Background(), es))
	return agent
}

func TestVerifyAgentPortCompatible_NamedTargetPortIsAlwaysSafe(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	agent := newTestAgentWithEndpoint(t, c, 8429)

	svc := &corev1.Service{
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 8428, TargetPort: intstr.FromString("http")}}},
	}
	require.NoError(t, verifyAgentPortCompatible(context.Background(), c, svc, agent))
}

func TestVerifyAgentPortCompatible_MatchingNumericPortIsSafe(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	agent := newTestAgentWithEndpoint(t, c, 8428)

	svc := &corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 8428}}}}
	require.NoError(t, verifyAgentPortCompatible(context.Background(), c, svc, agent))
}

func TestVerifyAgentPortCompatible_MismatchedNumericPortIsAnError(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	agent := newTestAgentWithEndpoint(t, c, 8429)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmsingle", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 8480}}},
	}
	err := verifyAgentPortCompatible(context.Background(), c, svc, agent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send this port's traffic nowhere")
}

func TestVerifyAgentPortCompatible_NamedPortOtherThanHTTPIsAnError(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	agent := newTestAgentWithEndpoint(t, c, 8429)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmsingle", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 8428, TargetPort: intstr.FromString("web")}}},
	}
	err := verifyAgentPortCompatible(context.Background(), c, svc, agent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only expose a port named \"http\"")
}

func TestVerifyAgentPortCompatible_UnrelatedProtocolPortIsAnError(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	agent := newTestAgentWithEndpoint(t, c, 8429)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmsingle", Namespace: "default"},
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
			{Name: "http", Port: 8428, TargetPort: intstr.FromString("http")},
			{Name: "graphite-tcp", Port: 2003, TargetPort: intstr.FromString("graphite-tcp")},
		}},
	}
	err := verifyAgentPortCompatible(context.Background(), c, svc, agent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graphite-tcp")
}

func TestDeleteDependentConfigs_SubstringNameDoesNotFalselyMatch(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vmv1beta1.AddToScheme(scheme))

	target := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1beta1.VMSingleSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				Volumes: []corev1.Volume{{
					Name: "myapp-config",
					VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "myapp-config"},
					}},
				}},
			},
		},
	}
	cm := corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&cm).Build()
	ctx := context.Background()

	require.NoError(t, deleteDependentConfigs(ctx, c, target, []corev1.ConfigMap{cm}, nil))

	var check corev1.ConfigMap
	err := c.Get(ctx, types.NamespacedName{Name: "app", Namespace: "default"}, &check)
	assert.True(t, k8serrors.IsNotFound(err))
}

func TestDeleteDependentConfigs_ReferencedByUnrelatedLiveWorkloadSurvives(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vmv1beta1.AddToScheme(scheme))

	target := &vmv1beta1.VMSingle{ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"}}
	secret := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shared-tls", Namespace: "default"}}
	unrelatedDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "unrelated-app", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{{
						Name:         "tls",
						VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "shared-tls"}},
					}},
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&secret, unrelatedDep).Build()
	ctx := context.Background()

	require.NoError(t, deleteDependentConfigs(ctx, c, target, nil, []corev1.Secret{secret}))

	var check corev1.Secret
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "shared-tls", Namespace: "default"}, &check))
}
