package finalize

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type orphanedCRD interface {
	SelectorLabels() map[string]string
	GetNamespace() string
}

// RemoveOrphanedDeployments removes deployments detached from given object
func RemoveOrphanedDeployments(ctx context.Context, rclient client.Client, cr orphanedCRD, keepNames map[string]struct{}) error {
	gvk := schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}
	return removeOrphaned(ctx, rclient, cr, gvk, keepNames)
}

// RemoveOrphanedSTSs removes deployments detached from given object
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

// removeOrphaned removes orphaned resources
func removeOrphaned(ctx context.Context, rclient client.Client, cr orphanedCRD, gvk schema.GroupVersionKind, keepNames map[string]struct{}) error {
	toRemove, err := getOrphaned(ctx, rclient, gvk, cr)
	if err != nil {
		return err
	}
	for i := range toRemove {
		item := toRemove[i]
		if _, ok := keepNames[item.GetName()]; !ok {
			// need to remove
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

// getOrphaned - returns resources by orphaned CR selector.
func getOrphaned(ctx context.Context, rclient client.Client, gvk schema.GroupVersionKind, cr orphanedCRD) ([]client.Object, error) {
	var l unstructured.UnstructuredList
	l.SetGroupVersionKind(gvk)
	opts := client.ListOptions{
		Namespace:     cr.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(cr.SelectorLabels()),
	}
	if err := rclient.List(ctx, &l, &opts); err != nil {
		return nil, err
	}
	resp := make([]client.Object, 0, len(l.Items))
	for i := range l.Items {
		item := &l.Items[i]
		resp = append(resp, item)
	}
	return resp, nil
}
