package vmagent

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v2"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func generateProbeConfig(
	ctx context.Context,
	cr *vmv1beta1.VMAgent,
	sc *vmv1beta1.VMProbe,
	ac *build.AssetsCache,
) (yaml.MapSlice, error) {
	spec := &sc.Spec
	apiserverConfig := cr.Spec.APIServerConfig
	se := cr.Spec.VMAgentSecurityEnforcements
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("probe/%s/%s", sc.Namespace, sc.Name),
		},
	}

	scrapeClass := getScrapeClass(spec.ScrapeClassName, cr)
	if scrapeClass != nil {
		mergeEndpointAuthWithScrapeClass(&spec.EndpointAuth, scrapeClass)
	}

	// add defaults
	if spec.VMProberSpec.Path == "" {
		spec.VMProberSpec.Path = "/probe"
	}
	spec.Path = spec.VMProberSpec.Path

	if len(spec.Module) > 0 {
		if spec.Params == nil {
			spec.Params = make(map[string][]string)
		}
		spec.Params["module"] = []string{spec.Module}
	}
	if len(spec.VMProberSpec.Scheme) > 0 {
		spec.Scheme = spec.VMProberSpec.Scheme
	}

	setScrapeIntervalToWithLimit(ctx, &spec.EndpointScrapeParams, cr)

	cfg = addCommonScrapeParamsTo(cfg, spec.EndpointScrapeParams, se)

	var relabelings []yaml.MapSlice

	if spec.Targets.StaticConfig != nil {
		staticConfig := yaml.MapSlice{
			{Key: "targets", Value: spec.Targets.StaticConfig.Targets},
		}
		if spec.Targets.StaticConfig.Labels != nil {
			staticConfig = append(staticConfig,
				yaml.MapSlice{
					{Key: "labels", Value: spec.Targets.StaticConfig.Labels},
				}...)
		}

		cfg = append(cfg, yaml.MapItem{
			Key:   "static_configs",
			Value: []yaml.MapSlice{staticConfig},
		})

		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__address__"}},
			{Key: "target_label", Value: "__param_target"},
		})
		// Add configured relabelings.
		for _, r := range spec.Targets.StaticConfig.RelabelConfigs {
			relabelings = append(relabelings, generateRelabelConfig(r))
		}
	}
	if spec.Targets.Ingress != nil {

		skipRelabelSelectors := cr.Spec.EnableKubernetesAPISelectors
		relabelings = addSelectorToRelabelingFor(relabelings, "ingress", spec.Targets.Ingress.Selector, skipRelabelSelectors)
		selectedNamespaces := getNamespacesFromNamespaceSelector(&spec.Targets.Ingress.NamespaceSelector, sc.Namespace, se.IgnoreNamespaceSelectors)

		k8sSDOpts := generateK8SSDConfigOptions{
			namespaces:         selectedNamespaces,
			shouldAddSelectors: cr.Spec.EnableKubernetesAPISelectors,
			selectors:          spec.Targets.Ingress.Selector,
			apiServerConfig:    apiserverConfig,
			role:               k8sSDRoleIngress,
			namespace:          sc.Namespace,
		}
		if c, err := generateK8SSDConfig(ac, k8sSDOpts); err != nil {
			return nil, err
		} else {
			cfg = append(cfg, c...)
		}

		// Relabelings for ingress SD.
		relabelings = append(relabelings, []yaml.MapSlice{
			{
				{Key: "source_labels", Value: []string{"__address__"}},
				{Key: "separator", Value: ";"},
				{Key: "regex", Value: "(.*)"},
				{Key: "target_label", Value: "__tmp_ingress_address"},
				{Key: "replacement", Value: "$1"},
				{Key: "action", Value: "replace"},
			},
			{
				{Key: "source_labels", Value: []string{"__meta_kubernetes_ingress_scheme", "__address__", "__meta_kubernetes_ingress_path"}},
				{Key: "separator", Value: ";"},
				{Key: "regex", Value: "(.+);(.+);(.+)"},
				{Key: "target_label", Value: "__param_target"},
				{Key: "replacement", Value: "${1}://${2}${3}"},
				{Key: "action", Value: "replace"},
			},
			{
				{Key: "source_labels", Value: []string{"__meta_kubernetes_namespace"}},
				{Key: "target_label", Value: "namespace"},
			},
			{
				{Key: "source_labels", Value: []string{"__meta_kubernetes_ingress_name"}},
				{Key: "target_label", Value: "ingress"},
			},
		}...)

		// Add configured relabelings.
		for _, r := range spec.Targets.Ingress.RelabelConfigs {
			relabelings = append(relabelings, generateRelabelConfig(r))
		}

	}

	if spec.JobName != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "target_label", Value: "job"},
			{Key: "replacement", Value: spec.JobName},
		})
	}

	// Relabelings for prober.
	relabelings = append(relabelings, []yaml.MapSlice{
		{
			{Key: "source_labels", Value: []string{"__param_target"}},
			{Key: "target_label", Value: "instance"},
		},
		{
			{Key: "target_label", Value: "__address__"},
			{Key: "replacement", Value: spec.VMProberSpec.URL},
		},
	}...)

	for _, trc := range cr.Spec.ProbeScrapeRelabelTemplate {
		relabelings = append(relabelings, generateRelabelConfig(trc))
	}
	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, sc.Namespace, se.EnforcedNamespaceLabel)

	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})
	cfg = addMetricRelabelingsTo(cfg, spec.MetricRelabelConfigs, se)
	if c, err := buildVMScrapeParams(sc.Namespace, spec.VMScrapeParams, ac); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}
	return addEndpointAuthTo(cfg, &spec.EndpointAuth, sc.Namespace, ac)
}
