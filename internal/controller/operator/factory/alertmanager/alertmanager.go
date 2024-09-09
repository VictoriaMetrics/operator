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
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget)); err != nil {
			return err
		}
	}
	// special hack, we need version for alertmanager a bit earlier.
	if cr.Spec.Image.Tag == "" {
		cr.Spec.Image.Tag = c.VMAlertManager.AlertManagerVersion
	}

	newSts, err := newStsForAlertManager(cr, c)
	if err != nil {
		return fmt.Errorf("cannot generate alertmanager sts, name: %s,err: %w", cr.Name, err)
	}

	stsOpts := reconcile.STSOptions{
		HasClaim:       len(newSts.Spec.VolumeClaimTemplates) > 0,
		VolumeName:     cr.GetVolumeName,
		SelectorLabels: cr.SelectorLabels,
	}
	return reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, c)
}
