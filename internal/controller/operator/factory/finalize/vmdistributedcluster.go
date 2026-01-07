package finalize

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// OnVMDistributedClusterDelete removes all objects related to vmdistributedcluster component
func OnVMDistributedClusterDelete(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributedCluster) error {
	ns := cr.GetNamespace()
	objsToRemove := []client.Object{}
	if len(cr.Spec.VMAgent.Name) > 0 && cr.Spec.VMAgent.Spec != nil {
		vmAgentMeta := metav1.ObjectMeta{
			Namespace: ns,
			Name:      cr.Spec.VMAgent.Name,
		}
		objsToRemove = append(objsToRemove, &vmv1beta1.VMAgent{ObjectMeta: vmAgentMeta})
	}
	if len(cr.Spec.VMAuth.Name) > 0 {
		vmAuthLBPrefixedMeta := metav1.ObjectMeta{
			Namespace: ns,
			Name:      cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
		}
		objsToRemove = append(objsToRemove, &appsv1.Deployment{ObjectMeta: vmAuthLBPrefixedMeta})
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: vmAuthLBPrefixedMeta})
		objsToRemove = append(objsToRemove, &corev1.ServiceAccount{ObjectMeta: vmAuthLBPrefixedMeta})
		objsToRemove = append(objsToRemove, &corev1.Secret{ObjectMeta: vmAuthLBPrefixedMeta})
		objsToRemove = append(objsToRemove, &rbacv1.Role{ObjectMeta: vmAuthLBPrefixedMeta})
		objsToRemove = append(objsToRemove, &rbacv1.RoleBinding{ObjectMeta: vmAuthLBPrefixedMeta})
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: vmAuthLBPrefixedMeta})
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: vmAuthLBPrefixedMeta})
	}
	for _, vmclusterSpec := range cr.Spec.Zones.VMClusters {
		// Don't attempt to delete referenced or plain invalid clusters
		if vmclusterSpec.Ref != nil || len(vmclusterSpec.Name) == 0 {
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
	// Remove the CR
	return removeFinalizeObjByName(ctx, rclient, cr, cr.Name, cr.Namespace)
}
