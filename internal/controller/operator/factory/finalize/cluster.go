package finalize

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
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
		&vpav1.VerticalPodAutoscaler{ObjectMeta: objMeta},
	}
	owner := cr.AsOwner()
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove, &owner); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentInsert)
	if err := RemoveOrphanedVMServiceScrapes(ctx, rclient, b, nil); err != nil {
		return fmt.Errorf("cannot remove orphaned serviceScrapes: %w", err)
	}
	return nil
}

// OnSelectDelete removes all objects related to select component
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
		&vpav1.VerticalPodAutoscaler{ObjectMeta: objMeta},
	}
	owner := cr.AsOwner()
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove, &owner); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
	if err := RemoveOrphanedVMServiceScrapes(ctx, rclient, b, nil); err != nil {
		return fmt.Errorf("cannot remove orphaned serviceScrapes: %w", err)
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
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: objMeta},
		&vpav1.VerticalPodAutoscaler{ObjectMeta: objMeta},
	}
	owner := cr.AsOwner()
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove, &owner); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentStorage)
	if err := RemoveOrphanedVMServiceScrapes(ctx, rclient, b, nil); err != nil {
		return fmt.Errorf("cannot remove orphaned serviceScrapes: %w", err)
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
		&policyv1.PodDisruptionBudget{ObjectMeta: objMeta},
	}
	owner := cr.AsOwner()
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove, &owner); err != nil {
			return fmt.Errorf("failed to remove lb object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentBalancer)
	if err := RemoveOrphanedVMServiceScrapes(ctx, rclient, b, nil); err != nil {
		return fmt.Errorf("cannot remove orphaned serviceScrapes: %w", err)
	}
	return nil
}

// ChildCleaner cleans dependent resources for cluster CRs excluding ones
// which are listed in cleaner maps
type ChildCleaner struct {
	pdbs     map[string]struct{}
	hpas     map[string]struct{}
	vpas     map[string]struct{}
	services map[string]struct{}
	scrapes  map[string]struct{}
}

// NewChildCleaner initializes ChildCleaner
func NewChildCleaner() *ChildCleaner {
	return &ChildCleaner{
		pdbs:     make(map[string]struct{}),
		hpas:     make(map[string]struct{}),
		vpas:     make(map[string]struct{}),
		services: make(map[string]struct{}),
		scrapes:  make(map[string]struct{}),
	}
}

// KeepPDB adds given PodDisruptionBudget's name to a map of resource names to be excluded from deletion
func (cc *ChildCleaner) KeepPDB(v string) {
	cc.pdbs[v] = struct{}{}
}

// KeepHPA adds given HorizontalPodAutoscaler's name to a map of resource names to be excluded from deletion
func (cc *ChildCleaner) KeepHPA(v string) {
	cc.hpas[v] = struct{}{}
}

// KeepVPA adds given VerticalPodAutoscaler's name to a map of resource names to be excluded from deletion
func (cc *ChildCleaner) KeepVPA(v string) {
	cc.vpas[v] = struct{}{}
}

// KeepService adds given HorizontalPodAutoscaler's name to a map of resource names to be excluded from deletion
func (cc *ChildCleaner) KeepService(v string) {
	cc.services[v] = struct{}{}
}

// KeepScrape adds given VMServiceScrape's name to a map of resource names to be excluded from deletion
func (cc *ChildCleaner) KeepScrape(v string) {
	cc.scrapes[v] = struct{}{}
}

// RemoveOrphaned removes cr dependent resources excluding ones, which are defined in cleaner's maps
func (cc *ChildCleaner) RemoveOrphaned(ctx context.Context, rclient client.Client, cr build.ParentOpts) error {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentCommon)
	if err := RemoveOrphanedPDBs(ctx, rclient, b, cc.pdbs); err != nil {
		return fmt.Errorf("cannot remove orphaned PDBs: %w", err)
	}
	if err := RemoveOrphanedHPAs(ctx, rclient, b, cc.hpas); err != nil {
		return fmt.Errorf("cannot remove orphaned HPAs: %w", err)
	}
	if err := RemoveOrphanedVPAs(ctx, rclient, b, cc.vpas); err != nil {
		return fmt.Errorf("cannot remove orphaned VPAs: %w", err)
	}
	if err := RemoveOrphanedVMServiceScrapes(ctx, rclient, b, cc.scrapes); err != nil {
		return fmt.Errorf("cannot remove orphaned vmservicescrapes: %w", err)
	}
	if err := RemoveOrphanedServices(ctx, rclient, b, cc.services); err != nil {
		return fmt.Errorf("cannot remove orphaned services: %w", err)
	}
	return nil
}
