package vlsingle

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

func createOrUpdatePVC(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLSingle) error {
	newPvc := newPVC(cr)
	var prevPVC *corev1.PersistentVolumeClaim
	if prevCR != nil && prevCR.Spec.Storage != nil {
		prevPVC = newPVC(prevCR)
	}
	return reconcile.PersistentVolumeClaim(ctx, rclient, newPvc, prevPVC)
}

func newPVC(r *vmv1.VLSingle) *corev1.PersistentVolumeClaim {
	pvcObject := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            r.PrefixedName(),
			Namespace:       r.Namespace,
			Labels:          labels.Merge(r.Spec.StorageMetadata.Labels, r.SelectorLabels()),
			Annotations:     r.Spec.StorageMetadata.Annotations,
			Finalizers:      []string{vmv1beta.FinalizerName},
			OwnerReferences: r.AsOwner(),
		},
		Spec: *r.Spec.Storage,
	}
	if len(pvcObject.Spec.AccessModes) == 0 {
		pvcObject.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteOnce,
		}
	}

	return pvcObject
}

// CreateOrUpdate performs an update for vlsingle resource
func CreateOrUpdate(ctx context.Context, rclient client.Client, cr *vmv1.VLSingle) error {
	var prevCR *vmv1.VLSingle
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	if err := deletePrevStateResources(ctx, cr, rclient); err != nil {
		return err
	}
	if cr.Spec.Storage != nil && cr.Spec.StorageDataPath == "" {
		if err := createOrUpdatePVC(ctx, rclient, cr, prevCR); err != nil {
			return err
		}
	}

	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			prevSA = build.ServiceAccount(prevCR)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr), prevSA); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	svc, err := createOrUpdateService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(svc, cr))
		if err != nil {
			return fmt.Errorf("cannot create serviceScrape for vlsingle: %w", err)
		}
	}

	var prevDeploy *appsv1.Deployment
	if prevCR != nil {
		prevDeploy, err = newDeploy(prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}

	newDeploy, err := newDeploy(cr)
	if err != nil {
		return fmt.Errorf("cannot generate new deploy for vlsingle: %w", err)
	}

	return reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, false)
}

func newDeploy(r *vmv1.VLSingle) (*appsv1.Deployment, error) {
	podSpec, err := newPodSpec(r)
	if err != nil {
		return nil, err
	}

	app := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            r.PrefixedName(),
			Namespace:       r.Namespace,
			Labels:          r.AllLabels(),
			Annotations:     r.AnnotationsFiltered(),
			OwnerReferences: r.AsOwner(),
			Finalizers:      []string{vmv1beta.FinalizerName},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: r.Spec.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: r.SelectorLabels(),
			},
			Strategy: appsv1.DeploymentStrategy{
				// we use recreate, coz of volume claim
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      r.PodLabels(),
					Annotations: r.PodAnnotations(),
				},
				Spec: *podSpec,
			},
		},
	}
	build.DeploymentAddCommonParams(app, ptr.Deref(r.Spec.UseStrictSecurity, false), &r.Spec.CommonApplicationDeploymentParams)
	return app, nil
}

// createOrUpdateService creates service for vlsingle
func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLSingle) (*corev1.Service, error) {
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevService = build.Service(prevCR, prevCR.Spec.Port, nil)
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.ServiceSpec)
	}

	newService := build.Service(cr, cr.Spec.Port, nil)
	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vlsingle additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vlsingle: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vlsingle: %w", err)
	}
	return newService, nil
}

func deletePrevStateResources(ctx context.Context, cr *vmv1.VLSingle, rclient client.Client) error {
	if cr.ParsedLastAppliedSpec == nil {
		return nil
	}
	prevSvc, currSvc := cr.ParsedLastAppliedSpec.ServiceSpec, cr.Spec.ServiceSpec
	if err := reconcile.AdditionalServices(ctx, rclient, cr.PrefixedName(), cr.Namespace, prevSvc, currSvc); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
	}

	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) && !ptr.Deref(cr.ParsedLastAppliedSpec.DisableSelfServiceScrape, false) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta.VMServiceScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	return nil
}
