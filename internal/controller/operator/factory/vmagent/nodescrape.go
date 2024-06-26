package vmagent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func generateNodeScrapeConfig(
	ctx context.Context,
	crAgent *vmv1beta1.VMAgent,
	cr *vmv1beta1.VMNodeScrape,
	i int,
	apiserverConfig *vmv1beta1.APIServerConfig,
	ssCache *scrapesSecretsCache,
	ignoreHonorLabels bool,
	overrideHonorTimestamps bool,
	enforcedNamespaceLabel string,
) yaml.MapSlice {
	nodeSpec := cr.Spec
	hl := honorLabels(nodeSpec.HonorLabels, ignoreHonorLabels)
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("nodeScrape/%s/%s/%d", cr.Namespace, cr.Name, i),
		},
		{
			Key:   "honor_labels",
			Value: hl,
		},
	}
	cfg = honorTimestamps(cfg, nodeSpec.HonorTimestamps, overrideHonorTimestamps)

	cfg = append(cfg, generateK8SSDConfig(nil, apiserverConfig, ssCache, kubernetesSDRoleNode, nil))

	var scrapeInterval string
	if nodeSpec.ScrapeInterval != "" {
		scrapeInterval = nodeSpec.ScrapeInterval
	} else if nodeSpec.Interval != "" {
		scrapeInterval = nodeSpec.Interval
	}
	scrapeInterval = limitScrapeInterval(ctx, scrapeInterval, crAgent.Spec.MinScrapeInterval, crAgent.Spec.MaxScrapeInterval)
	if scrapeInterval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: scrapeInterval})
	}

	if nodeSpec.ScrapeTimeout != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_timeout", Value: nodeSpec.ScrapeTimeout})
	}
	if nodeSpec.Path != "" {
		cfg = append(cfg, yaml.MapItem{Key: "metrics_path", Value: nodeSpec.Path})
	}
	if nodeSpec.ProxyURL != nil {
		cfg = append(cfg, yaml.MapItem{Key: "proxy_url", Value: nodeSpec.ProxyURL})
	}

	if cr.Spec.SampleLimit > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "sample_limit", Value: cr.Spec.SampleLimit})
	}
	if cr.Spec.SeriesLimit > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "series_limit", Value: cr.Spec.SeriesLimit})
	}
	if nodeSpec.Params != nil {
		cfg = append(cfg, yaml.MapItem{Key: "params", Value: nodeSpec.Params})
	}
	if cr.Spec.FollowRedirects != nil {
		cfg = append(cfg, yaml.MapItem{Key: "follow_redirects", Value: cr.Spec.FollowRedirects})
	}
	if nodeSpec.Scheme != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scheme", Value: nodeSpec.Scheme})
	}

	cfg = addTLStoYaml(cfg, cr.Namespace, nodeSpec.TLSConfig, false)

	if nodeSpec.BearerTokenFile != "" {
		cfg = append(cfg, yaml.MapItem{Key: "bearer_token_file", Value: nodeSpec.BearerTokenFile})
	}

	if nodeSpec.BearerTokenSecret != nil && nodeSpec.BearerTokenSecret.Name != "" {
		if s, ok := ssCache.bearerTokens[cr.AsMapKey()]; ok {
			cfg = append(cfg, yaml.MapItem{Key: "bearer_token", Value: s})
		}
	}
	if cr.Spec.BasicAuth != nil {
		var bac yaml.MapSlice
		if s, ok := ssCache.baSecrets[cr.AsMapKey()]; ok {
			bac = append(bac,
				yaml.MapItem{Key: "username", Value: s.Username},
				yaml.MapItem{Key: "password", Value: s.Password},
			)
		}
		if len(cr.Spec.BasicAuth.PasswordFile) > 0 {
			bac = append(bac, yaml.MapItem{Key: "password_file", Value: cr.Spec.BasicAuth.PasswordFile})
		}
		if len(bac) > 0 {
			cfg = append(cfg, yaml.MapItem{Key: "basic_auth", Value: bac})
		}
	}

	var (
		relabelings []yaml.MapSlice
		labelKeys   []string
	)
	// Filter targets by pods selected by the scrape.
	// Exact label matches.
	for k := range cr.Spec.Selector.MatchLabels {
		labelKeys = append(labelKeys, k)
	}
	sort.Strings(labelKeys)

	for _, k := range labelKeys {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "action", Value: "keep"},
			{Key: "source_labels", Value: []string{"__meta_kubernetes_node_label_" + sanitizeLabelName(k)}},
			{Key: "regex", Value: cr.Spec.Selector.MatchLabels[k]},
		})
	}
	// Set based label matching. We have to map the valid relations
	// `In`, `NotIn`, `Exists`, and `DoesNotExist`, into relabeling rules.
	for _, exp := range cr.Spec.Selector.MatchExpressions {
		switch exp.Operator {
		case metav1.LabelSelectorOpIn:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_node_label_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: strings.Join(exp.Values, "|")},
			})
		case metav1.LabelSelectorOpNotIn:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "drop"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_node_label_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: strings.Join(exp.Values, "|")},
			})
		case metav1.LabelSelectorOpExists:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_node_labelpresent_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: "true"},
			})
		case metav1.LabelSelectorOpDoesNotExist:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "drop"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_node_labelpresent_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: "true"},
			})
		}
	}

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
	for _, trc := range crAgent.Spec.NodeScrapeRelabelTemplate {
		relabelings = append(relabelings, generateRelabelConfig(trc))
	}

	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, cr.Namespace, enforcedNamespaceLabel)
	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})

	if nodeSpec.MetricRelabelConfigs != nil {
		var metricRelabelings []yaml.MapSlice
		for _, c := range nodeSpec.MetricRelabelConfigs {
			if c.TargetLabel != "" && enforcedNamespaceLabel != "" && c.TargetLabel == enforcedNamespaceLabel {
				continue
			}
			relabeling := generateRelabelConfig(c)

			metricRelabelings = append(metricRelabelings, relabeling)
		}
		cfg = append(cfg, yaml.MapItem{Key: "metric_relabel_configs", Value: metricRelabelings})
	}
	cfg = append(cfg, buildVMScrapeParams(cr.Namespace, cr.AsProxyKey(), cr.Spec.VMScrapeParams, ssCache)...)
	cfg = addOAuth2Config(cfg, cr.AsMapKey(), nodeSpec.OAuth2, ssCache.oauth2Secrets)
	cfg = addAuthorizationConfig(cfg, cr.AsMapKey(), nodeSpec.Authorization, ssCache.authorizationSecrets)
	return cfg
}
