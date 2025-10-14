package vmdistributedcluster

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateOrUpdate - handles VM deployment reconciliation.
func CreateOrUpdate(ctx context.Context, cr *vmv1alpha1.VMDistributedCluster, rclient client.Client) error {
	// Store the previous CR for comparison
	var prevCR *vmv1alpha1.VMDistributedCluster
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}

	// Fetch global loadbalancing vmauth
	vmauthObj, err := fetchVMAuth(ctx, rclient, cr.Spec.VMAuth)
	if err != nil {
		return fmt.Errorf("failed to fetch global loadbalancing vmauth: %w", err)
	}
	if vmauthObj == nil || vmauthObj.IsUnmanaged() {
		return errors.New("global loadbalancing vmauth is not managed")
	}

	// Fetch VMCLuster statuses by name
	vmClusters, err := fetchVMClusters(ctx, rclient, cr.Spec.VMClusters)
	if err != nil {
		return err
	}

	// Collect generations of vmcluster objects from the spec
	generations := make(map[string]int64, len(vmClusters))
	for _, vmCluster := range vmClusters {
		generations[vmCluster.Name] = vmCluster.Generation
	}

	// Compare generations of vmcluster objects from the spec with the previous CR
	if !reflect.DeepEqual(generations, getGenerationsFromStatus(cr.Status)) {
		if err := recordGenerations(ctx, rclient, cr, generations); err != nil {
			return err
		}
		// Exit early if generations change is detected
		// TODO[vrutkovs]: wrap error
		return fmt.Errorf("unexpected generations change detected: %w", errors.New("unexpected generations change detected"))
	}

	// TODO[vrutkovs]: Mark all VMClusters as paused?
	// This would prevent other reconciliation loops from changing them
	for _, vmClusterObj := range vmClusters {
		// Check if VMCluster is already up-to-date
		if vmClusterObj.Spec.ClusterVersion == cr.Spec.ClusterVersion {
			continue
		}
		// Disable this VMCluster in vmauth
		setVMClusterStatusInVMAuth(ctx, rclient, vmauthObj, &vmClusterObj, false)
		// Wait for VMCluster's vmagent metrics to show no queue
		waitForVMClusterVMAgentMetrics(ctx, rclient, &vmClusterObj)
		// Change VMCluster version and wait for it to be ready
		if err := changeVMClusterVersion(ctx, rclient, &vmClusterObj, cr.Spec.ClusterVersion); err != nil {
			return fmt.Errorf("failed to change VMCluster %s version: %w", vmClusterObj.Name, err)
		}
		// Enable this VMCluster in vmauth
		setVMClusterStatusInVMAuth(ctx, rclient, vmauthObj, &vmClusterObj, true)
	}
	return nil
}

func fetchVMClusters(ctx context.Context, rclient client.Client, vmClusterSpecs []vmv1beta1.VMCluster) (vmClusters []vmv1beta1.VMCluster, err error) {
	vmClusters = make([]vmv1beta1.VMCluster, len(vmClusterSpecs))
	var vmClusterObj *vmv1beta1.VMCluster
	for i, vmCluster := range vmClusterSpecs {
		vmClusterObj = &vmv1beta1.VMCluster{}
		if err := rclient.Get(ctx, types.NamespacedName{Name: vmCluster.Name, Namespace: vmCluster.Namespace}, vmClusterObj); err != nil {
			return nil, fmt.Errorf("failed to get VMCluster %s: %w", vmCluster.Name, err)
		}
		vmClusters[i] = *vmClusterObj
	}
	return vmClusters, nil
}

func fetchVMAuth(ctx context.Context, rclient client.Client, vmauth *vmv1beta1.VMAuth) (*vmv1beta1.VMAuth, error) {
	if err := rclient.Get(ctx, types.NamespacedName{Name: vmauth.Name, Namespace: vmauth.Namespace}, vmauth); err != nil {
		return nil, fmt.Errorf("failed to get VMAuth %s: %w", vmauth.Name, err)
	}
	return vmauth, nil
}

func getGenerationsFromStatus(status vmv1alpha1.VMDistributedClusterStatus) map[string]int64 {
	generations := make(map[string]int64, len(status.VMClusterGenerations))
	for _, vmClusterPair := range status.VMClusterGenerations {
		generations[vmClusterPair.VMClusterName] = vmClusterPair.Generation
	}
	return generations
}

func recordGenerations(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributedCluster, generations map[string]int64) error {
	cr.Status.VMClusterGenerations = make([]vmv1alpha1.VMClusterGenerationPair, 0, len(generations))
	for name, generation := range generations {
		cr.Status.VMClusterGenerations = append(cr.Status.VMClusterGenerations, vmv1alpha1.VMClusterGenerationPair{
			VMClusterName: name,
			Generation:    generation,
		})
	}
	if err := rclient.Status().Update(ctx, cr); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}
	return nil
}

func setVMClusterStatusInVMAuth(ctx context.Context, rclient client.Client, vmauth *vmv1beta1.VMAuth, vmCluster *vmv1beta1.VMCluster, status bool) error {
	time.Sleep(time.Second)
	return nil
}

func waitForVMClusterVMAgentMetrics(ctx context.Context, rclient client.Client, vmCluster *vmv1beta1.VMCluster) error {
	time.Sleep(time.Second)
	return nil
}

func changeVMClusterVersion(ctx context.Context, rclient client.Client, vmCluster *vmv1beta1.VMCluster, version string) error {
	vmCluster.Spec.ClusterVersion = version
	if err := rclient.Update(ctx, vmCluster); err != nil {
		return fmt.Errorf("failed to update VMCluster version: %w", err)
	}
	time.Sleep(time.Second)
	return nil
}
