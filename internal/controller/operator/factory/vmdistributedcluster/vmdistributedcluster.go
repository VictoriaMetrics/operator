package vmdistributedcluster

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-test/deep"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
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

	// Build TargetRefs for all VMClusters in the zones.
	vmUserTargetRefs := buildVMUserTargetRefs(vmClusters, tenantID)

	// Create or update an operator-managed VMUser named "<cr.Name>-user".
	vmUserObjs, err := createOrUpdateOperatorVMUser(ctx, rclient, cr, scheme, vmUserTargetRefs, vmClusters, tenantID)
	if err != nil {
		return fmt.Errorf("failed to create or update operator-managed VMUser: %w", err)
	}

	// Store current CR status
	previousCRStatus := cr.Status.DeepCopy()
	cr.Status = vmv1alpha1.VMDistributedClusterStatus{}

	// Ensure that all vmusers have a read rule for vmcluster and record vmcluster info in VMDistributedCluster status
	cr.Status.VMClusterInfo = make([]vmv1alpha1.VMClusterStatus, len(vmClusters))
	for i, vmCluster := range vmClusters {
		vmClusterInfo, err := setVMClusterInfo(vmCluster, vmUserObjs, previousCRStatus)
		if err != nil {
			return err
		}
		cr.Status.VMClusterInfo[i] = vmClusterInfo
	}
	cr.Status.Zones.VMClusters = cr.Spec.Zones.VMClusters

	// Compare generations of vmcluster objects from the spec with the previous CR and Zones configuration
	previousGenerations := getGenerationsFromStatus(previousCRStatus)
	currentGenerations := getGenerationsFromStatus(&cr.Status)
	if len(previousGenerations) != len(currentGenerations) {
		// Set status to expanding until all clusters have reported their status
		cr.Status.UpdateStatus = vmv1beta1.UpdateStatusExpanding
	}

	if diff := deep.Equal(previousGenerations, currentGenerations); len(diff) > 0 {
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
			// Ignore this error when running e2e tests - these need to run in the same network as pods
			if os.Getenv("E2E_TEST") == "true" {
				continue
			}
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
	desiredVMAgentSpec := vmv1alpha1.CustomVMAgentSpec{}
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
		desiredVMAgentSpec.RemoteWrite = make([]vmv1alpha1.CustomVMAgentRemoteWriteSpec, len(vmClusters))
		for i, vmCluster := range vmClusters {
			desiredVMAgentSpec.RemoteWrite[i].URL = remoteWriteURL(vmCluster, tenantPtr)
		}
	}

	// Assemble new VMAgentSpec
	newVMAgentSpec := vmv1beta1.VMAgentSpec{}
	newVMAgentSpec.PodMetadata = desiredVMAgentSpec.PodMetadata
	newVMAgentSpec.ManagedMetadata = desiredVMAgentSpec.ManagedMetadata
	newVMAgentSpec.LogLevel = desiredVMAgentSpec.LogLevel
	newVMAgentSpec.LogFormat = desiredVMAgentSpec.LogFormat
	newVMAgentSpec.RemoteWriteSettings = desiredVMAgentSpec.RemoteWriteSettings
	newVMAgentSpec.ShardCount = desiredVMAgentSpec.ShardCount
	newVMAgentSpec.UpdateStrategy = desiredVMAgentSpec.UpdateStrategy
	newVMAgentSpec.RollingUpdate = desiredVMAgentSpec.RollingUpdate
	newVMAgentSpec.PodDisruptionBudget = desiredVMAgentSpec.PodDisruptionBudget
	newVMAgentSpec.EmbeddedProbes = desiredVMAgentSpec.EmbeddedProbes
	newVMAgentSpec.DaemonSetMode = desiredVMAgentSpec.DaemonSetMode
	newVMAgentSpec.StatefulMode = desiredVMAgentSpec.StatefulMode
	newVMAgentSpec.StatefulStorage = desiredVMAgentSpec.StatefulStorage
	newVMAgentSpec.StatefulRollingUpdateStrategy = desiredVMAgentSpec.StatefulRollingUpdateStrategy
	newVMAgentSpec.PersistentVolumeClaimRetentionPolicy = desiredVMAgentSpec.PersistentVolumeClaimRetentionPolicy
	newVMAgentSpec.ClaimTemplates = desiredVMAgentSpec.ClaimTemplates
	newVMAgentSpec.License = desiredVMAgentSpec.License
	newVMAgentSpec.ServiceAccountName = desiredVMAgentSpec.ServiceAccountName
	newVMAgentSpec.VMAgentSecurityEnforcements = desiredVMAgentSpec.VMAgentSecurityEnforcements
	newVMAgentSpec.CommonDefaultableParams = desiredVMAgentSpec.CommonDefaultableParams
	newVMAgentSpec.CommonConfigReloaderParams = desiredVMAgentSpec.CommonConfigReloaderParams
	newVMAgentSpec.CommonApplicationDeploymentParams = desiredVMAgentSpec.CommonApplicationDeploymentParams

	if len(desiredVMAgentSpec.RemoteWrite) > 0 {
		newVMAgentSpec.RemoteWrite = make([]vmv1beta1.VMAgentRemoteWriteSpec, len(desiredVMAgentSpec.RemoteWrite))
		for i, remoteWrite := range desiredVMAgentSpec.RemoteWrite {
			vmAgentRemoteWrite := vmv1beta1.VMAgentRemoteWriteSpec{}
			vmAgentRemoteWrite.URL = remoteWrite.URL
			vmAgentRemoteWrite.BasicAuth = remoteWrite.BasicAuth
			vmAgentRemoteWrite.BearerTokenSecret = remoteWrite.BearerTokenSecret
			vmAgentRemoteWrite.OAuth2 = remoteWrite.OAuth2
			vmAgentRemoteWrite.TLSConfig = remoteWrite.TLSConfig
			vmAgentRemoteWrite.SendTimeout = remoteWrite.SendTimeout
			vmAgentRemoteWrite.Headers = remoteWrite.Headers
			vmAgentRemoteWrite.MaxDiskUsage = remoteWrite.MaxDiskUsage
			vmAgentRemoteWrite.ForceVMProto = remoteWrite.ForceVMProto
			vmAgentRemoteWrite.ProxyURL = remoteWrite.ProxyURL
			vmAgentRemoteWrite.AWS = remoteWrite.AWS
			newVMAgentSpec.RemoteWrite[i] = vmAgentRemoteWrite
		}
	}

	// Compare current spec with desired spec and apply if different
	if !reflect.DeepEqual(vmAgentObj.Spec, newVMAgentSpec) {
		vmAgentObj.Spec = *newVMAgentSpec.DeepCopy()
		vmAgentNeedsUpdate = true
	}

	// Ensure owner reference is set to current CR
	if modifiedOwnerRef, err := setOwnerRefIfNeeded(cr, vmAgentObj, scheme); err != nil {
		return nil, fmt.Errorf("failed to set owner reference for VMAgent %s: %w", vmAgentObj.Name, err)
	} else if modifiedOwnerRef {
		vmAgentNeedsUpdate = true
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
	return fmt.Sprintf("http://%s.%s.svc.cluster.local.:8480/insert/%s/prometheus/api/v1/write", vmCluster.PrefixedName(vmv1beta1.ClusterComponentInsert), vmCluster.Namespace, *tenant)
}

func buildLBConfigSecretMeta(cr *vmv1alpha1.VMDistributedCluster) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace:       cr.Namespace,
		Name:            cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
		Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentBalancer),
		Annotations:     cr.AnnotationsFiltered(),
		OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
	}
}

func buildVMAuthVMSelectRefs(vmClusters []*vmv1beta1.VMCluster) []string {
	result := make([]string, 0, len(vmClusters))
	for _, vmCluster := range vmClusters {
		targetHostSuffix := fmt.Sprintf("%s.svc", vmCluster.Namespace)
		if vmCluster.Spec.ClusterDomainName != "" {
			targetHostSuffix += fmt.Sprintf(".%s", vmCluster.Spec.ClusterDomainName)
		}
		selectPort := "8481"
		if vmCluster.Spec.VMSelect != nil {
			selectPort = vmCluster.Spec.VMSelect.Port
		}
		result = append(result, fmt.Sprintf(`
  - src_paths:
    - "/.*"
    url_prefix: "http://srv+%s.%s:%s"
    discover_backend_ips: true
      `, vmCluster.PrefixedInternalName(vmv1beta1.ClusterComponentSelect), targetHostSuffix, selectPort))
	}
	return result
}

func buildVMAuthLBSecret(cr *vmv1alpha1.VMDistributedCluster, vmClusters []*vmv1beta1.VMCluster) *corev1.Secret {
	lbScrt := &corev1.Secret{
		ObjectMeta: buildLBConfigSecretMeta(cr),
		StringData: map[string]string{"config.yaml": fmt.Sprintf(`
unauthorized_user:
  url_map:
  %s
      `, strings.Join(buildVMAuthVMSelectRefs(vmClusters), "\n"))},
	}
	return lbScrt
}

func buildVMAuthLBDeployment(cr *vmv1alpha1.VMDistributedCluster) (*appsv1.Deployment, error) {
	spec := cr.Spec.VMAuth.Spec
	const configMountName = "vmauth-lb-config"
	volumes := []corev1.Volume{
		{
			Name: configMountName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
				},
			},
		},
	}
	volumes = append(volumes, spec.Volumes...)
	vmounts := []corev1.VolumeMount{
		{
			MountPath: "/opt/vmauth-config/",
			Name:      configMountName,
		},
	}
	vmounts = append(vmounts, spec.VolumeMounts...)

	args := []string{
		"-auth.config=/opt/vmauth-config/config.yaml",
		"-configCheckInterval=30s",
	}
	if spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", spec.LogLevel))

	}
	if spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", spec.LogFormat))
	}

	cfg := config.MustGetBaseConfig()
	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", spec.Port))
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if len(spec.ExtraEnvs) > 0 || len(spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	args = build.AddExtraArgsOverrideDefaults(args, spec.ExtraArgs, "-")
	sort.Strings(args)
	vmauthLBCnt := corev1.Container{
		Name: "vmauth",
		Ports: []corev1.ContainerPort{
			{
				Protocol:      corev1.ProtocolTCP,
				Name:          "http",
				ContainerPort: intstr.Parse(spec.Port).IntVal,
			},
		},
		Args:            args,
		Env:             spec.ExtraEnvs,
		EnvFrom:         spec.ExtraEnvsFrom,
		Resources:       spec.Resources,
		Image:           fmt.Sprintf("%s:%s", spec.Image.Repository, spec.Image.Tag),
		ImagePullPolicy: spec.Image.PullPolicy,
		VolumeMounts:    vmounts,
	}
	vmauthLBCnt = build.Probe(vmauthLBCnt, spec)
	containers := []corev1.Container{
		vmauthLBCnt,
	}
	var err error

	build.AddStrictSecuritySettingsToContainers(spec.SecurityContext, containers, ptr.Deref(spec.UseStrictSecurity, cfg.EnableStrictSecurity))
	containers, err = k8stools.MergePatchContainers(containers, spec.Containers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch containers: %w", err)
	}
	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.VMAuth.Spec.UpdateStrategy != nil {
		strategyType = *cr.Spec.VMAuth.Spec.UpdateStrategy
	}
	lbDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       cr.Namespace,
			Name:            cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentBalancer),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentBalancer),
			},
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.VMAuth.Spec.RollingUpdate,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      cr.PodLabels(vmv1beta1.ClusterComponentBalancer),
					Annotations: cr.PodAnnotations(vmv1beta1.ClusterComponentBalancer),
				},
				Spec: corev1.PodSpec{
					Volumes:            volumes,
					InitContainers:     spec.InitContainers,
					Containers:         containers,
					ServiceAccountName: cr.GetServiceAccountName(),
				},
			},
		},
	}
	build.DeploymentAddCommonParams(lbDep, ptr.Deref(cr.Spec.VMAuth.Spec.UseStrictSecurity, cfg.EnableStrictSecurity), &spec.CommonApplicationDeploymentParams)
	return lbDep, nil
}

func createOrUpdateVMAuthLBService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1alpha1.VMDistributedCluster) error {
	builder := func(r *vmv1alpha1.VMDistributedCluster) *build.ChildBuilder {
		b := build.NewChildBuilder(r, vmv1beta1.ClusterComponentBalancer)
		b.SetFinalLabels(labels.Merge(b.AllLabels(), map[string]string{
			vmv1beta1.VMAuthLBServiceProxyTargetLabel: "vmauth",
		}))
		return b
	}
	b := builder(cr)
	svc := build.Service(b, cr.Spec.VMAuth.Spec.Port, nil)
	var prevSvc *corev1.Service
	if prevCR != nil {
		b = builder(prevCR)
		prevSvc = build.Service(b, prevCR.Spec.VMAuth.Spec.Port, nil)
	}

	if err := reconcile.Service(ctx, rclient, svc, prevSvc); err != nil {
		return fmt.Errorf("cannot reconcile vmauthlb service: %w", err)
	}
	svs := build.VMServiceScrapeForServiceWithSpec(svc, cr.Spec.VMAuth.Spec)
	svs.Spec.Selector.MatchLabels[vmv1beta1.VMAuthLBServiceProxyTargetLabel] = "vmauth"
	if err := reconcile.VMServiceScrapeForCRD(ctx, rclient, svs); err != nil {
		return fmt.Errorf("cannot reconcile vmauthlb vmservicescrape: %w", err)
	}
	return nil
}

func createOrUpdatePodDisruptionBudgetForVMAuthLB(ctx context.Context, rclient client.Client, cr, prevCR *vmv1alpha1.VMDistributedCluster) error {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentBalancer)
	pdb := build.PodDisruptionBudget(b, cr.Spec.VMAuth.Spec.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.VMAuth.Spec.PodDisruptionBudget != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentBalancer)
		prevPDB = build.PodDisruptionBudget(b, prevCR.Spec.VMAuth.Spec.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}

func createOrUpdateVMAuthLB(ctx context.Context, rclient client.Client, cr, prevCR *vmv1alpha1.VMDistributedCluster, vmClusters []*vmv1beta1.VMCluster) error {
	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = ptr.To(buildLBConfigSecretMeta(prevCR))
	}
	if err := reconcile.Secret(ctx, rclient, buildVMAuthLBSecret(cr, vmClusters), prevSecretMeta); err != nil {
		return fmt.Errorf("cannot reconcile vmauth lb secret: %w", err)
	}
	lbDep, err := buildVMAuthLBDeployment(cr)
	if err != nil {
		return fmt.Errorf("cannot build deployment for vmauth loadbalancing: %w", err)
	}
	var prevLB *appsv1.Deployment
	if prevCR != nil {
		prevLB, err = buildVMAuthLBDeployment(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev deployment for vmauth loadbalancing: %w", err)
		}
	}
	if err := reconcile.Deployment(ctx, rclient, lbDep, prevLB, false); err != nil {
		return fmt.Errorf("cannot reconcile vmauth lb deployment: %w", err)
	}
	if err := createOrUpdateVMAuthLBService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if cr.Spec.VMAuth.Spec.PodDisruptionBudget != nil {
		if err := createOrUpdatePodDisruptionBudgetForVMAuthLB(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot create or update PodDisruptionBudget for vmauth lb: %w", err)
		}
	}
	return nil
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
	PrefixedName() string
	GetName() string
	GetNamespace() string
}

// vmAgentAdapter provides a small adapter to satisfy VMAgentWithStatus for concrete vmv1beta1.VMAgent instances.
type vmAgentAdapter struct {
	*vmv1beta1.VMAgent
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
	// Wait for vmAgent to become ready
	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		vmAgentObj := &vmv1beta1.VMAgent{}
		namespacedName := types.NamespacedName{Name: vmAgent.GetName(), Namespace: vmAgent.GetNamespace()}
		err = rclient.Get(ctx, namespacedName, vmAgentObj)
		if err != nil {
			return false, err
		}
		return vmAgentObj.Status.UpdateStatus == vmv1beta1.UpdateStatusOperational, nil
	})
	if err != nil {
		return err
	}

	// Base values for fallback URL
	baseURL := vmAgent.AsURL()
	metricPath := vmAgent.GetMetricPath()

	var hosts []string
	// Poll until pod IPs are discovered
	err = wait.PollUntilContextTimeout(ctx, 1*time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		endpointList := &discoveryv1.EndpointSliceList{}
		if err := rclient.List(ctx, endpointList, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(labels.Set{"kubernetes.io/service-name": vmAgent.PrefixedName()}),
		}); err != nil {
			return false, err
		}
		if len(endpointList.Items) == 0 {
			return false, nil
		}
		hosts = parseEndpointSliceAddresses(&endpointList.Items[0])
		return len(hosts) > 0, nil
	})
	if err != nil {
		return err
	}

	// Poll until all pod IPs return empty query metric
	err = wait.PollUntilContextTimeout(ctx, 1*time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
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

func createOrUpdateOperatorVMUser(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributedCluster, scheme *runtime.Scheme, vmUserTargetRefs []vmv1beta1.TargetRef, vmClusters []*vmv1beta1.VMCluster, tenantID string) ([]*vmv1beta1.VMUser, error) {
	// Build the VMUser name
	namespacedName := types.NamespacedName{Name: cr.GetVMUserName(), Namespace: cr.Namespace}

	// Try to fetch existing VMUser
	vmUserObj := &vmv1beta1.VMUser{}
	vmUserExists := true
	if err := rclient.Get(ctx, namespacedName, vmUserObj); err != nil {
		if k8serrors.IsNotFound(err) {
			vmUserExists = false
			// Initialize a new VMUser for creation
			vmUserObj = &vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cr.GetVMUserName(),
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
			return nil, fmt.Errorf("failed to get VMUser %s/%s: %w", cr.Namespace, cr.GetVMUserName(), err)
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

	// Set owner reference to VMDistributedCluster
	_, err := setOwnerRefIfNeeded(cr, vmUserObj, scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to set owner reference for vmuser %s: %w", vmUserObj.Name, err)
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
