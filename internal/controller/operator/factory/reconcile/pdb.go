package reconcile

import (
	"context"
	"fmt"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PDB creates or updates PodDisruptionBudget
func PDB(ctx context.Context, rclient client.Client, pdb *policyv1.PodDisruptionBudget) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentPdb := &policyv1.PodDisruptionBudget{}
		err := rclient.Get(ctx, types.NamespacedName{Namespace: pdb.Namespace, Name: pdb.Name}, currentPdb)
		if err != nil {
			if errors.IsNotFound(err) {
				logger.WithContext(ctx).Info("creating new pdb", "pdb_name", pdb.Name)
				return rclient.Create(ctx, pdb)
			}
			return fmt.Errorf("cannot get existing pdb: %s, err: %w", pdb.Name, err)
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, currentPdb); err != nil {
			return err
		}
		pdb.Annotations = labels.Merge(currentPdb.Annotations, pdb.Annotations)
		if currentPdb.ResourceVersion != "" {
			pdb.ResourceVersion = currentPdb.ResourceVersion
		}
		// for some reason Status is not marked as status sub-resource at PDB CRD
		pdb.Status = currentPdb.Status

		if equality.Semantic.DeepEqual(pdb.Spec, currentPdb.Spec) &&
			equality.Semantic.DeepEqual(pdb.Labels, currentPdb.Labels) &&
			equality.Semantic.DeepEqual(pdb.Annotations, currentPdb.Annotations) {
			return nil
		}
		logger.WithContext(ctx).Info("updating HPA configuration", "pdb_name", pdb.Name)

		return rclient.Update(ctx, pdb)
	})
}
