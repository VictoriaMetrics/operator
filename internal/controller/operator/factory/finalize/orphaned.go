package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type orphanedCRD interface {
	SelectorLabels() map[string]string
	GetNamespace() string
}

// RemoveOrphanedDeployments removes deployments detached from given object
func RemoveOrphanedDeployments(ctx context.Context, rclient client.Client, cr orphanedCRD, keepDeployments map[string]struct{}) error {
	deployToRemove, err := discoverDeploymentsByLabels(ctx, rclient, cr.GetNamespace(), cr.SelectorLabels())
	if err != nil {
		return err
	}
	for i := range deployToRemove {
		dep := deployToRemove[i]
		if _, ok := keepDeployments[dep.Name]; !ok {
			// need to remove
			if err := RemoveFinalizer(ctx, rclient, dep); err != nil {
				return err
			}
			if err := SafeDelete(ctx, rclient, dep); err != nil {
				return err
			}
		}
	}
	return nil
}

// discoverDeploymentsByLabels - returns deployments with given args.
func discoverDeploymentsByLabels(ctx context.Context, rclient client.Client, ns string, selector map[string]string) ([]*appsv1.Deployment, error) {
	var deps appsv1.DeploymentList
	opts := client.ListOptions{
		Namespace:     ns,
		LabelSelector: labels.SelectorFromSet(selector),
	}
	if err := rclient.List(ctx, &deps, &opts); err != nil {
		return nil, err
	}
	resp := make([]*appsv1.Deployment, 0, len(deps.Items))
	for i := range deps.Items {
		resp = append(resp, &deps.Items[i])
	}
	return resp, nil
}

// RemoveSvcArgs defines interface for service deletion
type RemoveSvcArgs struct {
	PrefixedName   func() string
	SelectorLabels func() map[string]string
	GetNameSpace   func() string
}

// RemoveOrphanedSTSs removes deployments detached from given object
func RemoveOrphanedSTSs(ctx context.Context, rclient client.Client, cr orphanedCRD, keepSTSNames map[string]struct{}) error {
	deployToRemove, err := discoverSTSsByLabels(ctx, rclient, cr.GetNamespace(), cr.SelectorLabels())
	if err != nil {
		return err
	}
	for i := range deployToRemove {
		dep := deployToRemove[i]
		if _, ok := keepSTSNames[dep.Name]; !ok {
			// need to remove
			if err := RemoveFinalizer(ctx, rclient, dep); err != nil {
				return err
			}
			if err := SafeDelete(ctx, rclient, dep); err != nil {
				return err
			}
		}
	}
	return nil
}

// discoverDeploymentsByLabels - returns deployments with given args.
func discoverSTSsByLabels(ctx context.Context, rclient client.Client, ns string, selector map[string]string) ([]*appsv1.StatefulSet, error) {
	var deps appsv1.StatefulSetList
	opts := client.ListOptions{
		Namespace:     ns,
		LabelSelector: labels.SelectorFromSet(selector),
	}
	if err := rclient.List(ctx, &deps, &opts); err != nil {
		return nil, err
	}
	resp := make([]*appsv1.StatefulSet, 0, len(deps.Items))
	for i := range deps.Items {
		resp = append(resp, &deps.Items[i])
	}
	return resp, nil
}
