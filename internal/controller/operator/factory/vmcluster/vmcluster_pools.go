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
// pool is nil for the base (non-pool) cluster, in which case cr's own top-level
// VMStorage/VMInsert are treated as that "pool"'s definition.
func createOrUpdatePool(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster, pool *vmv1beta1.VMClusterPool, owner metav1.OwnerReference) error {
	var poolName string
	view, err := buildPoolView(cr, pool)
	if err != nil {
		return err
	}
	poolVMStorage := cr.Spec.VMStorage
	poolVMInsert := cr.Spec.VMInsert
	if pool != nil {
		poolName = pool.Name
		// every declared pool gets its own storage, falling back to the base
		// config; insert stays shared unless the pool overrides it.
		poolVMStorage = view.Spec.VMStorage
		poolVMInsert = pool.VMInsert
	} else {
		if len(cr.Spec.Pools) > 0 {
			poolVMStorage = nil
		}
		for _, p := range cr.Spec.Pools {
			if p.VMInsert != nil {
				poolVMInsert = nil
				break
			}
		}
	}
	var prevView *vmv1beta1.VMCluster
	if prevCR != nil {
		if pool == nil {
			prevView, err = buildPoolView(prevCR, nil)
			if err != nil {
				return err
			}
		} else if prevPool, ok := findPool(prevCR.Spec.Pools, poolName); ok {
			prevView, err = buildPoolView(prevCR, &prevPool)
			if err != nil {
				return err
			}
		}
	}
	if poolVMStorage != nil {
		if err = createOrUpdatePodDisruptionBudgetForVMStorage(ctx, rclient, view, prevView, poolName); err != nil {
			return fmt.Errorf("vmstorage pdb: %w", err)
		}
		if err = createOrUpdateVMStorage(ctx, rclient, view, prevView, poolName, owner); err != nil {
			return fmt.Errorf("vmstorage: %w", err)
		}
		if err = createOrUpdateVMStorageService(ctx, rclient, view, prevView, owner, poolName); err != nil {
			return fmt.Errorf("vmstorage service: %w", err)
		}
		if err = createOrUpdateVMStorageHPA(ctx, rclient, view, prevView, poolName); err != nil {
			return fmt.Errorf("vmstorage hpa: %w", err)
		}
		if err = createOrUpdateVMStorageVPA(ctx, rclient, view, prevView, poolName); err != nil {
			return fmt.Errorf("vmstorage vpa: %w", err)
		}
		if err = createOrUpdateNetworkPolicyForVMStorage(ctx, rclient, view, prevView, poolName); err != nil {
			return fmt.Errorf("vmstorage networkPolicy: %w", err)
		}
	}
	if poolVMInsert != nil {
		if err = createOrUpdatePodDisruptionBudgetForVMInsert(ctx, rclient, view, prevView, poolName); err != nil {
			return fmt.Errorf("vminsert pdb: %w", err)
		}
		if err = createOrUpdateVMInsert(ctx, rclient, view, prevView, poolName, owner); err != nil {
			return fmt.Errorf("vminsert: %w", err)
		}
		if err = createOrUpdateVMInsertService(ctx, rclient, view, prevView, owner, poolName); err != nil {
			return fmt.Errorf("vminsert service: %w", err)
		}
		if err = createOrUpdateVMInsertHPA(ctx, rclient, view, prevView, poolName); err != nil {
			return fmt.Errorf("vminsert hpa: %w", err)
		}
		if err = createOrUpdateVMInsertVPA(ctx, rclient, view, prevView, poolName); err != nil {
			return fmt.Errorf("vminsert vpa: %w", err)
		}
		if err = createOrUpdateNetworkPolicyForVMInsert(ctx, rclient, view, prevView, poolName); err != nil {
			return fmt.Errorf("vminsert networkPolicy: %w", err)
		}
	}
	return nil
}

// buildPoolView returns a deep copy of cr with VMStorage/VMInsert/RetentionPeriod substituted
// with the pool's merged values (pool == nil leaves the base cluster's own values as-is).
// view.Name stays equal to cr.Name so label methods still produce the correct instance label;
// pool-specific naming/labels are applied separately via build.NewPoolBuilder.
func buildPoolView(cr *vmv1beta1.VMCluster, pool *vmv1beta1.VMClusterPool) (*vmv1beta1.VMCluster, error) {
	view := cr.DeepCopy()
	storage, err := poolStorage(cr, pool)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve vmstorage: %w", err)
	}
	view.Spec.VMStorage = storage
	if storage != nil && storage.RetentionPeriod != "" {
		view.Spec.RetentionPeriod = storage.RetentionPeriod
	}

	if pool != nil {
		if pool.VMInsert != nil {
			insert, err := poolInsert(cr, *pool)
			if err != nil {
				return nil, fmt.Errorf("cannot resolve vminsert: %w", err)
			}
			view.Spec.VMInsert = insert
		} else {
			view.Spec.VMInsert = nil
		}
		// a per-pool view must not see sibling pools.
		view.Spec.Pools = nil
	}

	view.Spec.VMSelect = nil
	return view, nil
}

// poolStorage merges the pool's vmstorage over the top-level base (pool fields win, absent
// fields fall through). pool == nil, or a pool with no VMStorage override, both resolve to the
// base as-is.
func poolStorage(cr *vmv1beta1.VMCluster, pool *vmv1beta1.VMClusterPool) (*vmv1beta1.VMStorage, error) {
	base := cr.Spec.VMStorage
	if pool == nil || pool.VMStorage == nil {
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
