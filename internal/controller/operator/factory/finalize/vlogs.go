package finalize

import (
	"context"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OnVLogsDelete deletes all vlogs related resources
func OnVLogsDelete(ctx context.Context, rclient client.Client, crd *vmv1beta1.VLogs) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.Deployment{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	// check service
	if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	if crd.Spec.Storage != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &v1.PersistentVolumeClaim{}, crd.PrefixedName(), crd.Namespace); err != nil {
			return err
		}
	}
	if crd.Spec.ServiceSpec != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.Spec.ServiceSpec.NameOrDefault(crd.PrefixedName()), crd.Namespace); err != nil {
			return err
		}
	}
	if err := deleteSA(ctx, rclient, crd); err != nil {
		return err
	}

	return removeFinalizeObjByName(ctx, rclient, crd, crd.Name, crd.Namespace)
}
