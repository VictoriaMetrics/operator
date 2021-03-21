package finalize

import (
	"context"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func OnVMClusterDelete(ctx context.Context, rclient client.Client, crd *victoriametricsv1beta1.VMCluster) error {
	// check deployment
	if crd.Spec.VMInsert != nil {
		obj := crd.Spec.VMInsert
		if err := removeFinalizeObjByName(ctx, rclient, &appsv1.Deployment{}, obj.GetNameWithPrefix(crd.Name), crd.Namespace); err != nil {
			return err
		}
		// check service
		if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, obj.GetNameWithPrefix(crd.Name), crd.Namespace); err != nil {
			return err
		}
		if crd.Spec.VMInsert.ServiceSpec != nil {
			if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.Spec.VMInsert.ServiceSpec.Name, crd.Namespace); err != nil {
				return err
			}
		}
	}
	if crd.Spec.VMSelect != nil {
		obj := crd.Spec.VMSelect
		if err := removeFinalizeObjByName(ctx, rclient, &appsv1.StatefulSet{}, obj.GetNameWithPrefix(crd.Name), crd.Namespace); err != nil {
			return err
		}
		if crd.Spec.VMSelect.ServiceSpec != nil {
			if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.Spec.VMSelect.ServiceSpec.Name, crd.Namespace); err != nil {
				return err
			}
		}

		// check service
		if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, obj.GetNameWithPrefix(crd.Name), crd.Namespace); err != nil {
			return err
		}
	}
	if crd.Spec.VMStorage != nil {
		obj := crd.Spec.VMStorage
		if err := removeFinalizeObjByName(ctx, rclient, &appsv1.StatefulSet{}, obj.GetNameWithPrefix(crd.Name), crd.Namespace); err != nil {
			return err
		}
		// check service
		if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, obj.GetNameWithPrefix(crd.Name), crd.Namespace); err != nil {
			return err
		}
		if crd.Spec.VMStorage.ServiceSpec != nil {
			if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.Spec.VMStorage.ServiceSpec.Name, crd.Namespace); err != nil {
				return err
			}
		}
	}
	if err := finalizePsp(ctx, rclient, crd); err != nil {
		return err
	}

	if err := deleteSA(ctx, rclient, crd); err != nil {
		return err
	}
	return removeFinalizeObjByName(ctx, rclient, crd, crd.Name, crd.Namespace)
}
