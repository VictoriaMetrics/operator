package vm

import (
	"context"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// WithDowntimeCluster runs the WithDowntime strategy for victoria-metrics-cluster.
func WithDowntimeCluster(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1beta1.VMCluster) error {
	return migrate.Cluster(ctx, c, httpClient, opts, target, clusterEngine(), false)
}

// NoDowntimeCluster runs the NoDowntime strategy for victoria-metrics-cluster.
func NoDowntimeCluster(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1beta1.VMCluster) error {
	return migrate.Cluster(ctx, c, httpClient, opts, target, clusterEngine(), true)
}

// clusterEngine builds the per-component behaviors shared by WithDowntimeCluster and
// NoDowntimeCluster.
func clusterEngine() migrate.ClusterEngine[*vmv1beta1.VMCluster, *vmv1beta1.VMAgent] {
	return migrate.ClusterEngine[*vmv1beta1.VMCluster, *vmv1beta1.VMAgent]{
		ComponentPrefix: "vm",
		ComponentEnabled: func(target *vmv1beta1.VMCluster, kind vmv1beta1.ClusterComponent) bool {
			switch kind {
			case vmv1beta1.ClusterComponentStorage:
				return target.Spec.VMStorage != nil
			case vmv1beta1.ClusterComponentSelect:
				return target.Spec.VMSelect != nil
			case vmv1beta1.ClusterComponentInsert:
				return target.Spec.VMInsert != nil
			}
			return false
		},
		// vmselect's cache volume is the one component besides storage with a PVC template.
		PVCTemplateName: func(target *vmv1beta1.VMCluster, kind vmv1beta1.ClusterComponent) string {
			switch kind {
			case vmv1beta1.ClusterComponentStorage:
				if target.Spec.VMStorage != nil && target.Spec.VMStorage.Storage != nil {
					return target.Spec.VMStorage.GetStorageVolumeName()
				}
			case vmv1beta1.ClusterComponentSelect:
				if target.Spec.VMSelect != nil && target.Spec.VMSelect.StorageSpec != nil {
					return target.Spec.VMSelect.GetCacheMountVolumeName()
				}
			}
			return ""
		},
		QueueMetricName: vmv1beta1.VMAgentQueueMetricName,
		BuildAgent: func(name, namespace, writeURL, bufferSize string) (*vmv1beta1.VMAgent, error) {
			agent, err := newBufferAgent(name, namespace, writeURL, bufferSize)
			if err != nil {
				return nil, err
			}
			agent.Spec.RemoteWriteSettings = &vmv1beta1.VMAgentRemoteWriteSettings{UseMultiTenantMode: true}
			return agent, nil
		},
		AgentMatches: func(existing *vmv1beta1.VMAgent, wantURL string) bool {
			return len(existing.Spec.RemoteWrite) == 1 && existing.Spec.RemoteWrite[0].URL == wantURL &&
				existing.Spec.RemoteWriteSettings != nil && existing.Spec.RemoteWriteSettings.UseMultiTenantMode &&
				existing.Spec.StatefulMode && existing.Spec.StatefulStorage != nil && existing.Spec.StatefulStorage.EmptyDir == nil &&
				!existing.Spec.StatefulStorage.VolumeClaimTemplate.Spec.Resources.Requests.Storage().IsZero() &&
				existing.Spec.IngestOnlyMode != nil && *existing.Spec.IngestOnlyMode
		},
		StorageReplicaCount: func(target *vmv1beta1.VMCluster) int32 {
			if target.Spec.VMStorage == nil {
				return 1
			}
			if target.Spec.VMStorage.ReplicaCount != nil {
				return *target.Spec.VMStorage.ReplicaCount
			}
			if target.Spec.VMStorage.HPA != nil {
				return target.Spec.VMStorage.HPA.GetMinReplicas()
			}
			return 1
		},
	}
}
