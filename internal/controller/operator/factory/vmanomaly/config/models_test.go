package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
