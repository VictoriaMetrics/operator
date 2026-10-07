package vmestimator

import (
	"context"
	"crypto/sha256"
	"fmt"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const configFileName = "streams.yaml"

// defaultStreams are used if neither spec.streams nor spec.streamsConfigMap is set
var defaultStreams = []vmv1.VMEstimatorStream{
	{Interval: "5m"},
	{Interval: "5m", GroupBy: []string{"job"}},
	{Interval: "5m", GroupBy: []string{"__name__"}},
}

// streamsConfig defines vmestimator configuration file
// See https://docs.victoriametrics.com/victoriametrics/vmestimator/#configuration
type streamsConfig struct {
	Streams []vmv1.VMEstimatorStream `yaml:"streams"`
}

func configMapName(cr *vmv1.VMEstimator) string {
	return cr.PrefixedName(vmv1beta1.ClusterComponentRoot)
}

// buildConfig returns vmestimator configuration file content
func buildConfig(cr *vmv1.VMEstimator, ac *build.AssetsCache) ([]byte, error) {
	if len(cr.Spec.Streams) == 0 && cr.Spec.StreamsConfigMap == nil {
		return yaml.Marshal(streamsConfig{Streams: defaultStreams})
	}
	streams := append([]vmv1.VMEstimatorStream{}, cr.Spec.Streams...)
	if cm := cr.Spec.StreamsConfigMap; cm != nil {
		data, err := ac.LoadKeyFromConfigMap(cr.Namespace, cm)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch streams configmap=%q: %w", cm.Name, err)
		}
		var c streamsConfig
		if err := yaml.UnmarshalStrict([]byte(data), &c); err != nil {
			return nil, fmt.Errorf("cannot parse streams from configmap=%q, key=%q: %w", cm.Name, cm.Key, err)
		}
		for idx := range c.Streams {
			if err := c.Streams[idx].Validate(); err != nil {
				return nil, fmt.Errorf("incorrect stream at configmap=%q, key=%q, idx=%d: %w", cm.Name, cm.Key, idx, err)
			}
		}
		streams = append(streams, c.Streams...)
	}
	return yaml.Marshal(streamsConfig{Streams: streams})
}

func buildConfigMap(cr *vmv1.VMEstimator, data []byte) *corev1.ConfigMap {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentRoot)
	return &corev1.ConfigMap{
		ObjectMeta: build.ResourceMeta(build.SecretConfigResourceKind, b),
		Data: map[string]string{
			configFileName: string(data),
		},
	}
}

// createOrUpdateConfig reconciles vmestimator configuration and returns its hash
func createOrUpdateConfig(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) (string, error) {
	ac := build.NewAssetsCache(ctx, rclient, map[build.ResourceKind]*build.ResourceCfg{})
	data, err := buildConfig(cr, ac)
	if err != nil {
		return "", err
	}
	var prevMeta *metav1.ObjectMeta
	if prevCR != nil {
		b := build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentRoot)
		prevMeta = ptr.To(build.ResourceMeta(build.SecretConfigResourceKind, b))
	}
	owner := cr.AsOwner()
	if _, err := reconcile.ConfigMap(ctx, rclient, buildConfigMap(cr, data), prevMeta, &owner); err != nil {
		return "", fmt.Errorf("cannot reconcile configmap: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}
