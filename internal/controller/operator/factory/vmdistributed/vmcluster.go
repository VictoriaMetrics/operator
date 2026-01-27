package vmdistributed

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

// buildVMClusters prepares VMClusters for update
func buildVMClusters(cr *vmv1alpha1.VMDistributed) ([]*vmv1beta1.VMCluster, error) {
	vmClusters := make([]*vmv1beta1.VMCluster, len(cr.Spec.Zones))
	for i := range cr.Spec.Zones {
		zone := &cr.Spec.Zones[i]
		clusterName := zone.VMClusterName(cr)
		nsn := types.NamespacedName{Name: clusterName, Namespace: cr.Namespace}
		vmCluster := vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:            clusterName,
				Namespace:       cr.Namespace,
				OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			},
		}
		mergedSpec, err := k8stools.RenderPlaceholders(&cr.Spec.ZoneCommon.VMCluster.Spec, map[string]string{
			vmv1alpha1.ZonePlaceholder: zone.Name,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to render common vmcluster for spec.zones[%d].vmcluster=%s: %w", i, nsn, err)
		}

		// Apply cluster-specific override if it exist
		if err := build.MergeDeep(mergedSpec, &zone.VMCluster.Spec); err != nil {
			return nil, fmt.Errorf("failed to merge spec for spec.zones[%d].vmcluster=%s: %w", i, nsn, err)
		}
		vmCluster.Spec = *mergedSpec
		vmClusters[i] = &vmCluster
	}
	return vmClusters, nil
}
