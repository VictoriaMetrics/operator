package vmdistributed

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// getReferencedVMCluster fetches an existing VMCluster based on its reference.
func getReferencedVMCluster(ctx context.Context, rclient client.Client, namespace string, ref *corev1.LocalObjectReference) (*vmv1beta1.VMCluster, error) {
	vmClusterObj := &vmv1beta1.VMCluster{}
	nsn := types.NamespacedName{Name: ref.Name, Namespace: namespace}
	if err := rclient.Get(ctx, nsn, vmClusterObj); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("referenced VMCluster %s not found: %w", nsn.String(), err)
		}
		return nil, fmt.Errorf("failed to get referenced VMCluster %s: %w", nsn.String(), err)
	}
	return vmClusterObj, nil
}

// fetchVMClusters ensures that referenced VMClusters are fetched and validated.
func fetchVMClusters(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed) ([]*vmv1beta1.VMCluster, error) {
	logger.WithContext(ctx).Info("Fetching VMClusters")

	objOrRefs := cr.Spec.Zones.VMClusters
	vmClusters := make([]*vmv1beta1.VMCluster, len(objOrRefs))
	for i, objOrRef := range objOrRefs {
		vmCluster := &vmv1beta1.VMCluster{}
		switch {
		case objOrRef.Ref != nil && len(objOrRef.Ref.Name) > 0:
			nsn := types.NamespacedName{Name: objOrRef.Ref.Name, Namespace: cr.Namespace}
			if err := rclient.Get(ctx, nsn, vmCluster); err != nil {
				if k8serrors.IsNotFound(err) {
					return nil, fmt.Errorf("referenced vmclusters[%d]=%s not found: %w", i, nsn.String(), err)
				}
				return nil, fmt.Errorf("failed to get referenced vmclusters[%d]=%s: %w", i, nsn.String(), err)
			}
		case len(objOrRef.Name) > 0:
			// We try to fetch the object to get the current state (Generation, etc)
			nsn := types.NamespacedName{Name: objOrRef.Name, Namespace: cr.Namespace}
			if err := rclient.Get(ctx, nsn, vmCluster); err != nil {
				if k8serrors.IsNotFound(err) {
					vmCluster.Name = objOrRef.Name
					vmCluster.Namespace = cr.Namespace
					vmCluster.Spec = *objOrRef.Spec.DeepCopy()
				} else {
					return nil, fmt.Errorf("unexpected error while fetching vmclusters[%d]=%s: %w", i, nsn.String(), err)
				}
			}
		default:
			return nil, fmt.Errorf("invalid VMClusterObjOrRef at index %d: neither Ref nor Name is set", i)
		}
		if err := cr.Owns(vmCluster); err != nil {
			return nil, fmt.Errorf("failed to validate owner references for unreferenced vmcluster %s: %w", vmCluster.Name, err)
		}
		vmClusters[i] = vmCluster
	}

	// Sort VMClusters by observedGeneration in descending order (biggest first)
	sort.Slice(vmClusters, func(i, j int) bool {
		return vmClusters[i].Status.ObservedGeneration > vmClusters[j].Status.ObservedGeneration
	})

	logger.WithContext(ctx).Info(fmt.Sprintf("Found %d VMClusters", len(vmClusters)))
	return vmClusters, nil
}

// applyOverrideSpec merges an override VMClusterSpec into a base VMClusterSpec.
// Fields present in the override will overwrite corresponding fields in the base.
func applyOverrideSpec(base vmv1beta1.VMClusterSpec, override *apiextensionsv1.JSON) (vmv1beta1.VMClusterSpec, bool, error) {
	if override == nil || override.Raw == nil || len(override.Raw) == 0 {
		return base, false, nil
	}

	baseJSON, err := json.Marshal(base)
	if err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to marshal base VMClusterSpec: %w", err)
	}

	var baseMap map[string]interface{}
	if err := json.Unmarshal(baseJSON, &baseMap); err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to unmarshal base VMClusterSpec to map: %w", err)
	}
	var overrideMap map[string]interface{}
	if err := json.Unmarshal(override.Raw, &overrideMap); err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to unmarshal override VMClusterSpec to map: %w", err)
	}

	// Perform a deep merge: fields from overrideMap recursively overwrite corresponding fields in baseMap.
	// If an override value is explicitly nil, it signifies the removal or nullification of that field.
	modified := mergeMapsRecursive(baseMap, overrideMap)

	mergedSpecJSON, err := json.Marshal(baseMap)
	if err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to marshal merged VMClusterSpec map: %w", err)
	}

	mergedSpec := vmv1beta1.VMClusterSpec{}
	if err := json.Unmarshal(mergedSpecJSON, &mergedSpec); err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to unmarshal merged VMClusterSpec JSON: %w", err)
	}

	return mergedSpec, modified, nil
}

// mergeMapsRecursive deeply merges overrideMap into baseMap.
// It handles nested maps (which correspond to nested structs after JSON unmarshal).
// Values from overrideMap overwrite values in baseMap.
// It returns a boolean indicating if the baseMap was modified.
func mergeMapsRecursive(baseMap, overrideMap map[string]interface{}) bool {
	modified := false
	if baseMap == nil && len(overrideMap) > 0 {
		baseMap = make(map[string]any)
	}
	for key, overrideValue := range overrideMap {
		if baseVal, ok := baseMap[key]; ok {
			if baseMapNested, isBaseMap := baseVal.(map[string]interface{}); isBaseMap {
				if overrideMapNested, isOverrideMap := overrideValue.(map[string]interface{}); isOverrideMap {
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

// mergeVMClusterSpecs merges the zoneSpec into base and returns the merged result
// along with a boolean indicating whether any modifications were made.
func mergeVMClusterSpecs(base, zoneSpec vmv1beta1.VMClusterSpec) (vmv1beta1.VMClusterSpec, bool, error) {
	baseJSON, err := json.Marshal(base)
	if err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to marshal base VMClusterSpec: %w", err)
	}

	zoneSpecJSON, err := json.Marshal(zoneSpec)
	if err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to marshal zone VMClusterSpec: %w", err)
	}

	var baseMap map[string]interface{}
	if err := json.Unmarshal(baseJSON, &baseMap); err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to unmarshal base VMClusterSpec to map: %w", err)
	}

	var zoneMap map[string]interface{}
	if err := json.Unmarshal(zoneSpecJSON, &zoneMap); err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to unmarshal zone VMClusterSpec to map: %w", err)
	}

	// Perform a deep merge: fields from zoneMap recursively overwrite corresponding fields in baseMap.
	modified := mergeMapsRecursive(baseMap, zoneMap)

	mergedSpecJSON, err := json.Marshal(baseMap)
	if err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to marshal merged VMClusterSpec map: %w", err)
	}

	mergedSpec := vmv1beta1.VMClusterSpec{}
	if err := json.Unmarshal(mergedSpecJSON, &mergedSpec); err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to unmarshal merged VMClusterSpec JSON: %w", err)
	}

	return mergedSpec, modified, nil
}

func waitForVMClusterToReachStatus(ctx context.Context, rclient client.Client, vmCluster *vmv1beta1.VMCluster, deadline time.Duration, status vmv1beta1.UpdateStatus) error {
	var lastStatus vmv1beta1.UpdateStatus
	// Fetch VMCluster in a loop until it has UpdateStatusOperational status
	nsn := types.NamespacedName{Name: vmCluster.Name, Namespace: vmCluster.Namespace}
	err := wait.PollUntilContextTimeout(ctx, time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		if err := rclient.Get(ctx, nsn, vmCluster); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, nil
		}
		lastStatus = vmCluster.Status.UpdateStatus
		return vmCluster.GetGeneration() == vmCluster.Status.ObservedGeneration && vmCluster.Status.UpdateStatus == status, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for VMCluster %s to be ready: %w, current status: %s", nsn.String(), err, lastStatus)
	}

	return nil
}

func waitForVMClusterReady(ctx context.Context, rclient client.Client, vmCluster *vmv1beta1.VMCluster, deadline time.Duration) error {
	return waitForVMClusterToReachStatus(ctx, rclient, vmCluster, deadline, vmv1beta1.UpdateStatusOperational)
}

func waitForVMClusterToExpand(ctx context.Context, rclient client.Client, vmCluster *vmv1beta1.VMCluster, deadline time.Duration) error {
	return waitForVMClusterToReachStatus(ctx, rclient, vmCluster, deadline, vmv1beta1.UpdateStatusExpanding)
}
