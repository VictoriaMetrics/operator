package reconcile

import (
	"context"
	"fmt"

	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// PDB creates or updates PodDisruptionBudget
func PDB(ctx context.Context, rclient client.Client, newPDB, prevPDB *policyv1.PodDisruptionBudget) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentPdb := &policyv1.PodDisruptionBudget{}
		err := rclient.Get(ctx, types.NamespacedName{Namespace: newPDB.Namespace, Name: newPDB.Name}, currentPdb)
		if err != nil {
			if errors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new PDB %s", newPDB.Name))
				return rclient.Create(ctx, newPDB)
			}
			return fmt.Errorf("cannot get existing pdb: %s, err: %w", newPDB.Name, err)
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, currentPdb); err != nil {
			return err
		}

		var prevAnnotations map[string]string
		if prevPDB != nil {
			prevAnnotations = prevPDB.Annotations
		}

		if equality.Semantic.DeepEqual(newPDB.Spec, currentPdb.Spec) &&
			equality.Semantic.DeepEqual(newPDB.Labels, currentPdb.Labels) &&
			isAnnotationsEqual(currentPdb.Annotations, newPDB.Annotations, prevAnnotations) {
			return nil
		}
		logMsg := fmt.Sprintf("updating PDB %s configuration spec_diff: %s", newPDB.Name, diffDeep(newPDB.Spec, currentPdb.Spec))
		logger.WithContext(ctx).Info(logMsg)

		cloneSignificantMetadata(newPDB, currentPdb)
		// for some reason Status is not marked as status sub-resource at PDB CRD
		newPDB.Status = currentPdb.Status
		newPDB.Annotations = mergeAnnotations(currentPdb.Annotations, newPDB.Annotations, prevAnnotations)

		return rclient.Update(ctx, newPDB)
	})
}
