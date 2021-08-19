package factory

import (
	"fmt"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"gopkg.in/yaml.v2"
)

func generateStaticScrapeConfig(
	m *victoriametricsv1beta1.VMStaticScrape,
	ep *victoriametricsv1beta1.TargetEndpoint,
	i int,
	ssCache *scrapesSecretsCache,
	bearerTokens map[string]BearerToken,
	overrideHonorLabels, overrideHonorTimestamps bool,
	enforcedNamespaceLabel string,
) yaml.MapSlice {

	hl := honorLabels(ep.HonorLabels, overrideHonorLabels)
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("%s/%s/%d", m.Namespace, m.Name, i),
		},
		{
			Key:   "honor_labels",
			Value: hl,
		},
	}
	cfg = honorTimestamps(cfg, ep.HonorTimestamps, overrideHonorTimestamps)

	tgs := yaml.MapSlice{{Key: "targets", Value: ep.Targets}}
	if ep.Labels != nil {
		tgs = append(tgs, yaml.MapItem{Key: "labels", Value: ep.Labels})
	}

	cfg = append(cfg, yaml.MapItem{Key: "static_configs", Value: []yaml.MapSlice{tgs}})
	if ep.ScrapeInterval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: ep.ScrapeInterval})
	} else if ep.Interval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: ep.Interval})
	}
	if ep.ScrapeTimeout != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_timeout", Value: ep.ScrapeTimeout})
	}
	if ep.Path != "" {
		cfg = append(cfg, yaml.MapItem{Key: "metrics_path", Value: ep.Path})
	}
	if ep.ProxyURL != nil {
		cfg = append(cfg, yaml.MapItem{Key: "proxy_url", Value: ep.ProxyURL})
	}
	if ep.Params != nil {
		cfg = append(cfg, yaml.MapItem{Key: "params", Value: ep.Params})
	}
	if ep.Scheme != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scheme", Value: ep.Scheme})
	}

	cfg = addTLStoYaml(cfg, m.Namespace, ep.TLSConfig)

	if ep.BearerTokenFile != "" {
		cfg = append(cfg, yaml.MapItem{Key: "bearer_token_file", Value: ep.BearerTokenFile})
	}

	if ep.BearerTokenSecret.Name != "" {
		if s, ok := bearerTokens[m.AsKey(i)]; ok {
			cfg = append(cfg, yaml.MapItem{Key: "bearer_token", Value: s})
		}
	}

	if ep.BasicAuth != nil {
		if s, ok := ssCache.baSecrets[m.AsKey(i)]; ok {
			cfg = append(cfg, yaml.MapItem{
				Key: "basic_auth", Value: yaml.MapSlice{
					{Key: "username", Value: s.username},
					{Key: "password", Value: s.password},
				},
			})
		}
	}

	var relabelings []yaml.MapSlice

	if m.Spec.JobName != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "target_label", Value: "job"},
			{Key: "replacement", Value: m.Spec.JobName},
		})
	}

	if ep.RelabelConfigs != nil {
		for _, c := range ep.RelabelConfigs {
			relabelings = append(relabelings, generateRelabelConfig(c))
		}
	}
	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, m.Namespace, enforcedNamespaceLabel)
	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})

	if m.Spec.SampleLimit > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "sample_limit", Value: m.Spec.SampleLimit})
	}

	if ep.MetricRelabelConfigs != nil {
		var metricRelabelings []yaml.MapSlice
		for _, c := range ep.MetricRelabelConfigs {
			if c.TargetLabel != "" && enforcedNamespaceLabel != "" && c.TargetLabel == enforcedNamespaceLabel {
				continue
			}
			relabeling := generateRelabelConfig(c)
			metricRelabelings = append(metricRelabelings, relabeling)
		}
		cfg = append(cfg, yaml.MapItem{Key: "metric_relabel_configs", Value: metricRelabelings})
	}

	cfg = addTLStoYaml(cfg, m.Namespace, ep.TLSConfig)

	cfg = append(cfg, buildVMScrapeParams(ep.VMScrapeParams)...)
	if ep.OAuth2 != nil {
		r := buildOAuth2Config(m.AsMapKey(i), ep.OAuth2, ssCache.oauth2Secrets)
		if len(r) > 0 {
			cfg = append(cfg, yaml.MapItem{Key: "oauth2", Value: r})
		}
	}
	return cfg
}
