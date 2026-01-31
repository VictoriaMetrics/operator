package reconcile

import (
	"context"
	"fmt"

	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// PDB creates or updates PodDisruptionBudget
func PDB(ctx context.Context, rclient client.Client, newObj *policyv1.PodDisruptionBudget) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	return retryOnConflict(func() error {
		var existingObj policyv1.PodDisruptionBudget
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new PDB=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get existing PDB=%s: %w", nsn, err)
		}
		if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(newObj.Spec, existingObj.Spec) &&
			equality.Semantic.DeepEqual(newObj.Labels, existingObj.Labels) &&
			equality.Semantic.DeepEqual(newObj.Annotations, existingObj.Annotations) {
			return nil
		}
		specDiff := diffDeepDerivative(newObj.Spec, existingObj.Spec)
		existingObj.Labels = newObj.Labels
		existingObj.Annotations = newObj.Annotations
		existingObj.Spec = newObj.Spec
		logger.WithContext(ctx).Info(fmt.Sprintf("updating PDB=%s, spec_diff=%s", nsn, specDiff))
		return rclient.Update(ctx, &existingObj)
	})
}
