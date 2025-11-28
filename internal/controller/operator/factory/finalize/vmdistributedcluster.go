package finalize

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// OnVMDistributedClusterDelete removes all objects related to vmdistributedcluster component
func OnVMDistributedClusterDelete(ctx context.Context, rclient client.Client, obj *vmv1alpha1.VMDistributedCluster) error {
	// Remove created VMAgent
	vmagent := &vmv1beta1.VMAgent{}
	if err := rclient.Get(ctx, client.ObjectKey{Name: obj.Spec.VMAgent.Name, Namespace: obj.Namespace}, vmagent); err == nil {
		if err := rclient.Delete(ctx, vmagent); err != nil {
			return err
		}
	}

	// Remove created VMAuth
	vmauth := &vmv1beta1.VMAuth{}
	if err := rclient.Get(ctx, client.ObjectKey{Name: obj.Spec.VMAuth.Name, Namespace: obj.Namespace}, vmauth); err == nil {
		if err := rclient.Delete(ctx, vmauth); err != nil {
			return err
		}
	}

	// Remove created VMUser
	vmuser := &vmv1beta1.VMUser{}
	if err := rclient.Get(ctx, client.ObjectKey{Name: obj.GetVMUserName(), Namespace: obj.Namespace}, vmuser); err == nil {
		if err := rclient.Delete(ctx, vmuser); err != nil {
			return err
		}
	}

	// Remove created VMClusters
	for _, vmclusterSpec := range obj.Spec.Zones.VMClusters {
		if vmclusterSpec.Ref != nil {
			continue
		}
		vmcluster := &vmv1beta1.VMCluster{}
		if err := rclient.Get(ctx, client.ObjectKey{Name: vmclusterSpec.Name, Namespace: obj.Namespace}, vmcluster); err == nil {
			if err := rclient.Delete(ctx, vmcluster); err != nil {
				return err
			}
		}
	}

	return nil
}
