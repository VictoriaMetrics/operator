package vmcluster

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

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
		} else if prevPool, ok := prevCR.FindPool(poolName); ok {
			prevView, err = buildPoolView(prevCR, prevPool)
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

func poolStorage(cr *vmv1beta1.VMCluster, pool *vmv1beta1.VMClusterPool) (*vmv1beta1.VMStorage, error) {
	merged, err := cr.ResolvePoolVMStorage(pool)
	if err != nil {
		return nil, err
	}
	if merged != nil && pool != nil && pool.VMStorage != nil && cr.Spec.VMStorage == nil {
		build.DefaultPoolVMStorage(merged, cr)
	}
	return merged, nil
}

func poolInsert(cr *vmv1beta1.VMCluster, pool vmv1beta1.VMClusterPool) (*vmv1beta1.VMInsert, error) {
	merged, err := cr.ResolvePoolVMInsert(&pool)
	if err != nil {
		return nil, err
	}
	if merged != nil && cr.Spec.VMInsert == nil {
		build.DefaultPoolVMInsert(merged, cr)
	}
	return merged, nil
}
