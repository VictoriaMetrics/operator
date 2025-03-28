package vmagent

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"gopkg.in/yaml.v2"
)

func generateProbeConfig(
	ctx context.Context,
	vmagentCR *vmv1beta1.VMAgent,
	cr *vmv1beta1.VMProbe,
	i int,
	apiserverConfig *vmv1beta1.APIServerConfig,
	ssCache *scrapesSecretsCache,
	se vmv1beta1.VMAgentSecurityEnforcements,
) yaml.MapSlice {
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("probe/%s/%s/%d", cr.Namespace, cr.Name, i),
		},
	}

	// add defaults
	if cr.Spec.VMProberSpec.Path == "" {
		cr.Spec.VMProberSpec.Path = "/probe"
	}
	cr.Spec.EndpointScrapeParams.Path = cr.Spec.VMProberSpec.Path

	if len(cr.Spec.Module) > 0 {
		if cr.Spec.Params == nil {
			cr.Spec.Params = make(map[string][]string)
		}
		cr.Spec.Params["module"] = []string{cr.Spec.Module}
	}

	setScrapeIntervalToWithLimit(ctx, &cr.Spec.EndpointScrapeParams, vmagentCR)

	cfg = addCommonScrapeParamsTo(cfg, cr.Spec.EndpointScrapeParams, se)

	var relabelings []yaml.MapSlice

	if cr.Spec.Targets.StaticConfig != nil {
		staticConfig := yaml.MapSlice{
			{Key: "targets", Value: cr.Spec.Targets.StaticConfig.Targets},
		}
		if cr.Spec.Targets.StaticConfig.Labels != nil {
			staticConfig = append(staticConfig,
				yaml.MapSlice{
					{Key: "labels", Value: cr.Spec.Targets.StaticConfig.Labels},
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
		for _, r := range cr.Spec.Targets.StaticConfig.RelabelConfigs {
			relabelings = append(relabelings, generateRelabelConfig(r))
		}
	}
	if cr.Spec.Targets.Ingress != nil {

		relabelings = addSelectorToRelabelingFor(relabelings, "ingress", cr.Spec.Targets.Ingress.Selector)
		selectedNamespaces := getNamespacesFromNamespaceSelector(&cr.Spec.Targets.Ingress.NamespaceSelector, cr.Namespace, se.IgnoreNamespaceSelectors)

		k8sSDOpts := generateK8SSDConfigOptions{
			namespaces:         selectedNamespaces,
			shouldAddSelectors: vmagentCR.Spec.EnableKubernetesAPISelectors,
			selectors:          cr.Spec.Targets.Ingress.Selector,
			apiServerConfig:    apiserverConfig,
			role:               kubernetesSDRoleIngress,
		}
		cfg = append(cfg, generateK8SSDConfig(ssCache, k8sSDOpts))

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
		for _, r := range cr.Spec.Targets.Ingress.RelabelConfigs {
			relabelings = append(relabelings, generateRelabelConfig(r))
		}

	}

	if cr.Spec.JobName != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "target_label", Value: "job"},
			{Key: "replacement", Value: cr.Spec.JobName},
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
			{Key: "replacement", Value: cr.Spec.VMProberSpec.URL},
		},
	}...)

	for _, trc := range vmagentCR.Spec.ProbeScrapeRelabelTemplate {
		relabelings = append(relabelings, generateRelabelConfig(trc))
	}
	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, cr.Namespace, se.EnforcedNamespaceLabel)

	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})
	cfg = addMetricRelabelingsTo(cfg, cr.Spec.MetricRelabelConfigs, se)
	cfg = append(cfg, buildVMScrapeParams(cr.Namespace, cr.AsProxyKey(), cr.Spec.VMScrapeParams, ssCache)...)
	cfg = addTLStoYaml(cfg, cr.Namespace, cr.Spec.TLSConfig, false)
	cfg = addEndpointAuthTo(cfg, cr.Spec.EndpointAuth, cr.Namespace, cr.AsMapKey(), ssCache)

	return cfg
}
