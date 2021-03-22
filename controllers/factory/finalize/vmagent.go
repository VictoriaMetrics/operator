package finalize

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v12 "k8s.io/api/rbac/v1"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func OnVMAgentDelete(ctx context.Context, rclient client.Client, crd *victoriametricsv1beta1.VMAgent) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.Deployment{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	// check service
	if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	if crd.Spec.ServiceSpec != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.Spec.ServiceSpec.NameOrDefault(crd.PrefixedName()), crd.Namespace); err != nil {
			return err
		}
	}

	// check secret
	if err := removeFinalizeObjByName(ctx, rclient, &v1.Secret{}, crd.TLSAssetName(), crd.Namespace); err != nil {
		return err
	}

	// remove vmagents service discovery rbac.
	if err := removeFinalizeObjByName(ctx, rclient, &v12.ClusterRoleBinding{}, crd.GetClusterRoleName(), crd.GetNSName()); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &v12.ClusterRole{}, crd.GetClusterRoleName(), crd.GetNSName()); err != nil {
		return err
	}
	if err := SafeDelete(ctx, rclient, &v12.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: crd.GetClusterRoleName(), Namespace: crd.GetNSName()}}); err != nil {
		return err
	}

	if err := SafeDelete(ctx, rclient, &v12.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: crd.GetClusterRoleName(), Namespace: crd.GetNSName()}}); err != nil {
		return err
	}

	if err := finalizePsp(ctx, rclient, crd); err != nil {
		return err
	}
	// remove from self.
	if err := removeFinalizeObjByName(ctx, rclient, crd, crd.Name, crd.Namespace); err != nil {
		return err
	}
	if err := deleteSA(ctx, rclient, crd); err != nil {
		return err
	}
	return nil
}
