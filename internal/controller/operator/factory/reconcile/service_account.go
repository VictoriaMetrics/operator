package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// ServiceAccount creates service account or updates exist one
func ServiceAccount(ctx context.Context, rclient client.Client, newSA, prevSA *corev1.ServiceAccount) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var currentSA corev1.ServiceAccount
		if err := rclient.Get(ctx, types.NamespacedName{Name: newSA.Name, Namespace: newSA.Namespace}, &currentSA); err != nil {
			if errors.IsNotFound(err) {
				return rclient.Create(ctx, newSA)
			}
			return fmt.Errorf("cannot get ServiceAccount for given CRD Object=%q, err=%w", newSA.Name, err)
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, &currentSA); err != nil {
			return err
		}
		var prevAnnotations map[string]string
		if prevSA != nil {
			prevAnnotations = prevSA.Annotations
		}
		if equality.Semantic.DeepEqual(newSA.Labels, currentSA.Labels) &&
			isAnnotationsEqual(currentSA.Annotations, newSA.Annotations, prevAnnotations) {
			return nil
		}
		logger.WithContext(ctx).Info("updating ServiceAccount configuration")

		currentSA.Annotations = mergeAnnotations(currentSA.Annotations, newSA.Annotations, prevAnnotations)
		currentSA.Labels = newSA.Labels
		vmv1beta1.AddFinalizer(&currentSA, &currentSA)

		return rclient.Update(ctx, &currentSA)
	})
}
