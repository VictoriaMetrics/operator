package scrape

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

func selectPodScrapes(ctx context.Context, rclient client.Client, opts *k8stools.SelectorOpts) ([]*vmv1beta1.VMPodScrape, error) {
	var selectedConfigs []*vmv1beta1.VMPodScrape
	var namespacedNames []string
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMPodScrapeList) {
		for i := range list.Items {
			item := &list.Items[i]
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			selectedConfigs = append(selectedConfigs, item)
			namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, err
	}

	build.OrderByKeys(selectedConfigs, namespacedNames)
	logger.SelectedObjects(ctx, "VMPodScrapes", len(namespacedNames), 0, namespacedNames)

	return selectedConfigs, nil
}

func generatePodScrapeConfig(
	ctx context.Context,
	sc *vmv1beta1.VMPodScrape,
	ep vmv1beta1.PodMetricsEndpoint,
	i int,
	cr scraping,
	ac *build.AssetsCache,
) (yaml.MapSlice, error) {
	sp := cr.GetScrapeParams()
	apiserverConfig := cr.GetAPIServerConfig()
	se := sp.CommonScrapeSecurityEnforcements
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("podScrape/%s/%s/%d", sc.Namespace, sc.Name, i),
		},
	}
	selectedNamespaces := getNamespacesFromNamespaceSelector(&sc.Spec.NamespaceSelector, sc.Namespace, se.IgnoreNamespaceSelectors)
	if ep.AttachMetadata.Node == nil && sc.Spec.AttachMetadata.Node != nil {
		ep.AttachMetadata = sc.Spec.AttachMetadata
	}
	k8sOpts := k8sSDOpts{
		namespaces:          selectedNamespaces,
		shouldAddSelectors:  sp.EnableKubernetesAPISelectors,
		selectors:           sc.Spec.Selector,
		apiServerConfig:     apiserverConfig,
		role:                kubernetesSDRolePod,
		attachMetadata:      &ep.AttachMetadata,
		namespace:           sc.Namespace,
		mustUseNodeSelector: cr.MustUseNodeSelector(),
	}

	if c, err := generateK8SSDConfig(ac, k8sOpts); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}

	// set defaults
	if ep.SampleLimit == 0 {
		ep.SampleLimit = sc.Spec.SampleLimit
	}
	if ep.SeriesLimit == 0 {
		ep.SeriesLimit = sc.Spec.SeriesLimit
	}

	setScrapeIntervalToWithLimit(ctx, &ep.EndpointScrapeParams, &sp)

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
	relabelings = addSelectorToRelabelingFor(relabelings, "pod", sc.Spec.Selector, skipRelabelSelectors)

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
	for _, l := range sc.Spec.PodTargetLabels {
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
	if sc.Spec.JobLabel != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_label_" + sanitizeLabelName(sc.Spec.JobLabel)}},
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
