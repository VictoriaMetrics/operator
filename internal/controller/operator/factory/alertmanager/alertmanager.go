package alertmanager

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
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
func CreateOrUpdateAlertManager(ctx context.Context, cr *vmv1beta1.VMAlertmanager, rclient client.Client, c *config.BaseOperatorConf) error {
	l := logger.WithContext(ctx).WithValues("reconcile.VMAlertManager.sts", cr.Name, "ns", cr.Namespace)
	ctx = logger.AddToContext(ctx, l)
	if cr.IsOwnsServiceAccount() {
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr)); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
		if c.UseCustomConfigReloader {
			if err := createVMAlertmanagerSecretAccess(ctx, rclient, cr); err != nil {
				return err
			}
		}
	}

	if cr.Spec.PodDisruptionBudget != nil {
		// TODO verify lastSpec for missing PDB and detete it if needed
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget)); err != nil {
			return err
		}
	}
	var prevSts *appsv1.StatefulSet
	prevSpec, err := vmv1beta1.LastAppliedSpec[vmv1beta1.VMAlertmanagerSpec](cr)
	if err != nil {
		return fmt.Errorf("cannot parse last applied spec :%w", err)
	}
	if prevSpec != nil {
		prevCR := cr.DeepCopy()
		prevCR.Spec = *prevSpec
		prevSts, err = newStsForAlertManager(prevCR, c)
		if err != nil {
			return fmt.Errorf("cannot generate prev alertmanager sts, name: %s,err: %w", cr.Name, err)
		}
	}
	newSts, err := newStsForAlertManager(cr, c)
	if err != nil {
		return fmt.Errorf("cannot generate alertmanager sts, name: %s,err: %w", cr.Name, err)
	}

	stsOpts := reconcile.STSOptions{
		HasClaim:       len(newSts.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: cr.SelectorLabels,
	}
	return reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, prevSts, c)
}
