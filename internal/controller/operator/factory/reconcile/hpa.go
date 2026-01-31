package reconcile

import (
	"context"
	"fmt"

	v2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// HPA creates or update horizontalPodAutoscaler object
func HPA(ctx context.Context, rclient client.Client, newObj *v2.HorizontalPodAutoscaler) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	return retryOnConflict(func() error {
		var existingObj v2.HorizontalPodAutoscaler
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating HPA=%s configuration", nsn))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get existing HPA=%s: %w", nsn, err)
		}
		if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
			return err
		}
		if equality.Semantic.DeepEqual(newObj.Spec, existingObj.Spec) &&
			equality.Semantic.DeepEqual(newObj.Labels, existingObj.Labels) &&
			equality.Semantic.DeepEqual(newObj.Annotations, existingObj.Annotations) {
			return nil
		}
		specDiff := diffDeepDerivative(newObj.Spec, existingObj.Spec)
		existingObj.Labels = newObj.Labels
		existingObj.Annotations = newObj.Annotations
		existingObj.Spec = newObj.Spec
		logger.WithContext(ctx).Info(fmt.Sprintf("updating HPA=%s, spec_diff: %s", nsn, specDiff))
		return rclient.Update(ctx, &existingObj)
	})
}
