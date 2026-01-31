package reconcile

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// Ingress creates or updates Ingress object
func Ingress(ctx context.Context, rclient client.Client, newObj *networkingv1.Ingress) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	return retryOnConflict(func() error {
		var existingObj networkingv1.Ingress
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating Ingress=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get existing Ingress=%s: %w", nsn, err)
		}
		if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
			return err
		}

		isEqual := equality.Semantic.DeepEqual(newObj.Spec, existingObj.Spec)
		if isEqual &&
			equality.Semantic.DeepEqual(newObj.Labels, existingObj.Labels) &&
			equality.Semantic.DeepEqual(newObj.Annotations, existingObj.Annotations) {
			return nil
		}
		specDiff := diffDeepDerivative(newObj.Spec, existingObj.Spec)
		existingObj.Labels = newObj.Labels
		existingObj.Annotations = newObj.Annotations
		existingObj.Spec = newObj.Spec
		logger.WithContext(ctx).Info(fmt.Sprintf("updating Ingress=%s, spec_diff=%s", nsn, specDiff))
		return rclient.Update(ctx, &existingObj)
	})
}
