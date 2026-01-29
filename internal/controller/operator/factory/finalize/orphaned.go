package finalize

import (
	"context"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

type orphanedCRD interface {
	AsOwner() metav1.OwnerReference
	SelectorLabels() map[string]string
	GetNamespace() string
}

// RemoveOrphanedDeployments removes Deployments detached from given object
func RemoveOrphanedDeployments(ctx context.Context, rclient client.Client, cr orphanedCRD, keepNames map[string]struct{}) error {
	gvk := schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames)
}

// RemoveOrphanedSTSs removes StatefulSets detached from given object
func RemoveOrphanedSTSs(ctx context.Context, rclient client.Client, cr orphanedCRD, keepNames map[string]struct{}) error {
	gvk := schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "StatefulSet",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames)
}

// RemoveOrphanedPDBs removes PDBs detached from given object
func RemoveOrphanedPDBs(ctx context.Context, rclient client.Client, cr orphanedCRD, keepNames map[string]struct{}) error {
	gvk := schema.GroupVersionKind{
		Group:   "policy",
		Version: "v1",
		Kind:    "PodDisruptionBudget",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames)
}

// RemoveOrphanedServices removes Services detached from given object
func RemoveOrphanedServices(ctx context.Context, rclient client.Client, cr orphanedCRD, keepNames map[string]struct{}) error {
	gvk := schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Service",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames)
}

// RemoveOrphanedHPAs removes HPAs detached from given object
func RemoveOrphanedHPAs(ctx context.Context, rclient client.Client, cr orphanedCRD, keepNames map[string]struct{}) error {
	gvk := schema.GroupVersionKind{
		Group:   "autoscaling",
		Version: "v2",
		Kind:    "HorizontalPodAutoscaler",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames)

}

// RemoveOrphanedVMServiceScrapes removes VMSeviceScrapes detached from given object
func RemoveOrphanedVMServiceScrapes(ctx context.Context, rclient client.Client, cr orphanedCRD, keepNames map[string]struct{}) error {
	if build.IsControllerDisabled("VMServiceScrape") {
		return nil
	}
	gvk := schema.GroupVersionKind{
		Group:   "operator.victoriametrics.com",
		Version: "v1beta1",
		Kind:    "VMServiceScrape",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames)

}

// RemoveOrphanedVMPodScrapes removes VMPodScrapes detached from given object
func RemoveOrphanedVMPodScrapes(ctx context.Context, rclient client.Client, cr orphanedCRD, keepNames map[string]struct{}) error {
	if build.IsControllerDisabled("VMPodScrape") {
		return nil
	}
	gvk := schema.GroupVersionKind{
		Group:   "operator.victoriametrics.com",
		Version: "v1beta1",
		Kind:    "VMPodScrape",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames)

}

// removeOrphaned removes orphaned resources
func removeOrphaned(ctx context.Context, rclient client.Client, cr orphanedCRD, gvk schema.GroupVersionKind, keepNames map[string]struct{}) error {
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
	for i := range l.Items {
		item := &l.Items[i]
		if _, ok := keepNames[item.GetName()]; !ok && canBeRemoved(item, &owner) {
			if err := RemoveFinalizer(ctx, rclient, item); err != nil {
				return err
			}
			if err := SafeDelete(ctx, rclient, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func canBeRemoved(o client.Object, owner *metav1.OwnerReference) bool {
	if owner == nil || len(o.GetNamespace()) == 0 {
		return slices.Contains(o.GetFinalizers(), vmv1beta1.FinalizerName)
	}
	owners := o.GetOwnerReferences()
	return slices.ContainsFunc(owners, func(r metav1.OwnerReference) bool {
		return r.APIVersion == owner.APIVersion && r.Kind == owner.Kind && r.Name == owner.Name
	})
}
