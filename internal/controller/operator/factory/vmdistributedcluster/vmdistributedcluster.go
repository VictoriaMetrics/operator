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
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-test/deep"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
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

	// Extract tenantID from VMAgent spec or use default
	tenantID := "0"
	if cr.Spec.VMAgent.TenantID != nil {
		tenantID = *cr.Spec.VMAgent.TenantID
	}

	// Validate VMAuth
	if cr.Spec.VMAuth.Name == "" {
		return fmt.Errorf("VMAuth.Name must be set")
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

	// Ensure VMAuth exists first so we can set it as the owner for the automatically-created VMUser.
	vmAuthObjs, err := updateOrCreateVMAuth(ctx, rclient, cr, cr.Namespace, cr.Spec.VMAuth, scheme, nil)
	if err != nil {
		return fmt.Errorf("failed to update or create vmauth: %w", err)
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

	// Build TargetRefs for all VMClusters in the zones.
	vmUserTargetRefs := buildVMUserTargetRefs(vmClusters, tenantID)

	// Create or update an operator-managed VMUser named "<cr.Name>-user".
	vmUserObjs, err := createOrUpdateOperatorVMUser(ctx, rclient, cr, scheme, vmAuthObjs, vmUserTargetRefs, vmClusters, tenantID)
	if err != nil {
		return fmt.Errorf("failed to create or update operator-managed VMUser: %w", err)
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
	cr.Status.Zones.VMClusters = cr.Spec.Zones.VMClusters

	// Compare generations of vmcluster objects from the spec with the previous CR and Zones configuration
	if diff := deep.Equal(getGenerationsFromStatus(currentCRStatus), getGenerationsFromStatus(&cr.Status)); len(diff) > 0 {
		// Record new generations and zones config, then exit early if a change is detected
		if err := rclient.Status().Update(ctx, cr); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
		return fmt.Errorf("unexpected generations or zones config change detected: %v", diff)
	}

	// Update or create the VMAgent
	vmAgentObj, err := updateOrCreateVMAgent(ctx, rclient, cr, scheme, vmClusters)
	if err != nil {
		return fmt.Errorf("failed to update or create VMAgent: %w", err)
	}

	// Disable VMClusters one by one if overrideSpec needs to be applied
	httpClient := &http.Client{
		Timeout: httpTimeout,
	}
	for i, vmClusterObj := range vmClusters {
		zoneRefOrSpec := cr.Spec.Zones.VMClusters[i]

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
		modifiedOwnerRef, err := setOwnerRefIfNeeded(cr, vmClusterObj, scheme)
		if err != nil {
			return fmt.Errorf("failed to set owner reference for vmcluster %s: %w", vmClusterObj.Name, err)
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

		// Disable this VMCluster in vmusers if spec is modified
		if modifiedSpec {
			if err := setVMClusterStatusInVMUsers(ctx, rclient, cr, vmClusterObj, vmUserObjs, false); err != nil {
				return fmt.Errorf("failed to set VMCluster status in VMUsers: %w", err)
			}
		}

		// Apply the updated object
		if err := rclient.Update(ctx, vmClusterObj); err != nil {
			return fmt.Errorf("failed to update vmcluster %s at index %d after applying override spec: %w", vmClusterObj.Name, i, err)
		}

		if !modifiedSpec {
			continue
		}

		// Wait for VMCluster to be ready
		if err := waitForVMClusterReady(ctx, rclient, vmClusterObj, vmclusterWaitReadyDeadline); err != nil {
			return fmt.Errorf("failed to wait for VMCluster %s/%s to be ready: %w", vmClusterObj.Namespace, vmClusterObj.Name, err)
		}

		// Wait for VMAgent metrics to show no pending queue
		// Wrap concrete VMAgent into adapter that implements VMAgentWithStatus
		if err := waitForVMClusterVMAgentMetrics(ctx, httpClient, &vmAgentAdapter{VMAgent: vmAgentObj}, vmclusterWaitReadyDeadline, rclient); err != nil {
			return fmt.Errorf("failed to wait for VMAgent metrics to show no pending queue: %w", err)
		}

		// Enable this VMCluster in vmusers
		if err := setVMClusterStatusInVMUsers(ctx, rclient, cr, vmClusterObj, vmUserObjs, true); err != nil {
			return fmt.Errorf("failed to set VMCluster status in VMUsers: %w", err)
		}

		// Sleep for zoneUpdatePause time between VMClusters updates
		time.Sleep(zoneUpdatePause)
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

// fetchVMClusters ensures that referenced VMClusters are fetched and validated.
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

// updateOrCreateVMAgent ensures that the VMAgent is updated or created based on the provided VMDistributedCluster.
func updateOrCreateVMAgent(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributedCluster, scheme *runtime.Scheme, vmClusters []*vmv1beta1.VMCluster) (*vmv1beta1.VMAgent, error) {
	// Get existing vmagent obj using Name and cr namespace
	vmAgentExists := true
	vmAgentNeedsUpdate := false
	vmAgentObj := &vmv1beta1.VMAgent{}
	namespacedName := types.NamespacedName{Name: cr.Spec.VMAgent.Name, Namespace: cr.Namespace}
	if err := rclient.Get(ctx, namespacedName, vmAgentObj); err != nil {
		if k8serrors.IsNotFound(err) {
			vmAgentExists = false
			// If it doesn't exist, initialize object for creation.
			vmAgentObj = &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cr.Spec.VMAgent.Name,
					Namespace: cr.Namespace,
				},
			}
		} else {
			return nil, fmt.Errorf("failed to get VMAgent %s/%s: %w", cr.Namespace, cr.Spec.VMAgent.Name, err)
		}
	}

	// Prepare the desired spec for VMAgent.
	var desiredVMAgentSpec vmv1beta1.VMAgentSpec
	if cr.Spec.VMAgent.Spec != nil {
		desiredVMAgentSpec = *cr.Spec.VMAgent.Spec.DeepCopy()
	}

	// Determine tenant id (default to \"0\" when not provided)
	tenantID := "0"
	if cr.Spec.VMAgent.TenantID != nil && *cr.Spec.VMAgent.TenantID != "" {
		tenantID = *cr.Spec.VMAgent.TenantID
	}
	tenantPtr := &tenantID

	// Point VMAgent to all VMClusters by constructing RemoteWrite entries
	if len(vmClusters) > 0 {
		desiredVMAgentSpec.RemoteWrite = make([]vmv1beta1.VMAgentRemoteWriteSpec, len(vmClusters))
		for i, vmCluster := range vmClusters {
			desiredVMAgentSpec.RemoteWrite[i].URL = remoteWriteURL(vmCluster, tenantPtr)
		}
	}

	// Compare current spec with desired spec and apply if different
	if !reflect.DeepEqual(vmAgentObj.Spec, desiredVMAgentSpec) {
		vmAgentObj.Spec = desiredVMAgentSpec
		vmAgentNeedsUpdate = true
	}

	// Ensure owner reference is set to current CR
	if scheme != nil {
		if modifiedOwnerRef, err := setOwnerRefIfNeeded(cr, vmAgentObj, scheme); err != nil {
			return nil, fmt.Errorf("failed to set owner reference for VMAgent %s: %w", vmAgentObj.Name, err)
		} else if modifiedOwnerRef {
			vmAgentNeedsUpdate = true
		}
	}

	// Create or update the vmagent object if spec has changed or it doesn't exist yet
	if !vmAgentExists {
		if err := rclient.Create(ctx, vmAgentObj); err != nil {
			return nil, fmt.Errorf("failed to create VMAgent %s/%s: %w", vmAgentObj.Namespace, vmAgentObj.Name, err)
		}
	} else if vmAgentNeedsUpdate {
		if err := rclient.Update(ctx, vmAgentObj); err != nil {
			return nil, fmt.Errorf("failed to update VMAgent %s/%s: %w", vmAgentObj.Namespace, vmAgentObj.Name, err)
		}
	}
	return vmAgentObj, nil
}

// remoteWriteURL generates the remote write URL based on the provided VMCluster and tenant.
func remoteWriteURL(vmCluster *vmv1beta1.VMCluster, tenant *string) string {
	return fmt.Sprintf("http://%s.%s.cluster.local.:8480/insert/%s/prometheus/api/v1/write", vmCluster.Name, vmCluster.Namespace, *tenant)
}

// updateOrCreateVMAuth updates or creates a VMAuth object based on the provided VMCluster and tenant.
func updateOrCreateVMAuth(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributedCluster, namespace string, ref vmv1alpha1.VMAuthNameAndSpec, scheme *runtime.Scheme, vmUserObjs []*vmv1beta1.VMUser) ([]*vmv1beta1.VMAuth, error) {
	// Name must be provided either for fetching an existing object or for creating an inline one.
	if ref.Name == "" {
		return nil, errors.New("vmauth name is not specified")
	}

	namespacedName := types.NamespacedName{Name: ref.Name, Namespace: namespace}

	// First ensure VMUser objects have proper labels for VMAuth selection
	vmUserLabels := map[string]string{
		"app.kubernetes.io/managed-by": "vm-operator",
		"app.kubernetes.io/component":  "vmdistributedcluster",
		"vmdistributedcluster":         cr.Name,
	}

	for _, vmUser := range vmUserObjs {
		vmUserCopy := vmUser.DeepCopy()
		if vmUserCopy.Labels == nil {
			vmUserCopy.Labels = make(map[string]string)
		}
		needsUpdate := false
		for k, v := range vmUserLabels {
			if vmUserCopy.Labels[k] != v {
				vmUserCopy.Labels[k] = v
				needsUpdate = true
			}
		}
		if needsUpdate {
			if err := rclient.Update(ctx, vmUserCopy); err != nil {
				return nil, fmt.Errorf("failed to update VMUser %s labels: %w", vmUser.Name, err)
			}
			// Update the object in slice as well to reflect changes
			*vmUser = *vmUserCopy
		}
	}

	// If Spec is not provided inline, simply fetch the named VMAuth from the API server.
	if ref.Spec == nil {
		vmAuthObj := &vmv1beta1.VMAuth{}
		if err := rclient.Get(ctx, namespacedName, vmAuthObj); err != nil {
			return nil, fmt.Errorf("failed to get VMAuth %s/%s: %w", namespace, ref.Name, err)
		}
		return []*vmv1beta1.VMAuth{vmAuthObj}, nil
	}

	// Spec is provided inline: ensure the VMAuth exists and matches the provided spec.
	vmAuthObj := &vmv1beta1.VMAuth{}
	vmAuthExists := true
	if err := rclient.Get(ctx, namespacedName, vmAuthObj); err != nil {
		if k8serrors.IsNotFound(err) {
			vmAuthExists = false
			// Initialize object for creation with the provided inline spec.
			vmAuthObj = &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ref.Name,
					Namespace: namespace,
				},
				Spec: *ref.Spec.DeepCopy(),
			}

			// Ensure VMAuth selects our VMUser objects if no explicit selector provided
			if vmAuthObj.Spec.UserSelector == nil && !vmAuthObj.Spec.SelectAllByDefault {
				vmAuthObj.Spec.UserSelector = &metav1.LabelSelector{
					MatchLabels: vmUserLabels,
				}
			}
		} else {
			return nil, fmt.Errorf("failed to get VMAuth %s/%s: %w", namespace, ref.Name, err)
		}
	}

	// Determine if an update is needed (spec or ownerRef changes).
	vmAuthNeedsUpdate := false
	if vmAuthExists {
		if !reflect.DeepEqual(vmAuthObj.Spec, *ref.Spec) {
			vmAuthObj.Spec = *ref.Spec.DeepCopy()
			// Ensure VMAuth selects our VMUser objects if no explicit selector provided
			if vmAuthObj.Spec.UserSelector == nil && !vmAuthObj.Spec.SelectAllByDefault {
				vmAuthObj.Spec.UserSelector = &metav1.LabelSelector{
					MatchLabels: vmUserLabels,
				}
			}
			vmAuthNeedsUpdate = true
		}
	}

	// Ensure owner reference is set to current CR if scheme provided.
	if scheme != nil {
		if modifiedOwnerRef, err := setOwnerRefIfNeeded(cr, vmAuthObj, scheme); err != nil {
			return nil, fmt.Errorf("failed to set owner reference for VMAuth %s: %w", vmAuthObj.Name, err)
		} else if modifiedOwnerRef {
			vmAuthNeedsUpdate = true
		}
	}

	// Create or update the VMAuth resource as needed.
	if !vmAuthExists {
		if err := rclient.Create(ctx, vmAuthObj); err != nil {
			return nil, fmt.Errorf("failed to create VMAuth %s/%s: %w", vmAuthObj.Namespace, vmAuthObj.Name, err)
		}
	} else if vmAuthNeedsUpdate {
		if err := rclient.Update(ctx, vmAuthObj); err != nil {
			return nil, fmt.Errorf("failed to update VMAuth %s/%s: %w", vmAuthObj.Namespace, vmAuthObj.Name, err)
		}
	}

	return []*vmv1beta1.VMAuth{vmAuthObj}, nil
}

func buildVMUserTargetRefs(vmClusters []*vmv1beta1.VMCluster, tenantID string) []vmv1beta1.TargetRef {
	targets := make([]vmv1beta1.TargetRef, 0, len(vmClusters))
	for _, vmCluster := range vmClusters {
		targets = append(targets, vmv1beta1.TargetRef{
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster/vmselect",
				Name:      vmCluster.Name,
				Namespace: vmCluster.Namespace,
			},
			TargetPathSuffix: fmt.Sprintf("/select/%s/prometheus/api/v1", tenantID),
		})
	}
	return targets
}

func ensureVMUsersTargetRefs(ctx context.Context, rclient client.Client, vmClusters []*vmv1beta1.VMCluster, vmUserObjs []*vmv1beta1.VMUser, tenantID string) error {
	// For each VMCluster and each VMUser, ensure a TargetRef exists that points to the VMCluster's vmselect.
	// We reuse updateVMUserTargetRefs to perform the add operation. This ensures consistency with existing update logic.
	for _, vmCluster := range vmClusters {
		// Build the canonical TargetRef pointing to vmselect for this cluster.
		targetRef := &vmv1beta1.TargetRef{
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster/vmselect",
				Name:      vmCluster.Name,
				Namespace: vmCluster.Namespace,
			},
			TargetPathSuffix: fmt.Sprintf("/select/%s/prometheus/api/v1", tenantID),
		}
		for _, vmUserObj := range vmUserObjs {
			// Use updateVMUserTargetRefs to add the targetRef if missing.
			if err := updateVMUserTargetRefs(ctx, rclient, vmUserObj, targetRef, true); err != nil {
				return fmt.Errorf("failed to ensure targetRef for vmuser %s pointing to vmcluster %s: %w", vmUserObj.Name, vmCluster.Name, err)
			}
		}
	}
	return nil
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
	// PrefixedName returns the service prefixed name for discovery (when applicable)
	PrefixedName() string
}

// vmAgentAdapter provides a small adapter to satisfy VMAgentWithStatus for concrete vmv1beta1.VMAgent instances.
type vmAgentAdapter struct {
	*vmv1beta1.VMAgent
}

func (v *vmAgentAdapter) GetReplicas() int32 {
	return v.Status.Replicas
}

// GetName and GetNamespace are inherited from vmv1beta1.VMAgent

// PrefixedName is exposed explicitly on the adapter to satisfy VMAgentWithStatus.
func (v *vmAgentAdapter) PrefixedName() string {
	if v.VMAgent == nil {
		return ""
	}
	return v.VMAgent.PrefixedName()
}

// parseEndpointSliceAddresses extracts IPv4/IPv6 addresses from an EndpointSlice object.
func parseEndpointSliceAddresses(es *discoveryv1.EndpointSlice) []string {
	if es == nil {
		return nil
	}
	addrs := make([]string, 0)
	for _, ep := range es.Endpoints {
		for _, a := range ep.Addresses {
			if a != "" {
				addrs = append(addrs, a)
			}
		}
	}
	return addrs
}

// buildPerIPMetricURL constructs a per-IP metrics URL using the base service URL to
// infer scheme and (optionally) port. This centralizes scheme/port detection so the
// polling logic remains concise.
func buildPerIPMetricURL(baseURL, metricPath, ip string) string {
	scheme := "http"
	port := ""
	if u, err := url.Parse(baseURL); err == nil {
		if u.Scheme != "" {
			scheme = u.Scheme
		}
		if p := u.Port(); p != "" {
			port = p
		}
	}
	if port == "" {
		// Default VMAgent metrics port
		port = "8429"
	}
	return fmt.Sprintf("%s://%s:%s%s", scheme, ip, port, metricPath)
}

// waitForVMClusterVMAgentMetrics accepts a VMAgentWithStatus interface plus a client.Client.
// This allows callers that already have an implementation of VMAgentWithStatus (e.g., tests' mocks)
// to call this function directly. When callers have a concrete *vmv1beta1.VMAgent, they can wrap it
// with vmAgentAdapter (defined above) or use a type that implements VMAgentWithStatus.
//
// The function will try to discover pod IPs via an EndpointSlice named the same as the service
// (prefixed name). If discovery yields addresses, it polls each IP. Otherwise, it falls back to the
// single AsURL()+GetMetricPath() behavior.
func waitForVMClusterVMAgentMetrics(ctx context.Context, httpClient *http.Client, vmAgent VMAgentWithStatus, deadline time.Duration, rclient client.Client) error {
	if vmAgent == nil {
		// Don't throw error if VMAgent is nil, just exit early
		return nil
	}

	// If no client is provided, there's nothing to discover via EndpointSlices,
	// so we should fallback to the single-URL polling behavior instead of returning early.
	// Caller may invoke this function without a client in some contexts; do not treat that
	// as an error — continue and poll vmAgent.AsURL()+GetMetricPath().

	// Use GetReplicas from interface
	if vmAgent.GetReplicas() == 0 {
		return fmt.Errorf("VMAgent %s/%s is not ready", vmAgent.GetNamespace(), vmAgent.GetName())
	}

	// Base values for fallback URL
	baseURL := vmAgent.AsURL()
	metricPath := vmAgent.GetMetricPath()

	// Poll until all discovered pod IPs (or single URL fallback) report zero pending queue.
	err := wait.PollUntilContextTimeout(ctx, 1*time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		var hosts []string

		// Attempt EndpointSlice discovery only when a client is provided.
		if rclient != nil {
			// Use PrefixedName from the interface for discovery.
			svcName := vmAgent.PrefixedName()
			svcNamespace := vmAgent.GetNamespace()

			// Try to GET a single EndpointSlice that has the same name as the service
			var es discoveryv1.EndpointSlice
			if err := rclient.Get(ctx, types.NamespacedName{Name: svcName, Namespace: svcNamespace}, &es); err == nil {
				hosts = parseEndpointSliceAddresses(&es)
			}
			// If Get fails or returns no addresses, we'll fall back below.
		}

		// If we couldn't discover any hosts, fall back to the AsURL host (single host behavior)
		if len(hosts) == 0 {
			vmAgentPath := strings.Join([]string{baseURL, metricPath}, "")
			metricValue, err := fetchVMAgentDiskBufferMetric(ctx, httpClient, vmAgentPath)
			if err != nil {
				return false, err
			}
			return metricValue == 0, nil
		}

		// Query each discovered ip. If any returns non-zero metric, continue polling.
		for _, ip := range hosts {
			metricsURL := buildPerIPMetricURL(baseURL, metricPath, ip)
			metricValue, ferr := fetchVMAgentDiskBufferMetric(ctx, httpClient, metricsURL)
			if ferr != nil {
				// Treat fetch errors as transient -> not ready, continue polling.
				return false, nil
			}
			if metricValue != 0 {
				return false, nil
			}
		}
		// All discovered addresses reported zero -> done.
		return true, nil
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

func createOrUpdateOperatorVMUser(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributedCluster, scheme *runtime.Scheme, vmAuthObjs []*vmv1beta1.VMAuth, vmUserTargetRefs []vmv1beta1.TargetRef, vmClusters []*vmv1beta1.VMCluster, tenantID string) ([]*vmv1beta1.VMUser, error) {
	// Build the VMUser name
	vmUserName := fmt.Sprintf("%s-user", cr.Name)
	namespacedName := types.NamespacedName{Name: vmUserName, Namespace: cr.Namespace}

	// Try to fetch existing VMUser
	vmUserObj := &vmv1beta1.VMUser{}
	vmUserExists := true
	if err := rclient.Get(ctx, namespacedName, vmUserObj); err != nil {
		if k8serrors.IsNotFound(err) {
			vmUserExists = false
			// Initialize a new VMUser for creation
			vmUserObj = &vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vmUserName,
					Namespace: cr.Namespace,
					Labels: map[string]string{
						"vmdistributedcluster": cr.Name,
					},
				},
				Spec: vmv1beta1.VMUserSpec{
					TargetRefs: vmUserTargetRefs,
				},
			}
		} else {
			return nil, fmt.Errorf("failed to get VMUser %s/%s: %w", cr.Namespace, vmUserName, err)
		}
	} else {
		// Ensure TargetRefs are up-to-date.
		if !reflect.DeepEqual(vmUserObj.Spec.TargetRefs, vmUserTargetRefs) {
			vmUserObj.Spec.TargetRefs = vmUserTargetRefs
		}
		// Ensure labels used by VMAuth selection are present.
		if vmUserObj.Labels == nil {
			vmUserObj.Labels = map[string]string{}
		}
		if vmUserObj.Labels["vmdistributedcluster"] != cr.Name {
			vmUserObj.Labels["vmdistributedcluster"] = cr.Name
		}
	}

	// Set owner reference to VMAuth if available and scheme provided.
	if scheme != nil && len(vmAuthObjs) > 0 {
		vmAuthObj := vmAuthObjs[0]
		// Check whether ownerRef already present for VMAuth
		if ok, err := controllerutil.HasOwnerReference(vmUserObj.GetOwnerReferences(), vmAuthObj, scheme); err != nil {
			return nil, fmt.Errorf("failed to check owner reference for VMUser %s: %w", vmUserObj.Name, err)
		} else if !ok {
			if err := controllerutil.SetOwnerReference(vmAuthObj, vmUserObj, scheme); err != nil {
				return nil, fmt.Errorf("failed to set owner reference for VMUser %s: %w", vmUserObj.Name, err)
			}
		}
	}

	// Create or update the VMUser resource as needed.
	if !vmUserExists {
		if err := rclient.Create(ctx, vmUserObj); err != nil {
			return nil, fmt.Errorf("failed to create VMUser %s/%s: %w", vmUserObj.Namespace, vmUserObj.Name, err)
		}
	} else {
		if err := rclient.Update(ctx, vmUserObj); err != nil {
			return nil, fmt.Errorf("failed to update VMUser %s/%s: %w", vmUserObj.Namespace, vmUserObj.Name, err)
		}
	}

	// Prepare vmUserObjs slice for downstream logic.
	vmUserObjs := []*vmv1beta1.VMUser{vmUserObj}

	// Ensure VMUser TargetRefs are present/consistent (idempotent).
	if err := ensureVMUsersTargetRefs(ctx, rclient, vmClusters, vmUserObjs, tenantID); err != nil {
		return nil, fmt.Errorf("failed to ensure vmuser target refs: %w", err)
	}

	return vmUserObjs, nil
}
