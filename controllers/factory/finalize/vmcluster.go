package finalize

import (
	"context"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HPADelete handles case, when user wants to remove HPA configuration from cluster config.
func HPADelete(ctx context.Context, rclient client.Client, objectName, objectNamespace string) error {
	hpa := k8stools.NewHPAEmptyObject(func(obj client.Object) {
		obj.SetName(objectName)
		obj.SetNamespace(objectNamespace)
	})

	if err := removeFinalizeObjByName(ctx, rclient, hpa, objectName, objectNamespace); err != nil {
		return err
	}
	if err := SafeDelete(ctx, rclient, hpa); err != nil {
		return err
	}
	return nil
}

// OnVMClusterDelete deletes all vmcluster related resources
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
			if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.Spec.VMInsert.ServiceSpec.NameOrDefault(crd.Spec.VMInsert.GetNameWithPrefix(crd.Name)), crd.Namespace); err != nil {
				return err
			}
		}
		if err := removeFinalizeObjByName(ctx, rclient, k8stools.NewHPAEmptyObject(), obj.GetNameWithPrefix(crd.Name), crd.Namespace); err != nil {
			return err
		}

		// check PDB
		if err := finalizePBDWithName(ctx, rclient, obj.GetNameWithPrefix(crd.Name), crd.Namespace); err != nil {
			return err
		}
	}

	if crd.Spec.VMSelect != nil {
		obj := crd.Spec.VMSelect
		if err := removeFinalizeObjByName(ctx, rclient, &appsv1.StatefulSet{}, obj.GetNameWithPrefix(crd.Name), crd.Namespace); err != nil {
			return err
		}
		if crd.Spec.VMSelect.ServiceSpec != nil {
			if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.Spec.VMSelect.ServiceSpec.NameOrDefault(crd.Spec.VMSelect.GetNameWithPrefix(crd.Name)), crd.Namespace); err != nil {
				return err
			}
		}

		// check service
		if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, obj.GetNameWithPrefix(crd.Name), crd.Namespace); err != nil {
			return err
		}

		// remove hpa
		if err := removeFinalizeObjByName(ctx, rclient, k8stools.NewHPAEmptyObject(), obj.GetNameWithPrefix(crd.Name), crd.Namespace); err != nil {
			return err
		}

		// check PDB
		if err := finalizePBDWithName(ctx, rclient, obj.GetNameWithPrefix(crd.Name), crd.Namespace); err != nil {
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
			if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.Spec.VMStorage.ServiceSpec.NameOrDefault(crd.Spec.VMStorage.GetNameWithPrefix(crd.Name)), crd.Namespace); err != nil {
				return err
			}
		}

		// check PDB
		if err := finalizePBDWithName(ctx, rclient, obj.GetNameWithPrefix(crd.Name), crd.Namespace); err != nil {
			return err
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
