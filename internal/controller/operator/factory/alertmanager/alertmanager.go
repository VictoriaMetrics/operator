package alertmanager

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"

	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const templatesDir = "/etc/vm/templates"

var badConfigsTotal = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "operator_alertmanager_bad_objects_count",
	Help: "Number of child CRDs with bad or incomplete configurations",
	ConstLabels: prometheus.Labels{
		"crd": "vmalertmanager_config",
	},
})

func init() {
	metrics.Registry.MustRegister(badConfigsTotal)
}

// CreateOrUpdateAlertManager creates alertmanagerand and bulds config for it
func CreateOrUpdateAlertManager(ctx context.Context, cr *vmv1beta1.VMAlertmanager, rclient client.Client) error {
	if err := deletePrevStateResources(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot delete objects from prev state: %w", err)
	}
	if cr.IsOwnsServiceAccount() {
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr)); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
		if ptr.Deref(cr.Spec.UseVMConfigReloader, false) {
			if err := createVMAlertmanagerSecretAccess(ctx, rclient, cr); err != nil {
				return err
			}
		}
	}

	service, err := createOrUpdateAlertManagerService(ctx, cr, rclient)
	if err != nil {
		return err
	}
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForAlertmanager(service, cr))
		if err != nil {
			return err
		}
	}

	if cr.Spec.PodDisruptionBudget != nil {
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget)); err != nil {
			return err
		}
	}
	var prevSts *appsv1.StatefulSet

	if cr.ParsedLastAppliedSpec != nil {
		prevCR := cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
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

func deletePrevStateResources(ctx context.Context, cr *vmv1beta1.VMAlertmanager, rclient client.Client) error {
	if cr.ParsedLastAppliedSpec == nil {
		return nil
	}
	prevSvc, currSvc := cr.ParsedLastAppliedSpec.ServiceSpec, cr.Spec.ServiceSpec
	if err := reconcile.AdditionalServices(ctx, rclient, cr.PrefixedName(), cr.Namespace, prevSvc, currSvc); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
	}

	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if cr.Spec.PodDisruptionBudget == nil && cr.ParsedLastAppliedSpec.PodDisruptionBudget != nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot delete PDB from prev state: %w", err)
		}
	}
	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) && !ptr.Deref(cr.ParsedLastAppliedSpec.DisableSelfServiceScrape, false) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	return nil
}
