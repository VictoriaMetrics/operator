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

// clusterComponents builds the per-component descriptors WithDowntimeCluster needs. vmstorage
// /vmselect only get a TargetPVCTemplateName when the target CR actually configures storage
// for them, so old PVCs never get rebound toward a component that won't provision one.
func clusterComponents(target *vmv1beta1.VMCluster) []migrate.ClusterComponentSpec {
	components := []migrate.ClusterComponentSpec{
		{
			HelmComponentLabel:   "vmstorage",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentStorage),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentStorage),
		},
		{
			HelmComponentLabel:   "vmselect",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentSelect),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentSelect),
		},
		{
			HelmComponentLabel:   "vminsert",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentInsert),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentInsert),
		},
	}
	if target.Spec.VMStorage != nil && target.Spec.VMStorage.Storage != nil {
		components[0].TargetPVCTemplateName = target.Spec.VMStorage.GetStorageVolumeName()
	}
	if target.Spec.VMSelect != nil && target.Spec.VMSelect.StorageSpec != nil {
		components[1].TargetPVCTemplateName = target.Spec.VMSelect.GetCacheMountVolumeName()
	}
	return components
}
