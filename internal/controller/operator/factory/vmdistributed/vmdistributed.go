package vmdistributed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"time"

	"github.com/google/go-cmp/cmp"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

const (
	zonePlaceholder = "%ZONE%"
)

var (
	defaultVMClusterWaitReadyDeadline   = metav1.Duration{Duration: 5 * time.Minute}
	defaultVMAgentCheckInterval         = 30 * time.Second
	defaultVMAgentFlushDeadlineDeadline = metav1.Duration{Duration: 1 * time.Minute}
	defaultZoneUpdatePause              = metav1.Duration{Duration: 1 * time.Minute}
)

// CreateOrUpdate handles VM deployment reconciliation.
func CreateOrUpdate(ctx context.Context, cr *vmv1alpha1.VMDistributed, rclient client.Client, httpTimeout time.Duration) error {
	// No actions performed if CR is paused
	if cr.Paused() {
		return nil
	}

	if !build.MustSkipRuntimeValidation {
		if err := cr.Validate(); err != nil {
			return err
		}
	}

	logger.WithContext(ctx).Info("Starting reconciliation", "name", cr.Name)

	// Fetch VMCluster statuses by name (needed to build target refs VMAuth).
	vmClusters, err := fetchVMClusters(ctx, rclient, cr)
	if err != nil {
		return fmt.Errorf("failed to fetch vmclusters: %w", err)
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
		vmAgentObjs, err = listVMAgents(ctx, rclient, cr.Namespace, cr.Spec.VMAgent.LabelSelector)
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

	logger.WithContext(ctx).Info("Waiting for all VMClusters to be ready")
	for _, vmClusterObj := range vmClusters {
		if err := rclient.Get(ctx, types.NamespacedName{Name: vmClusterObj.Name, Namespace: vmClusterObj.Namespace}, vmClusterObj); k8serrors.IsNotFound(err) {
			continue
		}
		if err := waitForVMClusterReady(ctx, rclient, vmClusterObj, vmclusterWaitReadyDeadline); err != nil {
			return fmt.Errorf("failed to wait for VMCluster %s/%s to be ready: %w", vmClusterObj.Namespace, vmClusterObj.Name, err)
		}
	}
	logger.WithContext(ctx).Info("All VMClusters are ready")

	// Apply changes to VMClusters one by one if new spec needs to be applied
	logger.WithContext(ctx).Info("Reconciling VMClusters")
	for i, vmClusterObj := range vmClusters {
		zone := &cr.Spec.Zones[i]
		logger.WithContext(ctx).Info("Reconciling VMCluster", "index", i, "name", vmClusterObj.Name)

		needsToBeCreated := false
		// Get vmClusterObj in case it doesn't exist or has changed
		nsn := types.NamespacedName{Name: vmClusterObj.Name, Namespace: vmClusterObj.Namespace}
		if err = rclient.Get(ctx, nsn, vmClusterObj); err != nil {
			if !k8serrors.IsNotFound(err) {
				return fmt.Errorf("unexpected error during attempt to get spec.zones[%d].vmclusters object: %w", i, err)
			}
			needsToBeCreated = true
		}

		// Update the VMCluster when overrideSpec needs to be applied or ownerref set
		mergedSpec := vmClusterObj.Spec.DeepCopy()
		previousVMClusterObjSpec := mergedSpec.DeepCopy()
		var updatedSpec bool

		// Apply CommonZone if it is set
		if cr.Spec.CommonZone.VMCluster != nil && cr.Spec.CommonZone.VMCluster.Spec != nil {
			commonSpec := cr.Spec.CommonZone.VMCluster.Spec.DeepCopy()
			if len(zone.Name) > 0 {
				commonSpec, err = k8stools.RenderPlaceholders(commonSpec, map[string]string{
					zonePlaceholder: zone.Name,
				})
				if err != nil {
					return fmt.Errorf("failed to render common cluster: %w", err)
				}
			}
			if updated, err := mergeDeep(mergedSpec, commonSpec); err != nil {
				return fmt.Errorf("failed to apply common zone spec for vmcluster %s at index %d: %w", vmClusterObj.Name, i, err)
			} else if updated {
				updatedSpec = updated
			}
		}

		// Apply cluster-specific override if it exist
		if zone.VMCluster != nil && zone.VMCluster.Spec != nil {
			if updated, err := mergeDeep(mergedSpec, zone.VMCluster.Spec); err != nil {
				return fmt.Errorf("failed to merge spec for vmcluster %s at index %d: %w", vmClusterObj.Name, i, err)
			} else if updated {
				updatedSpec = updated
			}
		}
		diff := cmp.Diff(previousVMClusterObjSpec, mergedSpec)
		if diff != "" {
			logger.WithContext(ctx).Info("zone vmcluster diff", "diff", diff, "updatedSpec", updatedSpec, "index", i, "name", vmClusterObj.Name)
			logger.WithContext(ctx).Info(diff)
		}

		// Set owner reference for this vmcluster
		modifiedOwnerRef, err := setOwnerRefIfNeeded(cr, vmClusterObj, rclient.Scheme())
		if err != nil {
			return fmt.Errorf("failed to set owner reference for vmcluster %s: %w", vmClusterObj.Name, err)
		}
		if !needsToBeCreated && !updatedSpec && !modifiedOwnerRef {
			// No changes required
			continue
		}

		vmClusterObj.Spec = *mergedSpec

		if needsToBeCreated {
			logger.WithContext(ctx).Info("Creating VMCluster", "index", i, "name", vmClusterObj.Name)
			if err := rclient.Create(ctx, vmClusterObj); err != nil {
				return fmt.Errorf("failed to create vmcluster %s at index %d after applying override spec: %w", vmClusterObj.Name, i, err)
			}
		} else {
			// Drain cluster reads only if the spec has been modified
			if updatedSpec {
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
					if err := waitForVMAuthReady(ctx, rclient, vmAuth, cr.Spec.ReadyDeadline); err != nil {
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

		if updatedSpec {
			logger.WithContext(ctx).Info("Waiting for VMCluster to start expanding", "index", i, "name", vmClusterObj.Name)
			if err := waitForVMClusterToExpand(ctx, rclient, vmClusterObj, vmclusterWaitReadyDeadline); err != nil {
				return fmt.Errorf("failed to wait for VMCluster %s/%s to start expanding: %w", vmClusterObj.Namespace, vmClusterObj.Name, err)
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

		if len(vmClusters) > 1 {
			logger.WithContext(ctx).Info("Re-enabling VMCluster in vmauth", "index", i, "name", vmClusterObj.Name)
			if err := createOrUpdateVMAuthLB(ctx, rclient, cr, vmClusters); err != nil {
				return fmt.Errorf("failed to update vmauth lb with included vmcluster %s: %w", vmClusterObj.Name, err)
			}
			vmAuth := &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cr.Spec.VMAuth.Name,
					Namespace: cr.Namespace,
				},
			}
			if err := waitForVMAuthReady(ctx, rclient, vmAuth, cr.Spec.ReadyDeadline); err != nil {
				return fmt.Errorf("failed to wait for vmauth ready: %w", err)
			}
		}

	}
	logger.WithContext(ctx).Info("Reconciliation completed", "name", cr.Name)
	return nil
}

// setOwnerRefIfNeeded ensures obj has VMDistributed owner reference set.
func setOwnerRefIfNeeded(cr *vmv1alpha1.VMDistributed, obj client.Object, scheme *runtime.Scheme) (bool, error) {
	if ok, err := controllerutil.HasOwnerReference(obj.GetOwnerReferences(), cr, scheme); err != nil {
		return false, fmt.Errorf("failed to check owner reference for %T=%s/%s: %w", obj, obj.GetNamespace(), obj.GetName(), err)
	} else if !ok {
		if err := controllerutil.SetOwnerReference(cr, obj, scheme); err != nil {
			return false, fmt.Errorf("failed to set owner reference for %T=%s/%s: %w", obj, obj.GetNamespace(), obj.GetName(), err)
		}
		return true, nil
	}
	return false, nil
}

// mergeDeep merges an override object into a base one.
// Fields present in the override will overwrite corresponding fields in the base.
func mergeDeep[T any](base, override T) (bool, error) {
	if any(override) == nil {
		return false, nil
	}

	baseJSON, err := json.Marshal(base)
	if err != nil {
		return false, fmt.Errorf("failed to marshal base spec: %w", err)
	}
	overrideJSON, err := json.Marshal(override)
	if err != nil {
		return false, fmt.Errorf("failed to marshal override spec: %w", err)
	}

	var baseMap map[string]any
	if err := json.Unmarshal(baseJSON, &baseMap); err != nil {
		return false, fmt.Errorf("failed to unmarshal base spec to map: %w", err)
	}
	var overrideMap map[string]any
	if err := json.Unmarshal(overrideJSON, &overrideMap); err != nil {
		return false, fmt.Errorf("failed to unmarshal override spec to map: %w", err)
	}

	// Perform a deep merge: fields from overrideMap recursively overwrite corresponding fields in baseMap.
	// If an override value is explicitly nil, it signifies the removal or nullification of that field.
	updated := mergeMapsRecursive(baseMap, overrideMap)
	mergedSpecJSON, err := json.Marshal(baseMap)
	if err != nil {
		return false, fmt.Errorf("failed to marshal merged spec map: %w", err)
	}

	if err := json.Unmarshal(mergedSpecJSON, base); err != nil {
		return false, fmt.Errorf("failed to unmarshal merged spec JSON: %w", err)
	}

	return updated, nil
}

// mergeMapsRecursive deeply merges overrideMap into baseMap.
// It handles nested maps (which correspond to nested structs after JSON unmarshal).
// Values from overrideMap overwrite values in baseMap.
// It returns a boolean indicating if the baseMap was modified.
func mergeMapsRecursive(baseMap, overrideMap map[string]any) bool {
	var modified bool
	if baseMap == nil && len(overrideMap) > 0 {
		baseMap = make(map[string]any)
	}
	for key, overrideValue := range overrideMap {
		if baseVal, ok := baseMap[key]; ok {
			if baseMapNested, isBaseMap := baseVal.(map[string]any); isBaseMap {
				if overrideMapNested, isOverrideMap := overrideValue.(map[string]any); isOverrideMap {
					// Both are nested maps, recurse
					if mergeMapsRecursive(baseMapNested, overrideMapNested) {
						modified = true
					}
					continue
				}
			}
		}

		// For all other cases (scalar values, or when types for nested maps don't match),
		// override the baseMap value. This handles explicit zero values and ensures
		// overrides take precedence.
		// We assign first, then check if it was a modification.
		oldValue, exists := baseMap[key]
		baseMap[key] = overrideValue // Force the overwrite for this key
		if !exists || !reflect.DeepEqual(oldValue, overrideValue) {
			modified = true
		}
	}
	return modified
}
