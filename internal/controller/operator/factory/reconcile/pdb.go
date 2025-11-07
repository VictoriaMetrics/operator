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
		currentPdb := &policyv1.PodDisruptionBudget{}
		err := rclient.Get(ctx, types.NamespacedName{Namespace: newPDB.Namespace, Name: newPDB.Name}, currentPdb)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new PDB %s", newPDB.Name))
				return rclient.Create(ctx, newPDB)
			}
			return fmt.Errorf("cannot get existing pdb: %s, err: %w", newPDB.Name, err)
		}
		if !newPDB.DeletionTimestamp.IsZero() {
			return &errRecreate{
				origin: fmt.Errorf("waiting for PDB %q to be removed", newPDB.Name),
			}
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, currentPdb); err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(newPDB.Spec, currentPdb.Spec) &&
			equality.Semantic.DeepEqual(newPDB.Labels, currentPdb.Labels) &&
			isObjectMetaEqual(currentPdb, newPDB, prevPDB) {
			return nil
		}
		logMsg := fmt.Sprintf("updating PDB %s configuration spec_diff: %s", newPDB.Name, diffDeep(newPDB.Spec, currentPdb.Spec))
		logger.WithContext(ctx).Info(logMsg)

		mergeObjectMetadataIntoNew(currentPdb, newPDB, prevPDB)
		// for some reason Status is not marked as status sub-resource at PDB CRD
		newPDB.Status = currentPdb.Status

		return rclient.Update(ctx, newPDB)
	})
}
