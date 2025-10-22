package vmdistributedcluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

const (
	VMAgentBufferMetricName = "vmagent_remotewrite_pending_data_bytes"
)

// CreateOrUpdate - handles VM deployment reconciliation.
func CreateOrUpdate(ctx context.Context, cr *vmv1alpha1.VMDistributedCluster, rclient client.Client, deadline, httpTimeout time.Duration) error {
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

	// Store current CR status
	currentCRStatus := cr.Status.DeepCopy()
	cr.Status = vmv1alpha1.VMDistributedClusterStatus{}

	for i, zone := range cr.Spec.Zones {
		if zone.Spec != nil && zone.Name == "" {
			return fmt.Errorf("VMClusterRefOrSpec.Name must be set when Spec is provided for zone at index %d", i)
		}
	}

	// Fetch VMCLuster statuses by name
	vmClusters, err := fetchVMClusters(ctx, rclient, cr.Name, cr.Namespace, cr.Spec.Zones)
	if err != nil {
		return fmt.Errorf("failed to fetch vmclusters: %w", err)
	}

	// Ensure that all vmusers have a read rule for vmcluster and record vmcluster info
	cr.Status.VMClusterInfo = make([]vmv1alpha1.VMClusterStatus, len(vmClusters))
	for i, vmCluster := range vmClusters {
		ref, err := findVMUserReadRuleForVMCluster(vmUserObjs, vmCluster)
		if err != nil {
			return fmt.Errorf("failed to find the rule for vmcluster %s: %w", vmCluster.Name, err)
		}
		cr.Status.VMClusterInfo[i] = vmv1alpha1.VMClusterStatus{
			VMClusterName: vmCluster.Name,
			Generation:    vmCluster.Generation,
			TargetRef:     *ref.DeepCopy(),
		}
	}
	// Compare generations of vmcluster objects from the spec with the previous CR
	if !reflect.DeepEqual(getGenerationsFromStatus(currentCRStatus), getGenerationsFromStatus(&cr.Status)) {
		// Record new generations but exit early if generations change is detected
		if err := rclient.Status().Update(ctx, cr); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
		return fmt.Errorf("unexpected generations change detected: %w", errors.New("unexpected generations change detected"))
	}

	httpClient := &http.Client{
		Timeout: httpTimeout,
	}

	// TODO[vrutkovs]: Mark all VMClusters as paused?
	// This would prevent other reconciliation loops from changing them
	for _, vmClusterObj := range vmClusters {
		// Check if VMCluster is already up-to-date
		if vmClusterObj.Spec.ClusterVersion == cr.Spec.ClusterVersion {
			continue
		}
		// Disable this VMCluster in vmusers
		setVMClusterStatusInVMUsers(ctx, rclient, cr, vmClusterObj, vmUserObjs, false)
		// Wait for VMAgent metrics to show no queue
		vmAgent := &vmAgentAdapter{VMAgent: vmAgentObj}
		waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, deadline)
		// Change VMCluster version and wait for it to be ready
		if err := changeVMClusterVersion(ctx, rclient, vmClusterObj, cr.Spec.ClusterVersion); err != nil {
			return fmt.Errorf("failed to change VMCluster %s/%s version: %w", vmClusterObj.Namespace, vmClusterObj.Name, err)
		}
		// Wait for VMCluster to be ready
		if err := waitForVMClusterReady(ctx, rclient, vmClusterObj, deadline); err != nil {
			return fmt.Errorf("failed to wait for VMCluster %s/%s to be ready: %w", vmClusterObj.Namespace, vmClusterObj.Name, err)
		}
		// Enable this VMCluster in vmusers
		setVMClusterStatusInVMUsers(ctx, rclient, cr, vmClusterObj, vmUserObjs, true)
	}
	return nil
}

func fetchVMClusters(ctx context.Context, rclient client.Client, crName, namespace string, refs []vmv1alpha1.VMClusterRefOrSpec) ([]*vmv1beta1.VMCluster, error) {
	vmClusters := make([]*vmv1beta1.VMCluster, len(refs))
	for i, vmClusterObjOrRef := range refs {
		vmClusterObj := &vmv1beta1.VMCluster{}
		var namespacedName types.NamespacedName

		if vmClusterObjOrRef.Ref != nil {
			// Case 1: Reference to an existing VMCluster
			if vmClusterObjOrRef.Ref.Name == "" {
				return nil, fmt.Errorf("VMClusterRefOrSpec.Ref.Name must be set for reference at index %d", i)
			}
			namespacedName = types.NamespacedName{Name: vmClusterObjOrRef.Ref.Name, Namespace: namespace}
			if err := rclient.Get(ctx, namespacedName, vmClusterObj); err != nil {
				if k8serrors.IsNotFound(err) {
					return nil, fmt.Errorf("referenced VMCluster %s/%s not found: %w", namespace, vmClusterObjOrRef.Ref.Name, err)
				}
				return nil, fmt.Errorf("failed to get referenced VMCluster %s/%s: %w", namespace, vmClusterObjOrRef.Ref.Name, err)
			}
		} else if vmClusterObjOrRef.Spec != nil {
			// Case 2: Inline VMCluster spec
			// Create a new VMCluster based on the provided spec
			// Create a new VMCluster based on the provided spec
			if vmClusterObjOrRef.Name == "" {
				return nil, fmt.Errorf("VMClusterRefOrSpec.Name must be set when Spec is provided for index %d", i)
			}
			vmClusterObj.ObjectMeta = metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", crName, vmClusterObjOrRef.Name),
				Namespace: namespace,
			}
			vmClusterObj.Spec = *vmClusterObjOrRef.Spec
			if err := rclient.Create(ctx, vmClusterObj); err != nil {
				return nil, fmt.Errorf("failed to create VMCluster from inline spec at index %d: %w", i, err)
			}
			namespacedName = types.NamespacedName{Name: vmClusterObj.Name, Namespace: namespace}

			// Reconcile the spec if it already exists and needs update
			if err := rclient.Get(ctx, namespacedName, vmClusterObj); err != nil {
				return nil, fmt.Errorf("failed to get newly created VMCluster %s/%s: %w", namespace, vmClusterObj.Name, err)
			}
			if !reflect.DeepEqual(vmClusterObj.Spec, *vmClusterObjOrRef.Spec) {
				vmClusterObj.Spec = *vmClusterObjOrRef.Spec
				if err := rclient.Update(ctx, vmClusterObj); err != nil {
					return nil, fmt.Errorf("failed to update VMCluster %s/%s from inline spec: %w", namespace, vmClusterObj.Name, err)
				}
			}

		} else {
			// Error: Neither Ref nor Spec is set
			return nil, fmt.Errorf("VMClusterRefOrSpec at index %d must have either Ref or Spec set, got: %+v", i, vmClusterObjOrRef)
		}
		vmClusters[i] = vmClusterObj
	}
	return vmClusters, nil
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

	// Extract vmselect service name and ensure it exists
	// svcName := vmCluster.GetVMSelectName()
	// svcObj := &corev1.Service{}
	// if err := rclient.Get(ctx, types.NamespacedName{Name: svcName, Namespace: vmCluster.Namespace}, svcObj); err != nil {
	// 	return fmt.Errorf("failed to find vmselect service %s for vmcluster %s: %w", svcName, vmCluster.Name, err)
	// }
	// // Extract vmuser rules and find matches to svcObj
	// for _, rule := range rules {
	// 	if rule.Type == vmv1beta1.VMUserRuleTypeRead && rule.ServiceName == svcObj.Name {
	// 		return nil
	// 	}
	// }
	// return fmt.Errorf("no matching read rule found for vmcluster %s", vmCluster.Name)
}

func setVMClusterStatusInVMUsers(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributedCluster, vmCluster *vmv1beta1.VMCluster, vmUserObjs []*vmv1beta1.VMUser, status bool) error {
	// Find matching rule in the vmdistributed status
	var found *vmv1beta1.TargetRef
	for _, vmClusterInfo := range cr.Status.VMClusterInfo {
		if vmClusterInfo.VMClusterName == vmCluster.Name {
			found = &vmClusterInfo.TargetRef
			break
		}
	}
	if found == nil {
		return fmt.Errorf("no matching rule found for vmcluster %s in status of vmdistributedcluster %s", vmCluster.Name, cr.Name)
	}

	// Update all vmusers with the matching rule
	for _, vmUserObj := range vmUserObjs {
		// Fetch fresh copy of vmuser
		freshVMUserObj := &vmv1beta1.VMUser{}
		if err := rclient.Get(ctx, types.NamespacedName{Name: vmUserObj.Name, Namespace: vmUserObj.Namespace}, freshVMUserObj); err != nil {
			return fmt.Errorf("failed to fetch vmuser %s: %w", vmUserObj.Name, err)
		}

		// Check if this vmuser has the matching rule
		hasMatchingRule := false
		for _, ref := range freshVMUserObj.Spec.TargetRefs {
			if reflect.DeepEqual(ref.CRD, found.CRD) && ref.TargetPathSuffix == found.TargetPathSuffix {
				hasMatchingRule = true
				break
			}
		}

		if !hasMatchingRule {
			continue // Skip vmusers that don't have this rule
		}

		// Prepare new target references list
		newTargetRefs := make([]vmv1beta1.TargetRef, 0)
		if status {
			newTargetRefs = append(newTargetRefs, freshVMUserObj.Spec.TargetRefs...)
			// Only add if not already present
			alreadyExists := false
			for _, ref := range newTargetRefs {
				if reflect.DeepEqual(ref.CRD, found.CRD) && ref.TargetPathSuffix == found.TargetPathSuffix {
					alreadyExists = true
					break
				}
			}
			if !alreadyExists {
				newTargetRefs = append(newTargetRefs, *found)
			}
		} else {
			for _, targetRef := range freshVMUserObj.Spec.TargetRefs {
				if reflect.DeepEqual(targetRef.CRD, found.CRD) && targetRef.TargetPathSuffix == found.TargetPathSuffix {
					continue
				}
				newTargetRefs = append(newTargetRefs, targetRef)
			}
		}

		// Update vmuser with new target references
		freshVMUserObj.Spec.TargetRefs = newTargetRefs
		if err := rclient.Update(ctx, freshVMUserObj); err != nil {
			return fmt.Errorf("failed to update vmuser %s: %w", freshVMUserObj.Name, err)
		}
	}

	return nil
}

func changeVMClusterVersion(ctx context.Context, rclient client.Client, vmCluster *vmv1beta1.VMCluster, version string) error {
	// Fetch VMCluster again as it might have been updated by another controller
	if err := rclient.Get(ctx, types.NamespacedName{Name: vmCluster.Name, Namespace: vmCluster.Namespace}, vmCluster); err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("VMCluster not found")
		}
		return fmt.Errorf("failed to fetch VMCluster %s/%s: %w", vmCluster.Namespace, vmCluster.Name, err)
	}
	vmCluster.Spec.ClusterVersion = version
	if err := rclient.Update(ctx, vmCluster); err != nil {
		return fmt.Errorf("failed to update VMCluster %s/%s: %w", vmCluster.Namespace, vmCluster.Name, err)
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
		value, found := strings.CutPrefix(metric, VMAgentBufferMetricName)
		if found {
			value = strings.Trim(value, " ")
			res, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return 0, fmt.Errorf("could not parse metric value %s: %w", metric, err)
			}
			return int64(res), nil
		}
	}
	return 0, fmt.Errorf("metric %s not found", VMAgentBufferMetricName)
}
