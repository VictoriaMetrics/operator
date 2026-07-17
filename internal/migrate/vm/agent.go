package vm

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// newBufferAgent builds an ingest-only VMAgent CR that forwards to remoteWriteURL, buffering
// to disk so it can absorb writes for as long as a migration takes.
func newBufferAgent(name, namespace, remoteWriteURL, bufferSize string) (*vmv1beta1.VMAgent, error) {
	size, err := migrate.ParseAgentBufferSize(bufferSize)
	if err != nil {
		return nil, err
	}
	return &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vmv1beta1.VMAgentSpec{
			CommonScrapeParams: vmv1beta1.CommonScrapeParams{
				IngestOnlyMode: ptr.To(true),
			},
			StatefulMode:    true,
			StatefulStorage: migrate.BufferAgentStorage(size),
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{URL: remoteWriteURL},
			},
		},
	}, nil
}
