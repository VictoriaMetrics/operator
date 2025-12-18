package vmscrapes

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v2"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func generateNodeScrapeConfig(
	ctx context.Context,
	sp *vmv1beta1.CommonScrapeParams,
	pos *ParsedObjects,
	sc *vmv1beta1.VMNodeScrape,
	ac *build.AssetsCache,
) (yaml.MapSlice, error) {
	spec := &sc.Spec
	se := &sp.CommonScrapeSecurityEnforcements
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("nodeScrape/%s/%s", sc.Namespace, sc.Name),
		},
	}

	scrapeClass := getScrapeClass(sc.Spec.ScrapeClassName, sp)
	if scrapeClass != nil {
		mergeEndpointAuthWithScrapeClass(&sc.Spec.EndpointAuth, scrapeClass)
		mergeEndpointRelabelingsWithScrapeClass(&sc.Spec.EndpointRelabelings, scrapeClass)
	}

	setScrapeIntervalToWithLimit(ctx, &spec.EndpointScrapeParams, sp)

	k8sSDOpts := generateK8SSDConfigOptions{
		shouldAddSelectors: sp.EnableKubernetesAPISelectors,
		selectors:          sc.Spec.Selector,
		apiServerConfig:    pos.APIServerConfig,
		role:               k8sSDRoleNode,
		namespace:          sc.Namespace,
	}
	if c, err := generateK8SSDConfig(ac, k8sSDOpts); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}

	cfg = addCommonScrapeParamsTo(cfg, spec.EndpointScrapeParams, se)

	var relabelings []yaml.MapSlice

	skipRelabelSelectors := sp.EnableKubernetesAPISelectors
	relabelings = addSelectorToRelabelingFor(relabelings, "node", spec.Selector, skipRelabelSelectors)
	// Add __address__ as internalIP and pod and service labels into proper labels.
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

	if spec.Port != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__address__"}},
			{Key: "target_label", Value: "__address__"},
			{Key: "regex", Value: "^(.*):(.*)"},
			{Key: "replacement", Value: fmt.Sprintf("${1}:%s", spec.Port)},
		})
	}

	for _, c := range spec.RelabelConfigs {
		relabelings = append(relabelings, generateRelabelConfig(c))
	}
	for _, trc := range sp.NodeScrapeRelabelTemplate {
		relabelings = append(relabelings, generateRelabelConfig(trc))
	}

	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, sc.Namespace, se.EnforcedNamespaceLabel)

	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})
	cfg = addMetricRelabelingsTo(cfg, spec.MetricRelabelConfigs, se)
	if c, err := buildVMScrapeParams(sc.Namespace, sc.Spec.VMScrapeParams, ac); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}
	return addEndpointAuthTo(cfg, &spec.EndpointAuth, sc.Namespace, ac)
}
