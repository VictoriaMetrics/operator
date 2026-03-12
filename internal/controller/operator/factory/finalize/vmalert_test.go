package finalize

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestOnVMAlertDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		predefinedObjects []runtime.Object
		verify            func(client client.Client)
	}

	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		err := OnVMAlertDelete(ctx, cl, o.cr)
		assert.NoError(t, err)

		if o.verify != nil {
			o.verify(cl)
		}
	}

	cr := &vmv1beta1.VMAlert{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMAlert",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-vmalert",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
		Spec: vmv1beta1.VMAlertSpec{
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
					Name:            cr.PrefixedName(),
					Namespace:       cr.GetNamespace(),
					Labels:          cr.SelectorLabels(),
					OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
					Finalizers:      []string{vmv1beta1.FinalizerName},
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
					Name:       build.ResourceName(build.SecretConfigResourceKind, cr),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       build.ResourceName(build.TLSAssetsResourceKind, cr),
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
			// orphaned configmap
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "orphaned-cm",
					Namespace:       cr.GetNamespace(),
					Labels:          cr.SelectorLabels(),
					OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
					Finalizers:      []string{vmv1beta1.FinalizerName},
				},
			},
		},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
			var vmalert vmv1beta1.VMAlert
			err := cl.Get(ctx, nsnCR, &vmalert)
			assert.NoError(t, err)
			assert.Empty(t, vmalert.Finalizers)

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

			nsnConfigSecret := types.NamespacedName{Name: build.ResourceName(build.SecretConfigResourceKind, cr), Namespace: cr.Namespace}
			var configSecret corev1.Secret
			err = cl.Get(ctx, nsnConfigSecret, &configSecret)
			assert.NoError(t, err)
			assert.Empty(t, configSecret.Finalizers)

			nsnTLSAssets := types.NamespacedName{Name: build.ResourceName(build.TLSAssetsResourceKind, cr), Namespace: cr.Namespace}
			var tlsSecret corev1.Secret
			err = cl.Get(ctx, nsnTLSAssets, &tlsSecret)
			assert.NoError(t, err)
			assert.Empty(t, tlsSecret.Finalizers)

			var pdb policyv1.PodDisruptionBudget
			err = cl.Get(ctx, nsnPrefixed, &pdb)
			assert.NoError(t, err)
			assert.Empty(t, pdb.Finalizers)

			nsnCustomSvc := types.NamespacedName{Name: "custom-service", Namespace: cr.Namespace}
			var customSvc corev1.Service
			err = cl.Get(ctx, nsnCustomSvc, &customSvc)
			assert.NoError(t, err)
			assert.Empty(t, customSvc.Finalizers)

			nsnOrphanedCm := types.NamespacedName{Name: "orphaned-cm", Namespace: cr.Namespace}
			var orphanedCm corev1.ConfigMap
			err = cl.Get(ctx, nsnOrphanedCm, &orphanedCm)
			assert.NoError(t, err)
			assert.Empty(t, orphanedCm.Finalizers)
		},
	})
}
