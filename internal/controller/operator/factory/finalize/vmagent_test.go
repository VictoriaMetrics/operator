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
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestOnVMAgentDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAgent
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

		err := OnVMAgentDelete(ctx, cl, o.cr)
		assert.NoError(t, err)

		if o.verify != nil {
			o.verify(cl)
		}
	}

	cr := &vmv1beta1.VMAgent{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMAgent",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-vmagent",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
		Spec: vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					UrlRelabelConfig: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "relabel-cm",
						},
					},
				},
			},
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
					Name:      "test-vmagent",
					Namespace: "default",
					Labels:    cr.SelectorLabels(),
					OwnerReferences: []metav1.OwnerReference{
						cr.AsOwner(),
					},
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmagent",
					Namespace: "default",
					Labels:    cr.SelectorLabels(),
					OwnerReferences: []metav1.OwnerReference{
						cr.AsOwner(),
					},
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmagent",
					Namespace: "default",
					Labels:    cr.SelectorLabels(),
					OwnerReferences: []metav1.OwnerReference{
						cr.AsOwner(),
					},
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
			&appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
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
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "relabel-cm",
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
		},
		verify: func(cl client.Client) {
			nsn := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
			// cr finalizer should be removed
			var vmagent vmv1beta1.VMAgent
			err := cl.Get(ctx, nsn, &vmagent)
			assert.NoError(t, err)
			assert.Empty(t, vmagent.Finalizers)

			// orphaned deployment, sts, pdb should have finalizers removed, but not be deleted
			var dep appsv1.Deployment
			err = cl.Get(ctx, nsn, &dep)
			assert.NoError(t, err)
			assert.Empty(t, dep.Finalizers)

			var sts appsv1.StatefulSet
			err = cl.Get(ctx, nsn, &sts)
			assert.NoError(t, err)
			assert.Empty(t, sts.Finalizers)

			var pdb policyv1.PodDisruptionBudget
			err = cl.Get(ctx, nsn, &pdb)
			assert.NoError(t, err)
			assert.Empty(t, pdb.Finalizers)

			// ClusterRoleBinding and ClusterRole should be deleted by SafeDeleteWithFinalizer
			nsnRBAC := types.NamespacedName{Name: cr.GetRBACName()}
			var crb rbacv1.ClusterRoleBinding
			err = cl.Get(ctx, nsnRBAC, &crb)
			assert.Error(t, err)

			var crole rbacv1.ClusterRole
			err = cl.Get(ctx, nsnRBAC, &crole)
			assert.Error(t, err)

			// Other resources have their finalizers removed
			var svc corev1.Service
			err = cl.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: cr.Namespace}, &svc)
			assert.NoError(t, err)
			assert.Empty(t, svc.Finalizers)

			var customSvc corev1.Service
			err = cl.Get(ctx, types.NamespacedName{Name: "custom-service", Namespace: cr.Namespace}, &customSvc)
			assert.NoError(t, err)
			assert.Empty(t, customSvc.Finalizers)

			var extraScrape corev1.Secret
			err = cl.Get(ctx, types.NamespacedName{Name: "extra-scrape", Namespace: cr.Namespace}, &extraScrape)
			assert.NoError(t, err)
			assert.Empty(t, extraScrape.Finalizers)

			var relabelCm corev1.ConfigMap
			err = cl.Get(ctx, types.NamespacedName{Name: "relabel-cm", Namespace: cr.Namespace}, &relabelCm)
			assert.NoError(t, err)
			assert.Empty(t, relabelCm.Finalizers)
		},
	})

	cr2 := &vmv1beta1.VMAgent{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMAgent",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-vmagent-2",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
		Spec: vmv1beta1.VMAgentSpec{},
	}

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
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr2.PrefixedName(),
					Namespace:  cr2.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
		},
		verify: func(cl client.Client) {
			// cr finalizer should be removed
			var vmagent vmv1beta1.VMAgent
			err := cl.Get(ctx, types.NamespacedName{Name: cr2.Name, Namespace: cr2.Namespace}, &vmagent)
			assert.NoError(t, err)
			assert.Empty(t, vmagent.Finalizers)

			// ClusterRoleBinding and ClusterRole should NOT be deleted, finalizers should NOT be removed by VMAgentDelete
			// Because clusterWide is false!
			nsnRBAC := types.NamespacedName{Name: cr2.GetRBACName()}
			var crb rbacv1.ClusterRoleBinding
			err = cl.Get(ctx, nsnRBAC, &crb)
			assert.NoError(t, err)
			assert.NotEmpty(t, crb.Finalizers)

			var crole rbacv1.ClusterRole
			err = cl.Get(ctx, nsnRBAC, &crole)
			assert.NoError(t, err)
			assert.NotEmpty(t, crole.Finalizers)

			// Normal resources should have their finalizers removed
			var svc corev1.Service
			err = cl.Get(ctx, types.NamespacedName{Name: cr2.PrefixedName(), Namespace: cr2.Namespace}, &svc)
			assert.NoError(t, err)
			assert.Empty(t, svc.Finalizers)
		},
	})
}
