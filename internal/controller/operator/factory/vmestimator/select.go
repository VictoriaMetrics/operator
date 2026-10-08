package vmestimator

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const selectKind = vmv1beta1.ClusterComponentSelect

func createOrUpdateSelect(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) error {
	// select nodes are deployed only in cluster mode
	if !cr.IsClusterMode() || cr.Spec.Select == nil {
		return nil
	}
	var prev componentObjects
	if prevCR != nil && (!prevCR.IsClusterMode() || prevCR.Spec.Select == nil) {
		prevCR = nil
	}
	if prevCR != nil {
		ps := prevCR.Spec.Select
		prev = componentObjects{pdb: ps.PodDisruptionBudget, np: ps.NetworkPolicy, vpa: ps.VPA}
	}
	s := cr.Spec.Select
	cur := componentObjects{pdb: s.PodDisruptionBudget, np: s.NetworkPolicy, vpa: s.VPA}
	if err := createOrUpdateComponentObjects(ctx, rclient, cr, prevCR, selectKind, "Deployment", cur, prev); err != nil {
		return err
	}
	if err := createOrUpdateSelectHPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateSelectService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	return createOrUpdateSelectDeployment(ctx, rclient, cr, prevCR)
}

func createOrUpdateSelectHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) error {
	if cr.Spec.Select.HPA == nil {
		return nil
	}
	b := build.NewChildBuilder(cr, selectKind)
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       b.PrefixedName(),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	newHPA := build.HPA(b, targetRef, cr.Spec.Select.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.Select.HPA != nil {
		b = build.NewChildBuilder(prevCR, selectKind)
		prevHPA = build.HPA(b, targetRef, prevCR.Spec.Select.HPA)
	}
	owner := cr.AsOwner()
	return reconcile.HPA(ctx, rclient, newHPA, prevHPA, &owner)
}

func buildSelectScrape(cr *vmv1.VMEstimator, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if cr == nil || svc == nil || cr.Spec.Select == nil || ptr.Deref(cr.Spec.Select.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMServiceScrape(svc, cr.Spec.Select)
}

func buildSelectService(cr *vmv1.VMEstimator) *corev1.Service {
	b := build.NewChildBuilder(cr, selectKind)
	return build.Service(b, cr.Spec.Select.Port, nil)
}

func createOrUpdateSelectService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) error {
	var prevSvc, prevAdditionalSvc *corev1.Service
	if prevCR != nil {
		prevSvc = buildSelectService(prevCR)
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, prevCR.Spec.Select.ServiceSpec)
	}
	svc := buildSelectService(cr)
	owner := cr.AsOwner()
	if err := cr.Spec.Select.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(svc, s)
		if additionalSvc.Name == svc.Name {
			return fmt.Errorf("select additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, svc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalSvc, prevAdditionalSvc, &owner); err != nil {
			return fmt.Errorf("cannot reconcile select additional service: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := reconcile.Service(ctx, rclient, svc, prevSvc, &owner); err != nil {
		return fmt.Errorf("cannot reconcile select service: %w", err)
	}
	if !ptr.Deref(cr.Spec.Select.DisableSelfServiceScrape, false) {
		svs := buildSelectScrape(cr, svc)
		prevSvs := buildSelectScrape(prevCR, prevSvc)
		if err := reconcile.VMServiceScrape(ctx, rclient, svs, prevSvs, &owner, false); err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for select: %w", err)
		}
	}
	return nil
}

func createOrUpdateSelectDeployment(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) error {
	var prevDep *appsv1.Deployment
	if prevCR != nil {
		var err error
		prevDep, err = buildSelectDeployment(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev select deployment: %w", err)
		}
	}
	newDep, err := buildSelectDeployment(cr)
	if err != nil {
		return err
	}
	owner := cr.AsOwner()
	o := reconcile.DeploymentOpts{
		PatchSpec: func(existingSpec, newSpec *appsv1.DeploymentSpec) {
			if cr.Spec.Select.HPA != nil {
				newSpec.Replicas = nil
			}
		},
	}
	return reconcile.Deployment(ctx, rclient, newDep, prevDep, &owner, &o)
}

// storageNodeURLs returns urls of all storage nodes, which must be queried by select nodes
func storageNodeURLs(cr *vmv1.VMEstimator) []string {
	storage := cr.Spec.Storage
	if storage == nil {
		return nil
	}
	scheme := vmv1beta1.HTTPProtoFromFlags(storage.ExtraArgs)
	pathPrefix := strings.TrimSuffix(vmv1beta1.BuildPathWithPrefixFlag(storage.ExtraArgs, "/"), "/")
	if pathPrefix != "" && !strings.HasPrefix(pathPrefix, "/") {
		pathPrefix = "/" + pathPrefix
	}
	// statefulset has a single replica by default
	replicas := ptr.Deref(storage.ReplicaCount, 1)
	urls := make([]string, 0, replicas)
	for i := int32(0); i < replicas; i++ {
		addr := vmv1beta1.PodDNSAddress(cr.PrefixedName(storageKind), i, cr.Namespace, storage.Port, cr.Spec.ClusterDomainName)
		urls = append(urls, fmt.Sprintf("%s://%s%s", scheme, addr, pathPrefix))
	}
	return urls
}

func buildSelectDeployment(cr *vmv1.VMEstimator) (*appsv1.Deployment, error) {
	sel := cr.Spec.Select
	var args []string
	if nodes := storageNodeURLs(cr); len(nodes) > 0 {
		args = append(args, fmt.Sprintf("-storageNode=%s", strings.Join(nodes, ",")))
	}
	podSpec, err := buildPodTemplate(cr, &podOpts{
		kind:      selectKind,
		params:    &sel.CommonAppsParams,
		probe:     sel,
		logLevel:  sel.LogLevel,
		logFormat: sel.LogFormat,
		args:      args,
	})
	if err != nil {
		return nil, err
	}
	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if sel.UpdateStrategy != nil {
		strategyType = *sel.UpdateStrategy
	}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(selectKind),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(selectKind),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: sel.RollingUpdate,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(selectKind),
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(dep, &sel.CommonAppsParams)
	return dep, nil
}
