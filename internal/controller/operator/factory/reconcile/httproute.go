package reconcile

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// HTTPRoute creates or updates HTTPRoute object
func HTTPRoute(ctx context.Context, rclient client.Client, newHTTPRoute, prevHTTPRoute *gwapiv1.HTTPRoute) error {
	return retryOnConflict(func() error {
		var curHTTPRoute gwapiv1.HTTPRoute
		if err := rclient.Get(ctx, types.NamespacedName{Name: newHTTPRoute.GetName(), Namespace: newHTTPRoute.GetNamespace()}, &curHTTPRoute); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating HTTPRoute %s configuration", newHTTPRoute.Name))
				return rclient.Create(ctx, newHTTPRoute)
			}
			return fmt.Errorf("cannot get existing HTTPRoute object: %w", err)
		}
		if !curHTTPRoute.DeletionTimestamp.IsZero() {
			return &errRecreate{
				origin: fmt.Errorf("waiting for HTTPRoute %q to be removed", newHTTPRoute.Name),
			}
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, &curHTTPRoute); err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(newHTTPRoute.Spec, curHTTPRoute.Spec) &&
			equality.Semantic.DeepEqual(newHTTPRoute.Labels, curHTTPRoute.Labels) &&
			isObjectMetaEqual(&curHTTPRoute, newHTTPRoute, prevHTTPRoute) {
			return nil
		}

		mergeObjectMetadataIntoNew(&curHTTPRoute, newHTTPRoute, prevHTTPRoute)
		newHTTPRoute.Status = curHTTPRoute.Status

		logMsg := fmt.Sprintf("updating HTTPRoute %s configuration spec_diff: %s", newHTTPRoute.Name, diffDeepDerivative(newHTTPRoute.Spec, curHTTPRoute.Spec))
		logger.WithContext(ctx).Info(logMsg)

		return rclient.Update(ctx, newHTTPRoute)
	})
}
