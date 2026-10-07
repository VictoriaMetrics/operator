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

const (
	storageKind = vmv1beta1.ClusterComponentStorage
	// storage nodes expose local cardinality estimations at the separate path
	// in order to keep them away of the default scrape path, since select nodes expose merged estimations
	storageCardinalityMetricsPath = "/cardinality/metrics"
)

func createOrUpdateStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator, configHash string) error {
	if cr.Spec.Storage == nil {
		return nil
	}
	var prev componentObjects
	if prevCR != nil && prevCR.Spec.Storage == nil {
		prevCR = nil
	}
	if prevCR != nil {
		ps := prevCR.Spec.Storage
		prev = componentObjects{pdb: ps.PodDisruptionBudget, np: ps.NetworkPolicy, vpa: ps.VPA}
	}
	s := cr.Spec.Storage
	cur := componentObjects{pdb: s.PodDisruptionBudget, np: s.NetworkPolicy, vpa: s.VPA}
	if err := createOrUpdateComponentObjects(ctx, rclient, cr, prevCR, storageKind, "StatefulSet", cur, prev); err != nil {
		return err
	}
	if err := createOrUpdateStorageService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	return createOrUpdateStorageSTS(ctx, rclient, cr, prevCR, configHash)
}

func buildStorageScrape(cr *vmv1.VMEstimator, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if cr == nil || svc == nil || cr.Spec.Storage == nil || ptr.Deref(cr.Spec.Storage.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMServiceScrape(svc, cr.Spec.Storage)
}

// buildStorageService builds headless service, which provides stable network identities for storage pods
func buildStorageService(cr *vmv1.VMEstimator) *corev1.Service {
	b := build.NewChildBuilder(cr, storageKind)
	return build.Service(b, cr.Spec.Storage.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.PublishNotReadyAddresses = true
	})
}

// buildStorageInsertService builds service, which load-balances remote write requests among storage nodes.
// It's marked as additional service in order to exclude it from VMServiceScrape selector.
func buildStorageInsertService(cr *vmv1.VMEstimator, storageSvc *corev1.Service) *corev1.Service {
	return build.AdditionalServiceFromDefault(storageSvc, &vmv1beta1.AdditionalServiceSpec{
		EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
			Name: cr.PrefixedInsertName(),
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
		},
	})
}

func createOrUpdateStorageService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) error {
	var prevSvc, prevInsertSvc, prevAdditionalSvc *corev1.Service
	if prevCR != nil {
		prevSvc = buildStorageService(prevCR)
		prevInsertSvc = buildStorageInsertService(prevCR, prevSvc)
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, prevCR.Spec.Storage.ServiceSpec)
	}
	svc := buildStorageService(cr)
	insertSvc := buildStorageInsertService(cr, svc)
	owner := cr.AsOwner()
	if err := cr.Spec.Storage.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(svc, s)
		if additionalSvc.Name == svc.Name || additionalSvc.Name == insertSvc.Name {
			return fmt.Errorf("storage additional service name: %q cannot be the same as %q or %q", additionalSvc.Name, svc.Name, insertSvc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalSvc, prevAdditionalSvc, &owner); err != nil {
			return fmt.Errorf("cannot reconcile storage additional service: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := reconcile.Service(ctx, rclient, svc, prevSvc, &owner); err != nil {
		return fmt.Errorf("cannot reconcile storage service: %w", err)
	}
	if err := reconcile.Service(ctx, rclient, insertSvc, prevInsertSvc, &owner); err != nil {
		return fmt.Errorf("cannot reconcile storage insert service: %w", err)
	}
	if !ptr.Deref(cr.Spec.Storage.DisableSelfServiceScrape, false) {
		svs := buildStorageScrape(cr, svc)
		prevSvs := buildStorageScrape(prevCR, prevSvc)
		if err := reconcile.VMServiceScrape(ctx, rclient, svs, prevSvs, &owner, false); err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for storage: %w", err)
		}
	}
	return nil
}

func createOrUpdateStorageSTS(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator, configHash string) error {
	var prevSts *appsv1.StatefulSet
	if prevCR != nil {
		var err error
		// configuration hash doesn't affect the previous statefulset metadata
		prevSts, err = buildStorageSTS(prevCR, "")
		if err != nil {
			return fmt.Errorf("cannot build prev storage statefulset: %w", err)
		}
	}
	newSts, err := buildStorageSTS(cr, configHash)
	if err != nil {
		return err
	}
	o := reconcile.StatefulSetOpts{
		SelectorLabels: cr.SelectorLabels(storageKind),
		UpdateBehavior: cr.Spec.Storage.RollingUpdateStrategyBehavior,
	}
	owner := cr.AsOwner()
	return reconcile.StatefulSet(ctx, rclient, newSts, prevSts, &owner, &o)
}

func buildStorageSTS(cr *vmv1.VMEstimator, configHash string) (*appsv1.StatefulSet, error) {
	storage := cr.Spec.Storage
	podSpec, err := buildPodTemplate(cr, &podOpts{
		kind:      storageKind,
		params:    &storage.CommonAppsParams,
		probe:     storage,
		logLevel:  storage.LogLevel,
		logFormat: storage.LogFormat,
		args: []string{
			fmt.Sprintf("-cardinalityMetrics.exposeAt=%s", storageCardinalityMetricsPath),
		},
		configHash: configHash,
	})
	if err != nil {
		return nil, err
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(storageKind),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(storageKind),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(storageKind),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: storage.RollingUpdateStrategy,
			},
			Template:    *podSpec,
			ServiceName: cr.PrefixedName(storageKind),
		},
	}
	build.StatefulSetAddCommonParams(sts, &storage.CommonAppsParams)
	return sts, nil
}
