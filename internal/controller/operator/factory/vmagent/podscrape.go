package vmagent

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v2"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func generatePodScrapeConfig(
	ctx context.Context,
	sp *vmv1beta1.CommonScrapeParams,
	pos *parsedObjects,
	sc *vmv1beta1.VMPodScrape,
	ep vmv1beta1.PodMetricsEndpoint,
	i int,
	ac *build.AssetsCache,
) (yaml.MapSlice, error) {
	spec := &sc.Spec
	se := &sp.CommonScrapeSecurityEnforcements
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("podScrape/%s/%s/%d", sc.Namespace, sc.Name, i),
		},
	}

	scrapeClass := getScrapeClass(spec.ScrapeClassName, sp)
	if scrapeClass != nil {
		mergeEndpointAuthWithScrapeClass(&ep.EndpointAuth, scrapeClass)
		mergeEndpointRelabelingsWithScrapeClass(&ep.EndpointRelabelings, scrapeClass)
		mergeAttachMetadataWithScrapeClass(&ep.AttachMetadata, scrapeClass)
	}

	selectedNamespaces := getNamespacesFromNamespaceSelector(&spec.NamespaceSelector, sc.Namespace, pos.IgnoreNamespaceSelectors)
	if ep.AttachMetadata.Node == nil && sc.Spec.AttachMetadata.Node != nil {
		ep.AttachMetadata.Node = spec.AttachMetadata.Node
	}
	if ep.AttachMetadata.Namespace == nil && spec.AttachMetadata.Namespace != nil {
		ep.AttachMetadata.Namespace = spec.AttachMetadata.Namespace
	}

	k8sSDOpts := generateK8SSDConfigOptions{
		namespaces:          selectedNamespaces,
		shouldAddSelectors:  sp.EnableKubernetesAPISelectors,
		selectors:           spec.Selector,
		apiServerConfig:     pos.APIServerConfig,
		role:                k8sSDRolePod,
		attachMetadata:      &ep.AttachMetadata,
		namespace:           sc.Namespace,
		mustUseNodeSelector: pos.MustUseNodeSelector,
	}
	if c, err := generateK8SSDConfig(ac, k8sSDOpts); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}

	// set defaults
	if ep.SampleLimit == 0 {
		ep.SampleLimit = spec.SampleLimit
	}
	if ep.SeriesLimit == 0 {
		ep.SeriesLimit = spec.SeriesLimit
	}

	setScrapeIntervalToWithLimit(ctx, &ep.EndpointScrapeParams, sp)

	cfg = addCommonScrapeParamsTo(cfg, ep.EndpointScrapeParams, se)

	var relabelings []yaml.MapSlice

	if ep.FilterRunning == nil || *ep.FilterRunning {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "action", Value: "drop"},
			{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_phase"}},
			{Key: "regex", Value: "(Failed|Succeeded)"},
		})
	}

	skipRelabelSelectors := sp.EnableKubernetesAPISelectors
	relabelings = addSelectorToRelabelingFor(relabelings, "pod", spec.Selector, skipRelabelSelectors)

	// Filter targets based on correct port for the endpoint.
	switch {
	case ep.Port != nil && len(*ep.Port) > 0:
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "action", Value: "keep"},
			{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_container_port_name"}},
			{Key: "regex", Value: *ep.Port},
		})

	case ep.PortNumber != nil && *ep.PortNumber != 0:
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "action", Value: "keep"},
			{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_container_port_number"}},
			{Key: "regex", Value: *ep.PortNumber},
		})
	case ep.TargetPort != nil:
		if ep.TargetPort.StrVal != "" {
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_container_port_name"}},
				{Key: "regex", Value: ep.TargetPort.String()},
			})
		} else if ep.TargetPort.IntVal != 0 {
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_container_port_number"}},
				{Key: "regex", Value: ep.TargetPort.String()},
			})
		}
	}

	// Relabel namespace and pod and service labels into proper labels.
	relabelings = append(relabelings, []yaml.MapSlice{
		{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_namespace"}},
			{Key: "target_label", Value: "namespace"},
		},
		{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_container_name"}},
			{Key: "target_label", Value: "container"},
		},
		{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_name"}},
			{Key: "target_label", Value: "pod"},
		},
	}...)

	// Relabel targetLabels from Pod onto target.
	for _, l := range spec.PodTargetLabels {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_label_" + sanitizeLabelName(l)}},
			{Key: "target_label", Value: sanitizeLabelName(l)},
			{Key: "regex", Value: "(.+)"},
			{Key: "replacement", Value: "${1}"},
		})
	}

	// By default, generate a safe job name from the PodScrape. We also keep
	// this around if a jobLabel is set in case the targets don't actually have a
	// value for it. A single pod may potentially have multiple metrics
	// endpoints, therefore the endpoints labels is filled with the ports name or
	// as a fallback the port number.

	relabelings = append(relabelings, yaml.MapSlice{
		{Key: "target_label", Value: "job"},
		{Key: "replacement", Value: fmt.Sprintf("%s/%s", sc.GetNamespace(), sc.GetName())},
	})
	if spec.JobLabel != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_label_" + sanitizeLabelName(spec.JobLabel)}},
			{Key: "target_label", Value: "job"},
			{Key: "regex", Value: "(.+)"},
			{Key: "replacement", Value: "${1}"},
		})
	}

	if ep.Port != nil && len(*ep.Port) > 0 {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "target_label", Value: "endpoint"},
			{Key: "replacement", Value: *ep.Port},
		})
	} else if ep.TargetPort != nil && ep.TargetPort.String() != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "target_label", Value: "endpoint"},
			{Key: "replacement", Value: ep.TargetPort.String()},
		})
	}

	for _, c := range ep.RelabelConfigs {
		relabelings = append(relabelings, generateRelabelConfig(c))
	}
	for _, trc := range sp.PodScrapeRelabelTemplate {
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
