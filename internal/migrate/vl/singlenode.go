package vl

import (
	"context"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// WithDowntimeSingleNode runs the WithDowntime strategy for victoria-logs-single.
func WithDowntimeSingleNode(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1.VLSingle) error {
	return migrate.SingleNode(ctx, c, httpClient, opts, target, singleNodeEngine(), false)
}

// NoDowntimeSingleNode runs the NoDowntime strategy for victoria-logs-single.
func NoDowntimeSingleNode(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1.VLSingle) error {
	return migrate.SingleNode(ctx, c, httpClient, opts, target, singleNodeEngine(), true)
}

// singleNodeEngine builds the behaviors shared by WithDowntimeSingleNode and NoDowntimeSingleNode.
func singleNodeEngine() migrate.SingleNodeEngine[*vmv1.VLSingle, *vmv1.VLAgent] {
	return migrate.SingleNodeEngine[*vmv1.VLSingle, *vmv1.VLAgent]{
		QueueMetricName: vmv1.VLAgentQueueMetricName,
		HasStorage: func(target *vmv1.VLSingle) bool {
			return target.Spec.Storage != nil && !target.Spec.Storage.Resources.Requests.Storage().IsZero()
		},
		BuildAgent: newBufferAgent,
		AgentMatches: func(existing *vmv1.VLAgent, wantURL string) bool {
			return len(existing.Spec.RemoteWrite) == 1 && existing.Spec.RemoteWrite[0].URL == wantURL &&
				!existing.Spec.K8sCollector.Enabled &&
				existing.Spec.TmpDataPath == nil && existing.Spec.Storage != nil && existing.Spec.Storage.EmptyDir == nil &&
				!existing.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage().IsZero()
		},
	}
}
