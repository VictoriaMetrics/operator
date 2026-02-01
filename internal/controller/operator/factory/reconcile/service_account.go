package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// ServiceAccount creates service account or updates exist one
func ServiceAccount(ctx context.Context, rclient client.Client, newObj, prevObj *corev1.ServiceAccount) error {
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	return retryOnConflict(func() error {
		var existingObj corev1.ServiceAccount
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new ServiceAccount=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get ServiceAccount=%s: %w", nsn, err)
		}
		if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
			return err
		}
		if isObjectMetaEqual(&existingObj, newObj, prevMeta) {
			return nil
		}
		mergeObjectMetadataIntoNew(&existingObj, newObj, prevMeta)
		vmv1beta1.AddFinalizer(newObj, &existingObj)
		// keep significant fields
		newObj.Secrets = existingObj.Secrets
		newObj.AutomountServiceAccountToken = existingObj.AutomountServiceAccountToken
		newObj.ImagePullSecrets = existingObj.ImagePullSecrets
		logger.WithContext(ctx).Info(fmt.Sprintf("updating ServiceAccount=%s metadata", nsn))

		return rclient.Update(ctx, newObj)
	})
}
