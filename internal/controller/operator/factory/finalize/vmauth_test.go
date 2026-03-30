package finalize

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestOnVMAuthDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAuth
		predefinedObjects []runtime.Object
		verify            func(client client.Client)
	}

	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()

		// TODO: test for VPAAPIEnabled and GatewayAPIEnabled

		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		err := OnVMAuthDelete(ctx, cl, o.cr)
		assert.NoError(t, err)

		if o.verify != nil {
			o.verify(cl)
		}
	}

	cr := &vmv1beta1.VMAuth{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMAuth",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-vmauth",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
		Spec: vmv1beta1.VMAuthSpec{
			ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
				EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
					Name: "custom-service",
				},
			},
		},
	}

	f(opts{
		cr: cr,
		predefinedObjects: []runtime.Object{
			cr,
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.GetServiceAccountName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.ConfigSecretName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "custom-service",
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
		},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
			var vmauth vmv1beta1.VMAuth
			err := cl.Get(ctx, nsnCR, &vmauth)
			assert.NoError(t, err)
			assert.Empty(t, vmauth.Finalizers)

			nsnPrefixed := types.NamespacedName{Name: cr.PrefixedName(), Namespace: cr.Namespace}

			var dep appsv1.Deployment
			err = cl.Get(ctx, nsnPrefixed, &dep)
			assert.NoError(t, err)
			assert.Empty(t, dep.Finalizers)

			var svc corev1.Service
			err = cl.Get(ctx, nsnPrefixed, &svc)
			assert.NoError(t, err)
			assert.Empty(t, svc.Finalizers)

			nsnSA := types.NamespacedName{Name: cr.GetServiceAccountName(), Namespace: cr.Namespace}
			var sa corev1.ServiceAccount
			err = cl.Get(ctx, nsnSA, &sa)
			assert.NoError(t, err)
			assert.Empty(t, sa.Finalizers)

			nsnSecret := types.NamespacedName{Name: cr.ConfigSecretName(), Namespace: cr.Namespace}
			var secret corev1.Secret
			err = cl.Get(ctx, nsnSecret, &secret)
			assert.NoError(t, err)
			assert.Empty(t, secret.Finalizers)

			var ing networkingv1.Ingress
			err = cl.Get(ctx, nsnPrefixed, &ing)
			assert.NoError(t, err)
			assert.Empty(t, ing.Finalizers)

			var hpa autoscalingv2.HorizontalPodAutoscaler
			err = cl.Get(ctx, nsnPrefixed, &hpa)
			assert.NoError(t, err)
			assert.Empty(t, hpa.Finalizers)

			var rb rbacv1.RoleBinding
			err = cl.Get(ctx, nsnPrefixed, &rb)
			assert.NoError(t, err)
			assert.Empty(t, rb.Finalizers)

			var role rbacv1.Role
			err = cl.Get(ctx, nsnPrefixed, &role)
			assert.NoError(t, err)
			assert.Empty(t, role.Finalizers)

			var pdb policyv1.PodDisruptionBudget
			err = cl.Get(ctx, nsnPrefixed, &pdb)
			assert.NoError(t, err)
			assert.Empty(t, pdb.Finalizers)

			nsnCustomSvc := types.NamespacedName{Name: "custom-service", Namespace: cr.Namespace}
			var customSvc corev1.Service
			err = cl.Get(ctx, nsnCustomSvc, &customSvc)
			assert.NoError(t, err)
			assert.Empty(t, customSvc.Finalizers)
		},
	})
}
