package reconcile

import (
	"context"
	"fmt"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"

	v2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HPA creates or update horizontalPodAutoscaler object
func HPA(ctx context.Context, rclient client.Client, newHPA, prevHPA *v2.HorizontalPodAutoscaler) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var currentHPA v2.HorizontalPodAutoscaler
		if err := rclient.Get(ctx, types.NamespacedName{Name: newHPA.GetName(), Namespace: newHPA.GetNamespace()}, &currentHPA); err != nil {
			if errors.IsNotFound(err) {
				return rclient.Create(ctx, newHPA)
			}
			return fmt.Errorf("cannot get exist hpa object: %w", err)
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, &currentHPA); err != nil {
			return err
		}
		var prevAnnotations map[string]string
		if prevHPA != nil {
			prevAnnotations = prevHPA.Annotations
		}

		if equality.Semantic.DeepEqual(newHPA.Spec, currentHPA.Spec) &&
			equality.Semantic.DeepEqual(newHPA.Labels, currentHPA.Labels) &&
			isAnnotationsEqual(currentHPA.Annotations, newHPA.Annotations, prevAnnotations) {
			return nil
		}
		logger.WithContext(ctx).Info(fmt.Sprintf("updating HPA=%s configuration", newHPA.Name))

		newHPA.ResourceVersion = currentHPA.ResourceVersion
		newHPA.Status = currentHPA.Status
		newHPA.Annotations = mergeAnnotations(currentHPA.Annotations, newHPA.Annotations, prevAnnotations)

		return rclient.Update(ctx, newHPA)
	})
}
