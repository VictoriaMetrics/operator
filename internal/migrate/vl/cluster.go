package vl

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// WithDowntimeCluster runs the WithDowntime strategy for victoria-logs-cluster.
func WithDowntimeCluster(ctx context.Context, c client.Client, opts migrate.Options, target *vmv1.VLCluster) error {
	return migrate.WithDowntimeCluster(ctx, c, opts, target, clusterComponents(target))
}

// clusterComponents builds the per-component descriptors WithDowntimeCluster needs.
func clusterComponents(target *vmv1.VLCluster) []migrate.ClusterComponentSpec {
	var components []migrate.ClusterComponentSpec
	if target.Spec.VLStorage != nil {
		spec := migrate.ClusterComponentSpec{
			HelmComponentLabel:   "vlstorage",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentStorage),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentStorage),
		}
		if target.Spec.VLStorage.Storage != nil {
			spec.TargetPVCTemplateName = target.Spec.VLStorage.GetStorageVolumeName()
		}
		components = append(components, spec)
	}
	if target.Spec.VLSelect != nil {
		components = append(components, migrate.ClusterComponentSpec{
			HelmComponentLabel:   "vlselect",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentSelect),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentSelect),
		})
	}
	if target.Spec.VLInsert != nil {
		components = append(components, migrate.ClusterComponentSpec{
			HelmComponentLabel:   "vlinsert",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentInsert),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentInsert),
		})
	}
	return components
}
