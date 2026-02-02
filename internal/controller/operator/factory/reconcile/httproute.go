package reconcile

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// HTTPRoute creates or updates HTTPRoute object
func HTTPRoute(ctx context.Context, rclient client.Client, newObj, prevObj *gwapiv1.HTTPRoute, owner *metav1.OwnerReference) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var prevMeta *metav1.ObjectMeta
	var isPrevEqual bool
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
		isPrevEqual = equality.Semantic.DeepDerivative(prevObj.Spec, newObj.Spec)
	}
	return retryOnConflict(func() error {
		var existingObj gwapiv1.HTTPRoute
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating HTTPRoute=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get existing HTTPRoute=%s: %w", nsn, err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner)
		if err != nil {
			return err
		}
		isEqual := equality.Semantic.DeepDerivative(newObj.Spec, existingObj.Spec)
		if isEqual && isPrevEqual && !metaChanged {
			return nil
		}
		specDiff := diffDeepDerivative(newObj.Spec, existingObj.Spec)
		existingObj.Spec = newObj.Spec
		logMsg := fmt.Sprintf("updating HTTPRoute=%s configuration spec_diff: %s"+
			"is_prev_equal=%v,is_current_equal=%v,is_prev_nil=%v",
			nsn, specDiff, isPrevEqual, isEqual, prevObj == nil)
		logger.WithContext(ctx).Info(logMsg)
		return rclient.Update(ctx, &existingObj)
	})
}
