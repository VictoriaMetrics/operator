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

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestOnVMAnomalyDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1.VMAnomaly
		predefinedObjects []runtime.Object
		verify            func(client client.Client)
	}

	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		err := OnVMAnomalyDelete(ctx, cl, o.cr)
		assert.NoError(t, err)

		if o.verify != nil {
			o.verify(cl)
		}
	}

	cr := &vmv1.VMAnomaly{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1",
			Kind:       "VMAnomaly",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-vmanomaly",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
	}

	f(opts{
		cr: cr,
		predefinedObjects: []runtime.Object{
			cr,
			&appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:            cr.PrefixedName(),
					Namespace:       cr.GetNamespace(),
					Labels:          cr.SelectorLabels(),
					OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
					Finalizers:      []string{vmv1beta1.FinalizerName},
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
					Name:            cr.PrefixedName(),
					Namespace:       cr.GetNamespace(),
					Labels:          cr.SelectorLabels(),
					OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
					Finalizers:      []string{vmv1beta1.FinalizerName},
				},
			},
			// orphaned sts
			&appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "orphaned-sts",
					Namespace:       cr.GetNamespace(),
					Labels:          cr.SelectorLabels(),
					OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
					Finalizers:      []string{vmv1beta1.FinalizerName},
				},
			},
			// orphaned pdb
			&policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "orphaned-pdb",
					Namespace:       cr.GetNamespace(),
					Labels:          cr.SelectorLabels(),
					OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
					Finalizers:      []string{vmv1beta1.FinalizerName},
				},
			},
		},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
			var vmanomaly vmv1.VMAnomaly
			err := cl.Get(ctx, nsnCR, &vmanomaly)
			assert.NoError(t, err)
			assert.Empty(t, vmanomaly.Finalizers)

			nsnPrefixed := types.NamespacedName{Name: cr.PrefixedName(), Namespace: cr.Namespace}

			var sts appsv1.StatefulSet
			err = cl.Get(ctx, nsnPrefixed, &sts)
			assert.NoError(t, err)
			assert.Empty(t, sts.Finalizers)

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

			nsnOrphanedSts := types.NamespacedName{Name: "orphaned-sts", Namespace: cr.Namespace}
			var orphanedSts appsv1.StatefulSet
			err = cl.Get(ctx, nsnOrphanedSts, &orphanedSts)
			assert.NoError(t, err)
			assert.Empty(t, orphanedSts.Finalizers)

			nsnOrphanedPdb := types.NamespacedName{Name: "orphaned-pdb", Namespace: cr.Namespace}
			var orphanedPdb policyv1.PodDisruptionBudget
			err = cl.Get(ctx, nsnOrphanedPdb, &orphanedPdb)
			assert.NoError(t, err)
			assert.Empty(t, orphanedPdb.Finalizers)
		},
	})
}
