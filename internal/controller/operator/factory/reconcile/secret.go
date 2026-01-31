package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// Secret reconciles secret object
func Secret(ctx context.Context, rclient client.Client, newObj *corev1.Secret) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	return retryOnConflict(func() error {
		var existingObj corev1.Secret
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new Secret=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return err
		}
		if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
			return err
		}
		if equality.Semantic.DeepEqual(newObj.Data, existingObj.Data) &&
			equality.Semantic.DeepEqual(newObj.Labels, existingObj.Labels) &&
			equality.Semantic.DeepEqual(newObj.Annotations, existingObj.Annotations) {
			return nil
		}
		existingObj.Labels = newObj.Labels
		existingObj.Annotations = newObj.Annotations
		existingObj.Data = newObj.Data
		logger.WithContext(ctx).Info(fmt.Sprintf("updating configuration Secret=%s", nsn))
		return rclient.Update(ctx, &existingObj)
	})
}
