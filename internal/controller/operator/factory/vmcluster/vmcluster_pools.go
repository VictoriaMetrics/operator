package vmcluster

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// createOrUpdatePool reconciles all resources for a single pool in sequence:
// vmstorage StatefulSet → vmstorage Service → vminsert Deployment + Service (if dedicated).
func createOrUpdatePool(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster, pool vmv1beta1.VMClusterPool, owner metav1.OwnerReference) error {
	view, err := buildPoolView(cr, pool)
	if err != nil {
		return err
	}
	var prevView *vmv1beta1.VMCluster
	if prevCR != nil {
		if prevPool, ok := findPool(prevCR.Spec.Pools, pool.Name); ok {
			prevView, err = buildPoolView(prevCR, prevPool)
			if err != nil {
				return err
			}
		}
	}

	if view.Spec.VMStorage.PodDisruptionBudget != nil {
		if err := createOrUpdatePodDisruptionBudgetForVMStorage(ctx, rclient, view, prevView, pool.Name); err != nil {
			return fmt.Errorf("vmstorage pdb: %w", err)
		}
	}
	if err := createOrUpdateVMStorage(ctx, rclient, view, prevView, pool.Name, owner); err != nil {
		return fmt.Errorf("vmstorage: %w", err)
	}
	if err := createOrUpdateVMStorageService(ctx, rclient, view, prevView, owner, pool.Name); err != nil {
		return fmt.Errorf("vmstorage service: %w", err)
	}
	if err := createOrUpdateVMStorageHPA(ctx, rclient, view, prevView, pool.Name); err != nil {
		return fmt.Errorf("vmstorage hpa: %w", err)
	}
	if err := createOrUpdateVMStorageVPA(ctx, rclient, view, prevView, pool.Name); err != nil {
		return fmt.Errorf("vmstorage vpa: %w", err)
	}

	if pool.VMInsert != nil {
		if view.Spec.VMInsert.PodDisruptionBudget != nil {
			if err := createOrUpdatePodDisruptionBudgetForVMInsert(ctx, rclient, view, prevView, pool.Name); err != nil {
				return fmt.Errorf("vminsert pdb: %w", err)
			}
		}
		if err := createOrUpdateVMInsert(ctx, rclient, view, prevView, pool.Name, owner); err != nil {
			return fmt.Errorf("vminsert: %w", err)
		}
		if err := createOrUpdateVMInsertService(ctx, rclient, view, prevView, owner, pool.Name); err != nil {
			return fmt.Errorf("vminsert service: %w", err)
		}
		if err := createOrUpdateVMInsertHPA(ctx, rclient, view, prevView, pool.Name); err != nil {
			return fmt.Errorf("vminsert hpa: %w", err)
		}
		if err := createOrUpdateVMInsertVPA(ctx, rclient, view, prevView, pool.Name); err != nil {
			return fmt.Errorf("vminsert vpa: %w", err)
		}
	}
	return nil
}

// buildPoolView returns a deep copy of cr with its VMStorage, VMInsert, and
// RetentionPeriod substituted with the pool's merged values.
// view.Name stays equal to cr.Name so all label methods produce the correct instance label.
// Pool-specific naming and labels are handled by build.NewPoolBuilder inside the factory functions.
func buildPoolView(cr *vmv1beta1.VMCluster, pool vmv1beta1.VMClusterPool) (*vmv1beta1.VMCluster, error) {
	view := cr.DeepCopy()

	storage, err := poolStorage(cr, pool)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve vmstorage: %w", err)
	}
	view.Spec.VMStorage = storage
	if storage != nil && storage.RetentionPeriod != "" {
		view.Spec.RetentionPeriod = storage.RetentionPeriod
	}

	if pool.VMInsert != nil {
		insert, err := poolInsert(cr, pool)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve vminsert: %w", err)
		}
		view.Spec.VMInsert = insert
	} else {
		view.Spec.VMInsert = nil
	}

	view.Spec.VMSelect = nil
	view.Spec.Pools = nil
	return view, nil
}

// poolStorage merges the pool's vmstorage over the top-level base using MergeDeep.
// Pool fields take precedence; absent pool fields fall through to the base.
func poolStorage(cr *vmv1beta1.VMCluster, pool vmv1beta1.VMClusterPool) (*vmv1beta1.VMStorage, error) {
	base := cr.Spec.VMStorage
	if pool.VMStorage == nil {
		if base == nil {
			return nil, nil
		}
		return base.DeepCopy(), nil
	}
	merged := pool.VMStorage.DeepCopy()
	if base != nil {
		// reverse=true: merged (pool) fields win; base fills absent fields.
		if err := vmv1beta1.MergeDeep(merged, base, true); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

// poolInsert merges the pool's vminsert over the top-level base using MergeDeep.
func poolInsert(cr *vmv1beta1.VMCluster, pool vmv1beta1.VMClusterPool) (*vmv1beta1.VMInsert, error) {
	if pool.VMInsert == nil {
		return nil, nil
	}
	base := cr.Spec.VMInsert
	merged := pool.VMInsert.DeepCopy()
	if base != nil {
		if err := vmv1beta1.MergeDeep(merged, base, true); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

// storageNodeIDs returns available storage node IDs for the given VMStorage spec,
// skipping nodes under maintenance for the given component kind.
func storageNodeIDs(storage *vmv1beta1.VMStorage, kind vmv1beta1.ClusterComponent) []int32 {
	if storage == nil || (storage.ReplicaCount == nil && storage.HPA == nil) {
		return nil
	}
	maintenanceNodes := sets.New[int32]()
	switch kind {
	case vmv1beta1.ClusterComponentSelect:
		maintenanceNodes.Insert(storage.MaintenanceSelectNodeIDs...)
	case vmv1beta1.ClusterComponentInsert:
		maintenanceNodes.Insert(storage.MaintenanceInsertNodeIDs...)
	}
	var replicaCount int32
	if storage.ReplicaCount != nil {
		replicaCount = *storage.ReplicaCount
	} else if storage.HPA != nil {
		replicaCount = storage.HPA.GetMinReplicas()
	}
	var result []int32
	for i := int32(0); i < replicaCount; i++ {
		if !maintenanceNodes.Has(i) {
			result = append(result, i)
		}
	}
	return result
}

// findPool returns the pool with the given name and whether it was found.
func findPool(pools []vmv1beta1.VMClusterPool, name string) (vmv1beta1.VMClusterPool, bool) {
	for _, p := range pools {
		if p.Name == name {
			return p, true
		}
	}
	return vmv1beta1.VMClusterPool{}, false
}
