package finalize

import (
	"context"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func OnVMAlertDelete(ctx context.Context, rclient client.Client, crd *victoriametricsv1beta1.VMAlert) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.Deployment{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	// check service
	if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	var cmList v1.ConfigMapList
	if err := rclient.List(ctx, &cmList, crd.RulesConfigMapSelector()); err != nil {
		return err
	}
	for _, cm := range cmList.Items {
		if victoriametricsv1beta1.IsContainsFinalizer(cm.Finalizers, victoriametricsv1beta1.FinalizerName) {
			cm.Finalizers = victoriametricsv1beta1.RemoveFinalizer(cm.Finalizers, victoriametricsv1beta1.FinalizerName)
			if err := rclient.Update(ctx, &cm); err != nil {
				return err
			}
		}
	}
	// check secret
	if err := removeFinalizeObjByName(ctx, rclient, &v1.Secret{}, crd.TLSAssetName(), crd.Namespace); err != nil {
		return err
	}

	if err := finalizePsp(ctx, rclient, crd); err != nil {
		return err
	}
	if err := deleteSA(ctx, rclient, crd); err != nil {
		return err
	}

	if crd.Spec.ServiceSpec != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.Spec.ServiceSpec.Name, crd.Namespace); err != nil {
			return err
		}
	}
	if err := removeFinalizeObjByName(ctx, rclient, crd, crd.Name, crd.Namespace); err != nil {
		return err
	}
	return nil
}
