package reconcile

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// Ingress creates or updates Ingress object
func Ingress(ctx context.Context, rclient client.Client, newObj, prevObj *networkingv1.Ingress) error {
	var isPrevEqual bool
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
		isPrevEqual = equality.Semantic.DeepDerivative(prevObj.Spec, newObj.Spec)
	}
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
			isPrevEqual &&
			equality.Semantic.DeepEqual(newObj.Labels, existingObj.Labels) &&
			isObjectMetaEqual(&existingObj, newObj, prevMeta) {
			return nil
		}

		mergeObjectMetadataIntoNew(&existingObj, newObj, prevMeta)
		newObj.Status = existingObj.Status

		logMsg := fmt.Sprintf("updating Ingress=%s configuration spec_diff: %s"+
			"is_prev_equal=%v,is_current_equal=%v,is_prev_nil=%v",
			nsn, diffDeepDerivative(newObj.Spec, existingObj.Spec), isPrevEqual, isEqual, prevMeta == nil)

		logger.WithContext(ctx).Info(logMsg)

		return rclient.Update(ctx, newObj)
	})
}
