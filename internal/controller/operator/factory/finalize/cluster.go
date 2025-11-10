package finalize

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func OnClusterDelete(ctx context.Context, rclient client.Client, cr build.ParentOpts) error {
	if err := OnClusterLoadBalancerDelete(ctx, rclient, cr); err != nil {
		return fmt.Errorf("cannot delete cluster loadbalancer components: %w", err)
	}
	if err := OnInsertDelete(ctx, rclient, cr); err != nil {
		return fmt.Errorf("cannot remove insert component objects: %w", err)
	}

	if err := OnSelectDelete(ctx, rclient, cr); err != nil {
		return fmt.Errorf("cannot remove select component objects: %w", err)
	}
	if err := OnStorageDelete(ctx, rclient, cr); err != nil {
		return fmt.Errorf("cannot remove storage component objects: %w", err)
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentRoot)
	if err := deleteSA(ctx, rclient, b); err != nil {
		return err
	}
	ls := b.SelectorLabels()
	delete(ls, "app.kubernetes.io/name")
	b.SetSelectorLabels(ls)
	if err := RemoveOrphanedServices(ctx, rclient, b, nil); err != nil {
		return fmt.Errorf("cannot remove orphaned services: %w", err)
	}
	return removeFinalizeObjByName(ctx, rclient, cr, cr.GetName(), cr.GetNamespace())

}

// OnInsertDelete removes all objects related to insert component
func OnInsertDelete(ctx context.Context, rclient client.Client, cr build.ParentOpts) error {
	ns := cr.GetNamespace()
	objMeta := metav1.ObjectMeta{
		Namespace: ns,
		Name:      cr.PrefixedName(vmv1beta1.ClusterComponentInsert),
	}
	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&policyv1.PodDisruptionBudget{ObjectMeta: objMeta},
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: objMeta},
		&vmv1beta1.VMServiceScrape{ObjectMeta: objMeta},
		&vmv1beta1.VMServiceScrape{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.PrefixedInternalName(vmv1beta1.ClusterComponentInsert),
			Namespace: ns,
		}},
	}
	owner := cr.AsOwner()
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove, &owner); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnInsertDelete removes all objects related to insert component
func OnSelectDelete(ctx context.Context, rclient client.Client, cr build.ParentOpts) error {
	ns := cr.GetNamespace()
	objMeta := metav1.ObjectMeta{
		Namespace: ns,
		Name:      cr.PrefixedName(vmv1beta1.ClusterComponentSelect),
	}
	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&appsv1.StatefulSet{ObjectMeta: objMeta},
		&policyv1.PodDisruptionBudget{ObjectMeta: objMeta},
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: objMeta},
		&vmv1beta1.VMServiceScrape{ObjectMeta: objMeta},
		&vmv1beta1.VMServiceScrape{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.PrefixedInternalName(vmv1beta1.ClusterComponentSelect),
			Namespace: ns,
		}},
	}
	owner := cr.AsOwner()
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove, &owner); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnStorageDelete removes all objects related to storage component
func OnStorageDelete(ctx context.Context, rclient client.Client, cr build.ParentOpts) error {
	ns := cr.GetNamespace()
	objMeta := metav1.ObjectMeta{
		Namespace: ns,
		Name:      cr.PrefixedName(vmv1beta1.ClusterComponentStorage),
	}
	objsToRemove := []client.Object{
		&appsv1.StatefulSet{ObjectMeta: objMeta},
		&policyv1.PodDisruptionBudget{ObjectMeta: objMeta},
		&vmv1beta1.VMServiceScrape{ObjectMeta: objMeta},
	}
	owner := cr.AsOwner()
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove, &owner); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnClusterLoadBalancerDelete removes vmauth loadbalancer components for cluster
func OnClusterLoadBalancerDelete(ctx context.Context, rclient client.Client, cr build.ParentOpts) error {
	ns := cr.GetNamespace()
	objMeta := metav1.ObjectMeta{
		Namespace: ns,
		Name:      cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
	}
	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&corev1.Secret{ObjectMeta: objMeta},
		&vmv1beta1.VMServiceScrape{ObjectMeta: objMeta},
		&policyv1.PodDisruptionBudget{ObjectMeta: objMeta},
		&vmv1beta1.VMServiceScrape{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.PrefixedInternalName(vmv1beta1.ClusterComponentSelect),
			Namespace: ns,
		}},
		&vmv1beta1.VMServiceScrape{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.PrefixedInternalName(vmv1beta1.ClusterComponentInsert),
			Namespace: ns,
		}},
	}
	owner := cr.AsOwner()
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove, &owner); err != nil {
			return fmt.Errorf("failed to remove lb object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}
