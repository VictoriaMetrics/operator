package finalize

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// OnVMDistributedClusterDelete removes all objects related to vmdistributedcluster component
func OnVMDistributedClusterDelete(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributedCluster) error {
	ns := cr.GetNamespace()
	objsToRemove := []client.Object{}
	objsToDisown := []client.Object{}
	if len(cr.Spec.VMAgent.Name) > 0 && cr.Spec.VMAgent.Spec != nil {
		vmAgentMeta := metav1.ObjectMeta{
			Namespace: ns,
			Name:      cr.Spec.VMAgent.Name,
		}
		objsToRemove = append(objsToRemove, &vmv1beta1.VMAgent{ObjectMeta: vmAgentMeta})
	}
	if len(cr.Spec.VMAuth.Name) > 0 {
		vmAuthMeta := metav1.ObjectMeta{
			Namespace: ns,
			Name:      cr.Spec.VMAuth.Name,
		}
		objsToRemove = append(objsToRemove, &vmv1beta1.VMAuth{ObjectMeta: vmAuthMeta})
	}
	for _, vmclusterSpec := range cr.Spec.Zones.VMClusters {
		// Don't attempt to delete referenced or plain invalid clusters
		if len(vmclusterSpec.Name) == 0 {
			continue
		}
		if vmclusterSpec.Ref != nil {
			objsToDisown = append(objsToDisown, &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vmclusterSpec.Name,
					Namespace: cr.Namespace,
				},
			})
			continue
		}
		vmcluster := &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmclusterSpec.Name,
				Namespace: cr.Namespace,
			},
		}
		objsToRemove = append(objsToRemove, vmcluster)
	}
	owner := cr.AsOwner()
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove, &owner); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	for _, objToDisown := range objsToDisown {
		objToDisown.SetOwnerReferences([]metav1.OwnerReference{})
		if err := rclient.Update(ctx, objToDisown); err != nil {
			return fmt.Errorf("failed to disown object=%s: %w", objToDisown.GetObjectKind().GroupVersionKind(), err)
		}
	}
	// Remove the CR
	return removeFinalizeObjByName(ctx, rclient, cr, cr.Name, cr.Namespace)
}
