package finalize

import (
	"context"

	v12 "k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	v1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CRDObject interface {
	Annotations() map[string]string
	Labels() map[string]string
	PrefixedName() string
	GetServiceAccountName() string
	GetPSPName() string
	GetNSName() string
}

func AddFinalizer(ctx context.Context, rclient client.Client, instance client.Object) error {
	if !victoriametricsv1beta1.IsContainsFinalizer(instance.GetFinalizers(), victoriametricsv1beta1.FinalizerName) {
		instance.SetFinalizers(append(instance.GetFinalizers(), victoriametricsv1beta1.FinalizerName))
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
	obj.SetFinalizers(victoriametricsv1beta1.RemoveFinalizer(obj.GetFinalizers(), victoriametricsv1beta1.FinalizerName))
	return rclient.Update(ctx, obj)
}

func safeDelete(ctx context.Context, rclient client.Client, r client.Object) error {
	if err := rclient.Delete(ctx, r); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func deleteSA(ctx context.Context, rclient client.Client, crd CRDObject) error {
	return safeDelete(ctx, rclient, &v12.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: crd.GetNSName(), Name: crd.GetServiceAccountName()}})
}

// DeletePSPChain - removes psp, cluster role and cluster role binding,
// on finalize request for given CRD
func DeletePSPChain(ctx context.Context, rclient client.Client, crd CRDObject) error {
	if err := ensurePSPRemoved(ctx, rclient, crd); err != nil {
		return err
	}
	if err := ensureCRBRemoved(ctx, rclient, crd); err != nil {
		return err
	}
	return ensureCRRemoved(ctx, rclient, crd)
}

func ensurePSPRemoved(ctx context.Context, rclient client.Client, crd CRDObject) error {
	return safeDelete(ctx, rclient, &v1beta1.PodSecurityPolicy{ObjectMeta: metav1.ObjectMeta{
		Name: crd.GetPSPName()}})
}

func ensureCRRemoved(ctx context.Context, rclient client.Client, crd CRDObject) error {
	return safeDelete(ctx, rclient, &v1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: crd.PrefixedName()}})
}

func ensureCRBRemoved(ctx context.Context, rclient client.Client, crd CRDObject) error {
	return safeDelete(ctx, rclient, &v1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: crd.PrefixedName()}})
}

func finalizePsp(ctx context.Context, rclient client.Client, crd CRDObject) error {
	// check binding
	if err := removeFinalizeObjByName(ctx, rclient, &v1.ClusterRoleBinding{}, crd.PrefixedName(), crd.GetNSName()); err != nil {
		return err
	}
	// check role
	if err := removeFinalizeObjByName(ctx, rclient, &v1.ClusterRole{}, crd.PrefixedName(), crd.GetNSName()); err != nil {
		return err
	}
	// check sa
	if err := removeFinalizeObjByName(ctx, rclient, &v12.ServiceAccount{}, crd.GetServiceAccountName(), crd.GetNSName()); err != nil {
		return err
	}
	// check psp
	if err := removeFinalizeObjByName(ctx, rclient, &v1beta1.PodSecurityPolicy{}, crd.GetPSPName(), crd.GetNSName()); err != nil {
		return err
	}
	return DeletePSPChain(ctx, rclient, crd)
}
