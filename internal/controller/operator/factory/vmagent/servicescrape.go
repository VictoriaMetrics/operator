package vmagent

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v2"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func generateServiceScrapeConfig(
	ctx context.Context,
	cr *vmv1beta1.VMAgent,
	sc *vmv1beta1.VMServiceScrape,
	ep vmv1beta1.Endpoint,
	i int,
	apiserverConfig *vmv1beta1.APIServerConfig,
	ac *build.AssetsCache,
	se vmv1beta1.VMAgentSecurityEnforcements,
) (yaml.MapSlice, error) {
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("serviceScrape/%s/%s/%d", sc.Namespace, sc.Name, i),
		},
	}
	// service role.
	if sc.Spec.DiscoveryRole == "" {
		sc.Spec.DiscoveryRole = kubernetesSDRoleEndpoint
	}

	selectedNamespaces := getNamespacesFromNamespaceSelector(&sc.Spec.NamespaceSelector, sc.Namespace, se.IgnoreNamespaceSelectors)
	if ep.AttachMetadata.Node == nil && sc.Spec.AttachMetadata.Node != nil {
		ep.AttachMetadata = sc.Spec.AttachMetadata
	}
	k8sSDOpts := generateK8SSDConfigOptions{
		namespaces:         selectedNamespaces,
		shouldAddSelectors: cr.Spec.EnableKubernetesAPISelectors,
		selectors:          sc.Spec.Selector,
		apiServerConfig:    apiserverConfig,
		role:               sc.Spec.DiscoveryRole,
		attachMetadata:     &ep.AttachMetadata,
		namespace:          sc.Namespace,
	}
	if c, err := generateK8SSDConfig(ac, k8sSDOpts); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}

	if ep.SampleLimit == 0 {
		ep.SampleLimit = sc.Spec.SampleLimit
	}
	if ep.SeriesLimit == 0 {
		ep.SeriesLimit = sc.Spec.SeriesLimit
	}

	setScrapeIntervalToWithLimit(ctx, &ep.EndpointScrapeParams, cr)

	cfg = addCommonScrapeParamsTo(cfg, ep.EndpointScrapeParams, se)

	var relabelings []yaml.MapSlice

	// Exact label matches.
	skipRelabelSelectors := cr.Spec.EnableKubernetesAPISelectors
	relabelings = addSelectorToRelabelingFor(relabelings, "service", sc.Spec.Selector, skipRelabelSelectors)

	// Filter targets based on correct port for the endpoint.
	if ep.Port != "" {
		switch sc.Spec.DiscoveryRole {
		case kubernetesSDRoleEndpoint:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_endpoint_port_name"}},
				{Key: "regex", Value: ep.Port},
			})
		case kubernetesSDRoleEndpointSlices:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_endpointslice_port_name"}},
				{Key: "regex", Value: ep.Port},
			})
		case kubernetesSDRoleService:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_service_port_name"}},
				{Key: "regex", Value: ep.Port},
			})
		}
	} else if ep.TargetPort != nil && sc.Spec.DiscoveryRole != kubernetesSDRoleService {
		// not supported to service.
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

	switch sc.Spec.DiscoveryRole {
	case kubernetesSDRoleService:
		// nothing to do, service doesn't have relations with pods.
	case kubernetesSDRoleEndpointSlices:
		// Relabel namespace and pod and service labels into proper labels.
		relabelings = append(relabelings, []yaml.MapSlice{
			{ // Relabel node labels for pre v2.3 meta labels
				{Key: "source_labels", Value: []string{"__meta_kubernetes_endpointslice_address_target_kind", "__meta_kubernetes_endpointslice_address_target_name"}},
				{Key: "separator", Value: ";"},
				{Key: "regex", Value: "Node;(.*)"},
				{Key: "replacement", Value: "${1}"},
				{Key: "target_label", Value: "node"},
			},
			{ // Relabel pod labels for >=v2.3 meta labels
				{Key: "source_labels", Value: []string{"__meta_kubernetes_endpointslice_address_target_kind", "__meta_kubernetes_endpointslice_address_target_name"}},
				{Key: "separator", Value: ";"},
				{Key: "regex", Value: "Pod;(.*)"},
				{Key: "replacement", Value: "${1}"},
				{Key: "target_label", Value: "pod"},
			},
			{
				{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_name"}},
				{Key: "target_label", Value: "pod"},
			},
			{
				{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_container_name"}},
				{Key: "target_label", Value: "container"},
			},
		}...)
	case kubernetesSDRoleEndpoint:
		// Relabel namespace and pod and service labels into proper labels.
		relabelings = append(relabelings, []yaml.MapSlice{
			{ // Relabel node labels for pre v2.3 meta labels
				{Key: "source_labels", Value: []string{"__meta_kubernetes_endpoint_address_target_kind", "__meta_kubernetes_endpoint_address_target_name"}},
				{Key: "separator", Value: ";"},
				{Key: "regex", Value: "Node;(.*)"},
				{Key: "replacement", Value: "${1}"},
				{Key: "target_label", Value: "node"},
			},
			{ // Relabel pod labels for >=v2.3 meta labels
				{Key: "source_labels", Value: []string{"__meta_kubernetes_endpoint_address_target_kind", "__meta_kubernetes_endpoint_address_target_name"}},
				{Key: "separator", Value: ";"},
				{Key: "regex", Value: "Pod;(.*)"},
				{Key: "replacement", Value: "${1}"},
				{Key: "target_label", Value: "pod"},
			},
			{
				{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_name"}},
				{Key: "target_label", Value: "pod"},
			},
			{
				{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_container_name"}},
				{Key: "target_label", Value: "container"},
			},
		}...)

	}
	// Relabel namespace and pod and service labels into proper labels.
	relabelings = append(relabelings, []yaml.MapSlice{
		{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_namespace"}},
			{Key: "target_label", Value: "namespace"},
		},
		{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_service_name"}},
			{Key: "target_label", Value: "service"},
		},
	}...)

	// Relabel targetLabels from Service onto target.
	for _, l := range sc.Spec.TargetLabels {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_service_label_" + sanitizeLabelName(l)}},
			{Key: "target_label", Value: sanitizeLabelName(l)},
			{Key: "regex", Value: "(.+)"},
			{Key: "replacement", Value: "${1}"},
		})
	}

	if sc.Spec.DiscoveryRole != kubernetesSDRoleService {
		for _, l := range sc.Spec.PodTargetLabels {
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_label_" + sanitizeLabelName(l)}},
				{Key: "target_label", Value: sanitizeLabelName(l)},
				{Key: "regex", Value: "(.+)"},
				{Key: "replacement", Value: "${1}"},
			})
		}
	}

	// By default, generate a safe job name from the service name. We also keep
	// this around if a jobLabel is set in case the targets don't actually have a
	// value for it. A single service may potentially have multiple metrics
	// endpoints, therefore the endpoints labels is filled with the ports name or
	// as a fallback the port number.

	relabelings = append(relabelings, yaml.MapSlice{
		{Key: "source_labels", Value: []string{"__meta_kubernetes_service_name"}},
		{Key: "target_label", Value: "job"},
		{Key: "replacement", Value: "${1}"},
	})
	if sc.Spec.JobLabel != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_service_label_" + sanitizeLabelName(sc.Spec.JobLabel)}},
			{Key: "target_label", Value: "job"},
			{Key: "regex", Value: "(.+)"},
			{Key: "replacement", Value: "${1}"},
		})
	}

	if ep.Port != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "target_label", Value: "endpoint"},
			{Key: "replacement", Value: ep.Port},
		})
	} else if ep.TargetPort != nil && sc.Spec.DiscoveryRole != kubernetesSDRoleService && ep.TargetPort.String() != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "target_label", Value: "endpoint"},
			{Key: "replacement", Value: ep.TargetPort.String()},
		})
	}

	for _, c := range ep.RelabelConfigs {
		relabelings = append(relabelings, generateRelabelConfig(c))
	}

	for _, trc := range cr.Spec.ServiceScrapeRelabelTemplate {
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
