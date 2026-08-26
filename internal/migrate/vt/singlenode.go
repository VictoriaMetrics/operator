package vt

import (
	"context"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// WithDowntimeSingleNode runs the WithDowntime strategy for victoria-traces-single.
func WithDowntimeSingleNode(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1.VTSingle) error {
	return migrate.SingleNode(ctx, c, httpClient, opts, target, singleNodeEngine(), false)
}

// NoDowntimeSingleNode runs the NoDowntime strategy for victoria-traces-single.
func NoDowntimeSingleNode(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1.VTSingle) error {
	return migrate.SingleNode(ctx, c, httpClient, opts, target, singleNodeEngine(), true)
}

func singleNodeEngine() migrate.SingleNodeEngine[*vmv1.VTSingle, *vmv1.VTAgent] {
	return migrate.SingleNodeEngine[*vmv1.VTSingle, *vmv1.VTAgent]{
		QueueMetricName: vmv1.VTAgentQueueMetricName,
		HasStorage: func(target *vmv1.VTSingle) bool {
			return target.Spec.StorageDataPath == "" && target.Spec.Storage != nil && !target.Spec.Storage.Resources.Requests.Storage().IsZero()
		},
		BuildAgent: newBufferAgent,
		BuildAgentWriteURL: func(target *vmv1.VTSingle, remoteWriteURL string) string {
			return bufferAgentWriteURL(remoteWriteURL, migrate.NormalizePathPrefix(target.Spec.ExtraArgs[migrate.HTTPPathPrefixArg]))
		},
		AgentMatches: func(existing, want *vmv1.VTAgent) bool {
			return migrate.AgentBaseMatches(existing, want, existing.Spec.RemoteWrite, want.Spec.RemoteWrite) &&
				existing.Spec.TmpDataPath == nil && existing.Spec.Storage != nil && existing.Spec.Storage.EmptyDir == nil &&
				existing.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage().Cmp(*want.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage()) >= 0
		},
	}
}
