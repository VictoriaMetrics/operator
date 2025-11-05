package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// ServiceAccount creates service account or updates exist one
func ServiceAccount(ctx context.Context, rclient client.Client, newSA, prevSA *corev1.ServiceAccount) error {
	return retryOnConflict(func() error {
		var currentSA corev1.ServiceAccount
		if err := rclient.Get(ctx, types.NamespacedName{Name: newSA.Name, Namespace: newSA.Namespace}, &currentSA); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new ServiceAccount %s", newSA.Name))
				return rclient.Create(ctx, newSA)
			}
			return fmt.Errorf("cannot get ServiceAccount: %w", err)
		}
		if !newSA.DeletionTimestamp.IsZero() {
			return &errRecreate{
				origin: fmt.Errorf("waiting for serviceaccount %q to be removed", newSA.Name),
			}
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, &currentSA); err != nil {
			return err
		}
		if isObjectMetaEqual(&currentSA, newSA, prevSA) {
			return nil
		}
		mergeObjectMetadataIntoNew(&currentSA, newSA, prevSA)
		vmv1beta1.AddFinalizer(newSA, &currentSA)
		// keep significant fields
		newSA.Secrets = currentSA.Secrets
		newSA.AutomountServiceAccountToken = currentSA.AutomountServiceAccountToken
		newSA.ImagePullSecrets = currentSA.ImagePullSecrets
		logger.WithContext(ctx).Info(fmt.Sprintf("updating ServiceAccount %s metadata", newSA.Name))

		return rclient.Update(ctx, newSA)
	})
}
