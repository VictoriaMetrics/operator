package finalize

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// OnVMInsertDelete removes all objects related to vminsert component
func OnVMInsertDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster, obj *vmv1beta1.VMInsert) error {
	objMeta := metav1.ObjectMeta{
		Namespace: cr.Namespace,
		Name:      cr.GetVMInsertName(),
	}
	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
	}
	if obj.ServiceSpec != nil && !obj.ServiceSpec.UseAsDefault {
		objsToRemove = append(objsToRemove, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cr.Namespace,
				Name:      obj.ServiceSpec.NameOrDefault(cr.GetVMInsertName()),
			},
		})
	}
	if obj.PodDisruptionBudget != nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta})
	}
	if obj.HPA != nil {
		objsToRemove = append(objsToRemove, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: objMeta})
	}
	if !ptr.Deref(obj.DisableSelfServiceScrape, false) {
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta})
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: metav1.ObjectMeta{Name: cr.GetVMInsertLBName(), Namespace: cr.Namespace}})
	}

	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: cr.GetVMInsertLBName(), Namespace: cr.Namespace}})
	}
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnVMInsertDelete removes all objects related to vminsert component
func OnVMSelectDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster, obj *vmv1beta1.VMSelect) error {
	objMeta := metav1.ObjectMeta{
		Namespace: cr.Namespace,
		Name:      cr.GetVMSelectName(),
	}
	objsToRemove := []client.Object{
		&appsv1.StatefulSet{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
	}
	if obj.ServiceSpec != nil && !obj.ServiceSpec.UseAsDefault {
		objsToRemove = append(objsToRemove, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cr.Namespace,
				Name:      obj.ServiceSpec.NameOrDefault(cr.GetVMSelectName()),
			},
		})
	}
	if obj.PodDisruptionBudget != nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta})
	}
	if obj.HPA != nil {
		objsToRemove = append(objsToRemove, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: objMeta})
	}
	if !ptr.Deref(obj.DisableSelfServiceScrape, false) {
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta})
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: metav1.ObjectMeta{Name: cr.GetVMSelectLBName(), Namespace: cr.Namespace}})
	}
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: cr.GetVMSelectLBName(), Namespace: cr.Namespace}})
	}
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnVMInsertDelete removes all objects related to vminsert component
func OnVMStorageDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster, obj *vmv1beta1.VMStorage) error {
	objMeta := metav1.ObjectMeta{
		Namespace: cr.Namespace,
		Name:      cr.GetVMStorageName(),
	}
	objsToRemove := []client.Object{
		&appsv1.StatefulSet{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
	}
	if obj.ServiceSpec != nil && !obj.ServiceSpec.UseAsDefault {
		objsToRemove = append(objsToRemove, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cr.Namespace,
				Name:      obj.ServiceSpec.NameOrDefault(cr.GetVMStorageName()),
			},
		})
	}
	if obj.PodDisruptionBudget != nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta})
	}
	if !ptr.Deref(obj.DisableSelfServiceScrape, false) {
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta})
	}

	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnVMClusterDelete deletes all vmcluster related resources
func OnVMClusterDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster) error {
	// check deployment

	if cr.Spec.VMInsert != nil {
		if err := OnVMInsertDelete(ctx, rclient, cr, cr.Spec.VMInsert); err != nil {
			return fmt.Errorf("cannot remove vminsert component objects: %w", err)
		}
	}

	if cr.Spec.VMSelect != nil {
		if err := OnVMSelectDelete(ctx, rclient, cr, cr.Spec.VMSelect); err != nil {
			return fmt.Errorf("cannot remove vmselect component objects: %w", err)
		}
	}
	if cr.Spec.VMStorage != nil {
		if err := OnVMStorageDelete(ctx, rclient, cr, cr.Spec.VMStorage); err != nil {
			return fmt.Errorf("cannot remove vmstorage component objects: %w", err)
		}
	}

	if err := deleteSA(ctx, rclient, cr); err != nil {
		return err
	}
	if cr.Spec.RequestsLoadBalancer.Enabled {
		if err := OnVMClusterLoadBalancerDelete(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot delete vmcluster loadbalancer components: %w", err)
		}
	}
	return removeFinalizeObjByName(ctx, rclient, cr, cr.Name, cr.Namespace)
}

// OnVMClusterLoadBalancerDelete removes vmauth loadbalancer components for vmcluster
func OnVMClusterLoadBalancerDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster) error {
	lbMeta := metav1.ObjectMeta{
		Namespace: cr.Namespace,
		Name:      cr.GetVMAuthLBName(),
	}

	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: lbMeta},
		&corev1.Secret{ObjectMeta: lbMeta},
		&corev1.Service{ObjectMeta: lbMeta},
	}
	if !ptr.Deref(cr.Spec.RequestsLoadBalancer.Spec.DisableSelfServiceScrape, false) {
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: lbMeta})
	}
	if cr.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget != nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: lbMeta})
	}

	if cr.Spec.VMSelect != nil {
		if !ptr.Deref(cr.Spec.VMSelect.DisableSelfServiceScrape, false) {
			objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{Name: cr.GetVMSelectLBName(), Namespace: cr.Namespace}})
		}
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetVMSelectLBName(),
			Namespace: cr.Namespace,
		}})
	}
	if cr.Spec.VMInsert != nil {
		if !ptr.Deref(cr.Spec.VMInsert.DisableSelfServiceScrape, false) {
			objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{Name: cr.GetVMInsertLBName(), Namespace: cr.Namespace}})
		}
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetVMInsertLBName(),
			Namespace: cr.Namespace,
		}})
	}

	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove); err != nil {
			return fmt.Errorf("failed to remove lb object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}

	return nil
}
