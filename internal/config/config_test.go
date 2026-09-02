package config

import (
	"testing"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo"
	"github.com/caarlos0/env/v11"
)

func TestGetVersion(t *testing.T) {
	original := buildinfo.Version
	t.Cleanup(func() { buildinfo.Version = original })

	tests := []struct {
		name     string
		version  string
		fallback string
		want     string
	}{
		{
			name:    "stable",
			version: "operator-v0.75.0",
			want:    "v0.75.0",
		},
		{
			name:    "prerelease",
			version: "operator-20260902-v0.75.0-rc0",
			want:    "v0.75.0-rc0",
		},
		{
			name:    "enterprise",
			version: "operator-v1.151.0-enterprise",
			want:    "v1.151.0-enterprise",
		},
		{
			name:    "cluster",
			version: "operator-v1.151.0-cluster",
			want:    "v1.151.0-cluster",
		},
		{
			name:     "fallback",
			version:  "operator-development",
			fallback: "v0.75.0-rc0",
			want:     "v0.75.0-rc0",
		},
		{
			name:     "invalid",
			version:  "operator-v1.75",
			fallback: "v0.75.0-rc0",
			want:     "v0.75.0-rc0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buildinfo.Version = tt.version
			if got := getVersion(tt.fallback); got != tt.want {
				t.Fatalf("getVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigReloaderImageVersion(t *testing.T) {
	t.Setenv("VM_OPERATOR_VERSION", "v0.75.0-rc0")

	var cfg BaseOperatorConf
	if err := env.ParseWithOptions(&cfg, getEnvOpts()); err != nil {
		t.Fatalf("failed to parse config defaults: %v", err)
	}

	want := "victoriametrics/operator:config-reloader-v0.75.0-rc0"
	if cfg.ConfigReloader.Image != want {
		t.Fatalf("config-reloader image = %q, want %q", cfg.ConfigReloader.Image, want)
	}
}
