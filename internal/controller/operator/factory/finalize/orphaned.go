package finalize

import (
	"context"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// RemoveOrphanedDeployments removes Deployments detached from given object
func RemoveOrphanedDeployments(ctx context.Context, rclient client.Client, cr crObject, keepNames sets.Set[string], shouldRemove bool) error {
	gvk := schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames, shouldRemove)
}

// RemoveOrphanedSTSs removes Deployments detached from given object
func RemoveOrphanedSTSs(ctx context.Context, rclient client.Client, cr crObject, keepNames sets.Set[string], shouldRemove bool) error {
	gvk := schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "StatefulSet",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames, shouldRemove)
}

// RemoveOrphanedConfigMaps removes ConfigMaps detached from given object
func RemoveOrphanedConfigMaps(ctx context.Context, rclient client.Client, cr crObject, keepNames sets.Set[string], shouldRemove bool) error {
	gvk := schema.GroupVersionKind{
		Version: "v1",
		Kind:    "ConfigMap",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames, shouldRemove)
}

// RemoveOrphanedPDBs removes PDBs detached from given object
func RemoveOrphanedPDBs(ctx context.Context, rclient client.Client, cr crObject, keepNames sets.Set[string], shouldRemove bool) error {
	gvk := schema.GroupVersionKind{
		Group:   "policy",
		Version: "v1",
		Kind:    "PodDisruptionBudget",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames, shouldRemove)
}

// RemoveOrphanedServices removes Services detached from given object
func RemoveOrphanedServices(ctx context.Context, rclient client.Client, cr crObject, keepNames sets.Set[string], shouldRemove bool) error {
	gvk := schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Service",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames, shouldRemove)
}

// RemoveOrphanedHPAs removes HPAs detached from given object
func RemoveOrphanedHPAs(ctx context.Context, rclient client.Client, cr crObject, keepNames sets.Set[string], shouldRemove bool) error {
	gvk := schema.GroupVersionKind{
		Group:   "autoscaling",
		Version: "v2",
		Kind:    "HorizontalPodAutoscaler",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames, shouldRemove)

}

// RemoveOrphanedVPAs removes VPAs detached from given object
func RemoveOrphanedVPAs(ctx context.Context, rclient client.Client, cr crObject, keepNames sets.Set[string], shouldRemove bool) error {
	cfg := config.MustGetBaseConfig()
	if !cfg.VPAAPIEnabled {
		return nil
	}
	gvk := schema.GroupVersionKind{
		Group:   "autoscaling.k8s.io",
		Version: "v1",
		Kind:    "VerticalPodAutoscaler",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames, shouldRemove)
}

// RemoveOrphanedVMServiceScrapes removes VMSeviceScrapes detached from given object
func RemoveOrphanedVMServiceScrapes(ctx context.Context, rclient client.Client, cr crObject, keepNames sets.Set[string], shouldRemove bool) error {
	if build.IsControllerDisabled("VMServiceScrape") {
		return nil
	}
	gvk := schema.GroupVersionKind{
		Group:   "operator.victoriametrics.com",
		Version: "v1beta1",
		Kind:    "VMServiceScrape",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames, shouldRemove)

}

// RemoveOrphanedVMPodScrapes removes VMPodScrapes detached from given object
func RemoveOrphanedVMPodScrapes(ctx context.Context, rclient client.Client, cr crObject, keepNames sets.Set[string], shouldRemove bool) error {
	if build.IsControllerDisabled("VMPodScrape") {
		return nil
	}
	gvk := schema.GroupVersionKind{
		Group:   "operator.victoriametrics.com",
		Version: "v1beta1",
		Kind:    "VMPodScrape",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames, shouldRemove)
}

// removeOrphaned removes orphaned resources
func removeOrphaned(ctx context.Context, rclient client.Client, cr crObject, gvk schema.GroupVersionKind, keepNames sets.Set[string], shouldRemove bool) error {
	var l unstructured.UnstructuredList
	l.SetGroupVersionKind(gvk)
	opts := client.ListOptions{
		Namespace:     cr.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(cr.SelectorLabels()),
	}
	if err := rclient.List(ctx, &l, &opts); err != nil {
		return err
	}
	owner := cr.AsOwner()
	selector := cr.SelectorLabels()
	for i := range l.Items {
		item := &l.Items[i]
		if !keepNames.Has(item.GetName()) && canBeRemoved(item, selector, &owner) {
			if err := RemoveFinalizer(ctx, rclient, item); err != nil {
				return err
			}
			if !shouldRemove {
				continue
			}
			if err := SafeDelete(ctx, rclient, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func isSubset(set map[string]string, subset map[string]string) bool {
	for k, v := range subset {
		if sv, ok := set[k]; !ok || sv != v {
			return false
		}
	}
	return true
}

func canBeRemoved(o client.Object, selector map[string]string, owner *metav1.OwnerReference) bool {
	if owner == nil || len(o.GetNamespace()) == 0 {
		return isSubset(o.GetLabels(), selector)
	}
	owners := o.GetOwnerReferences()
	return slices.ContainsFunc(owners, func(r metav1.OwnerReference) bool {
		return r.APIVersion == owner.APIVersion && r.Kind == owner.Kind && r.Name == owner.Name
	})
}
