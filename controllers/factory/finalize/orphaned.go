package finalize

import (
	"context"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	v1 "k8s.io/api/core/v1"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type orphanedCRD interface {
	SelectorLabels() map[string]string
	GetNSName() string
}

// RemoveOrphanedDeployments removes deployments detached from given object
func RemoveOrphanedDeployments(ctx context.Context, rclient client.Client, cr orphanedCRD, keepDeployments map[string]struct{}) error {
	deployToRemove, err := discoverDeploymentsByLabels(ctx, rclient, cr.GetNSName(), cr.SelectorLabels())
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

type RemoveSvcArgs struct {
	PrefixedName   func() string
	SelectorLabels func() map[string]string
	GetNameSpace   func() string
}

// RemoveOrphanedServices removes services that no longer belongs to given crd by its args.
func RemoveOrphanedServices(ctx context.Context, rclient client.Client, args RemoveSvcArgs, spec *victoriametricsv1beta1.ServiceSpec) error {
	svcsToRemove, err := discoverServicesByLabels(ctx, rclient, args)
	if err != nil {
		return err
	}
	handleDelete := func(s *v1.Service) error {
		if err := RemoveFinalizer(ctx, rclient, s); err != nil {
			return err
		}
		return SafeDelete(ctx, rclient, s)
	}

	var additionalSvcName string
	if spec != nil {
		additionalSvcName = spec.NameOrDefault(args.PrefixedName())
	}
	cnt := 0
	// filter in-place,
	// keep services that doesn't match prefixedName and additional serviceName.
	for i := range svcsToRemove {
		svc := svcsToRemove[i]
		switch svc.Name {
		case args.PrefixedName():
		case additionalSvcName:
		default:
			// service must be removed
			svcsToRemove[cnt] = svc
			cnt++
		}
	}
	// remove left services.
	svcsToRemove = svcsToRemove[:cnt]
	for i := range svcsToRemove {
		if err := handleDelete(svcsToRemove[i]); err != nil {
			return err
		}
	}
	return nil
}

// discoverServicesByLabels - returns services with given args.
func discoverServicesByLabels(ctx context.Context, rclient client.Client, args RemoveSvcArgs) ([]*v1.Service, error) {
	var svcs v1.ServiceList
	opts := client.ListOptions{
		Namespace:     args.GetNameSpace(),
		LabelSelector: labels.SelectorFromSet(args.SelectorLabels()),
	}
	if err := rclient.List(ctx, &svcs, &opts); err != nil {
		return nil, err
	}
	resp := make([]*v1.Service, 0, len(svcs.Items))
	for i := range svcs.Items {
		resp = append(resp, &svcs.Items[i])
	}
	return resp, nil
}

// RemoveOrphanedSTSs removes deployments detached from given object
func RemoveOrphanedSTSs(ctx context.Context, rclient client.Client, cr orphanedCRD, keepSTSNames map[string]struct{}) error {
	deployToRemove, err := discoverSTSsByLabels(ctx, rclient, cr.GetNSName(), cr.SelectorLabels())
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
