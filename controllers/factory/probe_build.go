package factory

import (
	"fmt"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sort"
	"strings"
)

func generateProbeConfig(
	cr *victoriametricsv1beta1.VMProbe,
	i int,
	apiserverConfig *victoriametricsv1beta1.APIServerConfig,
	basicAuthSecrets map[string]BasicAuthCredentials,
	ignoreNamespaceSelectors bool,
	enforcedNamespaceLabel string,
) yaml.MapSlice {

	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("%s/%s/%d", cr.Namespace, cr.Name, i),
		},
	}
	var relabelings []yaml.MapSlice

	if cr.Spec.VMProberSpec.Path == "" {
		cr.Spec.VMProberSpec.Path = "/probe"
	}

	if cr.Spec.Interval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: cr.Spec.Interval})
	}
	if cr.Spec.ScrapeTimeout != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_timeout", Value: cr.Spec.ScrapeTimeout})
	}
	if cr.Spec.VMProberSpec.Scheme != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scheme", Value: cr.Spec.VMProberSpec.Scheme})
	}

	cfg = append(cfg, yaml.MapItem{Key: "metrics_path", Value: cr.Spec.VMProberSpec.Path})
	cfg = append(cfg, yaml.MapItem{Key: "module", Value: cr.Spec.Module})

	if cr.Spec.Targets.StaticConfig != nil {
		staticConfig := yaml.MapSlice{
			{Key: "targets", Value: cr.Spec.Targets.StaticConfig.Targets},
		}
		if cr.Spec.Targets.StaticConfig.Labels != nil {
			staticConfig = append(staticConfig,
				yaml.MapSlice{
					{Key: "labels", Value: cr.Spec.Targets.StaticConfig.Labels}}...)
		}

		cfg = append(cfg, yaml.MapItem{
			Key:   "static_configs",
			Value: []yaml.MapSlice{staticConfig},
		})

	}
	if cr.Spec.Targets.Ingress != nil {
		var (
			labelKeys []string
		)

		// Filter targets by ingresses selected by the probe.
		// Exact label matches.
		for k := range cr.Spec.Targets.Ingress.Selector.MatchLabels {
			labelKeys = append(labelKeys, k)
		}
		sort.Strings(labelKeys)

		for _, k := range labelKeys {
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_ingress_label_" + sanitizeLabelName(k)}},
				{Key: "regex", Value: cr.Spec.Targets.Ingress.Selector.MatchLabels[k]},
			})
		}

		// Set based label matching. We have to map the valid relations
		// `In`, `NotIn`, `Exists`, and `DoesNotExist`, into relabeling rules.
		for _, exp := range cr.Spec.Targets.Ingress.Selector.MatchExpressions {
			switch exp.Operator {
			case metav1.LabelSelectorOpIn:
				relabelings = append(relabelings, yaml.MapSlice{
					{Key: "action", Value: "keep"},
					{Key: "source_labels", Value: []string{"__meta_kubernetes_ingress_label_" + sanitizeLabelName(exp.Key)}},
					{Key: "regex", Value: strings.Join(exp.Values, "|")},
				})
			case metav1.LabelSelectorOpNotIn:
				relabelings = append(relabelings, yaml.MapSlice{
					{Key: "action", Value: "drop"},
					{Key: "source_labels", Value: []string{"__meta_kubernetes_ingress_label_" + sanitizeLabelName(exp.Key)}},
					{Key: "regex", Value: strings.Join(exp.Values, "|")},
				})
			case metav1.LabelSelectorOpExists:
				relabelings = append(relabelings, yaml.MapSlice{
					{Key: "action", Value: "keep"},
					{Key: "source_labels", Value: []string{"__meta_kubernetes_ingress_label_" + sanitizeLabelName(exp.Key)}},
					{Key: "regex", Value: ".+"},
				})
			case metav1.LabelSelectorOpDoesNotExist:
				relabelings = append(relabelings, yaml.MapSlice{
					{Key: "action", Value: "drop"},
					{Key: "source_labels", Value: []string{"__meta_kubernetes_ingress_label_" + sanitizeLabelName(exp.Key)}},
					{Key: "regex", Value: ".+"},
				})
			}
		}

		selectedNamespaces := getNamespacesFromNamespaceSelector(&cr.Spec.Targets.Ingress.NamespaceSelector, cr.Namespace, ignoreNamespaceSelectors)
		cfg = append(cfg, generateK8SSDConfig(selectedNamespaces, apiserverConfig, basicAuthSecrets, kubernetesSDRoleIngress))

		// Relabelings for ingress SD.
		relabelings = append(relabelings, []yaml.MapSlice{
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
			if r.TargetLabel != "" && enforcedNamespaceLabel != "" && r.TargetLabel == enforcedNamespaceLabel {
				continue
			}
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
			{Key: "source_labels", Value: []string{"__address__"}},
			{Key: "target_label", Value: "__param_target"},
		},
		{
			{Key: "source_labels", Value: []string{"__param_target"}},
			{Key: "target_label", Value: "instance"},
		},
		{
			{Key: "target_label", Value: "__address__"},
			{Key: "replacement", Value: cr.Spec.VMProberSpec.URL},
		},
	}...)
	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, cr.Namespace, enforcedNamespaceLabel)

	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})

	return cfg
}
