package finalize

import (
	"context"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	v12 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CRDObject interface {
	AnnotationsFiltered() map[string]string
	GetLabels() map[string]string
	PrefixedName() string
	GetServiceAccountName() string
	IsOwnsServiceAccount() bool
	GetNSName() string
}

// AddFinalizer adds finalizer to instance if needed.
func AddFinalizer(ctx context.Context, rclient client.Client, instance client.Object) error {
	if !victoriametricsv1beta1.IsContainsFinalizer(instance.GetFinalizers(), victoriametricsv1beta1.FinalizerName) {
		instance.SetFinalizers(append(instance.GetFinalizers(), victoriametricsv1beta1.FinalizerName))
		return rclient.Update(ctx, instance)
	}
	return nil
}

// RemoveFinalizer removes finalizer from instance if needed.
func RemoveFinalizer(ctx context.Context, rclient client.Client, instance client.Object) error {
	if victoriametricsv1beta1.IsContainsFinalizer(instance.GetFinalizers(), victoriametricsv1beta1.FinalizerName) {
		instance.SetFinalizers(victoriametricsv1beta1.RemoveFinalizer(instance.GetFinalizers(), victoriametricsv1beta1.FinalizerName))
		return rclient.Update(ctx, instance)
	}
	return nil
}

func removeFinalizeObjByName(ctx context.Context, rclient client.Client, obj client.Object, name, ns string) error {
	if err := rclient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, obj); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	// fast path
	if !victoriametricsv1beta1.IsContainsFinalizer(obj.GetFinalizers(), victoriametricsv1beta1.FinalizerName) {
		return nil
	}
	obj.SetFinalizers(victoriametricsv1beta1.RemoveFinalizer(obj.GetFinalizers(), victoriametricsv1beta1.FinalizerName))
	return rclient.Update(ctx, obj)
}

// SafeDelete removes object, ignores notfound error.
func SafeDelete(ctx context.Context, rclient client.Client, r client.Object) error {
	if err := rclient.Delete(ctx, r); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func deleteSA(ctx context.Context, rclient client.Client, crd CRDObject) error {
	if !crd.IsOwnsServiceAccount() {
		return nil
	}
	if err := removeFinalizeObjByName(ctx, rclient, &v12.ServiceAccount{}, crd.GetServiceAccountName(), crd.GetNSName()); err != nil {
		return err
	}
	return SafeDelete(ctx, rclient, &v12.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: crd.GetNSName(), Name: crd.GetServiceAccountName()}})
}

func finalizePsp(ctx context.Context, rclient client.Client, crd CRDObject) error {
	// check sa
	if err := deleteSA(ctx, rclient, crd); err != nil {
		return err
	}
	// fast path, cluster wide permissions is missing
	if !config.IsClusterWideAccessAllowed() {
		return nil
	}
	// check binding
	if err := removeFinalizeObjByName(ctx, rclient, &v1.ClusterRoleBinding{}, crd.PrefixedName(), crd.GetNSName()); err != nil {
		return err
	}
	// check role
	if err := removeFinalizeObjByName(ctx, rclient, &v1.ClusterRole{}, crd.PrefixedName(), crd.GetNSName()); err != nil {
		return err
	}

	if err := SafeDelete(ctx, rclient, &v1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: crd.PrefixedName()}}); err != nil {
		return err
	}

	if err := SafeDelete(ctx, rclient, &v1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: crd.PrefixedName()}}); err != nil {
		return err
	}
	return nil
}

func finalizePBD(ctx context.Context, rclient client.Client, crd CRDObject) error {
	return removeFinalizeObjByName(ctx, rclient, &policyv1.PodDisruptionBudget{}, crd.PrefixedName(), crd.GetNSName())
}

func finalizePBDWithName(ctx context.Context, rclient client.Client, ns, name string) error {
	return removeFinalizeObjByName(ctx, rclient, &policyv1.PodDisruptionBudget{}, name, ns)
}
