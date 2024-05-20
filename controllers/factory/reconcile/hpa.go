package reconcile

import (
	"context"
	"fmt"

	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HPA creates or update horizontalPodAutoscaler object
func HPA(ctx context.Context, rclient client.Client, targetHPA client.Object) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existHPA := k8stools.NewHPAEmptyObject()
		if err := rclient.Get(ctx, types.NamespacedName{Name: targetHPA.GetName(), Namespace: targetHPA.GetNamespace()}, existHPA); err != nil {
			if errors.IsNotFound(err) {
				return rclient.Create(ctx, targetHPA)
			}
			return fmt.Errorf("cannot get exist hpa object: %w", err)
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, existHPA); err != nil {
			return err
		}
		targetHPA.SetResourceVersion(existHPA.GetResourceVersion())
		victoriametricsv1beta1.MergeFinalizers(targetHPA, victoriametricsv1beta1.FinalizerName)
		targetHPA.SetAnnotations(labels.Merge(existHPA.GetAnnotations(), targetHPA.GetAnnotations()))

		return rclient.Update(ctx, targetHPA)
	})
}
