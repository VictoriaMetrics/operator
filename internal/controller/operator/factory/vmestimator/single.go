package vmestimator

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const singleKind = vmv1.VMEstimatorComponentSingle

func createOrUpdateSingle(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator, configHash string) error {
	if cr.Spec.Single == nil {
		return nil
	}
	var prev componentObjects
	if prevCR != nil && prevCR.Spec.Single == nil {
		prevCR = nil
	}
	if prevCR != nil {
		ps := prevCR.Spec.Single
		prev = componentObjects{pdb: ps.PodDisruptionBudget, np: ps.NetworkPolicy, vpa: ps.VPA}
	}
	s := cr.Spec.Single
	cur := componentObjects{pdb: s.PodDisruptionBudget, np: s.NetworkPolicy, vpa: s.VPA}
	if err := createOrUpdateComponentObjects(ctx, rclient, cr, prevCR, singleKind, "Deployment", cur, prev); err != nil {
		return err
	}
	if err := createOrUpdateSingleService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	return createOrUpdateSingleDeployment(ctx, rclient, cr, prevCR, configHash)
}

func buildSingleScrape(cr *vmv1.VMEstimator, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if cr == nil || svc == nil || cr.Spec.Single == nil || ptr.Deref(cr.Spec.Single.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMServiceScrape(svc, cr.Spec.Single)
}

func buildSingleService(cr *vmv1.VMEstimator) *corev1.Service {
	b := build.NewChildBuilder(cr, singleKind)
	return build.Service(b, cr.Spec.Single.Port, nil)
}

func createOrUpdateSingleService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) error {
	var prevSvc, prevAdditionalSvc *corev1.Service
	if prevCR != nil {
		prevSvc = buildSingleService(prevCR)
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, prevCR.Spec.Single.ServiceSpec)
	}
	svc := buildSingleService(cr)
	owner := cr.AsOwner()
	if err := cr.Spec.Single.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(svc, s)
		if additionalSvc.Name == svc.Name {
			return fmt.Errorf("single additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, svc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalSvc, prevAdditionalSvc, &owner); err != nil {
			return fmt.Errorf("cannot reconcile single additional service: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := reconcile.Service(ctx, rclient, svc, prevSvc, &owner); err != nil {
		return fmt.Errorf("cannot reconcile single service: %w", err)
	}
	if !ptr.Deref(cr.Spec.Single.DisableSelfServiceScrape, false) {
		svs := buildSingleScrape(cr, svc)
		prevSvs := buildSingleScrape(prevCR, prevSvc)
		if err := reconcile.VMServiceScrape(ctx, rclient, svs, prevSvs, &owner, false); err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for single: %w", err)
		}
	}
	return nil
}

func createOrUpdateSingleDeployment(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator, configHash string) error {
	var prevDep *appsv1.Deployment
	if prevCR != nil {
		var err error
		// configuration hash doesn't affect the previous deployment metadata
		prevDep, err = buildSingleDeployment(prevCR, "")
		if err != nil {
			return fmt.Errorf("cannot build prev single deployment: %w", err)
		}
	}
	newDep, err := buildSingleDeployment(cr, configHash)
	if err != nil {
		return err
	}
	owner := cr.AsOwner()
	return reconcile.Deployment(ctx, rclient, newDep, prevDep, &owner, nil)
}

func buildSingleDeployment(cr *vmv1.VMEstimator, configHash string) (*appsv1.Deployment, error) {
	single := cr.Spec.Single
	podSpec, err := buildPodTemplate(cr, &podOpts{
		kind:       singleKind,
		params:     &single.CommonAppsParams,
		probe:      single,
		logLevel:   single.LogLevel,
		logFormat:  single.LogFormat,
		configHash: configHash,
	})
	if err != nil {
		return nil, err
	}
	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if single.UpdateStrategy != nil {
		strategyType = *single.UpdateStrategy
	}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(singleKind),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(singleKind),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: single.RollingUpdate,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(singleKind),
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(dep, &single.CommonAppsParams)
	return dep, nil
}
