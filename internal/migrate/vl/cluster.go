package vl

import (
	"context"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// WithDowntimeCluster runs the WithDowntime strategy for victoria-logs-cluster.
func WithDowntimeCluster(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1.VLCluster) error {
	return migrate.Cluster(ctx, c, httpClient, opts, target, clusterEngine(), false)
}

// NoDowntimeCluster runs the NoDowntime strategy for victoria-logs-cluster.
func NoDowntimeCluster(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1.VLCluster) error {
	return migrate.Cluster(ctx, c, httpClient, opts, target, clusterEngine(), true)
}

// clusterEngine builds the per-component behaviors shared by WithDowntimeCluster and
// NoDowntimeCluster.
func clusterEngine() migrate.ClusterEngine[*vmv1.VLCluster, *vmv1.VLAgent] {
	return migrate.ClusterEngine[*vmv1.VLCluster, *vmv1.VLAgent]{
		ComponentPrefix: "vl",
		ComponentEnabled: func(target *vmv1.VLCluster, kind vmv1beta1.ClusterComponent) bool {
			switch kind {
			case vmv1beta1.ClusterComponentStorage:
				return target.Spec.VLStorage != nil
			case vmv1beta1.ClusterComponentSelect:
				return target.Spec.VLSelect != nil
			case vmv1beta1.ClusterComponentInsert:
				return target.Spec.VLInsert != nil
			}
			return false
		},
		PVCTemplateName: func(target *vmv1.VLCluster, kind vmv1beta1.ClusterComponent) string {
			if kind == vmv1beta1.ClusterComponentStorage && target.Spec.VLStorage != nil && target.Spec.VLStorage.Storage != nil && target.Spec.VLStorage.Storage.EmptyDir == nil {
				return target.Spec.VLStorage.GetStorageVolumeName()
			}
			return ""
		},
		QueueMetricName: vmv1.VLAgentQueueMetricName,
		BuildAgent:      newBufferAgent,
		AgentMatches: func(existing *vmv1.VLAgent, wantURL string) bool {
			return len(existing.Spec.RemoteWrite) == 1 && existing.Spec.RemoteWrite[0].URL == wantURL &&
				!existing.Spec.K8sCollector.Enabled &&
				existing.Spec.TmpDataPath == nil && existing.Spec.Storage != nil && existing.Spec.Storage.EmptyDir == nil &&
				!existing.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage().IsZero()
		},
		StorageReplicaCount: func(target *vmv1.VLCluster) int32 {
			if target.Spec.VLStorage == nil {
				return 1
			}
			if target.Spec.VLStorage.ReplicaCount != nil {
				return *target.Spec.VLStorage.ReplicaCount
			}
			if target.Spec.VLStorage.HPA != nil {
				return target.Spec.VLStorage.HPA.GetMinReplicas()
			}
			return 1
		},
	}
}
