package vmagent

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"gopkg.in/yaml.v2"
)

func generateServiceScrapeConfig(
	ctx context.Context,
	vmagentCR *vmv1beta1.VMAgent,
	m *vmv1beta1.VMServiceScrape,
	ep vmv1beta1.Endpoint,
	i int,
	apiserverConfig *vmv1beta1.APIServerConfig,
	ssCache *scrapesSecretsCache,
	se vmv1beta1.VMAgentSecurityEnforcements,
) yaml.MapSlice {
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("serviceScrape/%s/%s/%d", m.Namespace, m.Name, i),
		},
	}
	// service role.
	if m.Spec.DiscoveryRole == "" {
		m.Spec.DiscoveryRole = kubernetesSDRoleEndpoint
	}

	selectedNamespaces := getNamespacesFromNamespaceSelector(&m.Spec.NamespaceSelector, m.Namespace, se.IgnoreNamespaceSelectors)
	if ep.AttachMetadata.Node == nil && m.Spec.AttachMetadata.Node != nil {
		ep.AttachMetadata = m.Spec.AttachMetadata
	}
	cfg = append(cfg, generateK8SSDConfig(selectedNamespaces, apiserverConfig, ssCache, m.Spec.DiscoveryRole, &ep.AttachMetadata))

	if ep.SampleLimit == 0 {
		ep.SampleLimit = m.Spec.SampleLimit
	}
	if ep.SeriesLimit == 0 {
		ep.SeriesLimit = m.Spec.SeriesLimit
	}

	setScrapeIntervalToWithLimit(ctx, &ep.EndpointScrapeParams, vmagentCR)

	cfg = addCommonScrapeParamsTo(cfg, ep.EndpointScrapeParams, se)

	var relabelings []yaml.MapSlice

	// Filter targets by services selected by the scrape.

	// Exact label matches.
	relabelings = addSelectorToRelabelingFor(relabelings, "service", m.Spec.Selector)

	// Filter targets based on correct port for the endpoint.
	if ep.Port != "" {
		switch m.Spec.DiscoveryRole {
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
	} else if ep.TargetPort != nil && m.Spec.DiscoveryRole != kubernetesSDRoleService {
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

	switch m.Spec.DiscoveryRole {
	case kubernetesSDRoleService:
		// nothing to do, service doesnt have relations with pods.
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
	for _, l := range m.Spec.TargetLabels {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_service_label_" + sanitizeLabelName(l)}},
			{Key: "target_label", Value: sanitizeLabelName(l)},
			{Key: "regex", Value: "(.+)"},
			{Key: "replacement", Value: "${1}"},
		})
	}

	if m.Spec.DiscoveryRole != kubernetesSDRoleService {
		for _, l := range m.Spec.PodTargetLabels {
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_label_" + sanitizeLabelName(l)}},
				{Key: "target_label", Value: sanitizeLabelName(l)},
				{Key: "regex", Value: "(.+)"},
				{Key: "replacement", Value: "${1}"},
			})
		}
	}

	// By default, generate a safe job name from the service name.  We also keep
	// this around if a jobLabel is set in case the targets don't actually have a
	// value for it. A single service may potentially have multiple metrics
	// endpoints, therefore the endpoints labels is filled with the ports name or
	// as a fallback the port number.

	relabelings = append(relabelings, yaml.MapSlice{
		{Key: "source_labels", Value: []string{"__meta_kubernetes_service_name"}},
		{Key: "target_label", Value: "job"},
		{Key: "replacement", Value: "${1}"},
	})
	if m.Spec.JobLabel != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_service_label_" + sanitizeLabelName(m.Spec.JobLabel)}},
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
	} else if ep.TargetPort != nil && m.Spec.DiscoveryRole != kubernetesSDRoleService && ep.TargetPort.String() != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "target_label", Value: "endpoint"},
			{Key: "replacement", Value: ep.TargetPort.String()},
		})
	}

	for _, c := range ep.RelabelConfigs {
		relabelings = append(relabelings, generateRelabelConfig(c))
	}

	for _, trc := range vmagentCR.Spec.ServiceScrapeRelabelTemplate {
		relabelings = append(relabelings, generateRelabelConfig(trc))
	}

	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, m.Namespace, se.EnforcedNamespaceLabel)

	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})
	cfg = addMetricRelabelingsTo(cfg, ep.MetricRelabelConfigs, se)
	cfg = append(cfg, buildVMScrapeParams(m.Namespace, m.AsProxyKey(i), ep.VMScrapeParams, ssCache)...)
	cfg = addTLStoYaml(cfg, m.Namespace, ep.TLSConfig, false)
	cfg = addEndpointAuthTo(cfg, ep.EndpointAuth, m.AsMapKey(i), ssCache)

	return cfg
}

// ServiceMonitorDiscoveryRole returns the service discovery role name.
func ServiceMonitorDiscoveryRole(serviceMon *promv1.ServiceMonitor, discoveryRoleConfig string) string {
	// check if endpoint slice is supported
	supported := k8stools.IsEndpointSliceSupported()

	// if the role is set in the annotation.
	if serviceMon.Annotations != nil {
		role := serviceMon.Annotations[discoveryRoleAnnotation]
		if role == kubernetesSDRoleEndpoint || role == kubernetesSDRoleService || (role == kubernetesSDRoleEndpointSlices && supported) {
			return role
		}
	}

	// if the role is set in the config.
	if discoveryRoleConfig == kubernetesSDRoleEndpoint || discoveryRoleConfig == kubernetesSDRoleService || (discoveryRoleConfig == kubernetesSDRoleEndpointSlices && supported) {
		return discoveryRoleConfig
	}

	return ""
}
