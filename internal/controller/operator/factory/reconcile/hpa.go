package reconcile

import (
	"context"
	"fmt"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"

	v2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HPA creates or update horizontalPodAutoscaler object
func HPA(ctx context.Context, rclient client.Client, targetHPA *v2.HorizontalPodAutoscaler) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var existHPA v2.HorizontalPodAutoscaler
		if err := rclient.Get(ctx, types.NamespacedName{Name: targetHPA.GetName(), Namespace: targetHPA.GetNamespace()}, &existHPA); err != nil {
			if errors.IsNotFound(err) {
				return rclient.Create(ctx, targetHPA)
			}
			return fmt.Errorf("cannot get exist hpa object: %w", err)
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, &existHPA); err != nil {
			return err
		}

		targetHPA.Annotations = labels.Merge(existHPA.Annotations, targetHPA.Annotations)
		targetHPA.ResourceVersion = existHPA.ResourceVersion
		targetHPA.Status = existHPA.Status
		if equality.Semantic.DeepEqual(targetHPA.Spec, existHPA.Spec) &&
			equality.Semantic.DeepEqual(targetHPA.Labels, existHPA.Labels) &&
			equality.Semantic.DeepEqual(targetHPA.Annotations, existHPA.Annotations) {
			return nil
		}
		logger.WithContext(ctx).Info("updating HPA configuration", "hpa_name", targetHPA.Name)

		return rclient.Update(ctx, targetHPA)
	})
}
