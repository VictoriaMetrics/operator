package vmdistributedcluster

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/go-cmp/cmp"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

var (
	defaultVMClusterWaitReadyDeadline   = metav1.Duration{Duration: 5 * time.Minute}
	defaultVMAgentCheckInterval         = 30 * time.Second
	defaultVMAgentFlushDeadlineDeadline = metav1.Duration{Duration: 1 * time.Minute}
	defaultZoneUpdatePause              = metav1.Duration{Duration: 1 * time.Minute}
)

// CreateOrUpdate handles VM deployment reconciliation.
func CreateOrUpdate(ctx context.Context, cr *vmv1alpha1.VMDistributedCluster, rclient client.Client, httpTimeout time.Duration) error {
	// No actions performed if CR is paused
	if cr.Paused() {
		return nil
	}

	logger.WithContext(ctx).Info("Starting reconciliation", "name", cr.Name)

	// Fetch VMCluster statuses by name (needed to build target refs for the VMUser).
	vmClusters, err := fetchVMClusters(ctx, rclient, cr.Namespace, cr.Spec.Zones.VMClusters)
	if err != nil {
		return fmt.Errorf("failed to fetch vmclusters: %w", err)
	}

	// Throw error if VMCluster has any other owner - that means it is managed by one instance of VMDistributedCluster only
	for _, vmCluster := range vmClusters {
		err := ensureNoVMClusterOwners(cr, vmCluster)
		if err != nil {
			return fmt.Errorf("failed to validate owner references for unreferenced vmcluster %s: %w", vmCluster.Name, err)
		}
	}

	// TODO[vrutkovs]: if vmauth exists wait for it become operational

	// Update or create the VMAgent
	var vmAgentObjs []*vmv1beta1.VMAgent
	if cr.Spec.VMAgent.LabelSelector == nil {
		vmAgentObj, err := updateOrCreateVMAgent(ctx, rclient, cr, vmClusters)
		if err != nil {
			return fmt.Errorf("failed to update or create VMAgent: %w", err)
		}
		vmAgentObjs = append(vmAgentObjs, vmAgentObj)
	} else {
		vmAgentObjs, err := listVMAgents(ctx, rclient, cr.Namespace, cr.Spec.VMAgent.LabelSelector)
		if err != nil {
			return fmt.Errorf("failed to list VMAgents: %w", err)
		}
		if len(vmAgentObjs) == 0 {
			return fmt.Errorf("no VMAgents found with label selector %v", cr.Spec.VMAgent.LabelSelector)
		}
	}

	// Setup deadlines and timeouts
	vmclusterWaitReadyDeadline := defaultVMClusterWaitReadyDeadline.Duration
	if cr.Spec.ReadyDeadline != nil {
		vmclusterWaitReadyDeadline = cr.Spec.ReadyDeadline.Duration
	}
	vmAgentFlushDeadlineDeadline := defaultVMAgentFlushDeadlineDeadline.Duration
	if cr.Spec.VMAgentFlushDeadline != nil {
		vmAgentFlushDeadlineDeadline = cr.Spec.VMAgentFlushDeadline.Duration
	}
	zoneUpdatePause := defaultZoneUpdatePause.Duration
	if cr.Spec.ZoneUpdatePause != nil {
		zoneUpdatePause = cr.Spec.ZoneUpdatePause.Duration
	}

	// Setup custom HTTP client
	httpClient := &http.Client{
		Timeout: httpTimeout,
	}

	logger.WithContext(ctx).Info("Reconciling VMClusters")

	// Apply changes to VMClusters one by one if new spec needs to be applied
	for i, vmClusterObj := range vmClusters {
		logger.WithContext(ctx).Info("Reconciling VMCluster", "index", i, "name", vmClusterObj.Name)

		zoneRefOrSpec := cr.Spec.Zones.VMClusters[i]

		needsToBeCreated := false
		// Get vmClusterObj in case it doesn't exist or has changed
		if err = rclient.Get(ctx, types.NamespacedName{Name: vmClusterObj.Name, Namespace: vmClusterObj.Namespace}, vmClusterObj); k8serrors.IsNotFound(err) {
			needsToBeCreated = true
		}

		// Update the VMCluster when overrideSpec needs to be applied or ownerref set
		mergedSpec := vmClusterObj.Spec
		previousVMClusterObjSpec := vmClusterObj.Spec.DeepCopy()
		modifiedSpec := false

		// Apply GlobalOverrideSpec if it is set
		if cr.Spec.Zones.GlobalOverrideSpec != nil {
			mergedSpec, modifiedSpec, err = ApplyOverrideSpec(vmClusterObj.Spec, cr.Spec.Zones.GlobalOverrideSpec)
			if err != nil {
				return fmt.Errorf("failed to apply global override spec for vmcluster %s at index %d: %w", vmClusterObj.Name, i, err)
			}
		}
		diff := cmp.Diff(*previousVMClusterObjSpec, mergedSpec)
		if diff != "" {
			logger.WithContext(ctx).Info("GlobalOverrideSpec diff", "diff", diff, "modifiedSpec", modifiedSpec, "index", i, "name", vmClusterObj.Name)
			logger.WithContext(ctx).Info(diff)
		}

		// Apply cluster-specific override if it exist
		previousVMClusterObjSpec = mergedSpec.DeepCopy()
		if zoneRefOrSpec.Ref != nil && zoneRefOrSpec.OverrideSpec != nil {
			mergedSpec, modifiedSpec, err = ApplyOverrideSpec(mergedSpec, zoneRefOrSpec.OverrideSpec)
			if err != nil {
				return fmt.Errorf("failed to apply override spec for vmcluster %s at index %d: %w", vmClusterObj.Name, i, err)
			}
		}
		diff = cmp.Diff(*previousVMClusterObjSpec, mergedSpec)
		if diff != "" {
			logger.WithContext(ctx).Info("zoneRefOrSpec.Ref diff", "diff", diff, "modifiedSpec", modifiedSpec, "index", i, "name", vmClusterObj.Name)
			logger.WithContext(ctx).Info(diff)
		}

		previousVMClusterObjSpec = mergedSpec.DeepCopy()
		if zoneRefOrSpec.Spec != nil {
			mergedSpec, modifiedSpec, err = mergeVMClusterSpecs(mergedSpec, *zoneRefOrSpec.Spec)
			if err != nil {
				return fmt.Errorf("failed to merge spec for vmcluster %s at index %d: %w", vmClusterObj.Name, i, err)
			}
		}
		diff = cmp.Diff(*previousVMClusterObjSpec, mergedSpec)
		if diff != "" {
			logger.WithContext(ctx).Info("zoneRefOrSpec.Spec diff", "diff", diff, "modifiedSpec", modifiedSpec, "index", i, "name", vmClusterObj.Name)
			logger.WithContext(ctx).Info(diff)
		}

		// Apply VMDistributedCluster License to VMCluster if not already set
		if mergedSpec.License == nil && cr.Spec.License != nil {
			mergedSpec.License = cr.Spec.License.DeepCopy()
			modifiedSpec = true
		}

		// Set owner reference for this vmcluster
		modifiedOwnerRef, err := setOwnerRefIfNeeded(cr, vmClusterObj, rclient.Scheme())
		if err != nil {
			return fmt.Errorf("failed to set owner reference for vmcluster %s: %w", vmClusterObj.Name, err)
		}
		if !needsToBeCreated && !modifiedSpec && !modifiedOwnerRef {
			// No changes required
			continue
		}

		vmClusterObj.Spec = mergedSpec

		if needsToBeCreated {
			logger.WithContext(ctx).Info("Creating VMCluster", "index", i, "name", vmClusterObj.Name)
			if err := rclient.Create(ctx, vmClusterObj); err != nil {
				return fmt.Errorf("failed to create vmcluster %s at index %d after applying override spec: %w", vmClusterObj.Name, i, err)
			}
		} else {
			// Drain cluster reads only if the spec has been modified
			if modifiedSpec {
				logger.WithContext(ctx).Info("Excluding VMCluster from vmauth configuration", "index", i, "name", vmClusterObj.Name)
				// Update vmauth lb with excluded cluster
				activeVMClusters := make([]*vmv1beta1.VMCluster, 0, len(vmClusters)-1)
				for _, vmc := range vmClusters {
					if vmc.Name == vmClusterObj.Name {
						continue
					}
					activeVMClusters = append(activeVMClusters, vmc)
				}
				if err := createOrUpdateVMAuthLB(ctx, rclient, cr, activeVMClusters); err != nil {
					return fmt.Errorf("failed to update vmauth lb with excluded vmcluster %s: %w", vmClusterObj.Name, err)
				}
				if cr.Spec.VMAuth.Name != "" {
					vmAuth := &vmv1beta1.VMAuth{
						ObjectMeta: metav1.ObjectMeta{
							Name:      cr.Spec.VMAuth.Name,
							Namespace: cr.Namespace,
						},
					}
					if err := WaitForVMAuthReady(ctx, rclient, vmAuth, cr.Spec.ReadyDeadline); err != nil {
						return fmt.Errorf("failed to wait for vmauth ready: %w", err)
					}
				}
			}

			logger.WithContext(ctx).Info("Updating VMCluster", "index", i, "name", vmClusterObj.Name)
			// Apply the updated object
			if err := rclient.Update(ctx, vmClusterObj); err != nil {
				return fmt.Errorf("failed to update vmcluster %s at index %d after applying override spec: %w", vmClusterObj.Name, i, err)
			}
		}

		// Wait for VMCluster to be ready
		logger.WithContext(ctx).Info("Waiting for VMCluster to become operational", "index", i, "name", vmClusterObj.Name)
		if err := waitForVMClusterReady(ctx, rclient, vmClusterObj, vmclusterWaitReadyDeadline); err != nil {
			return fmt.Errorf("failed to wait for VMCluster %s/%s to be ready: %w", vmClusterObj.Namespace, vmClusterObj.Name, err)
		}

		// Sleep for zoneUpdatePause time between VMClusters updates (unless its the last one)
		if i != len(vmClusters)-1 {
			logger.WithContext(ctx).Info("Sleeping between zone updates", "index", i, "name", vmClusterObj.Name, "zoneUpdatePause", zoneUpdatePause)
			time.Sleep(zoneUpdatePause)
		}

		// Wait for VMAgent metrics to show no pending queue
		logger.WithContext(ctx).Info("Fetching VMAgent metrics", "index", i, "name", vmClusterObj.Name, "timeout", vmAgentFlushDeadlineDeadline)
		for _, vmAgentObj := range vmAgentObjs {
			if err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgentObj, vmAgentFlushDeadlineDeadline, defaultVMAgentCheckInterval, rclient); err != nil {
				// Ignore this error when running e2e tests - these need to run in the same network as pods
				if os.Getenv("E2E_TEST") != "true" {
					return fmt.Errorf("failed to wait for VMAgent %s metrics to show no pending queue: %w", vmAgentObj.Name, err)
				}
			}
		}

		logger.WithContext(ctx).Info("Re-enabling VMCluster in vmauth", "index", i, "name", vmClusterObj.Name)
		if err := createOrUpdateVMAuthLB(ctx, rclient, cr, vmClusters); err != nil {
			return fmt.Errorf("failed to update vmauth lb with included vmcluster %s: %w", vmClusterObj.Name, err)
		}
		if cr.Spec.VMAuth.Name != "" {
			vmAuth := &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cr.Spec.VMAuth.Name,
					Namespace: cr.Namespace,
				},
			}
			if err := WaitForVMAuthReady(ctx, rclient, vmAuth, cr.Spec.ReadyDeadline); err != nil {
				return fmt.Errorf("failed to wait for vmauth ready: %w", err)
			}
		}

	}
	logger.WithContext(ctx).Info("Reconciliation completed", "name", cr.Name)
	return nil
}
