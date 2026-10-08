package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestPeerOutlierModelRoundTrip(t *testing.T) {
	for _, class := range []string{"peer_outlier", "model.online.PeerOutlierModel"} {
		for _, params := range []string{"", `
min_peer_count: 10
epsilon_quantile: 0.9
tolerance: 6
decay: 0.999
min_n_samples_seen: 96
groupby: [service, mode]
queries: [cpu]
schedulers: [minute]
data_range: [0, 100]
clip_predictions: true
detection_direction: above_expected
`} {
			t.Run(class+params, func(t *testing.T) {
				input := []byte("class: " + class + "\n" + params)
				var m model
				require.NoError(t, m.Validate(input))
				output, err := yaml.Marshal(&m)
				require.NoError(t, err)
				var before, after map[string]any
				require.NoError(t, yaml.Unmarshal(input, &before))
				require.NoError(t, yaml.Unmarshal(output, &after))
				assert.Equal(t, before, after)
				if params != "" {
					m.addPrefix("ns-config")
					assert.Equal(t, []string{"ns-config-cpu"}, m.queries())
					assert.Equal(t, []string{"ns-config-minute"}, m.schedulers())
					assert.Equal(t, []string{"service", "mode"}, m.anomalyModel.(*peerOutlierModel).GroupBy)
				}
			})
		}
	}
}

func TestPeerOutlierModelValidation(t *testing.T) {
	for _, params := range []string{
		"min_peer_count: 2", "epsilon_quantile: 0", "epsilon_quantile: 1",
		"tolerance: 0", "decay: 0", "decay: 1.1", "min_n_samples_seen: 0",
		"args: {unknown: 1}", "decay: 0.9", "decay: 0.5\nmin_n_samples_seen: 3",
	} {
		t.Run(params, func(t *testing.T) {
			var m model
			assert.Error(t, m.Validate(fmt.Appendf(nil, "class: peer_outlier\n%s\n", params)))
		})
	}
	var m model
	require.NoError(t, m.Validate([]byte("class: peer_outlier\ndecay: 0.5\nmin_n_samples_seen: 2\n")))
}

func TestV130ModelValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name: "history strength must be positive",
			config: `
class: zscore
history_strength: 0
`,
			wantErr: "history_strength must be greater than 0",
		},
		{
			name: "autotune anomaly percentage is required",
			config: `
class: auto
tuned_class_name: temporal_envelope
optimization_params: {}
`,
			wantErr: "anomaly_percentage is required",
		},
		{
			name: "autotune parallelism is bounded",
			config: `
class: auto
tuned_class_name: temporal_envelope
optimization_params:
  anomaly_percentage: 0.02
  n_jobs: 0
`,
			wantErr: "n_jobs must be -1 or a positive integer",
		},
		{
			name: "envelope quantiles cannot be empty",
			config: `
class: temporal_envelope
quantiles: []
`,
			wantErr: "quantiles must contain 2 ordered values",
		},
		{
			name: "envelope seasonality must be a preset",
			config: `
class: temporal_envelope
seasonalities: [hourly]
`,
			wantErr: "unsupported temporal_envelope seasonality",
		},
		{
			name: "multivariate advisory limit cannot exceed hard limit",
			config: `
class: temporal_envelope_multivariate
max_channels: 10
recommended_max_channels: 20
`,
			wantErr: "recommended_max_channels must not exceed max_channels",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m model
			err := m.Validate([]byte(tt.config))
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
