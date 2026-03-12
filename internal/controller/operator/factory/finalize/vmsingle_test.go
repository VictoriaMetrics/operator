package finalize

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestOnVMSingleDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMSingle
		predefinedObjects []runtime.Object
		clusterWide       bool
		verify            func(client client.Client)
	}

	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		if o.clusterWide {
			// Save and restore cluster wide config
			original := config.MustGetBaseConfig().WatchNamespaces
			config.MustGetBaseConfig().WatchNamespaces = nil
			defer func() {
				config.MustGetBaseConfig().WatchNamespaces = original
			}()
		} else {
			original := config.MustGetBaseConfig().WatchNamespaces
			config.MustGetBaseConfig().WatchNamespaces = []string{"default"}
			defer func() {
				config.MustGetBaseConfig().WatchNamespaces = original
			}()
		}

		err := OnVMSingleDelete(ctx, cl, o.cr)
		assert.NoError(t, err)

		if o.verify != nil {
			o.verify(cl)
		}
	}

	cr := &vmv1beta1.VMSingle{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMSingle",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-vmsingle",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
		Spec: vmv1beta1.VMSingleSpec{
			RemovePvcAfterDelete: true, // we will keep owner ref on pvc to be garbage collected
		},
	}
	cr.Spec.ServiceSpec = &vmv1beta1.AdditionalServiceSpec{
		EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
			Name: "custom-service",
		},
	}
	cr.Spec.AdditionalScrapeConfigs = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: "extra-scrape",
		},
	}

	f(opts{
		cr:          cr,
		clusterWide: true,
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
					Name:       cr.PrefixedName(),
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
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       build.ResourceName(build.RelabelConfigResourceKind, cr),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       build.ResourceName(build.StreamAggrConfigResourceKind, cr),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.GetRBACName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.GetRBACName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.GetRBACName(),
					Finalizers: []string{vmv1beta1.FinalizerName},
					OwnerReferences: []metav1.OwnerReference{
						cr.AsOwner(),
					},
					Labels: cr.SelectorLabels(),
				},
			},
			&rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.GetRBACName(),
					Finalizers: []string{vmv1beta1.FinalizerName},
					OwnerReferences: []metav1.OwnerReference{
						cr.AsOwner(),
					},
					Labels: cr.SelectorLabels(),
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
					Name:       "extra-scrape",
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
					OwnerReferences: []metav1.OwnerReference{
						cr.AsOwner(),
					},
				},
			},
		},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
			var vmsingle vmv1beta1.VMSingle
			err := cl.Get(ctx, nsnCR, &vmsingle)
			assert.NoError(t, err)
			assert.Empty(t, vmsingle.Finalizers)

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

			var sec corev1.Secret
			err = cl.Get(ctx, nsnPrefixed, &sec)
			assert.NoError(t, err)
			assert.Empty(t, sec.Finalizers)

			nsnTLSAssets := types.NamespacedName{Name: build.ResourceName(build.TLSAssetsResourceKind, cr), Namespace: cr.Namespace}
			var tlsSecret corev1.Secret
			err = cl.Get(ctx, nsnTLSAssets, &tlsSecret)
			assert.NoError(t, err)
			assert.Empty(t, tlsSecret.Finalizers)

			nsnRelabel := types.NamespacedName{Name: build.ResourceName(build.RelabelConfigResourceKind, cr), Namespace: cr.Namespace}
			var relabelCm corev1.ConfigMap
			err = cl.Get(ctx, nsnRelabel, &relabelCm)
			assert.NoError(t, err)
			assert.Empty(t, relabelCm.Finalizers)

			nsnStreamAggr := types.NamespacedName{Name: build.ResourceName(build.StreamAggrConfigResourceKind, cr), Namespace: cr.Namespace}
			var streamAggrCm corev1.ConfigMap
			err = cl.Get(ctx, nsnStreamAggr, &streamAggrCm)
			assert.NoError(t, err)
			assert.Empty(t, streamAggrCm.Finalizers)

			nsnRBAC := types.NamespacedName{Name: cr.GetRBACName(), Namespace: cr.Namespace}
			var role rbacv1.Role
			err = cl.Get(ctx, nsnRBAC, &role)
			assert.NoError(t, err)
			assert.Empty(t, role.Finalizers)

			var rb rbacv1.RoleBinding
			err = cl.Get(ctx, nsnRBAC, &rb)
			assert.NoError(t, err)
			assert.Empty(t, rb.Finalizers)

			// ClusterRoleBinding and ClusterRole should be deleted by SafeDeleteWithFinalizer
			nsnClusterRBAC := types.NamespacedName{Name: cr.GetRBACName()}
			var crb rbacv1.ClusterRoleBinding
			err = cl.Get(ctx, nsnClusterRBAC, &crb)
			assert.Error(t, err)

			var crole rbacv1.ClusterRole
			err = cl.Get(ctx, nsnClusterRBAC, &crole)
			assert.Error(t, err)

			nsnCustomSvc := types.NamespacedName{Name: "custom-service", Namespace: cr.Namespace}
			var customSvc corev1.Service
			err = cl.Get(ctx, nsnCustomSvc, &customSvc)
			assert.NoError(t, err)
			assert.Empty(t, customSvc.Finalizers)

			nsnExtraScrape := types.NamespacedName{Name: "extra-scrape", Namespace: cr.Namespace}
			var extraScrape corev1.Secret
			err = cl.Get(ctx, nsnExtraScrape, &extraScrape)
			assert.NoError(t, err)
			assert.Empty(t, extraScrape.Finalizers)

			// PVC behavior check
			var pvc corev1.PersistentVolumeClaim
			err = cl.Get(ctx, nsnPrefixed, &pvc)
			assert.NoError(t, err)
			assert.Empty(t, pvc.Finalizers)
			assert.NotEmpty(t, pvc.OwnerReferences) // Since RemovePvcAfterDelete=true, deleteOwnerReferences=false
		},
	})

	// Keep PVCs
	cr2 := cr.DeepCopy()
	cr2.Name = "test-vmsingle-2"
	cr2.Spec.RemovePvcAfterDelete = false

	f(opts{
		cr:          cr2,
		clusterWide: false,
		predefinedObjects: []runtime.Object{
			cr2,
			&rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr2.GetRBACName(),
					Finalizers: []string{vmv1beta1.FinalizerName},
					OwnerReferences: []metav1.OwnerReference{
						cr2.AsOwner(),
					},
					Labels: cr2.SelectorLabels(),
				},
			},
			&rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr2.GetRBACName(),
					Finalizers: []string{vmv1beta1.FinalizerName},
					OwnerReferences: []metav1.OwnerReference{
						cr2.AsOwner(),
					},
					Labels: cr2.SelectorLabels(),
				},
			},
			&corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr2.PrefixedName(),
					Namespace:  cr2.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
					OwnerReferences: []metav1.OwnerReference{
						cr2.AsOwner(),
					},
				},
			},
		},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: cr2.Name, Namespace: cr2.Namespace}
			var vmsingle vmv1beta1.VMSingle
			err := cl.Get(ctx, nsnCR, &vmsingle)
			assert.NoError(t, err)
			assert.Empty(t, vmsingle.Finalizers)

			// ClusterRoleBinding and ClusterRole should NOT be deleted, finalizers should NOT be removed
			// Because clusterWide is false!
			nsnClusterRBAC := types.NamespacedName{Name: cr2.GetRBACName()}
			var crb rbacv1.ClusterRoleBinding
			err = cl.Get(ctx, nsnClusterRBAC, &crb)
			assert.NoError(t, err)
			assert.NotEmpty(t, crb.Finalizers)

			var crole rbacv1.ClusterRole
			err = cl.Get(ctx, nsnClusterRBAC, &crole)
			assert.NoError(t, err)
			assert.NotEmpty(t, crole.Finalizers)

			// PVC behavior check
			nsnPrefixed := types.NamespacedName{Name: cr2.PrefixedName(), Namespace: cr2.Namespace}
			var pvc corev1.PersistentVolumeClaim
			err = cl.Get(ctx, nsnPrefixed, &pvc)
			assert.NoError(t, err)
			assert.Empty(t, pvc.Finalizers)
			assert.Empty(t, pvc.OwnerReferences) // Since RemovePvcAfterDelete=false, deleteOwnerReferences=true
		},
	})
}
