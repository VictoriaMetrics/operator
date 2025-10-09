package reconcile

import (
	"context"
	"fmt"

	v2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// HPA creates or update horizontalPodAutoscaler object
func HPA(ctx context.Context, rclient client.Client, newHPA, prevHPA *v2.HorizontalPodAutoscaler) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var currentHPA v2.HorizontalPodAutoscaler
		if err := rclient.Get(ctx, types.NamespacedName{Name: newHPA.GetName(), Namespace: newHPA.GetNamespace()}, &currentHPA); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating HPA %s configuration", newHPA.Name))
				return rclient.Create(ctx, newHPA)
			}
			return fmt.Errorf("cannot get exist hpa object: %w", err)
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, &currentHPA); err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(newHPA.Spec, currentHPA.Spec) &&
			equality.Semantic.DeepEqual(newHPA.Labels, currentHPA.Labels) &&
			isObjectMetaEqual(&currentHPA, newHPA, prevHPA) {
			return nil
		}

		mergeObjectMetadataIntoNew(&currentHPA, newHPA, prevHPA)
		newHPA.Status = currentHPA.Status

		logMsg := fmt.Sprintf("updating HPA %s configuration spec_diff: %s", newHPA.Name, diffDeepDerivative(newHPA.Spec, currentHPA.Spec))
		logger.WithContext(ctx).Info(logMsg)

		return rclient.Update(ctx, newHPA)
	})
}
