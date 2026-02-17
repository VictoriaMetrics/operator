package v1alpha1

import (
	"fmt"
	"testing"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/internal/config"
)

// TestConvertNomadScrapeConfigMultiNamespace tests the conversion of a
// Prometheus ScrapeConfig with multiple nomadSDConfigs targeting different namespaces.
func TestConvertNomadScrapeConfigMultiNamespace(t *testing.T) {
	promSC := &promv1alpha1.ScrapeConfig{
		Spec: promv1alpha1.ScrapeConfigSpec{
			ScrapeInterval: promv1.DurationPointer("20s"),
			ScrapeTimeout:  promv1.DurationPointer("19s"),
			NomadSDConfigs: []promv1alpha1.NomadSDConfig{
				{
					Server:          "https://nomad.example.com:4646",
					Namespace:       ptr.To("default"),
					AllowStale:      ptr.To(true),
					RefreshInterval: promv1.DurationPointer("15s"),
					Authorization: &promv1.SafeAuthorization{
						Credentials: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "nomad-secret"},
							Key:                  "NOMAD_TOKEN",
						},
					},
				},
				{
					Server:          "https://nomad.example.com:4646",
					Namespace:       ptr.To("staging"),
					AllowStale:      ptr.To(true),
					RefreshInterval: promv1.DurationPointer("15s"),
					Authorization: &promv1.SafeAuthorization{
						Credentials: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "nomad-secret"},
							Key:                  "NOMAD_TOKEN",
						},
					},
				},
			},
			RelabelConfigs: []promv1.RelabelConfig{
				{Action: "replace", SourceLabels: []promv1.LabelName{"__meta_nomad_namespace"}, TargetLabel: "namespace"},
				{Action: "replace", SourceLabels: []promv1.LabelName{"__meta_nomad_service"}, TargetLabel: "service"},
				{Action: "replace", SourceLabels: []promv1.LabelName{"__meta_nomad_node_id"}, TargetLabel: "node"},
				{Action: "keep", Regex: "metrics", SourceLabels: []promv1.LabelName{"__meta_nomad_service"}},
			},
			MetricRelabelConfigs: []promv1.RelabelConfig{
				{
					Action:       "replace",
					Regex:        `org-id-123`,
					Replacement:  ptr.To("my-org"),
					SourceLabels: []promv1.LabelName{"customer"},
					TargetLabel:  "tenant",
				},
			},
		},
	}

	got := ConvertScrapeConfig(promSC, &config.BaseOperatorConf{})

	// Check NomadSDConfigs were converted
	if len(got.Spec.NomadSDConfigs) != 2 {
		t.Fatalf("expected 2 NomadSDConfigs, got %d", len(got.Spec.NomadSDConfigs))
	}

	// Check first config
	cfg0 := got.Spec.NomadSDConfigs[0]
	assertEqual(t, "cfg[0].Server", cfg0.Server, "https://nomad.example.com:4646")
	assertPtrEqual(t, "cfg[0].Namespace", cfg0.Namespace, "default")
	assertPtrEqual(t, "cfg[0].AllowStale", cfg0.AllowStale, true)

	if cfg0.Authorization == nil {
		t.Error("cfg[0].Authorization is nil, expected non-nil")
	} else {
		if cfg0.Authorization.Credentials == nil {
			t.Error("cfg[0].Authorization.Credentials is nil")
		} else {
			assertEqual(t, "cfg[0].Authorization.Credentials.Name", cfg0.Authorization.Credentials.Name, "nomad-secret")
			assertEqual(t, "cfg[0].Authorization.Credentials.Key", cfg0.Authorization.Credentials.Key, "NOMAD_TOKEN")
		}
	}

	// Check second config
	cfg1 := got.Spec.NomadSDConfigs[1]
	assertEqual(t, "cfg[1].Server", cfg1.Server, "https://nomad.example.com:4646")
	assertPtrEqual(t, "cfg[1].Namespace", cfg1.Namespace, "staging")

	// Check scrape params were converted
	assertEqual(t, "ScrapeInterval", got.Spec.EndpointScrapeParams.ScrapeInterval, "20s")
	assertEqual(t, "ScrapeTimeout", got.Spec.EndpointScrapeParams.ScrapeTimeout, "19s")

	// Check relabel configs
	if len(got.Spec.RelabelConfigs) != 4 {
		t.Errorf("expected 4 RelabelConfigs, got %d", len(got.Spec.RelabelConfigs))
	}

	// Check metric relabel configs
	if len(got.Spec.MetricRelabelConfigs) != 1 {
		t.Errorf("expected 1 MetricRelabelConfigs, got %d", len(got.Spec.MetricRelabelConfigs))
	}
}

// TestConvertNomadRefreshInterval verifies that refreshInterval is correctly
// preserved during Prometheus→VM conversion.
func TestConvertNomadRefreshInterval(t *testing.T) {
	promSC := &promv1alpha1.ScrapeConfig{
		Spec: promv1alpha1.ScrapeConfigSpec{
			NomadSDConfigs: []promv1alpha1.NomadSDConfig{{
				Server:          "https://nomad.example.com:4646",
				RefreshInterval: promv1.DurationPointer("15s"),
			}},
		},
	}

	got := ConvertScrapeConfig(promSC, &config.BaseOperatorConf{})

	if len(got.Spec.NomadSDConfigs) != 1 {
		t.Fatalf("expected 1 NomadSDConfig, got %d", len(got.Spec.NomadSDConfigs))
	}

	cfg := got.Spec.NomadSDConfigs[0]
	assertEqual(t, "Server", cfg.Server, "https://nomad.example.com:4646")

	if cfg.RefreshInterval == nil {
		t.Fatal("RefreshInterval is nil, expected '15s'")
	}
	assertEqual(t, "RefreshInterval", *cfg.RefreshInterval, "15s")
}

func assertEqual[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func assertPtrEqual[T comparable](t *testing.T, name string, got *T, want T) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: got nil, want %v", name, want)
		return
	}
	if *got != want {
		t.Errorf("%s: got %v, want %v", name, *got, want)
	}
}

func ptrStr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func ptrBool(p *bool) string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", *p)
}

// TestVerifyNomadFieldMapping checks that all Prometheus NomadSDConfig JSON
// fields that have a corresponding VM field are correctly mapped.
func TestVerifyNomadFieldMapping(t *testing.T) {
	promSC := &promv1alpha1.ScrapeConfig{
		Spec: promv1alpha1.ScrapeConfigSpec{
			NomadSDConfigs: []promv1alpha1.NomadSDConfig{{
				Server:          "https://nomad.example.com:4646",
				Namespace:       ptr.To("prod"),
				Region:          ptr.To("us-west-1"),
				TagSeparator:    ptr.To(","),
				AllowStale:      ptr.To(true),
				FollowRedirects: ptr.To(true),
				RefreshInterval: promv1.DurationPointer("30s"),
				BasicAuth: &promv1.BasicAuth{
					Username: corev1.SecretKeySelector{Key: "user"},
					Password: corev1.SecretKeySelector{Key: "pass"},
				},
				TLSConfig: &promv1.SafeTLSConfig{
					InsecureSkipVerify: ptr.To(true),
				},
			}},
		},
	}

	got := ConvertScrapeConfig(promSC, &config.BaseOperatorConf{})
	cfg := got.Spec.NomadSDConfigs[0]

	assertEqual(t, "Server", cfg.Server, "https://nomad.example.com:4646")
	assertPtrEqual(t, "Namespace", cfg.Namespace, "prod")
	assertPtrEqual(t, "Region", cfg.Region, "us-west-1")
	assertPtrEqual(t, "TagSeparator", cfg.TagSeparator, ",")
	assertPtrEqual(t, "AllowStale", cfg.AllowStale, true)
	assertPtrEqual(t, "FollowRedirects", cfg.FollowRedirects, true)
	assertPtrEqual(t, "RefreshInterval", cfg.RefreshInterval, "30s")

	if cfg.BasicAuth == nil {
		t.Fatal("BasicAuth is nil")
	}
	assertEqual(t, "BasicAuth.Username.Key", cfg.BasicAuth.Username.Key, "user")
	assertEqual(t, "BasicAuth.Password.Key", cfg.BasicAuth.Password.Key, "pass")

	if cfg.TLSConfig == nil {
		t.Fatal("TLSConfig is nil")
	}
	if !cfg.TLSConfig.InsecureSkipVerify {
		t.Error("TLSConfig.InsecureSkipVerify should be true")
	}
}
