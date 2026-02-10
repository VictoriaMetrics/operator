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
func Ingress(ctx context.Context, rclient client.Client, newObj, prevObj *networkingv1.Ingress) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var prevAnnotations, prevLabels map[string]string
	var isPrevEqual bool
	if prevObj != nil {
		prevAnnotations = prevObj.Annotations
		prevLabels = prevObj.Labels
		isPrevEqual = equality.Semantic.DeepDerivative(prevObj.Spec, newObj.Spec)
	}
	return retryOnConflict(func() error {
		var existingObj networkingv1.Ingress
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating Ingress=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get existing Ingress=%s: %w", nsn, err)
		}
		if err := needsGarbageCollection(ctx, rclient, &existingObj); err != nil {
			return err
		}
		isEqual := equality.Semantic.DeepDerivative(newObj.Spec, existingObj.Spec)
		if isEqual &&
			isPrevEqual &&
			areMapsEqual(existingObj.Labels, newObj.Labels, prevLabels) &&
			areMapsEqual(existingObj.Annotations, newObj.Annotations, prevAnnotations) {
			return nil
		}
		specDiff := diffDeepDerivative(newObj.Spec, existingObj.Spec)
		existingObj.Labels = mergeMaps(existingObj.Labels, newObj.Labels, prevLabels)
		existingObj.Annotations = mergeMaps(existingObj.Annotations, newObj.Annotations, prevAnnotations)
		existingObj.Spec = newObj.Spec
		addFinalizerIfAbsent(&existingObj)
		logMsg := fmt.Sprintf("updating Ingress=%s spec_diff: %s"+
			"is_prev_equal=%v,is_current_equal=%v,is_prev_nil=%v",
			nsn, specDiff, isPrevEqual, isEqual, prevObj == nil)
		logger.WithContext(ctx).Info(logMsg)
		return rclient.Update(ctx, &existingObj)
	})
}
