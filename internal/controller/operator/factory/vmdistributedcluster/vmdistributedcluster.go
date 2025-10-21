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

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	"github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	VMAgentBufferMetricName = "vmagent_remotewrite_pending_data_bytes"
)

type VMClusterInfo struct {
	VMCluster *vmv1beta1.VMCluster
	VMAgent   *vmv1beta1.VMAgent
}

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

	// Fetch global loadbalancing vmuser
	vmUserObj, err := fetchVMUser(ctx, rclient, cr.Namespace, cr.Spec.VMUser)
	if err != nil {
		return fmt.Errorf("failed to fetch global loadbalancing vmuser: %w", err)
	}

	// Store current CR status
	currentCRStatus := cr.Status.DeepCopy()
	cr.Status = vmv1alpha1.VMDistributedClusterStatus{}

	// Fetch VMCLuster statuses by name
	vmClusters, err := fetchVMClusters(ctx, rclient, cr.Namespace, cr.Spec.VMClusters)
	if err != nil {
		return fmt.Errorf("failed to fetch vmclusters: %w", err)
	}

	// Ensure that all vmuser has a read rule for vmcluster and record vmcluster info
	cr.Status.VMClusterInfo = make([]vmv1alpha1.VMClusterStatus, len(vmClusters))
	for i, vmClusterAgentPair := range vmClusters {
		vmCluster := vmClusterAgentPair.VMCluster
		ref, err := findVMUserReadRuleForVMCluster(vmUserObj, vmCluster)
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
	for _, vmClusterAgentPair := range vmClusters {
		vmClusterObj := vmClusterAgentPair.VMCluster
		// Check if VMCluster is already up-to-date
		if vmClusterObj.Spec.ClusterVersion == cr.Spec.ClusterVersion {
			continue
		}
		// Disable this VMCluster in vmuser
		setVMClusterStatusInVMUser(ctx, rclient, cr, vmClusterObj, vmUserObj, false)
		// Wait for VMCluster's vmagent metrics to show no queue
		var vmAgent VMAgentWithStatus
		if vmClusterAgentPair.VMAgent != nil {
			vmAgent = &vmAgentAdapter{VMAgent: vmClusterAgentPair.VMAgent}
		}
		waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, deadline)
		// Change VMCluster version and wait for it to be ready
		if err := changeVMClusterVersion(ctx, rclient, vmClusterObj, cr.Spec.ClusterVersion); err != nil {
			return fmt.Errorf("failed to change VMCluster %s/%s version: %w", vmClusterObj.Namespace, vmClusterObj.Name, err)
		}
		// Wait for VMCluster to be ready
		if err := waitForVMClusterReady(ctx, rclient, vmClusterObj, deadline); err != nil {
			return fmt.Errorf("failed to wait for VMCluster %s/%s to be ready: %w", vmClusterObj.Namespace, vmClusterObj.Name, err)
		}
		// Enable this VMCluster in vmuser
		setVMClusterStatusInVMUser(ctx, rclient, cr, vmClusterObj, vmUserObj, true)
	}
	return nil
}

func fetchVMClusters(ctx context.Context, rclient client.Client, namespace string, refs []vmv1alpha1.VMClusterAgentPair) (vmClusters []VMClusterInfo, err error) {
	vmClusters = make([]VMClusterInfo, len(refs))
	var vmClusterObj *vmv1beta1.VMCluster
	for i, vmClusterAgentPair := range refs {
		vmClusterObj = &vmv1beta1.VMCluster{}
		namespacedName := types.NamespacedName{Name: vmClusterAgentPair.Name, Namespace: namespace}
		if err := rclient.Get(ctx, namespacedName, vmClusterObj); err != nil {
			return nil, fmt.Errorf("failed to get VMCluster %s/%s: %w", namespace, vmClusterAgentPair.Name, err)
		}
		vmClusterInfo := VMClusterInfo{
			VMCluster: vmClusterObj,
		}
		if vmClusterAgentPair.VMAgent != nil {
			vmAgent, err := fetchVMAgent(ctx, rclient, namespace, *vmClusterAgentPair.VMAgent)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch VMAgent %s/%s for VMCluster %s/%s: %w", vmClusterObj.Namespace, vmClusterObj.Name, vmClusterObj.Namespace, vmClusterObj.Name, err)
			}
			vmClusterInfo.VMAgent = vmAgent
		}
		vmClusters[i] = vmClusterInfo
	}
	return vmClusters, nil
}

func fetchVMUser(ctx context.Context, rclient client.Client, namespace string, ref corev1.LocalObjectReference) (*vmv1beta1.VMUser, error) {
	if ref.Name == "" {
		return nil, errors.New("global loadbalancing vmuser is not specified")
	}
	vmUserObj := &vmv1beta1.VMUser{}
	namespacedName := types.NamespacedName{Name: ref.Name, Namespace: namespace}
	if err := rclient.Get(ctx, namespacedName, vmUserObj); err != nil {
		return nil, fmt.Errorf("failed to get VMUser %s/%s: %w", namespace, ref.Name, err)
	}
	return vmUserObj, nil
}

func fetchVMAgent(ctx context.Context, rclient client.Client, namespace string, ref corev1.LocalObjectReference) (*vmv1beta1.VMAgent, error) {
	if ref.Name == "" {
		return nil, errors.New("vmagent name is not specified")
	}
	vmAgentObj := &vmv1beta1.VMAgent{}
	namespacedName := types.NamespacedName{Name: ref.Name, Namespace: namespace}
	if err := rclient.Get(ctx, namespacedName, vmAgentObj); err != nil {
		return nil, fmt.Errorf("failed to get VMAgent %s/%s: %w", namespace, ref.Name, err)
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

func findVMUserReadRuleForVMCluster(vmUserObj *vmv1beta1.VMUser, vmCluster *v1beta1.VMCluster) (*vmv1beta1.TargetRef, error) {
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
	// 2. Match static url to vmselect service
	// TODO[vrutkovs]: match static url to vmselect service
	return nil, fmt.Errorf("vmuser %s has no target refs", vmUserObj.Name)

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

func setVMClusterStatusInVMUser(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributedCluster, vmCluster *vmv1beta1.VMCluster, vmUserObj *vmv1beta1.VMUser, status bool) error {
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

	// Fetch fresh copy of vmuser
	if err := rclient.Get(ctx, types.NamespacedName{Name: vmUserObj.Name, Namespace: vmUserObj.Namespace}, vmUserObj); err != nil {
		return fmt.Errorf("failed to fetch vmuser %s: %w", vmUserObj.Name, err)
	}

	// Prepare new target references list
	newTargetRefs := make([]vmv1beta1.TargetRef, 0)
	if status {
		newTargetRefs = append(newTargetRefs, vmUserObj.Spec.TargetRefs...)
		newTargetRefs = append(newTargetRefs, *found)
	} else {
		for _, targetRef := range vmUserObj.Spec.TargetRefs {
			if reflect.DeepEqual(targetRef.CRD, found.CRD) && targetRef.TargetPathSuffix == found.TargetPathSuffix {
				continue
			}
			newTargetRefs = append(newTargetRefs, targetRef)
		}
	}

	// Update vmuser with new target references
	vmUserObj.Spec.TargetRefs = newTargetRefs
	if err := rclient.Update(ctx, vmUserObj); err != nil {
		return fmt.Errorf("failed to update vmuser %s: %w", vmUserObj.Name, err)
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
