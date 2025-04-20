package vmagent

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"gopkg.in/yaml.v2"
)

func generateStaticScrapeConfig(
	ctx context.Context,
	vmagentCR *vmv1beta1.VMAgent,
	m *vmv1beta1.VMStaticScrape,
	ep *vmv1beta1.TargetEndpoint,
	i int,
	ssCache *scrapesSecretsCache,
	se vmv1beta1.VMAgentSecurityEnforcements,
) yaml.MapSlice {
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("staticScrape/%s/%s/%d", m.Namespace, m.Name, i),
		},
	}

	tgs := yaml.MapSlice{{Key: "targets", Value: ep.Targets}}
	if ep.Labels != nil {
		tgs = append(tgs, yaml.MapItem{Key: "labels", Value: ep.Labels})
	}

	cfg = append(cfg, yaml.MapItem{Key: "static_configs", Value: []yaml.MapSlice{tgs}})

	// set defaults
	if ep.SampleLimit == 0 {
		ep.SampleLimit = m.Spec.SampleLimit
	}
	if ep.SeriesLimit == 0 {
		ep.SeriesLimit = m.Spec.SeriesLimit
	}
	if ep.ScrapeTimeout == "" {
		ep.ScrapeTimeout = vmagentCR.Spec.ScrapeTimeout
	}
	setScrapeIntervalToWithLimit(ctx, &ep.EndpointScrapeParams, vmagentCR)

	cfg = addCommonScrapeParamsTo(cfg, ep.EndpointScrapeParams, se)

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
	for _, trc := range vmagentCR.Spec.StaticScrapeRelabelTemplate {
		relabelings = append(relabelings, generateRelabelConfig(trc))
	}
	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, m.Namespace, se.EnforcedNamespaceLabel)

	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})
	cfg = addMetricRelabelingsTo(cfg, ep.MetricRelabelConfigs, se)
	cfg = append(cfg, buildVMScrapeParams(m.Namespace, m.AsProxyKey(i), ep.VMScrapeParams, ssCache)...)
	cfg = addTLStoYaml(cfg, m.Namespace, ep.TLSConfig, false)
	cfg = addEndpointAuthTo(cfg, ep.EndpointAuth, m.Namespace, m.AsMapKey(i), ssCache)

	return cfg
}
