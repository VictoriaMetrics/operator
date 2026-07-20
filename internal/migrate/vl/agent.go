package vl

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// newBufferAgent builds a VLAgent CR that forwards to remoteWriteURL, buffering to disk so it
// can absorb writes for as long as a migration takes.
func newBufferAgent(name, namespace, remoteWriteURL, bufferSize string) (*vmv1.VLAgent, error) {
	if bufferSize == "" {
		bufferSize = migrate.DefaultAgentBufferSize
	}
	size, err := resource.ParseQuantity(bufferSize)
	if err != nil {
		return nil, fmt.Errorf("cannot parse agent buffer size %q: %w", bufferSize, err)
	}
	return &vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vmv1.VLAgentSpec{
			Storage: &vmv1beta1.StorageSpec{
				VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceStorage: size},
						},
					},
				},
			},
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{URL: remoteWriteURL},
			},
		},
	}, nil
}
