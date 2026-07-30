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

// clusterComponents builds the per-component descriptors WithDowntimeCluster needs. vlstorage
// only gets a TargetPVCTemplateName when the target CR configures storage for it
// (vlselect/vlinsert never have persistent storage).
func clusterComponents(target *vmv1.VLCluster) []migrate.ClusterComponentSpec {
	components := []migrate.ClusterComponentSpec{
		{
			HelmComponentLabel:   "vlstorage",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentStorage),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentStorage),
		},
		{
			HelmComponentLabel:   "vlselect",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentSelect),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentSelect),
		},
		{
			HelmComponentLabel:   "vlinsert",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentInsert),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentInsert),
		},
	}
	if target.Spec.VLStorage != nil && target.Spec.VLStorage.Storage != nil {
		components[0].TargetPVCTemplateName = target.Spec.VLStorage.GetStorageVolumeName()
	}
	return components
}
