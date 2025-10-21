package vmagent

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v2"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func generateStaticScrapeConfig(
	ctx context.Context,
	cr *vmv1beta1.VMAgent,
	sc *vmv1beta1.VMStaticScrape,
	ep *vmv1beta1.TargetEndpoint,
	i int,
	ac *build.AssetsCache,
	se vmv1beta1.VMAgentSecurityEnforcements,
) (yaml.MapSlice, error) {
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("staticScrape/%s/%s/%d", sc.Namespace, sc.Name, i),
		},
	}

	scrapeClass := getScrapeClass(sc.Spec.ScrapeClassName, cr)
	if scrapeClass != nil {
		mergeEndPointAuthWithScrapeClass(&ep.EndpointAuth, scrapeClass)
		mergeEndpointRelabelingsWithScrapeClass(&ep.EndpointRelabelings, scrapeClass)
	}

	tgs := yaml.MapSlice{{Key: "targets", Value: ep.Targets}}
	if ep.Labels != nil {
		tgs = append(tgs, yaml.MapItem{Key: "labels", Value: ep.Labels})
	}

	cfg = append(cfg, yaml.MapItem{Key: "static_configs", Value: []yaml.MapSlice{tgs}})

	// set defaults
	if ep.SampleLimit == 0 {
		ep.SampleLimit = sc.Spec.SampleLimit
	}
	if ep.SeriesLimit == 0 {
		ep.SeriesLimit = sc.Spec.SeriesLimit
	}
	if ep.ScrapeTimeout == "" {
		ep.ScrapeTimeout = cr.Spec.ScrapeTimeout
	}
	setScrapeIntervalToWithLimit(ctx, &ep.EndpointScrapeParams, cr)

	cfg = addCommonScrapeParamsTo(cfg, ep.EndpointScrapeParams, se)

	var relabelings []yaml.MapSlice

	if sc.Spec.JobName != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "target_label", Value: "job"},
			{Key: "replacement", Value: sc.Spec.JobName},
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
	relabelings = enforceNamespaceLabel(relabelings, sc.Namespace, se.EnforcedNamespaceLabel)

	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})
	cfg = addMetricRelabelingsTo(cfg, ep.MetricRelabelConfigs, se)
	if c, err := buildVMScrapeParams(sc.Namespace, ep.VMScrapeParams, ac); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}
	return addEndpointAuthTo(cfg, &ep.EndpointAuth, sc.Namespace, ac)
}
