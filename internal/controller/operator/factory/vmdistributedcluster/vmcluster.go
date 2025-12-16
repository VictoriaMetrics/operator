package vmdistributedcluster

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// getReferencedVMCluster fetches an existing VMCluster based on its reference.
func getReferencedVMCluster(ctx context.Context, rclient client.Client, namespace string, ref *corev1.LocalObjectReference) (*vmv1beta1.VMCluster, error) {
	vmClusterObj := &vmv1beta1.VMCluster{}
	namespacedName := types.NamespacedName{Name: ref.Name, Namespace: namespace}
	if err := rclient.Get(ctx, namespacedName, vmClusterObj); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("referenced VMCluster %s/%s not found: %w", namespace, ref.Name, err)
		}
		return nil, fmt.Errorf("failed to get referenced VMCluster %s/%s: %w", namespace, ref.Name, err)
	}
	return vmClusterObj, nil
}

// fetchVMClusters ensures that referenced VMClusters are fetched and validated.
func fetchVMClusters(ctx context.Context, rclient client.Client, namespace string, refs []vmv1alpha1.VMClusterRefOrSpec) ([]*vmv1beta1.VMCluster, error) {
	var err error
	vmClusters := make([]*vmv1beta1.VMCluster, len(refs))
	for i, vmClusterObjOrRef := range refs {
		switch {
		case vmClusterObjOrRef.Ref != nil:
			vmClusters[i], err = getReferencedVMCluster(ctx, rclient, namespace, vmClusterObjOrRef.Ref)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch vmclusters: %w", err)
			}
		case vmClusterObjOrRef.Spec != nil:
			// Create an in-memory VMCluster object, it will be reconciled in the main loop.
			vmClusters[i] = &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vmClusterObjOrRef.Name,
					Namespace: namespace,
				},
				Spec: *vmClusterObjOrRef.Spec.DeepCopy(),
			}
			// We try to fetch the object to get the current state (Generation, etc)
			if err := rclient.Get(ctx, types.NamespacedName{Name: vmClusterObjOrRef.Name, Namespace: namespace}, vmClusters[i]); err != nil {
				if !k8serrors.IsNotFound(err) {
					return nil, fmt.Errorf("failed to fetch vmcluster %s: %w", vmClusterObjOrRef.Name, err)
				}
			}
		default:
			return nil, fmt.Errorf("invalid VMClusterRefOrSpec at index %d: neither Ref nor Spec is set", i)
		}
	}

	// Sort VMClusters by observedGeneration in descending order (biggest first)
	sort.Slice(vmClusters, func(i, j int) bool {
		return vmClusters[i].Status.ObservedGeneration > vmClusters[j].Status.ObservedGeneration
	})

	return vmClusters, nil
}

// ApplyOverrideSpec merges an override VMClusterSpec into a base VMClusterSpec.
// Fields present in the overrideSpec (and not nil) will overwrite corresponding fields in the baseSpec.
func ApplyOverrideSpec(baseSpec vmv1beta1.VMClusterSpec, overrideSpec *apiextensionsv1.JSON) (vmv1beta1.VMClusterSpec, bool, error) {
	if overrideSpec == nil || overrideSpec.Raw == nil || len(overrideSpec.Raw) == 0 {
		return baseSpec, false, nil
	}

	baseSpecJSON, err := json.Marshal(baseSpec)
	if err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to marshal base VMClusterSpec: %w", err)
	}

	var baseMap map[string]interface{}
	if err := json.Unmarshal(baseSpecJSON, &baseMap); err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to unmarshal base VMClusterSpec to map: %w", err)
	}
	var overrideMap map[string]interface{}
	if err := json.Unmarshal(overrideSpec.Raw, &overrideMap); err != nil {
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
// If an override value is nil, it explicitly clears the corresponding field in baseMap.
// It returns a boolean indicating if the baseMap was modified.
func mergeMapsRecursive(baseMap, overrideMap map[string]interface{}) bool {
	modified := false
	for key, overrideValue := range overrideMap {
		if overrideValue == nil {
			if _, ok := baseMap[key]; ok {
				// If override explicitly sets a field to nil and it existed in base, delete it.
				delete(baseMap, key)
				modified = true
			}
			continue
		}

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

// mergeVMClusterSpecs merges the zoneSpec into baseSpec and returns the merged result
// along with a boolean indicating whether any modifications were made.
func mergeVMClusterSpecs(baseSpec, zoneSpec vmv1beta1.VMClusterSpec) (vmv1beta1.VMClusterSpec, bool, error) {
	baseSpecJSON, err := json.Marshal(baseSpec)
	if err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to marshal base VMClusterSpec: %w", err)
	}

	zoneSpecJSON, err := json.Marshal(zoneSpec)
	if err != nil {
		return vmv1beta1.VMClusterSpec{}, false, fmt.Errorf("failed to marshal zone VMClusterSpec: %w", err)
	}

	var baseMap map[string]interface{}
	if err := json.Unmarshal(baseSpecJSON, &baseMap); err != nil {
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

// waitForVMClusterReady polls VMCluster until it reports UpdateStatusOperational or deadline is hit.
func waitForVMClusterReady(ctx context.Context, rclient client.Client, vmCluster *vmv1beta1.VMCluster, deadline time.Duration) error {
	var lastStatus vmv1beta1.UpdateStatus
	// Fetch VMCluster in a loop until it has UpdateStatusOperational status
	err := wait.PollUntilContextTimeout(ctx, time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		if err := rclient.Get(ctx, types.NamespacedName{Name: vmCluster.Name, Namespace: vmCluster.Namespace}, vmCluster); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, fmt.Errorf("VMCluster not found")
			}
			return false, fmt.Errorf("failed to fetch VMCluster %s/%s: %w", vmCluster.Namespace, vmCluster.Name, err)
		}
		lastStatus = vmCluster.Status.UpdateStatus
		return vmCluster.GetGeneration() == vmCluster.Status.ObservedGeneration && vmCluster.Status.UpdateStatus == vmv1beta1.UpdateStatusOperational, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for VMCluster %s/%s to be ready: %w, current status: %s", vmCluster.Namespace, vmCluster.Name, err, lastStatus)
	}

	return nil
}

// setOwnerRefIfNeeded ensures obj has VMDistributedCluster owner reference set.
func setOwnerRefIfNeeded(cr *vmv1alpha1.VMDistributedCluster, obj client.Object, scheme *runtime.Scheme) (bool, error) {
	if ok, err := controllerutil.HasOwnerReference(obj.GetOwnerReferences(), cr, scheme); err != nil {
		return false, fmt.Errorf("failed to check owner reference for %s %s: %w", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), err)
	} else if !ok {
		// Set owner reference for the VMCluster to the VMDistributedCluster
		if err := controllerutil.SetOwnerReference(cr, obj, scheme); err != nil {
			return false, fmt.Errorf("failed to set owner reference for %s %s: %w", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), err)
		}
		return true, nil
	}
	return false, nil
}

// ensureNoVMClusterOwners validates that vmClusterObj has no owner references other than cr.
func ensureNoVMClusterOwners(cr *vmv1alpha1.VMDistributedCluster, vmClusterObj *vmv1beta1.VMCluster) error {
	for _, owner := range vmClusterObj.GetOwnerReferences() {
		isCROwner := owner.APIVersion == cr.APIVersion &&
			owner.Kind == cr.Kind &&
			owner.Name == cr.GetName()

		if !isCROwner {
			return fmt.Errorf("vmcluster %s has unexpected owner reference: %s/%s/%s", vmClusterObj.Name, owner.APIVersion, owner.Kind, owner.Name)
		}
	}
	return nil
}
