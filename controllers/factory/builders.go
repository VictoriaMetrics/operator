package factory

import (
	"context"
	"fmt"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/config"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func buildResources(crdResources v1.ResourceRequirements, defaultResources config.Resource, useDefault bool) v1.ResourceRequirements {
	if crdResources.Requests == nil {
		crdResources.Requests = v1.ResourceList{}
	}
	if crdResources.Limits == nil {
		crdResources.Limits = v1.ResourceList{}
	}

	var cpuResourceIsSet bool
	var memResourceIsSet bool

	if _, ok := crdResources.Limits[v1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := crdResources.Limits[v1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if _, ok := crdResources.Requests[v1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := crdResources.Requests[v1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}

	if !cpuResourceIsSet && useDefault {
		if defaultResources.Request.Cpu != config.UnLimitedResource {
			crdResources.Requests[v1.ResourceCPU] = resource.MustParse(defaultResources.Request.Cpu)
		}
		if defaultResources.Limit.Cpu != config.UnLimitedResource {
			crdResources.Limits[v1.ResourceCPU] = resource.MustParse(defaultResources.Limit.Cpu)
		}
	}
	if !memResourceIsSet && useDefault {
		if defaultResources.Request.Mem != config.UnLimitedResource {
			crdResources.Requests[v1.ResourceMemory] = resource.MustParse(defaultResources.Request.Mem)
		}
		if defaultResources.Limit.Mem != config.UnLimitedResource {
			crdResources.Limits[v1.ResourceMemory] = resource.MustParse(defaultResources.Limit.Mem)
		}
	}
	return crdResources
}

type svcBuilderArgs interface {
	client.Object
	PrefixedName() string
	Annotations() map[string]string
	Labels() map[string]string
	SelectorLabels() map[string]string
	AsOwner() []metav1.OwnerReference
	GetNSName() string
}

// mergeServiceSpec merges serviceSpec to the given services
// it should help to avoid boilerplate at CRD spec,
// base fields filled by operator.
func mergeServiceSpec(svc *v1.Service, svcSpec *victoriametricsv1beta1.ServiceSpec) {
	if svcSpec == nil {
		return
	}
	svc.Name = svcSpec.NameOrDefault(svc.Name)
	// in case of labels, we must keep base labels to be able to discover this service later.
	svc.Labels = labels.Merge(svcSpec.Labels, svc.Labels)
	svc.Annotations = labels.Merge(svc.Annotations, svcSpec.Annotations)
	defaultSvc := svc.DeepCopy()
	svc.Spec = svcSpec.Spec
	if svc.Spec.Selector == nil {
		svc.Spec.Selector = defaultSvc.Spec.Selector
	}
	// use may want to override port definition.
	if svc.Spec.Ports == nil {
		svc.Spec.Ports = defaultSvc.Spec.Ports
	}
	if svc.Spec.Type == "" {
		svc.Spec.Type = defaultSvc.Spec.Type
	}
	// note clusterIP not checked, its users responsibility.
}

func buildDefaultService(cr svcBuilderArgs, defaultPort string, setOptions func(svc *v1.Service)) *v1.Service {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.GetNSName(),
			Labels:          cr.Labels(),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Spec: v1.ServiceSpec{
			Type:     v1.ServiceTypeClusterIP,
			Selector: cr.SelectorLabels(),
			Ports: []v1.ServicePort{
				{
					Name:       "http",
					Protocol:   "TCP",
					Port:       intstr.Parse(defaultPort).IntVal,
					TargetPort: intstr.Parse(defaultPort),
				},
			},
		},
	}
	if setOptions != nil {
		setOptions(svc)
	}
	return svc
}

// reconcileServiceForCRD - reconcile needed and actual state of service for given crd,
// it will recreate service if needed.
// NOTE it doesn't perform validation:
// in case of spec.type= LoadBalancer or NodePort, clusterIP: None is not allowed,
// its users responsibility to define it correctly.
func reconcileServiceForCRD(ctx context.Context, rclient client.Client, newService *v1.Service) (*v1.Service, error) {
	// helper for proper service deletion.
	handleDelete := func(svc *v1.Service) error {
		if err := finalize.RemoveFinalizer(ctx, rclient, svc); err != nil {
			return err
		}
		return finalize.SafeDelete(ctx, rclient, svc)
	}
	existingService := &v1.Service{}
	err := rclient.Get(ctx, types.NamespacedName{Name: newService.Name, Namespace: newService.Namespace}, existingService)
	if err != nil {
		if errors.IsNotFound(err) {
			// service not exists, creating it.
			err := rclient.Create(ctx, newService)
			if err != nil {
				return nil, fmt.Errorf("cannot create new service: %w", err)
			}
			return newService, nil
		}
		return nil, fmt.Errorf("cannot get service for existing service: %w", err)
	}
	// lets save annotations and labels even after recreation.
	newService.Annotations = labels.Merge(existingService.Annotations, newService.Annotations)
	newService.Labels = labels.Merge(existingService.Labels, newService.Labels)
	if newService.Spec.Type != existingService.Spec.Type {
		// type mismatch.
		// need to remove it and recreate.
		if err := handleDelete(existingService); err != nil {
			return nil, err
		}
		// recursive call. operator reconciler must throttle it.
		return reconcileServiceForCRD(ctx, rclient, newService)
	}
	// invariants.
	if newService.Spec.ClusterIP != "" && newService.Spec.ClusterIP != "None" && newService.Spec.ClusterIP != existingService.Spec.ClusterIP {
		// ip was changed by user, remove old service and create new one.
		if err := handleDelete(existingService); err != nil {
			return nil, err
		}
		return reconcileServiceForCRD(ctx, rclient, newService)
	}
	// existing service isn't None
	if newService.Spec.ClusterIP == "None" && existingService.Spec.ClusterIP != "None" {
		if err := handleDelete(existingService); err != nil {
			return nil, err
		}
		return reconcileServiceForCRD(ctx, rclient, newService)
	}
	// make service non-headless.
	if newService.Spec.ClusterIP == "" && existingService.Spec.ClusterIP == "None" {
		if err := handleDelete(existingService); err != nil {
			return nil, err
		}
		return reconcileServiceForCRD(ctx, rclient, newService)
	}
	// keep given clusterIP for service.
	if newService.Spec.ClusterIP != "None" {
		newService.Spec.ClusterIP = existingService.Spec.ClusterIP
	}

	// need to keep allocated node ports.
	if newService.Spec.Type == existingService.Spec.Type {
		// there is no need in optimization, it should be fast enough.
		for i := range existingService.Spec.Ports {
			existPort := existingService.Spec.Ports[i]
			for j := range newService.Spec.Ports {
				newPort := &newService.Spec.Ports[j]
				// add missing port, only if its not defined by user.
				if existPort.Name == newPort.Name && newPort.NodePort == 0 {
					newPort.NodePort = existPort.NodePort
					break
				}
			}
		}
	}
	if existingService.ResourceVersion != "" {
		newService.ResourceVersion = existingService.ResourceVersion
	}

	newService.Finalizers = victoriametricsv1beta1.MergeFinalizers(existingService, victoriametricsv1beta1.FinalizerName)

	err = rclient.Update(ctx, newService)
	if err != nil {
		return nil, fmt.Errorf("cannot update vmalert server: %w", err)
	}

	return newService, nil
}

type rSvcArgs struct {
	PrefixedName   func() string
	SelectorLabels func() map[string]string
	GetNameSpace   func() string
}

// removeOrphanedServices removes services that no longer belongs to given crd by its args.
func removeOrphanedServices(ctx context.Context, rclient client.Client, args rSvcArgs, spec *victoriametricsv1beta1.ServiceSpec) error {
	svcsToRemove, err := discoverServicesByLabels(ctx, rclient, args)
	if err != nil {
		return err
	}
	handleDelete := func(s *v1.Service) error {
		if err := finalize.RemoveFinalizer(ctx, rclient, s); err != nil {
			return err
		}
		return finalize.SafeDelete(ctx, rclient, s)
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
func discoverServicesByLabels(ctx context.Context, rclient client.Client, args rSvcArgs) ([]*v1.Service, error) {
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
