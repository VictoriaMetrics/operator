package reconcile

import (
	"context"
	"fmt"

	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// PDB creates or updates PodDisruptionBudget
func PDB(ctx context.Context, rclient client.Client, newPDB, prevPDB *policyv1.PodDisruptionBudget) error {
	return retryOnConflict(func() error {
		currentPDB := &policyv1.PodDisruptionBudget{}
		err := rclient.Get(ctx, types.NamespacedName{Namespace: newPDB.Namespace, Name: newPDB.Name}, currentPDB)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new PDB %s", newPDB.Name))
				return rclient.Create(ctx, newPDB)
			}
			return fmt.Errorf("cannot get existing pdb: %s, err: %w", newPDB.Name, err)
		}
		if !currentPDB.DeletionTimestamp.IsZero() {
			return &errRecreate{
				origin: fmt.Errorf("waiting for PDB %q to be removed", newPDB.Name),
			}
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, currentPDB); err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(newPDB.Spec, currentPDB.Spec) &&
			equality.Semantic.DeepEqual(newPDB.Labels, currentPDB.Labels) &&
			isObjectMetaEqual(currentPDB, newPDB, prevPDB) {
			return nil
		}
		logMsg := fmt.Sprintf("updating PDB %s configuration spec_diff: %s", newPDB.Name, diffDeep(newPDB.Spec, currentPDB.Spec))
		logger.WithContext(ctx).Info(logMsg)

		mergeObjectMetadataIntoNew(currentPDB, newPDB, prevPDB)
		// for some reason Status is not marked as status sub-resource at PDB CRD
		newPDB.Status = currentPDB.Status

		return rclient.Update(ctx, newPDB)
	})
}
