package vmalertmanager

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const templatesDir = "/etc/vm/templates"

var badConfigsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "operator_alertmanager_bad_objects_count",
	Help: "Number of child CRDs with bad or incomplete configurations",
	ConstLabels: prometheus.Labels{
		"crd": "vmalertmanager_config",
	},
},
	[]string{"object_namespace"},
)

func init() {
	metrics.Registry.MustRegister(badConfigsTotal)
}

// CreateOrUpdateAlertManager creates alertmanager and builds config for it
func CreateOrUpdateAlertManager(ctx context.Context, cr *vmv1beta1.VMAlertmanager, rclient client.Client) error {
	var prevCR *vmv1beta1.VMAlertmanager
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
		if err := deleteOrphaned(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot delete orphaned resources: %w", err)
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
		cfg := config.MustGetBaseConfig()
		if ptr.Deref(cr.Spec.UseVMConfigReloader, cfg.UseVMConfigReloader) {
			if err := createConfigSecretAccess(ctx, rclient, cr, prevCR); err != nil {
				return err
			}
		}
	}

	service, err := createOrUpdateAlertManagerService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}
	cfg := config.MustGetBaseConfig()
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, cfg.DisableSelfServiceScrapeCreation) {
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForAlertmanager(service, cr))
		if err != nil {
			return err
		}
	}

	if cr.Spec.PodDisruptionBudget != nil {
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
			prevPDB = build.PodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget)
		}
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget), prevPDB); err != nil {
			return err
		}
	}
	var prevSts *appsv1.StatefulSet
	if prevCR != nil {
		var err error
		prevSts, err = newStsForAlertManager(prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev alertmanager sts, name: %s,err: %w", cr.Name, err)
		}
	}
	newSts, err := newStsForAlertManager(cr)
	if err != nil {
		return fmt.Errorf("cannot generate alertmanager sts, name: %s,err: %w", cr.Name, err)
	}

	stsOpts := reconcile.STSOptions{
		HasClaim:       len(newSts.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: cr.SelectorLabels,
	}
	return reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, prevSts)
}

func deleteOrphaned(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlertmanager) error {
	owner := cr.AsOwner()
	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if cr.Spec.PodDisruptionBudget == nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot delete PDB from prev state: %w", err)
		}
	}
	cfg := config.MustGetBaseConfig()
	disableSelfScrape := cfg.DisableSelfServiceScrapeCreation
	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, disableSelfScrape) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	svcName := cr.PrefixedName()
	keepServices := map[string]struct{}{
		svcName: {},
	}
	if cr.Spec.ServiceSpec != nil && !cr.Spec.ServiceSpec.UseAsDefault {
		extraSvcName := cr.Spec.ServiceSpec.NameOrDefault(svcName)
		keepServices[extraSvcName] = struct{}{}
	}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, cr, keepServices); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
	}
	if !cr.IsOwnsServiceAccount() {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.ServiceAccount{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot remove serviceaccount: %w", err)
		}
	}
	return nil
}
