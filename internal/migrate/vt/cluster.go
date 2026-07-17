// Package vt implements VictoriaTraces-specific `helm-converter migrate` orchestration.
// Engine-agnostic pieces live in the parent internal/migrate package instead.
package vt

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// WithDowntimeCluster runs the WithDowntime strategy for victoria-traces-cluster.
func WithDowntimeCluster(ctx context.Context, c client.Client, opts migrate.Options, target *vmv1.VTCluster) error {
	return migrate.WithDowntimeCluster(ctx, c, opts, target, clusterComponents(target))
}

// clusterComponents builds the per-component descriptors WithDowntimeCluster needs.
func clusterComponents(target *vmv1.VTCluster) []migrate.ClusterComponentSpec {
	var components []migrate.ClusterComponentSpec
	if target.Spec.Storage != nil {
		spec := migrate.ClusterComponentSpec{
			HelmComponentLabel:   "vtstorage",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentStorage),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentStorage),
		}
		if target.Spec.Storage.Storage != nil {
			spec.TargetPVCTemplateName = target.Spec.Storage.GetStorageVolumeName()
		}
		components = append(components, spec)
	}
	if target.Spec.Select != nil {
		components = append(components, migrate.ClusterComponentSpec{
			HelmComponentLabel:   "vtselect",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentSelect),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentSelect),
		})
	}
	if target.Spec.Insert != nil {
		components = append(components, migrate.ClusterComponentSpec{
			HelmComponentLabel:   "vtinsert",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentInsert),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentInsert),
		})
	}
	return components
}
