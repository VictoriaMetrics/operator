package vt

import (
	"context"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// WithDowntimeCluster runs the WithDowntime strategy for victoria-traces-cluster.
func WithDowntimeCluster(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1.VTCluster) error {
	return migrate.Cluster(ctx, c, httpClient, opts, target, clusterEngine(), false)
}

// NoDowntimeCluster runs the NoDowntime strategy for victoria-traces-cluster.
func NoDowntimeCluster(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1.VTCluster) error {
	return migrate.Cluster(ctx, c, httpClient, opts, target, clusterEngine(), true)
}

// clusterEngine builds the per-component behaviors shared by WithDowntimeCluster and
// NoDowntimeCluster.
func clusterEngine() migrate.ClusterEngine[*vmv1.VTCluster, *vmv1.VTAgent] {
	return migrate.ClusterEngine[*vmv1.VTCluster, *vmv1.VTAgent]{
		ComponentPrefix: "vt",
		ComponentEnabled: func(target *vmv1.VTCluster, kind vmv1beta1.ClusterComponent) bool {
			switch kind {
			case vmv1beta1.ClusterComponentStorage:
				return target.Spec.Storage != nil
			case vmv1beta1.ClusterComponentSelect:
				return target.Spec.Select != nil
			case vmv1beta1.ClusterComponentInsert:
				return target.Spec.Insert != nil
			}
			return false
		},
		PVCTemplateName: func(target *vmv1.VTCluster, kind vmv1beta1.ClusterComponent) string {
			if kind == vmv1beta1.ClusterComponentStorage && target.Spec.Storage != nil && target.Spec.Storage.Storage != nil && target.Spec.Storage.Storage.EmptyDir == nil {
				return target.Spec.Storage.GetStorageVolumeName()
			}
			return ""
		},
		QueueMetricName: vmv1.VTAgentQueueMetricName,
		BuildAgent:      newBufferAgent,
		BuildAgentWriteURL: func(target *vmv1.VTCluster, remoteWriteURL string) string {
			var extraArgs map[string]string
			if target.Spec.Insert != nil {
				extraArgs = target.Spec.Insert.ExtraArgs
			}
			return bufferAgentWriteURL(remoteWriteURL, migrate.NormalizePathPrefix(extraArgs[migrate.HTTPPathPrefixArg]))
		},
		AgentMatches: func(existing, want *vmv1.VTAgent) bool {
			return migrate.AgentBaseMatches(existing, want, existing.Spec.RemoteWrite, want.Spec.RemoteWrite) &&
				existing.Spec.TmpDataPath == nil && existing.Spec.Storage != nil && existing.Spec.Storage.EmptyDir == nil &&
				existing.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage().Cmp(*want.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage()) >= 0
		},
		StorageReplicaCount: func(target *vmv1.VTCluster) int32 {
			if target.Spec.Storage == nil {
				return 1
			}
			if target.Spec.Storage.ReplicaCount != nil {
				return *target.Spec.Storage.ReplicaCount
			}
			if target.Spec.Storage.HPA != nil {
				return target.Spec.Storage.HPA.GetMinReplicas()
			}
			return 1
		},
		StorageUsesHPA: func(target *vmv1.VTCluster) bool {
			return target.Spec.Storage != nil && target.Spec.Storage.ReplicaCount == nil && target.Spec.Storage.HPA != nil
		},
		StorageMaxReplicas: func(target *vmv1.VTCluster) int32 {
			if target.Spec.Storage == nil || target.Spec.Storage.HPA == nil {
				return 0
			}
			return target.Spec.Storage.HPA.MaxReplicas
		},
	}
}
