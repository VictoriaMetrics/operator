package vl

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// newBufferAgent builds a VLAgent CR that forwards to remoteWriteURL, buffering to disk so it
// can absorb writes for as long as a migration takes.
func newBufferAgent(name, namespace, remoteWriteURL, bufferSize string) (*vmv1.VLAgent, error) {
	size, err := migrate.ParseAgentBufferSize(bufferSize)
	if err != nil {
		return nil, err
	}
	return &vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vmv1.VLAgentSpec{
			Storage: migrate.BufferAgentStorage(size),
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{URL: remoteWriteURL},
			},
		},
	}, nil
}
