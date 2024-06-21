package reconcile

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/logger"
	policyv1 "k8s.io/api/policy/v1"
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
		pdb.Status = currentPdb.Status
		vmv1beta1.MergeFinalizers(pdb, vmv1beta1.FinalizerName)
		return rclient.Update(ctx, pdb)
	})
}
