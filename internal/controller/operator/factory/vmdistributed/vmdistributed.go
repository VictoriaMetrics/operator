package vmdistributed

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/go-cmp/cmp"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	defaultMetricsCheckInterval = 30 * time.Second
	defaultStatusCheckInterval  = 5 * time.Second
)

func zoneUpgrade(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, vmClusters []*vmv1beta1.VMCluster, i int) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, cr.Spec.ZoneCommon.ReadyTimeout.Duration)
	defer cancel()

	vmCluster := vmClusters[i]
	item := fmt.Sprintf("%d/%d", i+1, len(vmClusters))
	nsn := types.NamespacedName{Name: vmCluster.Name, Namespace: vmCluster.Namespace}
	logger.WithContext(ctx).Info("reconciling VMCluster", "item", item, "name", nsn)

	var prevVMCluster vmv1beta1.VMCluster
	if err := rclient.Get(ctx, nsn, &prevVMCluster); err != nil {
		return false, fmt.Errorf("unexpected error during attempt to get VMCluster=%s: %w", nsn, err)
	}

	// Set owner reference for this vmcluster
	modifiedOwnerRef, err := setOwnerRefIfNeeded(cr, &prevVMCluster, rclient.Scheme())
	if err != nil {
		return false, fmt.Errorf("failed to set owner reference for VMCluster=%s: %w", nsn, err)
	}

	diff := cmp.Diff(&prevVMCluster.Spec, &vmCluster.Spec)
	specChanged := len(diff) > 0
	if !modifiedOwnerRef && !specChanged {
		// No changes required
		return false, nil
	}
	prevVMCluster.Spec = vmCluster.Spec

	if specChanged {
		logger.WithContext(ctx).Info("updating VMCluster", "diff", diff, "item", item, "name", nsn)
		// Drain cluster reads only if the spec has been modified
		var activeClusters []*vmv1beta1.VMCluster
		activeClusters = append(activeClusters[:0], vmClusters[:i]...)
		activeClusters = append(activeClusters, vmClusters[i+1:]...)
		if err := reconcileVMAuthLB(ctx, rclient, cr, activeClusters); err != nil {
			return false, fmt.Errorf("failed to update vmauth lb with excluded VMCluster=%s: %w", nsn, err)
		}
	}
	if err := rclient.Update(ctx, &prevVMCluster); err != nil {
		return false, err
	}

	if !specChanged {
		return false, nil
	}

	// Wait for VMCluster to be ready
	logger.WithContext(ctx).Info("waiting for VMCluster to become operational", "name", vmCluster.Name)
	if err := reconcile.WaitForStatus(ctx, rclient, vmCluster.DeepCopy(), defaultStatusCheckInterval, vmv1beta1.UpdateStatusOperational); err != nil {
		return false, fmt.Errorf("failed to wait for VMCluster=%s to be ready: %w", vmCluster.Name, err)
	}
	if err := reconcileVMAgent(ctx, rclient, cr, vmClusters); err != nil {
		return false, fmt.Errorf("failed to sync VMAgent: %w", err)
	}
	return true, nil
}

func prepareUpgrade(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, vmClusters []*vmv1beta1.VMCluster) error {
	var resultErr error
	var wg sync.WaitGroup
	var once sync.Once

	ctx, stop := context.WithTimeout(ctx, cr.Spec.ZoneCommon.ReadyTimeout.Duration)
	defer stop()

	cancel := func(err error) {
		once.Do(func() {
			resultErr = err
			stop()
		})
	}

	logger.WithContext(ctx).Info("waiting for all VMClusters to be ready")

	for _, vmCluster := range vmClusters {
		wg.Add(1)
		go func(ctx context.Context, cluster *vmv1beta1.VMCluster) {
			defer wg.Done()
			nsn := types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}
			var prevVMCluster vmv1beta1.VMCluster
			if err := rclient.Get(ctx, nsn, &prevVMCluster); err != nil {
				if !k8serrors.IsNotFound(err) {
					cancel(fmt.Errorf("unexpected error during attempt to get VMCluster=%s: %w", nsn, err))
					return
				}
				if err := rclient.Create(ctx, cluster); err != nil {
					cancel(fmt.Errorf("failed to create VMCluster=%s: %w", nsn, err))
					return
				}
			}
			if err := reconcile.WaitForStatus(ctx, rclient, cluster.DeepCopy(), defaultStatusCheckInterval, vmv1beta1.UpdateStatusOperational); err != nil {
				cancel(fmt.Errorf("failed to wait for VMCluster=%s to be ready: %w", nsn, err))
				return
			}
		}(ctx, vmCluster)
	}
	wg.Wait()
	if resultErr != nil {
		return resultErr
	}

	// Sort VMClusters by observedGeneration in descending order (biggest first)
	sort.Slice(vmClusters, func(i, j int) bool {
		if vmClusters[i].Status.ObservedGeneration != vmClusters[j].Status.ObservedGeneration {
			return vmClusters[i].Status.ObservedGeneration > vmClusters[j].Status.ObservedGeneration
		}
		return vmClusters[i].Name > vmClusters[j].Name
	})

	logger.WithContext(ctx).Info("all VMClusters are ready")

	// TODO[vrutkovs]: if vmauth exists wait for it become operational

	// Update or create the VMAgent
	if err := reconcileVMAgent(ctx, rclient, cr, vmClusters); err != nil {
		return fmt.Errorf("failed to sync VMAgent: %w", err)
	}

	logger.WithContext(ctx).Info("enabling all VMClusters in VMAuth")
	if err := reconcileVMAuthLB(ctx, rclient, cr, vmClusters); err != nil {
		return fmt.Errorf("failed to reconcile VMAuth: %w", err)
	}
	return nil
}

// CreateOrUpdate handles VM deployment reconciliation.
func CreateOrUpdate(ctx context.Context, cr *vmv1alpha1.VMDistributed, rclient client.Client) error {
	// No actions performed if CR is paused
	if cr.Paused() {
		return nil
	}

	if !build.MustSkipRuntimeValidation {
		if err := cr.Validate(); err != nil {
			return err
		}
	}

	logger.WithContext(ctx).Info("starting reconciliation", "name", cr.Name)

	// build VMCluster statuses by name (needed to build target refs VMAuth).
	vmClusters, err := buildVMClusters(cr)
	if err != nil {
		return fmt.Errorf("failed to build VMClusters: %w", err)
	}

	if err := prepareUpgrade(ctx, rclient, cr, vmClusters); err != nil {
		return fmt.Errorf("failed to prepare VMDistributed=%s/%s upgrade: %w", cr.Namespace, cr.Name, err)
	}

	// Apply changes to VMClusters one by one if new spec needs to be applied
	for i := range vmClusters {
		upgraded, err := zoneUpgrade(ctx, rclient, cr, vmClusters, i)
		if err != nil {
			return err
		}

		if !upgraded {
			continue
		}

		// Sleep for zoneUpdatePause time between VMClusters updates (unless its the last one)
		if i != len(vmClusters)-1 {
			item := fmt.Sprintf("%d/%d", i+1, len(vmClusters))
			logger.WithContext(ctx).Info("sleeping between zone updates", "item", item, "updatePause", cr.Spec.ZoneCommon.UpdatePause.Duration)
			select {
			case <-time.After(cr.Spec.ZoneCommon.UpdatePause.Duration):
			case <-ctx.Done():
				return fmt.Errorf("stopping update since context cancelled")
			}
		}
	}

	logger.WithContext(ctx).Info("ensuring all VMClusters are present in VMAuth")
	if err := reconcileVMAuthLB(ctx, rclient, cr, vmClusters); err != nil {
		return fmt.Errorf("failed to reconcile VMAuth: %w", err)
	}

	logger.WithContext(ctx).Info("reconciliation completed", "name", cr.Name)
	return nil
}
