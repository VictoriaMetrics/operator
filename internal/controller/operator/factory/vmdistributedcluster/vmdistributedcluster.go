package vmdistributedcluster

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

var (
	defaultVMClusterWaitReadyDeadline = metav1.Duration{Duration: 5 * time.Minute}
	defaultZoneUpdatePause            = metav1.Duration{Duration: 1 * time.Minute}
)

// CreateOrUpdate handles VM deployment reconciliation.
func CreateOrUpdate(ctx context.Context, cr *vmv1alpha1.VMDistributedCluster, rclient client.Client, scheme *runtime.Scheme, httpTimeout time.Duration) error {
	// Store the previous CR for comparison
	var prevCR *vmv1alpha1.VMDistributedCluster
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}

	// No actions performed if CR is paused
	if cr.Paused() {
		return nil
	}

	// Validate zones, exit early if invalid
	for i, zone := range cr.Spec.Zones.VMClusters {
		if zone.Spec != nil && zone.Name == "" {
			return fmt.Errorf("VMClusterRefOrSpec.Name must be set when Spec is provided for zone at index %d", i)
		}
		if zone.Spec == nil && zone.Ref == nil {
			return fmt.Errorf("VMClusterRefOrSpec.Spec or VMClusterRefOrSpec.Ref must be set for zone at index %d", i)
		}
		if zone.Spec != nil && zone.Ref != nil {
			return fmt.Errorf("either VMClusterRefOrSpec.Spec or VMClusterRefOrSpec.Ref must be set for zone at index %d", i)
		}
	}

	// Validate VMAgent
	if cr.Spec.VMAgent.Name == "" {
		return fmt.Errorf("VMAgent.Name must be set")
	}

	// Validate VMAuth
	if cr.Spec.VMAuth.Name == "" {
		return fmt.Errorf("VMAuth.Name must be set")
	}

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

	// Ensure VMAuth exists first so we can set it as the owner for the automatically-created VMUser.
	err = createOrUpdateVMAuthLB(ctx, rclient, cr, prevCR, vmClusters)
	if err != nil {
		return fmt.Errorf("failed to update or create vmauth: %w", err)
	}

	// Store current CR status
	previousCRStatus := cr.Status.DeepCopy()
	cr.Status = vmv1alpha1.VMDistributedClusterStatus{}

	// Record vmcluster info in VMDistributedCluster status
	cr.Status.VMClusterInfo = make([]vmv1alpha1.VMClusterStatus, len(vmClusters))
	for i, vmCluster := range vmClusters {
		cr.Status.VMClusterInfo[i] = vmv1alpha1.VMClusterStatus{
			VMClusterName: vmCluster.Name,
			Generation:    vmCluster.Generation,
		}
	}
	cr.Status.Zones.VMClusters = cr.Spec.Zones.VMClusters

	// Compare generations of vmcluster objects from the spec with the previous CR and Zones configuration
	previousGenerations := getGenerationsFromStatus(previousCRStatus)
	currentGenerations := getGenerationsFromStatus(&cr.Status)
	if len(previousGenerations) != len(currentGenerations) {
		// Set status to expanding until all clusters have reported their status
		cr.Status.UpdateStatus = vmv1beta1.UpdateStatusExpanding
	}

	// if diff := deep.Equal(previousGenerations, currentGenerations); len(diff) > 0 {
	// 	// Record new generations and zones config, then exit early if a change is detected
	// 	if err := rclient.Status().Update(ctx, cr); err != nil {
	// 		return fmt.Errorf("failed to update status: %w", err)
	// 	}
	// 	return fmt.Errorf("unexpected generations or zones config change detected: %v", diff)
	// }

	// Update or create the VMAgent
	vmAgentObj, err := updateOrCreateVMAgent(ctx, rclient, cr, scheme, vmClusters)
	if err != nil {
		return fmt.Errorf("failed to update or create VMAgent: %w", err)
	}

	// Setup deadlines and timeouts
	vmclusterWaitReadyDeadline := defaultVMClusterWaitReadyDeadline.Duration
	if cr.Spec.ReadyDeadline != nil {
		vmclusterWaitReadyDeadline = cr.Spec.ReadyDeadline.Duration
	}
	zoneUpdatePause := defaultZoneUpdatePause.Duration
	if cr.Spec.ZoneUpdatePause != nil {
		zoneUpdatePause = cr.Spec.ZoneUpdatePause.Duration
	}

	// Setup custom HTTP client
	httpClient := &http.Client{
		Timeout: httpTimeout,
	}

	// Apply changes to VMClusters one by one if new spec needs to be applied
	for i, vmClusterObj := range vmClusters {
		zoneRefOrSpec := cr.Spec.Zones.VMClusters[i]

		needsToBeCreated := false
		// Get vmClusterObj in case it doesn't exist or has changed
		if err = rclient.Get(ctx, types.NamespacedName{Name: vmClusterObj.Name, Namespace: vmClusterObj.Namespace}, vmClusterObj); k8serrors.IsNotFound(err) {
			needsToBeCreated = true
		}

		// Update the VMCluster when overrideSpec needs to be applied or ownerref set
		var mergedSpec vmv1beta1.VMClusterSpec
		var modifiedSpec bool

		if zoneRefOrSpec.Ref != nil {
			mergedSpec, modifiedSpec, err = ApplyOverrideSpec(vmClusterObj.Spec, zoneRefOrSpec.OverrideSpec)
			if err != nil {
				return fmt.Errorf("failed to apply override spec for vmcluster %s at index %d: %w", vmClusterObj.Name, i, err)
			}
		}
		if zoneRefOrSpec.Spec != nil {
			mergedSpec, modifiedSpec, err = mergeVMClusterSpecs(vmClusterObj.Spec, *zoneRefOrSpec.Spec)
			if err != nil {
				return fmt.Errorf("failed to merge spec for vmcluster %s at index %d: %w", vmClusterObj.Name, i, err)
			}
		}

		// Set owner reference for this vmcluster
		modifiedOwnerRef, err := setOwnerRefIfNeeded(cr, vmClusterObj, scheme)
		if err != nil {
			return fmt.Errorf("failed to set owner reference for vmcluster %s: %w", vmClusterObj.Name, err)
		}
		if !needsToBeCreated && !modifiedSpec && !modifiedOwnerRef {
			// No changes required
			continue
		}

		vmClusterObj.Spec = mergedSpec

		if needsToBeCreated {
			if err := rclient.Create(ctx, vmClusterObj); err != nil {
				return fmt.Errorf("failed to create vmcluster %s at index %d after applying override spec: %w", vmClusterObj.Name, i, err)
			}
			cr.Status.VMClusterInfo[i].Generation = vmClusterObj.Generation
			if err := createOrUpdateVMAuthLB(ctx, rclient, cr, prevCR, vmClusters); err != nil {
				return fmt.Errorf("failed to update vmauth lb with included vmcluster %s: %w", vmClusterObj.Name, err)
			}
			// No further action needed after creation
			continue
		}

		// Update vmauth lb with excluded cluster
		activeVMClusters := make([]*vmv1beta1.VMCluster, 0, len(vmClusters)-1)
		for _, vmc := range vmClusters {
			if vmc.Name == vmClusterObj.Name {
				continue
			}
			activeVMClusters = append(activeVMClusters, vmc)
		}
		if err := createOrUpdateVMAuthLB(ctx, rclient, cr, prevCR, activeVMClusters); err != nil {
			return fmt.Errorf("failed to update vmauth lb with excluded vmcluster %s: %w", vmClusterObj.Name, err)
		}

		// Apply the updated object
		if err := rclient.Update(ctx, vmClusterObj); err != nil {
			return fmt.Errorf("failed to update vmcluster %s at index %d after applying override spec: %w", vmClusterObj.Name, i, err)
		}
		cr.Status.VMClusterInfo[i].Generation = vmClusterObj.Generation

		// Wait for VMCluster to be ready
		if err := waitForVMClusterReady(ctx, rclient, vmClusterObj, vmclusterWaitReadyDeadline); err != nil {
			return fmt.Errorf("failed to wait for VMCluster %s/%s to be ready: %w", vmClusterObj.Namespace, vmClusterObj.Name, err)
		}

		// Record new vmcluster generations
		// if err := rclient.Status().Update(ctx, cr); err != nil {
		// 	return fmt.Errorf("failed to update status: %w", err)
		// }

		// Wait for VMAgent metrics to show no pending queue
		if err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgentObj, vmclusterWaitReadyDeadline, rclient); err != nil {
			// Ignore this error when running e2e tests - these need to run in the same network as pods
			if os.Getenv("E2E_TEST") != "true" {
				return fmt.Errorf("failed to wait for VMAgent metrics to show no pending queue: %w", err)
			}
		}

		if err := createOrUpdateVMAuthLB(ctx, rclient, cr, prevCR, vmClusters); err != nil {
			return fmt.Errorf("failed to update vmauth lb with included vmcluster %s: %w", vmClusterObj.Name, err)
		}

		// Sleep for zoneUpdatePause time between VMClusters updates
		time.Sleep(zoneUpdatePause)
	}
	return nil
}
