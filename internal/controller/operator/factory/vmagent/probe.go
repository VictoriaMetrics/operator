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
	i int,
	apiserverConfig *vmv1beta1.APIServerConfig,
	ac *build.AssetsCache,
	se vmv1beta1.VMAgentSecurityEnforcements,
) (yaml.MapSlice, error) {
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("probe/%s/%s/%d", sc.Namespace, sc.Name, i),
		},
	}

	scrapeClass := getScrapeClass(sc.Spec.ScrapeClassName, cr)
	if scrapeClass != nil {
		mergeEndPointAuthWithScrapeClass(&sc.Spec.EndpointAuth, scrapeClass)
	}

	// add defaults
	if sc.Spec.VMProberSpec.Path == "" {
		sc.Spec.VMProberSpec.Path = "/probe"
	}
	sc.Spec.Path = sc.Spec.VMProberSpec.Path

	if len(sc.Spec.Module) > 0 {
		if sc.Spec.Params == nil {
			sc.Spec.Params = make(map[string][]string)
		}
		sc.Spec.Params["module"] = []string{sc.Spec.Module}
	}
	if len(sc.Spec.VMProberSpec.Scheme) > 0 {
		sc.Spec.Scheme = sc.Spec.VMProberSpec.Scheme
	}

	setScrapeIntervalToWithLimit(ctx, &sc.Spec.EndpointScrapeParams, cr)

	cfg = addCommonScrapeParamsTo(cfg, sc.Spec.EndpointScrapeParams, se)

	var relabelings []yaml.MapSlice

	if sc.Spec.Targets.StaticConfig != nil {
		staticConfig := yaml.MapSlice{
			{Key: "targets", Value: sc.Spec.Targets.StaticConfig.Targets},
		}
		if sc.Spec.Targets.StaticConfig.Labels != nil {
			staticConfig = append(staticConfig,
				yaml.MapSlice{
					{Key: "labels", Value: sc.Spec.Targets.StaticConfig.Labels},
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
		for _, r := range sc.Spec.Targets.StaticConfig.RelabelConfigs {
			relabelings = append(relabelings, generateRelabelConfig(r))
		}
	}
	if sc.Spec.Targets.Ingress != nil {

		skipRelabelSelectors := cr.Spec.EnableKubernetesAPISelectors
		relabelings = addSelectorToRelabelingFor(relabelings, "ingress", sc.Spec.Targets.Ingress.Selector, skipRelabelSelectors)
		selectedNamespaces := getNamespacesFromNamespaceSelector(&sc.Spec.Targets.Ingress.NamespaceSelector, sc.Namespace, se.IgnoreNamespaceSelectors)

		k8sSDOpts := generateK8SSDConfigOptions{
			namespaces:         selectedNamespaces,
			shouldAddSelectors: cr.Spec.EnableKubernetesAPISelectors,
			selectors:          sc.Spec.Targets.Ingress.Selector,
			apiServerConfig:    apiserverConfig,
			role:               kubernetesSDRoleIngress,
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
		for _, r := range sc.Spec.Targets.Ingress.RelabelConfigs {
			relabelings = append(relabelings, generateRelabelConfig(r))
		}

	}

	if sc.Spec.JobName != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "target_label", Value: "job"},
			{Key: "replacement", Value: sc.Spec.JobName},
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
			{Key: "replacement", Value: sc.Spec.VMProberSpec.URL},
		},
	}...)

	for _, trc := range cr.Spec.ProbeScrapeRelabelTemplate {
		relabelings = append(relabelings, generateRelabelConfig(trc))
	}
	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, sc.Namespace, se.EnforcedNamespaceLabel)

	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})
	cfg = addMetricRelabelingsTo(cfg, sc.Spec.MetricRelabelConfigs, se)
	if c, err := buildVMScrapeParams(sc.Namespace, sc.Spec.VMScrapeParams, ac); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}
	return addEndpointAuthTo(cfg, &sc.Spec.EndpointAuth, sc.Namespace, ac)
}
