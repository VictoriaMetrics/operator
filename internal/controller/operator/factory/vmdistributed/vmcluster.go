package vmdistributed

import (
	"context"
	"fmt"
	"sort"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// fetchVMClusters ensures that referenced VMClusters are fetched and validated.
func fetchVMClusters(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed) ([]*vmv1beta1.VMCluster, error) {
	logger.WithContext(ctx).Info("Fetching VMClusters")

	vmClusters := make([]*vmv1beta1.VMCluster, len(cr.Spec.Zones))
	for i := range cr.Spec.Zones {
		zone := &cr.Spec.Zones[i]
		objOrRef := zone.VMCluster.DeepCopy()
		if objOrRef == nil {
			objOrRef = &vmv1alpha1.VMClusterObjOrRef{}
		}
		commonObjOrRef := cr.Spec.CommonZone.VMCluster.DeepCopy()
		if commonObjOrRef != nil && commonObjOrRef.Spec != nil {
			commonObjOrRef.Spec = nil
		}
		if len(zone.Name) > 0 {
			var err error
			commonObjOrRef, err = k8stools.RenderPlaceholders(commonObjOrRef, map[string]string{
				zonePlaceholder: zone.Name,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to render common cluster: %w", err)
			}
			if _, err = mergeDeep(objOrRef, commonObjOrRef); err != nil {
				return nil, fmt.Errorf("failed to merge common zone spec into vmcluster at index %d: %w", i, err)
			}
		}
		var vmCluster vmv1beta1.VMCluster
		switch {
		case objOrRef.IsRefSet():
			nsn := types.NamespacedName{Name: objOrRef.Ref.Name, Namespace: cr.Namespace}
			if err := rclient.Get(ctx, nsn, &vmCluster); err != nil {
				return nil, fmt.Errorf("failed to get referenced vmclusters[%d]=%s: %w", i, nsn.String(), err)
			}
		case objOrRef.IsNameSet():
			// We try to fetch the object to get the current state (Generation, etc)
			nsn := types.NamespacedName{Name: objOrRef.Name, Namespace: cr.Namespace}
			if err := rclient.Get(ctx, nsn, &vmCluster); err != nil {
				if k8serrors.IsNotFound(err) {
					vmCluster.Name = objOrRef.Name
					vmCluster.Namespace = cr.Namespace
					if objOrRef.Spec != nil {
						vmCluster.Spec = *objOrRef.Spec.DeepCopy()
					}
				} else {
					return nil, fmt.Errorf("unexpected error while fetching vmclusters[%d]=%s: %w", i, nsn.String(), err)
				}
			}
		default:
			return nil, fmt.Errorf("invalid VMClusterObjOrRef at index %d: neither Ref nor Name is set", i)
		}
		if err := cr.Owns(&vmCluster); err != nil {
			return nil, fmt.Errorf("failed to validate owner references for unreferenced vmcluster %s: %w", vmCluster.Name, err)
		}
		vmClusters[i] = &vmCluster
	}

	// Sort VMClusters by observedGeneration in descending order (biggest first)
	sort.Slice(vmClusters, func(i, j int) bool {
		return vmClusters[i].Status.ObservedGeneration > vmClusters[j].Status.ObservedGeneration
	})

	logger.WithContext(ctx).Info(fmt.Sprintf("Found %d VMClusters", len(vmClusters)))
	return vmClusters, nil
}

// waitForVMClusterToReachStatus waits till VMCluster reaches defined status
func waitForVMClusterToReachStatus(ctx context.Context, rclient client.Client, vmCluster *vmv1beta1.VMCluster, deadline time.Duration, status vmv1beta1.UpdateStatus) error {
	var lastStatus vmv1beta1.UpdateStatus
	nsn := types.NamespacedName{Name: vmCluster.Name, Namespace: vmCluster.Namespace}
	err := wait.PollUntilContextTimeout(ctx, time.Second, deadline, true, func(ctx context.Context) (done bool, err error) {
		if err = rclient.Get(ctx, nsn, vmCluster); err != nil {
			if k8serrors.IsNotFound(err) {
				err = nil
			}
			return
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
