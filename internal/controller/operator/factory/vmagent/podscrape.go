package vmagent

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func generatePodScrapeConfig(
	ctx context.Context,
	vmagentCR *vmv1beta1.VMAgent,
	m *vmv1beta1.VMPodScrape,
	ep vmv1beta1.PodMetricsEndpoint,
	i int,
	apiserverConfig *vmv1beta1.APIServerConfig,
	ssCache *scrapesSecretsCache,
	se vmv1beta1.VMAgentSecurityEnforcements,
) yaml.MapSlice {
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("podScrape/%s/%s/%d", m.Namespace, m.Name, i),
		},
	}

	selectedNamespaces := getNamespacesFromNamespaceSelector(&m.Spec.NamespaceSelector, m.Namespace, se.IgnoreNamespaceSelectors)
	if ep.AttachMetadata.Node == nil && m.Spec.AttachMetadata.Node != nil {
		ep.AttachMetadata = m.Spec.AttachMetadata
	}
	cfg = append(cfg, generatePodK8SSDConfig(selectedNamespaces, m.Spec.Selector, apiserverConfig, ssCache, kubernetesSDRolePod, &ep.AttachMetadata))

	// set defaults
	if ep.SampleLimit == 0 {
		ep.SampleLimit = m.Spec.SampleLimit
	}
	if ep.SeriesLimit == 0 {
		ep.SeriesLimit = m.Spec.SeriesLimit
	}

	setScrapeIntervalToWithLimit(ctx, &ep.EndpointScrapeParams, vmagentCR)

	cfg = addCommonScrapeParamsTo(cfg, ep.EndpointScrapeParams, se)

	var relabelings []yaml.MapSlice

	if ep.FilterRunning == nil || *ep.FilterRunning {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "action", Value: "drop"},
			{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_phase"}},
			{Key: "regex", Value: "(Failed|Succeeded)"},
		})
	}

	relabelings = addSelectorToRelabelingFor(relabelings, "pod", m.Spec.Selector)

	// Filter targets based on correct port for the endpoint.
	if ep.Port != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "action", Value: "keep"},
			{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_container_port_name"}},
			{Key: "regex", Value: ep.Port},
		})
	} else if ep.TargetPort != nil {
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
	for _, l := range m.Spec.PodTargetLabels {
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
		{Key: "replacement", Value: fmt.Sprintf("%s/%s", m.GetNamespace(), m.GetName())},
	})
	if m.Spec.JobLabel != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_label_" + sanitizeLabelName(m.Spec.JobLabel)}},
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
	} else if ep.TargetPort != nil && ep.TargetPort.String() != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "target_label", Value: "endpoint"},
			{Key: "replacement", Value: ep.TargetPort.String()},
		})
	}

	for _, c := range ep.RelabelConfigs {
		relabelings = append(relabelings, generateRelabelConfig(c))
	}
	for _, trc := range vmagentCR.Spec.PodScrapeRelabelTemplate {
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

func generatePodK8SSDConfig(namespaces []string, labelSelector metav1.LabelSelector, apiserverConfig *vmv1beta1.APIServerConfig, ssCache *scrapesSecretsCache, role string, am *vmv1beta1.AttachMetadata) yaml.MapItem {
	cfg := generateK8SSDConfig(namespaces, apiserverConfig, ssCache, role, am)

	if len(labelSelector.MatchLabels) != 0 {
		k8sSDs, flag := cfg.Value.([]yaml.MapSlice)
		if !flag {
			return cfg
		}

		selector := yaml.MapSlice{}
		selector = append(selector, yaml.MapItem{
			Key:   "role",
			Value: role,
		})
		selector = append(selector, yaml.MapItem{
			Key:   "label",
			Value: combineSelectorStr(labelSelector.MatchLabels),
		})

		for i := range k8sSDs {
			k8sSDs[i] = append(k8sSDs[i], yaml.MapItem{
				Key: "selectors",
				Value: []yaml.MapSlice{
					selector,
				},
			})
		}

		cfg.Value = k8sSDs
	}

	return cfg
}
