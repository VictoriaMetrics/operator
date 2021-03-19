package factory

import (
	"context"
	"fmt"

	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
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

func mergeServiceSpec(svc *v1.Service, svcSpec *victoriametricsv1beta1.ServiceSpec) {
	if svcSpec == nil {
		return
	}
	// in case of labels, we must keep base labels to be able to discover this service later.
	svc.Labels = labels.Merge(svcSpec.Labels, svc.Labels)
	svc.Annotations = labels.Merge(svc.Annotations, svcSpec.Annotations)
	if svcSpec.Name != "" {
		svc.Name = svcSpec.Name
	}
	defaultSvc := svc.DeepCopy()
	svc.Spec = svcSpec.Spec
	if svc.Spec.Selector == nil {
		svc.Spec.Selector = defaultSvc.Spec.Selector
	}
	if svc.Spec.Ports == nil {
		svc.Spec.Ports = defaultSvc.Spec.Ports
	}
	if svc.Spec.Type == "" {
		svc.Spec.Type = defaultSvc.Spec.Type
	}
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

// invariants?
// if newService == nil, there is no option to remove it?
// check is delete?
// not sure, that its possible.
// probably, do not add finalizer and owner?
// seems to be bad option.
// or find related service by labels?
// we must perform service delete -> remove finalizer, delete service or vice versa?
// check invariant for clusterIP and ServiceType:
// clusterIP == None
// serviceType == ClusterIP, clusterIP may be changed.
// serviceType != serviceType -> recreate with variants.
func handleService(ctx context.Context, rclient client.Client, newService *v1.Service, shouldCheckClusterIP bool) (*v1.Service, error) {
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
			err := rclient.Create(ctx, newService)
			if err != nil {
				return nil, fmt.Errorf("cannot create new service: %w", err)
			}
			return newService, nil
		}
		return nil, fmt.Errorf("cannot get service for existing service: %w", err)
	}
	// lets save annotations even after recreate
	newService.Annotations = labels.Merge(newService.Annotations, existingService.Annotations)
	newService.Labels = labels.Merge(existingService.Labels, newService.Labels)
	if newService.Spec.Type != existingService.Spec.Type {
		// type mismatch.
		// need to remove it and recreate.
		if err := handleDelete(existingService); err != nil {
			return nil, err
		}
		// recursive call.
		fmt.Printf("not match,: %v, secon: %v\n", newService.Spec.Type, existingService.Spec.Type)
		return handleService(ctx, rclient, existingService, shouldCheckClusterIP)
	}
	if newService.Spec.Type == v1.ServiceTypeClusterIP {

	}
	// type= LoadBalancer and NodePort cannot have clusterIP: None
	if newService.Spec.ClusterIP != "" && newService.Spec.ClusterIP != "None" && newService.Spec.ClusterIP != existingService.Spec.ClusterIP {
		// ip was changed by user, remove old service and create new one.
		if err := handleDelete(existingService); err != nil {
			return nil, err
		}
		return handleService(ctx, rclient, newService, shouldCheckClusterIP)
	}
	if newService.Spec.ClusterIP == "None" && existingService.Spec.ClusterIP != "None" {
		if err := handleDelete(existingService); err != nil {
			return nil, err
		}
		return handleService(ctx, rclient, newService, shouldCheckClusterIP)
	}
	if newService.Spec.ClusterIP == "" && existingService.Spec.ClusterIP == "None" {
		if err := handleDelete(existingService); err != nil {
			return nil, err
		}
		return handleService(ctx, rclient, newService, shouldCheckClusterIP)
	}
	if newService.Spec.ClusterIP != "None" {
		newService.Spec.ClusterIP = existingService.Spec.ClusterIP
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

// remove missing services for given spec.
func reconcileMissingServices(ctx context.Context, rclient client.Client, instance svcBuilderArgs, spec *victoriametricsv1beta1.ServiceSpec) error {
	relatedSvc, err := discoverServicesByLalebes(ctx, rclient, instance, instance.SelectorLabels)
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
		additionalSvcName = spec.Name
	}
	cnt := 0
	// filter in-place
	for i := range relatedSvc {
		svc := relatedSvc[i]
		switch svc.Name {
		case instance.PrefixedName():
		case additionalSvcName:
		default:
			// service must be removed
			relatedSvc[cnt] = svc
			cnt++
		}
	}
	relatedSvc = relatedSvc[:cnt]
	for i := range relatedSvc {
		if err := handleDelete(relatedSvc[i]); err != nil {
			return err
		}
	}
	return nil
}

// need to find all existing services for given instance,
// to be able to reconcile it names later.
func discoverServicesByLalebes(ctx context.Context, rclient client.Client, instance svcBuilderArgs, selector func() map[string]string) ([]*v1.Service, error) {
	var svcs v1.ServiceList
	opts := client.ListOptions{
		Namespace:     instance.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(selector()),
	}
	if err := rclient.List(ctx, &svcs, &opts); err != nil {
	}
	resp := make([]*v1.Service, 0, len(svcs.Items))
	for i := range svcs.Items {
		resp = append(resp, &svcs.Items[i])
	}
	return resp, nil
}
