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

func TestAutoPeerModelRoundTrip(t *testing.T) {
	input := []byte(`class: auto
tuned_class_name: peer_outlier
queries: [cpu]
clip_predictions: true
optimization_params:
  anomaly_percentage: 0.02
  frozen_params:
    groupby: [service, mode]
    min_peer_count: 5
  exact: true
`)
	var m model
	require.NoError(t, m.Validate(input))
	output, err := yaml.Marshal(&m)
	require.NoError(t, err)
	var before, after map[string]any
	require.NoError(t, yaml.Unmarshal(input, &before))
	require.NoError(t, yaml.Unmarshal(output, &after))
	assert.Equal(t, before, after)
	m.addPrefix("ns-config")
	assert.Equal(t, []string{"ns-config-cpu"}, m.queries())
	assert.Equal(t, "peer_outlier", m.anomalyModel.(*autoTunedModel).TunedClassName)
}

func TestPeerOutlierModelValidation(t *testing.T) {
	for _, tc := range []struct {
		params  string
		wantErr string
	}{
		{"min_peer_count: 2", "min_peer_count must be at least 3"},
		{"epsilon_quantile: 0", "epsilon_quantile must be in range (0, 1)"},
		{"epsilon_quantile: 1", "epsilon_quantile must be in range (0, 1)"},
		{"tolerance: 0", "tolerance must be finite and greater than 0"},
		{"tolerance: -1", "tolerance must be finite and greater than 0"},
		{"tolerance: .inf", "tolerance must be finite and greater than 0"},
		{"tolerance: -.inf", "tolerance must be finite and greater than 0"},
		{"tolerance: .nan", "tolerance must be finite and greater than 0"},
		{"decay: 0", "decay must be in range (0, 1]"},
		{"decay: 1.1", "decay must be in range (0, 1]"},
		{"min_n_samples_seen: 0", "min_n_samples_seen must be positive"},
		{"args: {unknown: 1}", "peer_outlier does not accept arbitrary args"},
		{"decay: 0.9", "warmup derived from epsilon_quantile (32) cannot be reached with decay 0.9"},
		{"decay: 0.9\nepsilon_quantile: 0.9", "warmup derived from epsilon_quantile (80) cannot be reached with decay 0.9"},
		{"decay: 0.5\nmin_n_samples_seen: 3", "min_n_samples_seen (3) cannot be reached with decay 0.5"},
	} {
		t.Run(tc.params, func(t *testing.T) {
			var m model
			assert.ErrorContains(t, m.Validate(fmt.Appendf(nil, "class: peer_outlier\n%s\n", tc.params)), tc.wantErr)
		})
	}
	var m model
	require.NoError(t, m.Validate([]byte("class: peer_outlier\ndecay: 0.5\nmin_n_samples_seen: 2\n")))
	require.NoError(t, m.Validate([]byte("class: peer_outlier\ndecay: 0.9\nmin_n_samples_seen: 16\n")))
	require.NoError(t, m.Validate([]byte("class: peer_outlier\ndecay: 0.9\nepsilon_quantile: 0.5\n")))
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

func TestEnvelopeVersionRoundTrip(t *testing.T) {
	for _, class := range []string{"temporal_envelope", "model.online.TemporalEnvelopeModel", "temporal_envelope_multivariate", "model.online.TemporalEnvelopeMultivariateModel"} {
		for _, params := range []string{"", "model_version: v1\nseasonalities: [month_smooth]\n", "model_version: v2\nseasonalities: [hod_smooth, dow_smooth, calendar_month]\n"} {
			t.Run(class+params, func(t *testing.T) {
				input := []byte("class: " + class + "\n" + params)
				var m model
				require.NoError(t, yaml.UnmarshalStrict(input, &m))
				require.NoError(t, m.validate())
				output, err := yaml.Marshal(&m)
				require.NoError(t, err)
				var before, after map[string]any
				require.NoError(t, yaml.Unmarshal(input, &before))
				require.NoError(t, yaml.Unmarshal(output, &after))
				assert.Equal(t, before, after)
			})
		}
	}
}

func TestEnvelopeVersionValidation(t *testing.T) {
	for _, class := range []string{"temporal_envelope", "temporal_envelope_multivariate"} {
		for _, tc := range []struct{ params, wantErr string }{
			{"model_version: v3", "model_version must be v1 or v2"},
			{"model_version: ''", "model_version must be v1 or v2"},
			{"seasonalities: [calendar_month]", "calendar_month requires model_version=v2"},
			{"model_version: v1\nseasonalities: [calendar_month]", "calendar_month requires model_version=v2"},
			{"model_version: v2\nseasonalities: [month_smooth]", "month_smooth requires model_version=v1"},
			{"model_version: v2\nseasonalities: [month_plateau]", "month_plateau requires model_version=v1"},
			{"model_version: v2\nseasonalities: [calendar_month, calendar_month]", "duplicate temporal_envelope seasonality"},
			{"model_version: v2\nseasonalities: [unknown]", "unsupported temporal_envelope seasonality"},
		} {
			t.Run(class+tc.params, func(t *testing.T) {
				var m model
				assert.ErrorContains(t, m.Validate([]byte("class: "+class+"\n"+tc.params)), tc.wantErr)
			})
		}
	}
	for _, version := range []string{"v1", "v2"} {
		t.Run("dependency_strength/"+version, func(t *testing.T) {
			var m model
			err := m.Validate([]byte("class: temporal_envelope_multivariate\nmodel_version: " + version + "\nprovide_series: [dependency_strength]\n"))
			if version == "v1" {
				require.ErrorContains(t, err, "dependency_strength requires model_version=v2")
			} else {
				require.NoError(t, err)
				output, err := yaml.Marshal(&m)
				require.NoError(t, err)
				assert.Contains(t, string(output), "dependency_strength")
			}
		})
	}
}
