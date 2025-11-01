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

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func OnVLClusterDelete(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster) error {

	if cr.Spec.VLInsert != nil {
		if err := OnVLInsertDelete(ctx, rclient, cr, cr.Spec.VLInsert); err != nil {
			return fmt.Errorf("cannot remove insert component objects: %w", err)
		}
	}

	if cr.Spec.VLSelect != nil {
		if err := OnVLSelectDelete(ctx, rclient, cr, cr.Spec.VLSelect); err != nil {
			return fmt.Errorf("cannot remove select component objects: %w", err)
		}
	}
	if cr.Spec.VLStorage != nil {
		if err := OnVLStorageDelete(ctx, rclient, cr, cr.Spec.VLStorage); err != nil {
			return fmt.Errorf("cannot remove storage component objects: %w", err)
		}
	}

	if err := deleteSA(ctx, rclient, cr); err != nil {
		return err
	}
	if cr.Spec.RequestsLoadBalancer.Enabled {
		if err := OnVLClusterLoadBalancerDelete(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot delete cluster loadbalancer components: %w", err)
		}
	}
	return removeFinalizeObjByName(ctx, rclient, cr, cr.Name, cr.Namespace)

}

// OnVLInsertDelete removes all objects related to vlinsert component
func OnVLInsertDelete(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster, obj *vmv1.VLInsert) error {
	objMeta := metav1.ObjectMeta{
		Namespace: cr.Namespace,
		Name:      cr.GetVLInsertName(),
	}
	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
	}
	if obj.ServiceSpec != nil && !obj.ServiceSpec.UseAsDefault {
		objsToRemove = append(objsToRemove, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cr.Namespace,
				Name:      obj.ServiceSpec.NameOrDefault(cr.GetVLInsertName()),
			},
		})
	}
	if obj.PodDisruptionBudget != nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta})
	}
	if obj.HPA != nil {
		objsToRemove = append(objsToRemove, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: objMeta})
	}
	if !ptr.Deref(obj.DisableSelfServiceScrape, getCfg().DisableSelfServiceScrapeCreation) {
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta})
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: metav1.ObjectMeta{Name: cr.GetVLInsertLBName(), Namespace: cr.Namespace}})
	}

	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: cr.GetVLInsertLBName(), Namespace: cr.Namespace}})
	}
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnVLInsertDelete removes all objects related to vlinsert component
func OnVLSelectDelete(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster, obj *vmv1.VLSelect) error {
	objMeta := metav1.ObjectMeta{
		Namespace: cr.Namespace,
		Name:      cr.GetVLSelectName(),
	}
	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
	}
	if obj.ServiceSpec != nil && !obj.ServiceSpec.UseAsDefault {
		objsToRemove = append(objsToRemove, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cr.Namespace,
				Name:      obj.ServiceSpec.NameOrDefault(cr.GetVLSelectName()),
			},
		})
	}
	if obj.PodDisruptionBudget != nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta})
	}
	if obj.HPA != nil {
		objsToRemove = append(objsToRemove, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: objMeta})
	}
	if !ptr.Deref(obj.DisableSelfServiceScrape, getCfg().DisableSelfServiceScrapeCreation) {
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta})
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: metav1.ObjectMeta{Name: cr.GetVLSelectLBName(), Namespace: cr.Namespace}})
	}
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: cr.GetVLSelectLBName(), Namespace: cr.Namespace}})
	}
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnVLInsertDelete removes all objects related to vlinsert component
func OnVLStorageDelete(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster, obj *vmv1.VLStorage) error {
	objMeta := metav1.ObjectMeta{
		Namespace: cr.Namespace,
		Name:      cr.GetVLStorageName(),
	}
	objsToRemove := []client.Object{
		&appsv1.StatefulSet{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
	}
	if obj.ServiceSpec != nil && !obj.ServiceSpec.UseAsDefault {
		objsToRemove = append(objsToRemove, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cr.Namespace,
				Name:      obj.ServiceSpec.NameOrDefault(cr.GetVLStorageName()),
			},
		})
	}
	if obj.PodDisruptionBudget != nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta})
	}
	if !ptr.Deref(obj.DisableSelfServiceScrape, getCfg().DisableSelfServiceScrapeCreation) {
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta})
	}

	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnVLClusterLoadBalancerDelete removes vmauth loadbalancer components for vlcluster
func OnVLClusterLoadBalancerDelete(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster) error {
	lbMeta := metav1.ObjectMeta{
		Namespace: cr.Namespace,
		Name:      cr.GetVMAuthLBName(),
	}

	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: lbMeta},
		&corev1.Secret{ObjectMeta: lbMeta},
		&corev1.Service{ObjectMeta: lbMeta},
	}
	if !ptr.Deref(cr.Spec.RequestsLoadBalancer.Spec.DisableSelfServiceScrape, getCfg().DisableSelfServiceScrapeCreation) {
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: lbMeta})
	}
	if cr.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget != nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: lbMeta})
	}

	if cr.Spec.VLSelect != nil {
		if !ptr.Deref(cr.Spec.VLSelect.DisableSelfServiceScrape, getCfg().DisableSelfServiceScrapeCreation) {
			objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{Name: cr.GetVLSelectLBName(), Namespace: cr.Namespace}})
		}
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetVLSelectLBName(),
			Namespace: cr.Namespace,
		}})
	}
	if cr.Spec.VLInsert != nil {
		if !ptr.Deref(cr.Spec.VLInsert.DisableSelfServiceScrape, getCfg().DisableSelfServiceScrapeCreation) {
			objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{Name: cr.GetVLInsertLBName(), Namespace: cr.Namespace}})
		}
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetVLInsertLBName(),
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
