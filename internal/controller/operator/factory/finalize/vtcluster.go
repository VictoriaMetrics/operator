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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// OnVTClusterDelete removes all child objects and releases finalizers
func OnVTClusterDelete(ctx context.Context, rclient client.Client, cr *vmv1.VTCluster) error {

	if cr.Spec.Insert != nil {
		if err := OnVTInsertDelete(ctx, rclient, cr, cr.Spec.Insert); err != nil {
			return fmt.Errorf("cannot remove insert component objects: %w", err)
		}
	}

	if cr.Spec.Select != nil {
		if err := OnVTSelectDelete(ctx, rclient, cr, cr.Spec.Select); err != nil {
			return fmt.Errorf("cannot remove select component objects: %w", err)
		}
	}
	if cr.Spec.Storage != nil {
		if err := OnVTStorageDelete(ctx, rclient, cr, cr.Spec.Storage); err != nil {
			return fmt.Errorf("cannot remove storage component objects: %w", err)
		}
	}

	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentRoot)
	if err := deleteSA(ctx, rclient, b); err != nil {
		return err
	}
	if cr.Spec.RequestsLoadBalancer.Enabled {
		if err := OnVTClusterLoadBalancerDelete(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot delete cluster loadbalancer components: %w", err)
		}
	}
	return removeFinalizeObjByName(ctx, rclient, cr, cr.Name, cr.Namespace)

}

// OnVTInsertDelete removes all objects related to vtinsert component
func OnVTInsertDelete(ctx context.Context, rclient client.Client, cr *vmv1.VTCluster, obj *vmv1.VTInsert) error {
	commonName := cr.PrefixedName(vmv1beta1.ClusterComponentInsert)
	commonInternalName := cr.PrefixedInternalName(vmv1beta1.ClusterComponentInsert)
	objMeta := metav1.ObjectMeta{
		Namespace: cr.Namespace,
		Name:      commonName,
	}
	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
	}
	if obj.ServiceSpec != nil && !obj.ServiceSpec.UseAsDefault {
		objsToRemove = append(objsToRemove, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cr.Namespace,
				Name:      obj.ServiceSpec.NameOrDefault(commonName),
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
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: metav1.ObjectMeta{Name: commonInternalName, Namespace: cr.Namespace}})
	}

	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: commonInternalName, Namespace: cr.Namespace}})
	}
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnVTInsertDelete removes all objects related to vtinsert component
func OnVTSelectDelete(ctx context.Context, rclient client.Client, cr *vmv1.VTCluster, obj *vmv1.VTSelect) error {
	commonName := cr.PrefixedName(vmv1beta1.ClusterComponentSelect)
	commonInternalName := cr.PrefixedInternalName(vmv1beta1.ClusterComponentSelect)
	objMeta := metav1.ObjectMeta{
		Namespace: cr.Namespace,
		Name:      commonName,
	}
	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
	}
	if obj.ServiceSpec != nil && !obj.ServiceSpec.UseAsDefault {
		objsToRemove = append(objsToRemove, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cr.Namespace,
				Name:      obj.ServiceSpec.NameOrDefault(commonName),
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
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: metav1.ObjectMeta{Name: commonInternalName, Namespace: cr.Namespace}})
	}
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: commonInternalName, Namespace: cr.Namespace}})
	}
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnVTInsertDelete removes all objects related to vtinsert component
func OnVTStorageDelete(ctx context.Context, rclient client.Client, cr *vmv1.VTCluster, obj *vmv1.VTStorage) error {
	commonName := cr.PrefixedName(vmv1beta1.ClusterComponentStorage)
	objMeta := metav1.ObjectMeta{
		Namespace: cr.Namespace,
		Name:      commonName,
	}
	objsToRemove := []client.Object{
		&appsv1.StatefulSet{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
	}
	if obj.ServiceSpec != nil && !obj.ServiceSpec.UseAsDefault {
		objsToRemove = append(objsToRemove, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cr.Namespace,
				Name:      obj.ServiceSpec.NameOrDefault(commonName),
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

// OnVTClusterLoadBalancerDelete removes vmauth loadbalancer components for vtcluster
func OnVTClusterLoadBalancerDelete(ctx context.Context, rclient client.Client, cr *vmv1.VTCluster) error {
	commonName := cr.PrefixedName(vmv1beta1.ClusterComponentBalancer)
	objMeta := metav1.ObjectMeta{
		Namespace: cr.Namespace,
		Name:      commonName,
	}

	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&corev1.Secret{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
	}
	if !ptr.Deref(cr.Spec.RequestsLoadBalancer.Spec.DisableSelfServiceScrape, getCfg().DisableSelfServiceScrapeCreation) {
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta})
	}
	if cr.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget != nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta})
	}

	if cr.Spec.Select != nil {
		name := cr.PrefixedInternalName(vmv1beta1.ClusterComponentSelect)
		if !ptr.Deref(cr.Spec.Select.DisableSelfServiceScrape, getCfg().DisableSelfServiceScrapeCreation) {
			objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cr.Namespace}})
		}
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cr.Namespace,
		}})
	}
	if cr.Spec.Insert != nil {
		name := cr.PrefixedInternalName(vmv1beta1.ClusterComponentInsert)
		if !ptr.Deref(cr.Spec.Insert.DisableSelfServiceScrape, getCfg().DisableSelfServiceScrapeCreation) {
			objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cr.Namespace}})
		}
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      name,
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
