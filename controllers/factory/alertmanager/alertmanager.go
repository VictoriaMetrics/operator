package alertmanager

import (
	"context"
	"fmt"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/build"
	"github.com/VictoriaMetrics/operator/controllers/factory/logger"
	"github.com/VictoriaMetrics/operator/controllers/factory/reconcile"
	"github.com/VictoriaMetrics/operator/internal/config"
	version "github.com/hashicorp/go-version"
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
func CreateOrUpdateAlertManager(ctx context.Context, cr *victoriametricsv1beta1.VMAlertmanager, rclient client.Client, c *config.BaseOperatorConf) error {
	l := logger.WithContext(ctx).WithValues("reconcile.VMAlertManager.sts", cr.Name, "ns", cr.Namespace)
	ctx = logger.AddToContext(ctx, l)
	if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr)); err != nil {
		return fmt.Errorf("failed create service account: %w", err)
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
	// Deprecated, since next release, operator will support only most recent alertmanager versions
	// And it shouldn't aware of any version compatability
	// We must provide a list of supported alertmanager versions
	amVersion, err := version.NewVersion(cr.Spec.Image.Tag)
	if err != nil {
		l.Error(err, "cannot parse alertmanager version", "tag", cr.Spec.Image.Tag)
	}
	newSts, err := newStsForAlertManager(cr, c, amVersion)
	if err != nil {
		return fmt.Errorf("cannot generate alertmanager sts, name: %s,err: %w", cr.Name, err)
	}
	if c.UseCustomConfigReloader {
		if err := CreateVMAlertmanagerSecretAccess(ctx, rclient, cr); err != nil {
			return err
		}
	}
	// check secret with config
	if err := createDefaultAMConfig(ctx, cr, rclient, amVersion); err != nil {
		return fmt.Errorf("failed to check default Alertmanager config: %w", err)
	}

	stsOpts := reconcile.STSOptions{
		HasClaim:       len(newSts.Spec.VolumeClaimTemplates) > 0,
		VolumeName:     cr.GetVolumeName,
		SelectorLabels: cr.SelectorLabels,
		UpdateStrategy: cr.UpdateStrategy,
	}
	return reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, c)
}
