package vmagent

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v2"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func generateNodeScrapeConfig(
	ctx context.Context,
	cr *vmv1beta1.VMAgent,
	sc *vmv1beta1.VMNodeScrape,
	apiserverConfig *vmv1beta1.APIServerConfig,
	ac *build.AssetsCache,
	se vmv1beta1.VMAgentSecurityEnforcements,
) (yaml.MapSlice, error) {
	nodeSpec := &sc.Spec
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("nodeScrape/%s/%s", sc.Namespace, sc.Name),
		},
	}

	setScrapeIntervalToWithLimit(ctx, &nodeSpec.EndpointScrapeParams, cr)

	k8sSDOpts := generateK8SSDConfigOptions{
		shouldAddSelectors: cr.Spec.EnableKubernetesAPISelectors,
		selectors:          sc.Spec.Selector,
		apiServerConfig:    apiserverConfig,
		role:               kubernetesSDRoleNode,
		namespace:          sc.Namespace,
	}
	if c, err := generateK8SSDConfig(ac, k8sSDOpts); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}

	cfg = addCommonScrapeParamsTo(cfg, nodeSpec.EndpointScrapeParams, se)

	var relabelings []yaml.MapSlice

	skipRelabelSelectors := cr.Spec.EnableKubernetesAPISelectors
	relabelings = addSelectorToRelabelingFor(relabelings, "node", nodeSpec.Selector, skipRelabelSelectors)
	// Add __address__ as internalIP  and pod and service labels into proper labels.
	relabelings = append(relabelings, []yaml.MapSlice{
		{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_node_name"}},
			{Key: "target_label", Value: "node"},
		},
	}...)

	// Relabel targetLabels from Node onto target.
	for _, l := range sc.Spec.TargetLabels {
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
		{Key: "replacement", Value: fmt.Sprintf("%s/%s", sc.GetNamespace(), sc.GetName())},
	})
	if sc.Spec.JobLabel != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_node_label_" + sanitizeLabelName(sc.Spec.JobLabel)}},
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
	for _, trc := range cr.Spec.NodeScrapeRelabelTemplate {
		relabelings = append(relabelings, generateRelabelConfig(trc))
	}

	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, sc.Namespace, se.EnforcedNamespaceLabel)

	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})
	cfg = addMetricRelabelingsTo(cfg, nodeSpec.MetricRelabelConfigs, se)
	if c, err := buildVMScrapeParams(sc.Namespace, sc.Spec.VMScrapeParams, ac); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}
	return addEndpointAuthTo(cfg, &nodeSpec.EndpointAuth, sc.Namespace, ac)
}
