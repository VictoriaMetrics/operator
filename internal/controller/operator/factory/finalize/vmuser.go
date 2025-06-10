package finalize

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// OnVMUserDelete deletes all vmuser related resources
func OnVMUserDelete(ctx context.Context, rclient client.Client, crd *vmv1beta1.VMUser) error {
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, crd.SecretName(), crd.Namespace); err != nil {
		return err
	}

	if err := removeFinalizeObjByName(ctx, rclient, crd, crd.Name, crd.Namespace); err != nil {
		return err
	}
	return nil
}
