package finalize

import (
	"context"

	"github.com/VictoriaMetrics/operator/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func OnVMUserDelete(ctx context.Context, rclient client.Client, crd *v1beta1.VMUser) error {
	if err := removeFinalizeObjByName(ctx, rclient, &v1.Secret{}, crd.SecretName(), crd.Namespace); err != nil {
		return err
	}

	if err := removeFinalizeObjByName(ctx, rclient, crd, crd.Name, crd.Namespace); err != nil {
		return err
	}
	return nil
}
