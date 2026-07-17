package migrate

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// DefaultAgentBufferSize is the default buffer-agent persistent-queue disk size.
const DefaultAgentBufferSize = "10Gi"

// ParseAgentBufferSize parses and validates a buffer-agent persistent-queue disk size,
// defaulting to DefaultAgentBufferSize when bufferSize is empty. A non-positive quantity would
// pass resource.ParseQuantity but leave NoDowntime with no usable write buffer.
func ParseAgentBufferSize(bufferSize string) (resource.Quantity, error) {
	if bufferSize == "" {
		bufferSize = DefaultAgentBufferSize
	}
	size, err := resource.ParseQuantity(bufferSize)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("cannot parse agent buffer size %q: %w", bufferSize, err)
	}
	if size.Sign() <= 0 {
		return resource.Quantity{}, fmt.Errorf("agent buffer size %q must be positive", bufferSize)
	}
	return size, nil
}

// BufferAgentStorage builds the persistent-queue StorageSpec shared by the VMAgent and VLAgent
// buffer agents, sized to hold size worth of buffered writes.
func BufferAgentStorage(size resource.Quantity) *vmv1beta1.StorageSpec {
	return &vmv1beta1.StorageSpec{
		VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: size},
				},
			},
		},
	}
}

// Strategy selects the migration cutover mechanism.
type Strategy string

const (
	StrategyWithDowntime Strategy = "WithDowntime"
	StrategyNoDowntime   Strategy = "NoDowntime"
)

// Chart identifies the source Helm chart, matching internal/converter's own chart names.
type Chart string

const (
	ChartVMSingle  Chart = "victoria-metrics-single"
	ChartVMCluster Chart = "victoria-metrics-cluster"
	ChartVLSingle  Chart = "victoria-logs-single"
	ChartVLCluster Chart = "victoria-logs-cluster"
	ChartVTSingle  Chart = "victoria-traces-single"
	ChartVTCluster Chart = "victoria-traces-cluster"
)

// Options configures a single migrate invocation.
type Options struct {
	Chart             Chart
	Strategy          Strategy
	Namespace         string
	ReleaseName       string
	ValuesFile        string
	TargetName        string
	Kubeconfig        string
	Yes               bool
	DryRun            bool
	AgentBufferSize   string
	SnapshotClassName string
}

func helmLabels(releaseName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/instance":   releaseName,
		"app.kubernetes.io/managed-by": "Helm",
	}
}
