package vmscrapes

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func Test_generateRelabelConfig(t *testing.T) {
	f := func(rc *vmv1beta1.RelabelConfig, want string) {
		// related fields only filled during json unmarshal
		j, err := json.Marshal(rc)
		assert.NoError(t, err)
		var rlbCfg vmv1beta1.RelabelConfig
		assert.NoError(t, json.Unmarshal(j, &rlbCfg))
		got := generateRelabelConfig(&rlbCfg)
		gotBytes, err := yaml.Marshal(got)
		assert.NoError(t, err)
		assert.Equal(t, want, string(gotBytes))
	}

	// ok base cfg
	f(&vmv1beta1.RelabelConfig{
		TargetLabel:  "address",
		SourceLabels: []string{"__address__"},
		Action:       "replace",
	}, `source_labels:
- __address__
target_label: address
action: replace
`)

	// ok base with underscore
	f(&vmv1beta1.RelabelConfig{
		UnderScoreTargetLabel:  "address",
		UnderScoreSourceLabels: []string{"__address__"},
		Action:                 "replace",
	}, `source_labels:
- __address__
target_label: address
action: replace
`)

	// ok base with graphite match labels
	f(&vmv1beta1.RelabelConfig{
		UnderScoreTargetLabel:  "address",
		UnderScoreSourceLabels: []string{"__address__"},
		Action:                 "graphite",
		Labels:                 map[string]string{"job": "$1", "instance": "${2}:8080"},
		Match:                  `foo.*.*.bar`,
	}, `source_labels:
- __address__
target_label: address
action: graphite
match: foo.*.*.bar
labels:
  instance: ${2}:8080
  job: $1
`)

	// with empty replacement and separator
	f(&vmv1beta1.RelabelConfig{
		UnderScoreTargetLabel:  "address",
		UnderScoreSourceLabels: []string{"__address__"},
		Action:                 "graphite",
		Labels:                 map[string]string{"job": "$1", "instance": "${2}:8080"},
		Match:                  `foo.*.*.bar`,
		Separator:              ptr.To(""),
		Replacement:            ptr.To(""),
	}, `source_labels:
- __address__
separator: ""
target_label: address
replacement: ""
action: graphite
match: foo.*.*.bar
labels:
  instance: ${2}:8080
  job: $1
`)
}

func getAssetsCache(ctx context.Context, rclient client.Client) *build.AssetsCache {
	cfg := map[build.ResourceKind]*build.ResourceCfg{
		build.SecretConfigResourceKind: {
			MountDir:   "/conf",
			SecretName: "conf-secret",
		},
		build.TLSAssetsResourceKind: {
			MountDir:   "/tls",
			SecretName: "tls-secret",
		},
	}
	return build.NewAssetsCache(ctx, rclient, cfg)
}
