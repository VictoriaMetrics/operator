package finalize

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestOnVMAlertManagerDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlertmanager
		predefinedObjects []runtime.Object
		verify            func(client client.Client)
	}

	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		err := OnVMAlertManagerDelete(ctx, cl, o.cr)
		assert.NoError(t, err)

		if o.verify != nil {
			o.verify(cl)
		}
	}

	cr := &vmv1beta1.VMAlertmanager{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMAlertmanager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-vmalertmanager",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
		Spec: vmv1beta1.VMAlertmanagerSpec{
			ConfigSecret: "custom-config-secret",
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
			&appsv1.StatefulSet{
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
			&policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
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
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "custom-service",
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "custom-config-secret",
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
		},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
			var vmalertmanager vmv1beta1.VMAlertmanager
			err := cl.Get(ctx, nsnCR, &vmalertmanager)
			assert.NoError(t, err)
			assert.Empty(t, vmalertmanager.Finalizers)

			nsnPrefixed := types.NamespacedName{Name: cr.PrefixedName(), Namespace: cr.Namespace}

			var sts appsv1.StatefulSet
			err = cl.Get(ctx, nsnPrefixed, &sts)
			assert.NoError(t, err)
			assert.Empty(t, sts.Finalizers)

			var svc corev1.Service
			err = cl.Get(ctx, nsnPrefixed, &svc)
			assert.NoError(t, err)
			assert.Empty(t, svc.Finalizers)

			nsnSA := types.NamespacedName{Name: cr.GetServiceAccountName(), Namespace: cr.Namespace}
			var sa corev1.ServiceAccount
			err = cl.Get(ctx, nsnSA, &sa)
			assert.NoError(t, err)
			assert.Empty(t, sa.Finalizers)

			var pdb policyv1.PodDisruptionBudget
			err = cl.Get(ctx, nsnPrefixed, &pdb)
			assert.NoError(t, err)
			assert.Empty(t, pdb.Finalizers)

			nsnConfigSecret := types.NamespacedName{Name: cr.ConfigSecretName(), Namespace: cr.Namespace}
			var configSecret corev1.Secret
			err = cl.Get(ctx, nsnConfigSecret, &configSecret)
			assert.NoError(t, err)
			assert.Empty(t, configSecret.Finalizers)

			var rb rbacv1.RoleBinding
			err = cl.Get(ctx, nsnPrefixed, &rb)
			assert.NoError(t, err)
			assert.Empty(t, rb.Finalizers)

			var role rbacv1.Role
			err = cl.Get(ctx, nsnPrefixed, &role)
			assert.NoError(t, err)
			assert.Empty(t, role.Finalizers)

			nsnCustomSvc := types.NamespacedName{Name: "custom-service", Namespace: cr.Namespace}
			var customSvc corev1.Service
			err = cl.Get(ctx, nsnCustomSvc, &customSvc)
			assert.NoError(t, err)
			assert.Empty(t, customSvc.Finalizers)

			nsnCustomConfigSecret := types.NamespacedName{Name: "custom-config-secret", Namespace: cr.Namespace}
			var customConfigSecret corev1.Secret
			err = cl.Get(ctx, nsnCustomConfigSecret, &customConfigSecret)
			assert.NoError(t, err)
			assert.Empty(t, customConfigSecret.Finalizers)
		},
	})
}
