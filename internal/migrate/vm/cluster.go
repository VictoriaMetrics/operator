package vm

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// WithDowntimeCluster runs the WithDowntime strategy for victoria-metrics-cluster.
func WithDowntimeCluster(ctx context.Context, c client.Client, opts migrate.Options, target *vmv1beta1.VMCluster) error {
	return migrate.WithDowntimeCluster(ctx, c, opts, target, clusterComponents(target))
}

// clusterComponents builds the per-component descriptors WithDowntimeCluster needs.
func clusterComponents(target *vmv1beta1.VMCluster) []migrate.ClusterComponentSpec {
	var components []migrate.ClusterComponentSpec
	if target.Spec.VMStorage != nil {
		spec := migrate.ClusterComponentSpec{
			HelmComponentLabel:   "vmstorage",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentStorage),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentStorage),
		}
		if target.Spec.VMStorage.Storage != nil {
			spec.TargetPVCTemplateName = target.Spec.VMStorage.GetStorageVolumeName()
		}
		components = append(components, spec)
	}
	if target.Spec.VMSelect != nil {
		spec := migrate.ClusterComponentSpec{
			HelmComponentLabel:   "vmselect",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentSelect),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentSelect),
		}
		if target.Spec.VMSelect.StorageSpec != nil {
			spec.TargetPVCTemplateName = target.Spec.VMSelect.GetCacheMountVolumeName()
		}
		components = append(components, spec)
	}
	if target.Spec.VMInsert != nil {
		components = append(components, migrate.ClusterComponentSpec{
			HelmComponentLabel:   "vminsert",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentInsert),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentInsert),
		})
	}
	return components
}
