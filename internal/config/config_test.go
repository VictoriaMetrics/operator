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
			name:    "ubi",
			version: "operator-20260902-v0.75.0-ubi",
			want:    "v0.75.0-ubi",
		},
		{
			name:    "prerelease ubi",
			version: "operator-20260902-v0.75.0-rc0-ubi",
			want:    "v0.75.0-rc0-ubi",
		},
		{
			name:    "fips",
			version: "operator-20260902-v0.75.0-fips",
			want:    "v0.75.0-fips",
		},
		{
			name:    "prerelease fips",
			version: "operator-20260902-v0.75.0-rc0-fips",
			want:    "v0.75.0-rc0-fips",
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
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"prerelease", "v0.75.0-rc0", "victoriametrics/operator:config-reloader-v0.75.0-rc0"},
		{"ubi", "v0.75.0-ubi", "victoriametrics/operator:config-reloader-v0.75.0-ubi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("VM_OPERATOR_VERSION", tt.version)

			var cfg BaseOperatorConf
			if err := env.ParseWithOptions(&cfg, getEnvOpts()); err != nil {
				t.Fatalf("failed to parse config defaults: %v", err)
			}

			if cfg.ConfigReloader.Image != tt.want {
				t.Fatalf("config-reloader image = %q, want %q", cfg.ConfigReloader.Image, tt.want)
			}
		})
	}
}
