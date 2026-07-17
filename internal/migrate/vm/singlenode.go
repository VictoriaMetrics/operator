package vm

import (
	"context"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// WithDowntimeSingleNode runs the WithDowntime strategy for victoria-metrics-single.
func WithDowntimeSingleNode(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1beta1.VMSingle) error {
	return migrate.SingleNode(ctx, c, httpClient, opts, target, singleNodeEngine(), false)
}

// NoDowntimeSingleNode runs the NoDowntime strategy for victoria-metrics-single.
func NoDowntimeSingleNode(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1beta1.VMSingle) error {
	return migrate.SingleNode(ctx, c, httpClient, opts, target, singleNodeEngine(), true)
}

// singleNodeEngine builds the behaviors shared by WithDowntimeSingleNode and NoDowntimeSingleNode.
func singleNodeEngine() migrate.SingleNodeEngine[*vmv1beta1.VMSingle, *vmv1beta1.VMAgent] {
	return migrate.SingleNodeEngine[*vmv1beta1.VMSingle, *vmv1beta1.VMAgent]{
		QueueMetricName: vmv1beta1.VMAgentQueueMetricName,
		HasStorage: func(target *vmv1beta1.VMSingle) bool {
			return target.Spec.Storage != nil && !target.Spec.Storage.Resources.Requests.Storage().IsZero()
		},
		BuildAgent: newBufferAgent,
		AgentMatches: func(existing *vmv1beta1.VMAgent, wantURL string) bool {
			return len(existing.Spec.RemoteWrite) == 1 && existing.Spec.RemoteWrite[0].URL == wantURL &&
				existing.Spec.StatefulMode && existing.Spec.StatefulStorage != nil && existing.Spec.StatefulStorage.EmptyDir == nil &&
				!existing.Spec.StatefulStorage.VolumeClaimTemplate.Spec.Resources.Requests.Storage().IsZero() &&
				existing.Spec.IngestOnlyMode != nil && *existing.Spec.IngestOnlyMode
		},
	}
}
