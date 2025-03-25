package vmagent

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"gopkg.in/yaml.v2"
)

func generateNodeScrapeConfig(
	ctx context.Context,
	vmagentCR *vmv1beta1.VMAgent,
	cr *vmv1beta1.VMNodeScrape,
	i int,
	apiserverConfig *vmv1beta1.APIServerConfig,
	ssCache *scrapesSecretsCache,
	se vmv1beta1.VMAgentSecurityEnforcements,
) yaml.MapSlice {
	nodeSpec := &cr.Spec
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("nodeScrape/%s/%s/%d", cr.Namespace, cr.Name, i),
		},
	}

	setScrapeIntervalToWithLimit(ctx, &nodeSpec.EndpointScrapeParams, vmagentCR)

	k8sSDOpts := generateK8SSDConfigOptions{
		shouldAddSelectors: vmagentCR.Spec.EnableKubernetesAPISelectors,
		selectors:          cr.Spec.Selector,
		apiServerConfig:    apiserverConfig,
		role:               kubernetesSDRoleNode,
	}
	cfg = append(cfg, generateK8SSDConfig(ssCache, k8sSDOpts))

	cfg = addCommonScrapeParamsTo(cfg, nodeSpec.EndpointScrapeParams, se)

	var relabelings []yaml.MapSlice

	relabelings = addSelectorToRelabelingFor(relabelings, "node", nodeSpec.Selector)
	// Add __address__ as internalIP  and pod and service labels into proper labels.
	relabelings = append(relabelings, []yaml.MapSlice{
		{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_node_name"}},
			{Key: "target_label", Value: "node"},
		},
	}...)

	// Relabel targetLabels from Node onto target.
	for _, l := range cr.Spec.TargetLabels {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_node_label_" + sanitizeLabelName(l)}},
			{Key: "target_label", Value: sanitizeLabelName(l)},
			{Key: "regex", Value: "(.+)"},
			{Key: "replacement", Value: "${1}"},
		})
	}

	// By default, generate a safe job name from the NodeScrape. We also keep
	// this around if a jobLabel is set in case the targets don't actually have a
	// value for it. A single pod may potentially have multiple metrics
	// endpoints, therefore the endpoints labels is filled with the ports name or
	// as a fallback the port number.

	relabelings = append(relabelings, yaml.MapSlice{
		{Key: "target_label", Value: "job"},
		{Key: "replacement", Value: fmt.Sprintf("%s/%s", cr.GetNamespace(), cr.GetName())},
	})
	if cr.Spec.JobLabel != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_node_label_" + sanitizeLabelName(cr.Spec.JobLabel)}},
			{Key: "target_label", Value: "job"},
			{Key: "regex", Value: "(.+)"},
			{Key: "replacement", Value: "${1}"},
		})
	}

	if nodeSpec.Port != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__address__"}},
			{Key: "target_label", Value: "__address__"},
			{Key: "regex", Value: "^(.*):(.*)"},
			{Key: "replacement", Value: fmt.Sprintf("${1}:%s", nodeSpec.Port)},
		})
	}

	for _, c := range nodeSpec.RelabelConfigs {
		relabelings = append(relabelings, generateRelabelConfig(c))
	}
	for _, trc := range vmagentCR.Spec.NodeScrapeRelabelTemplate {
		relabelings = append(relabelings, generateRelabelConfig(trc))
	}

	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, cr.Namespace, se.EnforcedNamespaceLabel)

	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})
	cfg = addMetricRelabelingsTo(cfg, nodeSpec.MetricRelabelConfigs, se)
	cfg = append(cfg, buildVMScrapeParams(cr.Namespace, cr.AsProxyKey(), cr.Spec.VMScrapeParams, ssCache)...)
	cfg = addTLStoYaml(cfg, cr.Namespace, nodeSpec.TLSConfig, false)
	cfg = addEndpointAuthTo(cfg, nodeSpec.EndpointAuth, cr.AsMapKey(), ssCache)

	return cfg
}
