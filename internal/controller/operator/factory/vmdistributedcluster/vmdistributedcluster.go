/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vmdistributedcluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
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

const (
	VMAgentQueueMetricName = "vm_persistentqueue_bytes_pending"
)

// CreateOrUpdate handles VM deployment reconciliation.
func CreateOrUpdate(ctx context.Context, cr *vmv1alpha1.VMDistributedCluster, rclient client.Client, scheme *runtime.Scheme, vmclusterWaitReadyDeadline, httpTimeout time.Duration) error {
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
	for i, zone := range cr.Spec.Zones {
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

	// Fetch global vmagent
	vmAgentObj, err := fetchVMAgent(ctx, rclient, cr.Namespace, cr.Spec.VMAgent)
	if err != nil {
		return fmt.Errorf("failed to fetch global vmagent: %w", err)
	}

	// Fetch all vmusers
	vmUserObjs, err := fetchVMUsers(ctx, rclient, cr.Namespace, cr.Spec.VMUsers)
	if err != nil {
		return fmt.Errorf("failed to fetch vmusers: %w", err)
	}

	// Fetch VMCLuster statuses by name
	vmClusters, err := fetchVMClusters(ctx, rclient, cr.Namespace, cr.Spec.Zones)
	if err != nil {
		return fmt.Errorf("failed to fetch vmclusters: %w", err)
	}

	// Store current CR status
	currentCRStatus := cr.Status.DeepCopy()
	cr.Status = vmv1alpha1.VMDistributedClusterStatus{}

	// Ensure that all vmusers have a read rule for vmcluster and record vmcluster info in VMDistributedCluster status
	cr.Status.VMClusterInfo = make([]vmv1alpha1.VMClusterStatus, len(vmClusters))
	for i, vmCluster := range vmClusters {
		vmClusterInfo, err := setVMClusterInfo(vmCluster, vmUserObjs, currentCRStatus)
		if err != nil {
			return err
		}
		cr.Status.VMClusterInfo[i] = vmClusterInfo
	}
	cr.Status.Zones = cr.Spec.Zones

	// Compare generations of vmcluster objects from the spec with the previous CR and Zones configuration
	if diff := deep.Equal(getGenerationsFromStatus(currentCRStatus), getGenerationsFromStatus(&cr.Status)); len(diff) > 0 {
		// Record new generations and zones config, then exit early if a change is detected
		if err := rclient.Status().Update(ctx, cr); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
		return fmt.Errorf("unexpected generations or zones config change detected: %v", diff)
	}

	// TODO[vrutkovs]: Mark all VMClusters as paused?
	// This would prevent other reconciliation loops from changing them

	// Disable VMClusters one by one if overrideSpec needs to be applied
	httpClient := &http.Client{
		Timeout: httpTimeout,
	}
	for i, vmClusterObj := range vmClusters {
		zoneRefOrSpec := cr.Spec.Zones[i]

		needsToBeCreated := false
		// Get vmClusterObj in case it doesn't exist or has changed
		if err = rclient.Get(ctx, types.NamespacedName{Name: vmClusterObj.Name, Namespace: vmClusterObj.Namespace}, vmClusterObj); k8serrors.IsNotFound(err) {
			needsToBeCreated = true
		}

		// Update the VMCluster when overrideSpec needs to be applied or ownerref set
		mergedSpec, modifiedSpec, err := ApplyOverrideSpec(vmClusterObj.Spec, zoneRefOrSpec.OverrideSpec)
		if err != nil {
			return fmt.Errorf("failed to apply override spec for vmcluster %s at index %d: %w", vmClusterObj.Name, i, err)
		}
		modifiedOwnerRef, err := setOwnerRefIfNeeded(cr, vmClusterObj, i, scheme)
		if err != nil {
			return fmt.Errorf("failed to set owner reference for vmcluster %s at index %d: %w", vmClusterObj.Name, i, err)
		}
		if !needsToBeCreated && !modifiedSpec && !modifiedOwnerRef {
			continue
		}

		vmClusterObj.Spec = mergedSpec

		if needsToBeCreated {
			if err := rclient.Create(ctx, vmClusterObj); err != nil {
				return fmt.Errorf("failed to create vmcluster %s at index %d after applying override spec: %w", vmClusterObj.Name, i, err)
			}
			// No further action needed after creation
			continue
		}
		// Apply the updated object
		if err := rclient.Update(ctx, vmClusterObj); err != nil {
			return fmt.Errorf("failed to update vmcluster %s at index %d after applying override spec: %w", vmClusterObj.Name, i, err)
		}

		// Don't switch the cluster off in VMUser unless it was modified
		if !modifiedSpec {
			continue
		}

		// Perform rollout steps for the (now reconciled/updated) VMCluster
		// Disable this VMCluster in vmusers
		if err := setVMClusterStatusInVMUsers(ctx, rclient, cr, vmClusterObj, vmUserObjs, false); err != nil {
			return fmt.Errorf("failed to set VMCluster status in VMUsers: %w", err)
		}
		// Wait for VMCluster to be ready
		if err := waitForVMClusterReady(ctx, rclient, vmClusterObj, vmclusterWaitReadyDeadline); err != nil {
			return fmt.Errorf("failed to wait for VMCluster %s/%s to be ready: %w", vmClusterObj.Namespace, vmClusterObj.Name, err)
		}

		// Wait for VMAgent metrics to show no pending queue
		vmAgent := &vmAgentAdapter{VMAgent: vmAgentObj}
		if err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, vmclusterWaitReadyDeadline); err != nil {
			return fmt.Errorf("failed to wait for VMAgent metrics to show no pending queue: %w", err)
		}

		// Enable this VMCluster in vmusers
		if err := setVMClusterStatusInVMUsers(ctx, rclient, cr, vmClusterObj, vmUserObjs, true); err != nil {
			return fmt.Errorf("failed to set VMCluster status in VMUsers: %w", err)
		}
	}
	return nil
}

// validateVMClusterRefOrSpec checks for mutual exclusivity and required fields for VMClusterRefOrSpec.
func validateVMClusterRefOrSpec(i int, refOrSpec vmv1alpha1.VMClusterRefOrSpec) error {
	if refOrSpec.Spec != nil && refOrSpec.Ref != nil {
		return fmt.Errorf("VMClusterRefOrSpec at index %d must specify either Ref or Spec, got: %+v", i, refOrSpec)
	}
	if refOrSpec.Spec == nil && refOrSpec.Ref == nil {
		return fmt.Errorf("VMClusterRefOrSpec at index %d must have either Ref or Spec set, got: %+v", i, refOrSpec)
	}
	if refOrSpec.Spec != nil && refOrSpec.OverrideSpec != nil {
		return fmt.Errorf("VMClusterRefOrSpec at index %d cannot have both Spec and OverrideSpec set, got: %+v", i, refOrSpec)
	}
	if refOrSpec.Spec != nil && refOrSpec.Name == "" {
		return fmt.Errorf("VMClusterRefOrSpec.Name must be set when Spec is provided for index %d", i)
	}
	if refOrSpec.Ref != nil && refOrSpec.Ref.Name == "" {
		return fmt.Errorf("VMClusterRefOrSpec.Ref.Name must be set for reference at index %d", i)
	}
	return nil
}

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

func fetchVMClusters(ctx context.Context, rclient client.Client, namespace string, refs []vmv1alpha1.VMClusterRefOrSpec) ([]*vmv1beta1.VMCluster, error) {
	var err error
	vmClusters := make([]*vmv1beta1.VMCluster, len(refs))
	for i, vmClusterObjOrRef := range refs {
		if err = validateVMClusterRefOrSpec(i, vmClusterObjOrRef); err != nil {
			return nil, err
		}

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
		default:
			return nil, fmt.Errorf("invalid VMClusterRefOrSpec at index %d: neither Ref nor Spec is set", i)
		}
	}
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

// TODO[vrutkovs]: Pretty sure an existing function can be reused for merging maps.
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

func fetchVMUsers(ctx context.Context, rclient client.Client, namespace string, refs []corev1.LocalObjectReference) ([]*vmv1beta1.VMUser, error) {
	vmUsers := make([]*vmv1beta1.VMUser, len(refs))
	for i, ref := range refs {
		if ref.Name == "" {
			return nil, errors.New("vmuser name is not specified")
		}
		vmUserObj := &vmv1beta1.VMUser{}
		namespacedName := types.NamespacedName{Name: ref.Name, Namespace: namespace}
		if err := rclient.Get(ctx, namespacedName, vmUserObj); err != nil {
			return nil, fmt.Errorf("failed to get VMUser %s/%s: %w", namespace, ref.Name, err)
		}
		vmUsers[i] = vmUserObj
	}
	return vmUsers, nil
}

func setVMClusterInfo(vmCluster *vmv1beta1.VMCluster, vmUserObjs []*vmv1beta1.VMUser, currentCRStatus *vmv1alpha1.VMDistributedClusterStatus) (vmv1alpha1.VMClusterStatus, error) {
	vmClusterInfo := vmv1alpha1.VMClusterStatus{
		VMClusterName: vmCluster.Name,
		Generation:    vmCluster.Generation,
	}
	ref, err := findVMUserReadRuleForVMCluster(vmUserObjs, vmCluster)
	if err != nil {
		// Check if targetref already set in vmcluster status
		// Exit early if targetref is already set
		idx := slices.IndexFunc(currentCRStatus.VMClusterInfo, func(info vmv1alpha1.VMClusterStatus) bool {
			return info.VMClusterName == vmCluster.Name
		})
		if idx == -1 {
			return vmClusterInfo, fmt.Errorf("failed to find the rule for vmcluster %s: %w", vmCluster.Name, err)
		}
		ref = &currentCRStatus.VMClusterInfo[idx].TargetRef
	}
	vmClusterInfo.TargetRef = *ref
	return vmClusterInfo, nil
}

func fetchVMAgent(ctx context.Context, rclient client.Client, namespace string, ref corev1.LocalObjectReference) (*vmv1beta1.VMAgent, error) {
	if ref.Name == "" {
		return nil, errors.New("global vmagent name is not specified")
	}
	vmAgentObj := &vmv1beta1.VMAgent{}
	namespacedName := types.NamespacedName{Name: ref.Name, Namespace: namespace}
	if err := rclient.Get(ctx, namespacedName, vmAgentObj); err != nil {
		return nil, fmt.Errorf("failed to get global VMAgent %s/%s: %w", namespace, ref.Name, err)
	}
	return vmAgentObj, nil
}

func getGenerationsFromStatus(status *vmv1alpha1.VMDistributedClusterStatus) map[string]int64 {
	generations := make(map[string]int64, len(status.VMClusterInfo))
	for _, vmClusterPair := range status.VMClusterInfo {
		generations[vmClusterPair.VMClusterName] = vmClusterPair.Generation
	}
	return generations
}

func findVMUserReadRuleForVMCluster(vmUserObjs []*vmv1beta1.VMUser, vmCluster *vmv1beta1.VMCluster) (*vmv1beta1.TargetRef, error) {
	// Search through all vmusers for a matching rule
	for _, vmUserObj := range vmUserObjs {
		// 1. Match spec.crd to vmcluster
		var found *vmv1beta1.TargetRef
		for _, ref := range vmUserObj.Spec.TargetRefs {
			if ref.CRD == nil || ref.CRD.Kind != "VMCluster/vmselect" || ref.CRD.Name != vmCluster.Name || ref.CRD.Namespace != vmCluster.Namespace {
				continue
			}
			// Check that target_path_suffix
			if strings.HasPrefix(ref.TargetPathSuffix, "/select/") {
				found = &ref
				break
			}
		}
		if found != nil {
			return found, nil
		}
	}
	// 2. Match static url to vmselect service
	// TODO[vrutkovs]: match static url to vmselect service
	return nil, fmt.Errorf("no vmuser has target refs for vmcluster %s", vmCluster.Name)
}

func setVMClusterStatusInVMUsers(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributedCluster, vmCluster *vmv1beta1.VMCluster, vmUserObjs []*vmv1beta1.VMUser, status bool) error {
	// Find matching rule in the vmdistributed status
	var found *vmv1beta1.TargetRef
	for _, vmClusterInfo := range cr.Status.VMClusterInfo {
		if vmClusterInfo.VMClusterName == vmCluster.Name {
			found = vmClusterInfo.TargetRef.DeepCopy()
			break
		}
	}
	if found == nil {
		return fmt.Errorf("no matching rule found for vmcluster %s in status of vmdistributedcluster %s", vmCluster.Name, cr.Name)
	}

	// Update all vmusers with the matching rule
	for _, vmUserObj := range vmUserObjs {
		if err := updateVMUserTargetRefs(ctx, rclient, vmUserObj, found, status); err != nil {
			return err
		}
	}

	return nil
}

// updateVMUserTargetRefs updates a single VMUser's TargetRefs based on the provided VMCluster rule and status.
func updateVMUserTargetRefs(ctx context.Context, rclient client.Client, vmUserObj *vmv1beta1.VMUser, found *vmv1beta1.TargetRef, status bool) error {
	// Fetch fresh copy of vmuser
	freshVMUserObj := &vmv1beta1.VMUser{}
	if err := rclient.Get(ctx, types.NamespacedName{Name: vmUserObj.Name, Namespace: vmUserObj.Namespace}, freshVMUserObj); err != nil {
		return fmt.Errorf("failed to fetch vmuser %s: %w", vmUserObj.Name, err)
	}

	var newTargetRefs []vmv1beta1.TargetRef
	needsUpdate := false

	if status { // true: add the targetRef if not already present
		alreadyPresent := false
		for _, ref := range freshVMUserObj.Spec.TargetRefs {
			if reflect.DeepEqual(ref.CRD, found.CRD) && ref.TargetPathSuffix == found.TargetPathSuffix {
				alreadyPresent = true
				break
			}
		}
		if !alreadyPresent {
			newTargetRefs = append(newTargetRefs, freshVMUserObj.Spec.TargetRefs...)
			newTargetRefs = append(newTargetRefs, *found.DeepCopy())
			needsUpdate = true
		} else {
			// If already present, no update needed
			newTargetRefs = freshVMUserObj.Spec.TargetRefs
		}
	} else { // false: remove the targetRef if present
		newTargetRefs = make([]vmv1beta1.TargetRef, 0)
		removed := false
		for _, ref := range freshVMUserObj.Spec.TargetRefs {
			if reflect.DeepEqual(ref.CRD, found.CRD) && ref.TargetPathSuffix == found.TargetPathSuffix {
				removed = true
				continue
			}
			newTargetRefs = append(newTargetRefs, ref)
		}
		if removed {
			needsUpdate = true
		} else {
			// If not present, no update needed
			newTargetRefs = freshVMUserObj.Spec.TargetRefs // Ensure newTargetRefs is assigned current refs if no removal
		}
	}

	if !needsUpdate {
		return nil
	}

	// Only update if the new list of target refs is actually different from the old one
	// This helps prevent unnecessary updates if the needsUpdate logic had a nuance.
	if reflect.DeepEqual(freshVMUserObj.Spec.TargetRefs, newTargetRefs) {
		return nil
	}

	freshVMUserObj.Spec.TargetRefs = newTargetRefs
	if err := rclient.Update(ctx, freshVMUserObj); err != nil {
		return fmt.Errorf("failed to update vmuser %s: %w", freshVMUserObj.Name, err)
	}

	return nil
}

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
		return vmCluster.Status.UpdateStatus == vmv1beta1.UpdateStatusOperational, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for VMCluster %s/%s to be ready: %w, current status: %s", vmCluster.Namespace, vmCluster.Name, err, lastStatus)
	}

	return nil
}

// VMAgentMetrics defines the interface for VMAgent objects that can provide metrics URLs
type VMAgentMetrics interface {
	AsURL() string
	GetMetricPath() string
}

// VMAgentWithStatus extends VMAgentMetrics to include status checking
type VMAgentWithStatus interface {
	VMAgentMetrics
	GetReplicas() int32
	GetNamespace() string
	GetName() string
}

// vmAgentAdapter wraps VMAgent to implement VMAgentWithStatus interface
type vmAgentAdapter struct {
	*vmv1beta1.VMAgent
}

func (v *vmAgentAdapter) GetReplicas() int32 {
	return v.Status.Replicas
}

// GetName and GetNamespace are inherited from vmv1beta1.VMAgent

func waitForVMClusterVMAgentMetrics(ctx context.Context, httpClient *http.Client, vmAgent VMAgentWithStatus, deadline time.Duration) error {
	if vmAgent == nil {
		// Don't throw error if VMAgent is nil, just exit early
		return nil
	}

	if vmAgent.GetReplicas() == 0 {
		return fmt.Errorf("VMAgent %s/%s is not ready", vmAgent.GetNamespace(), vmAgent.GetName())
	}

	parts := []string{vmAgent.AsURL(), vmAgent.GetMetricPath()}
	vmAgentPath := strings.Join(parts, "")

	// Loop until VMAgent metrics show that disk buffer is empty
	err := wait.PollUntilContextTimeout(ctx, 1*time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		metricValue, err := fetchVMAgentDiskBufferMetric(ctx, httpClient, vmAgentPath)
		if err != nil {
			return false, err
		}
		return metricValue == 0, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for VMAgent metrics: %w", err)
	}
	return nil
}

func fetchVMAgentDiskBufferMetric(ctx context.Context, httpClient *http.Client, metricsPath string) (int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsPath, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request for VMAgent at %s: %w", metricsPath, err)
	}
	resp, err := httpClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch metrics from VMAgent at %s: %w", metricsPath, err)
	}
	defer resp.Body.Close()
	metricBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read VMAgent metrics at %s: %w", metricsPath, err)
	}
	metrics := string(metricBytes)
	for _, metric := range strings.Split(metrics, "\n") {
		value, found := strings.CutPrefix(metric, VMAgentQueueMetricName)
		if found {
			value = strings.Trim(value, " ")
			res, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return 0, fmt.Errorf("could not parse metric value %s: %w", metric, err)
			}
			return int64(res), nil
		}
	}
	return 0, fmt.Errorf("metric %s not found", VMAgentQueueMetricName)
}

func setOwnerRefIfNeeded(cr *vmv1alpha1.VMDistributedCluster, vmClusterObj *vmv1beta1.VMCluster, index int, scheme *runtime.Scheme) (bool, error) {
	ref := metav1.OwnerReference{
		APIVersion: cr.APIVersion,
		Kind:       cr.Kind,
		UID:        cr.GetUID(),
		Name:       cr.GetName(),
	}
	if ok, err := controllerutil.HasOwnerReference([]metav1.OwnerReference{ref}, vmClusterObj, scheme); err != nil {
		return false, fmt.Errorf("failed to check owner reference for vmcluster %s at index %d: %w", vmClusterObj.Name, index, err)
	} else if !ok {
		// Set owner reference for the VMCluster to the VMDistributedCluster
		if err := controllerutil.SetOwnerReference(cr, vmClusterObj, scheme); err != nil {
			return false, fmt.Errorf("failed to set owner reference for vmcluster %s at index %d: %w", vmClusterObj.Name, index, err)
		}
		return true, nil
	}
	return false, nil
}
