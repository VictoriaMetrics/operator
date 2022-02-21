package factory

import (
	"fmt"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"gopkg.in/yaml.v2"
)

func generateStaticScrapeConfig(
	cr *victoriametricsv1beta1.VMAgent,
	m *victoriametricsv1beta1.VMStaticScrape,
	ep *victoriametricsv1beta1.TargetEndpoint,
	i int,
	ssCache *scrapesSecretsCache,
	overrideHonorLabels, overrideHonorTimestamps bool,
	enforcedNamespaceLabel string,
) yaml.MapSlice {

	hl := honorLabels(ep.HonorLabels, overrideHonorLabels)
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("staticScrape/%s/%s/%d", m.Namespace, m.Name, i),
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

	var scrapeInterval string
	if ep.ScrapeInterval != "" {
		scrapeInterval = ep.ScrapeInterval
	} else if ep.Interval != "" {
		scrapeInterval = ep.Interval
	}
	scrapeInterval = limitScrapeInterval(scrapeInterval, cr.Spec.MinScrapeInterval, cr.Spec.MaxScrapeInterval)

	if scrapeInterval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: scrapeInterval})
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

	if ep.FollowRedirects != nil {
		cfg = append(cfg, yaml.MapItem{Key: "follow_redirects", Value: ep.FollowRedirects})
	}
	if ep.Params != nil {
		cfg = append(cfg, yaml.MapItem{Key: "params", Value: ep.Params})
	}
	if ep.Scheme != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scheme", Value: ep.Scheme})
	}

	cfg = addTLStoYaml(cfg, m.Namespace, ep.TLSConfig, false)

	if ep.BearerTokenFile != "" {
		cfg = append(cfg, yaml.MapItem{Key: "bearer_token_file", Value: ep.BearerTokenFile})
	}

	if ep.BearerTokenSecret != nil && ep.BearerTokenSecret.Name != "" {
		if s, ok := ssCache.bearerTokens[m.AsMapKey(i)]; ok {
			cfg = append(cfg, yaml.MapItem{Key: "bearer_token", Value: s})
		}
	}
	if ep.BasicAuth != nil {
		var bac yaml.MapSlice
		if s, ok := ssCache.baSecrets[m.AsMapKey(i)]; ok {
			bac = append(bac,
				yaml.MapItem{Key: "username", Value: s.username},
				yaml.MapItem{Key: "password", Value: s.password},
			)
		}
		if len(ep.BasicAuth.PasswordFile) > 0 {
			bac = append(bac, yaml.MapItem{Key: "password_file", Value: ep.BasicAuth.PasswordFile})
		}
		if len(bac) > 0 {
			cfg = append(cfg, yaml.MapItem{Key: "basic_auth", Value: bac})
		}
	}

	var relabelings []yaml.MapSlice

	if m.Spec.JobName != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "target_label", Value: "job"},
			{Key: "replacement", Value: m.Spec.JobName},
		})
	}

	for _, c := range ep.RelabelConfigs {
		relabelings = append(relabelings, generateRelabelConfig(c))
	}
	for _, trc := range cr.Spec.StaticScrapeRelabelTemplate {
		relabelings = append(relabelings, generateRelabelConfig(trc))
	}
	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, m.Namespace, enforcedNamespaceLabel)
	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})

	if ep.SampleLimit > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "sample_limit", Value: ep.SampleLimit})
	} else if m.Spec.SampleLimit > 0 {
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

	cfg = append(cfg, buildVMScrapeParams(m.Namespace, m.AsProxyKey(i), ep.VMScrapeParams, ssCache)...)

	if ep.OAuth2 != nil {
		r := buildOAuth2Config(m.AsMapKey(i), ep.OAuth2, ssCache.oauth2Secrets)
		if len(r) > 0 {
			cfg = append(cfg, yaml.MapItem{Key: "oauth2", Value: r})
		}
	}
	return cfg
}
