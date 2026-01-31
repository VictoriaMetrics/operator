package reconcile

import (
	"context"
	"fmt"

	v2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// HPA creates or update horizontalPodAutoscaler object
func HPA(ctx context.Context, rclient client.Client, newObj, prevObj *v2.HorizontalPodAutoscaler) error {
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
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
			isObjectMetaEqual(&existingObj, newObj, prevMeta) {
			return nil
		}

		mergeObjectMetadataIntoNew(&existingObj, newObj, prevMeta)
		newObj.Status = existingObj.Status

		logMsg := fmt.Sprintf("updating HPA=%s configuration spec_diff: %s", nsn, diffDeepDerivative(newObj.Spec, existingObj.Spec))
		logger.WithContext(ctx).Info(logMsg)

		return rclient.Update(ctx, newObj)
	})
}
