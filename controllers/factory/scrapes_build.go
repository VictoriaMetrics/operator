package factory

import (
	"fmt"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	defaultScrapeInterval          = "30s"
	tlsAssetsDir                   = "/etc/vmagent-tls/certs"
	configFilename                 = "vmagent.yaml.gz"
	configEnvsubstFilename         = "vmagent.env.yaml"
	kubernetesSDRoleEndpoint       = "endpoints"
	kubernetesSDRoleService        = "service"
	kubernetesSDRoleEndpointSlices = "endpointslices"
	kubernetesSDRolePod            = "pod"
	kubernetesSDRoleIngress        = "ingress"
	kubernetesSDRoleNode           = "node"
)

var (
	invalidLabelCharRE = regexp.MustCompile(`[^a-zA-Z0-9_]`)
)

// BasicAuthCredentials represents a username password pair to be used with
// basic http authentication, see https://tools.ietf.org/html/rfc7617.
type BasicAuthCredentials struct {
	username string
	password string
}

func generateConfig(
	cr *victoriametricsv1beta1.VMAgent,
	sMons map[string]*victoriametricsv1beta1.VMServiceScrape,
	pMons map[string]*victoriametricsv1beta1.VMPodScrape,
	probes map[string]*victoriametricsv1beta1.VMProbe,
	nodes map[string]*victoriametricsv1beta1.VMNodeScrape,
	statics map[string]*victoriametricsv1beta1.VMStaticScrape,
	secretsCache *scrapesSecretsCache,
	additionalScrapeConfigs []byte,
) ([]byte, error) {

	cfg := yaml.MapSlice{}

	if cr.Spec.ScrapeInterval == "" {
		cr.Spec.ScrapeInterval = defaultScrapeInterval
	}

	globalItems := yaml.MapSlice{
		{Key: "scrape_interval", Value: cr.Spec.ScrapeInterval},
		{Key: "external_labels", Value: buildExternalLabels(cr)},
	}

	cfg = append(cfg, yaml.MapItem{Key: "global", Value: globalItems})

	sMonIdentifiers := make([]string, len(sMons))
	i := 0
	for k := range sMons {
		sMonIdentifiers[i] = k
		i++
	}

	// Sorting ensures, that we always generate the config in the same order.
	sort.Strings(sMonIdentifiers)

	pMonIdentifiers := make([]string, len(pMons))
	i = 0
	for k := range pMons {
		pMonIdentifiers[i] = k
		i++
	}

	// Sorting ensures, that we always generate the config in the same order.
	sort.Strings(pMonIdentifiers)

	probeIdentifiers := make([]string, len(probes))
	i = 0
	for k := range probes {
		probeIdentifiers[i] = k
		i++
	}
	// Sorting ensures, that we always generate the config in the same order.
	sort.Strings(probeIdentifiers)

	nodeIdentifiers := make([]string, len(nodes))
	i = 0
	for k := range nodes {
		nodeIdentifiers[i] = k
		i++
	}
	// Sorting ensures, that we always generate the config in the same order.
	sort.Strings(nodeIdentifiers)

	staticsIdentifiers := make([]string, len(statics))
	i = 0
	for k := range statics {
		staticsIdentifiers[i] = k
		i++
	}

	// Sorting ensures, that we always generate the config in the same order.
	sort.Strings(staticsIdentifiers)

	apiserverConfig := cr.Spec.APIServerConfig

	var scrapeConfigs []yaml.MapSlice
	for _, identifier := range sMonIdentifiers {
		for i, ep := range sMons[identifier].Spec.Endpoints {
			scrapeConfigs = append(scrapeConfigs,
				generateServiceScrapeConfig(
					sMons[identifier],
					ep, i,
					apiserverConfig,
					secretsCache,
					cr.Spec.OverrideHonorLabels,
					cr.Spec.OverrideHonorTimestamps,
					cr.Spec.IgnoreNamespaceSelectors,
					cr.Spec.EnforcedNamespaceLabel))
		}
	}
	for _, identifier := range pMonIdentifiers {
		for i, ep := range pMons[identifier].Spec.PodMetricsEndpoints {
			scrapeConfigs = append(scrapeConfigs,
				generatePodScrapeConfig(
					pMons[identifier], ep, i,
					apiserverConfig,
					secretsCache,
					cr.Spec.OverrideHonorLabels,
					cr.Spec.OverrideHonorTimestamps,
					cr.Spec.IgnoreNamespaceSelectors,
					cr.Spec.EnforcedNamespaceLabel))
		}
	}

	for i, identifier := range probeIdentifiers {
		scrapeConfigs = append(scrapeConfigs,
			generateProbeConfig(
				probes[identifier],
				i,
				apiserverConfig,
				secretsCache,
				cr.Spec.IgnoreNamespaceSelectors,
				cr.Spec.EnforcedNamespaceLabel))
	}
	for i, identifier := range nodeIdentifiers {
		scrapeConfigs = append(scrapeConfigs,
			generateNodeScrapeConfig(
				nodes[identifier],
				i,
				apiserverConfig,
				secretsCache,
				cr.Spec.OverrideHonorLabels,
				cr.Spec.OverrideHonorTimestamps,
				cr.Spec.EnforcedNamespaceLabel))

	}

	for _, identifier := range staticsIdentifiers {
		for i, ep := range statics[identifier].Spec.TargetEndpoints {
			scrapeConfigs = append(scrapeConfigs,
				generateStaticScrapeConfig(
					statics[identifier],
					ep, i,
					secretsCache,
					cr.Spec.OverrideHonorLabels,
					cr.Spec.OverrideHonorTimestamps,
					cr.Spec.EnforcedNamespaceLabel,
				))
		}
	}
	var additionalScrapeConfigsYaml []yaml.MapSlice
	if err := yaml.Unmarshal(additionalScrapeConfigs, &additionalScrapeConfigsYaml); err != nil {
		return nil, fmt.Errorf("unmarshalling additional scrape configs failed: %w", err)
	}

	var inlineScrapeConfigsYaml []yaml.MapSlice
	if len(cr.Spec.InlineScrapeConfig) > 0 {
		if err := yaml.Unmarshal([]byte(cr.Spec.InlineScrapeConfig), &inlineScrapeConfigsYaml); err != nil {
			return nil, fmt.Errorf("unmarshalling  inline additional scrape configs failed: %w", err)
		}
	}
	additionalScrapeConfigsYaml = append(additionalScrapeConfigsYaml, inlineScrapeConfigsYaml...)
	cfg = append(cfg, yaml.MapItem{
		Key:   "scrape_configs",
		Value: append(scrapeConfigs, additionalScrapeConfigsYaml...),
	})

	return yaml.Marshal(cfg)
}

func makeConfigSecret(cr *victoriametricsv1beta1.VMAgent, config *config.BaseOperatorConf) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Annotations:     cr.Annotations(),
			Labels:          config.Labels.Merge(cr.Labels()),
			Namespace:       cr.Namespace,
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Data: map[string][]byte{
			configFilename: {},
		},
	}
}

func sanitizeLabelName(name string) string {
	return invalidLabelCharRE.ReplaceAllString(name, "_")
}

func stringMapToMapSlice(m map[string]string) yaml.MapSlice {
	res := yaml.MapSlice{}
	ks := make([]string, 0)

	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)

	for _, k := range ks {
		res = append(res, yaml.MapItem{Key: k, Value: m[k]})
	}

	return res
}

// honorLabels determinates the value of honor_labels.
// if overrideHonorLabels is true and user tries to set the
// value to true, we want to set honor_labels to false.
func honorLabels(userHonorLabels, overrideHonorLabels bool) bool {
	if userHonorLabels && overrideHonorLabels {
		return false
	}
	return userHonorLabels
}

// honorTimestamps adds option to enforce honor_timestamps option in scrape_config.
// We want to disable honoring timestamps when user specified it or when global
// override is set. For backwards compatibility with prometheus <2.9.0 we don't
// set honor_timestamps when that option wasn't specified anywhere
func honorTimestamps(cfg yaml.MapSlice, userHonorTimestamps *bool, overrideHonorTimestamps bool) yaml.MapSlice {
	// Ensuring backwards compatibility by checking if user set any option
	if userHonorTimestamps == nil && !overrideHonorTimestamps {
		return cfg
	}

	honor := false
	if userHonorTimestamps != nil {
		honor = *userHonorTimestamps
	}

	return append(cfg, yaml.MapItem{Key: "honor_timestamps", Value: honor && !overrideHonorTimestamps})
}

func generatePodScrapeConfig(
	m *victoriametricsv1beta1.VMPodScrape,
	ep victoriametricsv1beta1.PodMetricsEndpoint,
	i int,
	apiserverConfig *victoriametricsv1beta1.APIServerConfig,
	ssCache *scrapesSecretsCache,
	ignoreHonorLabels bool,
	overrideHonorTimestamps bool,
	ignoreNamespaceSelectors bool,
	enforcedNamespaceLabel string) yaml.MapSlice {

	hl := honorLabels(ep.HonorLabels, ignoreHonorLabels)
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("%s/%s/%d", m.Namespace, m.Name, i),
		},
		{
			Key:   "honor_labels",
			Value: hl,
		},
	}
	cfg = honorTimestamps(cfg, ep.HonorTimestamps, overrideHonorTimestamps)

	selectedNamespaces := getNamespacesFromNamespaceSelector(&m.Spec.NamespaceSelector, m.Namespace, ignoreNamespaceSelectors)
	cfg = append(cfg, generatePodK8SSDConfig(selectedNamespaces, m.Spec.Selector, apiserverConfig, ssCache.baSecrets, kubernetesSDRolePod))

	if ep.ScrapeInterval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: ep.ScrapeInterval})
	} else if ep.Interval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: ep.Interval})
	}

	if ep.ScrapeTimeout != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_timeout", Value: ep.ScrapeTimeout})
	}
	if ep.Path != "" {
		cfg = append(cfg, yaml.MapItem{Key: "metrics_path", Value: ep.Path})
	}
	if ep.ProxyURL != nil {
		cfg = append(cfg, yaml.MapItem{Key: "proxy_url", Value: ep.ProxyURL})
	}
	if ep.Params != nil {
		cfg = append(cfg, yaml.MapItem{Key: "params", Value: ep.Params})
	}

	if ep.FollowRedirects != nil {
		cfg = append(cfg, yaml.MapItem{Key: "follow_redirects", Value: ep.FollowRedirects})
	}
	if ep.Scheme != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scheme", Value: ep.Scheme})
	}
	cfg = addTLStoYaml(cfg, m.Namespace, ep.TLSConfig, false)

	if ep.BearerTokenFile != "" {
		cfg = append(cfg, yaml.MapItem{Key: "bearer_token_file", Value: ep.BearerTokenFile})
	}

	if ep.BearerTokenSecret.Name != "" {
		if s, ok := ssCache.bearerTokens[m.AsMapKey(i)]; ok {
			cfg = append(cfg, yaml.MapItem{Key: "bearer_token", Value: s})
		}
	}

	if ep.BasicAuth != nil {
		var bac yaml.MapSlice
		if s, ok := ssCache.baSecrets[m.AsMapKey(i)]; ok {
			bac = append(bac,
				yaml.MapItem{Key: "username", Value: s.username},
				yaml.MapItem{Key: "password", Value: s.password},
			)
		}
		if len(ep.BasicAuth.PasswordFile) > 0 {
			bac = append(bac, yaml.MapItem{Key: "password_file", Value: ep.BasicAuth.PasswordFile})
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
	for k := range m.Spec.Selector.MatchLabels {
		labelKeys = append(labelKeys, k)
	}
	sort.Strings(labelKeys)

	for _, k := range labelKeys {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "action", Value: "keep"},
			{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_label_" + sanitizeLabelName(k)}},
			{Key: "regex", Value: m.Spec.Selector.MatchLabels[k]},
		})
	}
	// Set based label matching. We have to map the valid relations
	// `In`, `NotIn`, `Exists`, and `DoesNotExist`, into relabeling rules.
	for _, exp := range m.Spec.Selector.MatchExpressions {
		switch exp.Operator {
		case metav1.LabelSelectorOpIn:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_label_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: strings.Join(exp.Values, "|")},
			})
		case metav1.LabelSelectorOpNotIn:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "drop"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_label_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: strings.Join(exp.Values, "|")},
			})
		case metav1.LabelSelectorOpExists:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_label_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: ".+"},
			})
		case metav1.LabelSelectorOpDoesNotExist:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "drop"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_label_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: ".+"},
			})
		}
	}

	// Filter targets based on correct port for the endpoint.
	if ep.Port != "" {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "action", Value: "keep"},
			{Key: "source_labels", Value: []string{"__meta_kubernetes_pod_container_port_name"}},
			{Key: "regex", Value: ep.Port},
		})
	} else if ep.TargetPort != nil {
		//.Warn(cg.logger).Log("msg", "PodMonitor 'targetPort' is deprecated, use 'port' instead.",
		//	"podScrape", m.Name)
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

	if ep.RelabelConfigs != nil {
		for _, c := range ep.RelabelConfigs {
			relabelings = append(relabelings, generateRelabelConfig(c))
		}
	}
	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, m.Namespace, enforcedNamespaceLabel)
	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})

	if m.Spec.SampleLimit > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "sample_limit", Value: m.Spec.SampleLimit})
	}

	if ep.MetricRelabelConfigs != nil {
		var metricRelabelings []yaml.MapSlice
		for _, c := range ep.MetricRelabelConfigs {
			if c.TargetLabel != "" && enforcedNamespaceLabel != "" && c.TargetLabel == enforcedNamespaceLabel {
				continue
			}
			relabeling := generateRelabelConfig(c)

			metricRelabelings = append(metricRelabelings, relabeling)
		}
		cfg = append(cfg, yaml.MapItem{Key: "metric_relabel_configs", Value: metricRelabelings})
	}

	cfg = append(cfg, buildVMScrapeParams(m.Namespace, m.AsProxyKey(i), ep.VMScrapeParams, ssCache)...)
	if ep.OAuth2 != nil {
		r := buildOAuth2Config(m.AsMapKey(i), ep.OAuth2, ssCache.oauth2Secrets)
		if len(r) > 0 {
			cfg = append(cfg, yaml.MapItem{Key: "oauth2", Value: r})
		}
	}
	return cfg
}

func generateServiceScrapeConfig(
	m *victoriametricsv1beta1.VMServiceScrape,
	ep victoriametricsv1beta1.Endpoint,
	i int,
	apiserverConfig *victoriametricsv1beta1.APIServerConfig,
	ssCache *scrapesSecretsCache,
	overrideHonorLabels bool,
	overrideHonorTimestamps bool,
	ignoreNamespaceSelectors bool,
	enforcedNamespaceLabel string) yaml.MapSlice {

	hl := honorLabels(ep.HonorLabels, overrideHonorLabels)
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("%s/%s/%d", m.Namespace, m.Name, i),
		},
		{
			Key:   "honor_labels",
			Value: hl,
		},
	}
	// service role.
	if m.Spec.DiscoveryRole == "" {
		m.Spec.DiscoveryRole = kubernetesSDRoleEndpoint
	}
	cfg = honorTimestamps(cfg, ep.HonorTimestamps, overrideHonorTimestamps)

	selectedNamespaces := getNamespacesFromNamespaceSelector(&m.Spec.NamespaceSelector, m.Namespace, ignoreNamespaceSelectors)
	cfg = append(cfg, generateK8SSDConfig(selectedNamespaces, apiserverConfig, ssCache.baSecrets, m.Spec.DiscoveryRole))

	if ep.ScrapeInterval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: ep.ScrapeInterval})
	} else if ep.Interval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: ep.Interval})
	}
	if ep.ScrapeTimeout != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_timeout", Value: ep.ScrapeTimeout})
	}
	if ep.Path != "" {
		cfg = append(cfg, yaml.MapItem{Key: "metrics_path", Value: ep.Path})
	}
	if ep.ProxyURL != nil {
		cfg = append(cfg, yaml.MapItem{Key: "proxy_url", Value: ep.ProxyURL})
	}
	if ep.FollowRedirects != nil {
		cfg = append(cfg, yaml.MapItem{Key: "follow_redirects", Value: ep.FollowRedirects})
	}
	if ep.Params != nil {
		cfg = append(cfg, yaml.MapItem{Key: "params", Value: ep.Params})
	}
	if ep.Scheme != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scheme", Value: ep.Scheme})
	}

	cfg = addTLStoYaml(cfg, m.Namespace, ep.TLSConfig, false)

	if ep.BearerTokenFile != "" {
		cfg = append(cfg, yaml.MapItem{Key: "bearer_token_file", Value: ep.BearerTokenFile})
	}

	if ep.BearerTokenSecret.Name != "" {
		if s, ok := ssCache.bearerTokens[m.AsMapKey(i)]; ok {
			cfg = append(cfg, yaml.MapItem{Key: "bearer_token", Value: s})
		}
	}

	if ep.BasicAuth != nil {
		var bac yaml.MapSlice
		if s, ok := ssCache.baSecrets[m.AsMapKey(i)]; ok {
			bac = append(bac,
				yaml.MapItem{Key: "username", Value: s.username},
				yaml.MapItem{Key: "password", Value: s.password},
			)
		}
		if len(ep.BasicAuth.PasswordFile) > 0 {
			bac = append(bac, yaml.MapItem{Key: "password_file", Value: ep.BasicAuth.PasswordFile})
		}
		if len(bac) > 0 {
			cfg = append(cfg, yaml.MapItem{Key: "basic_auth", Value: bac})
		}
	}

	var relabelings []yaml.MapSlice

	// Filter targets by services selected by the scrape.

	// Exact label matches.
	var labelKeys []string
	for k := range m.Spec.Selector.MatchLabels {
		labelKeys = append(labelKeys, k)
	}
	sort.Strings(labelKeys)

	for _, k := range labelKeys {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "action", Value: "keep"},
			{Key: "source_labels", Value: []string{"__meta_kubernetes_service_label_" + sanitizeLabelName(k)}},
			{Key: "regex", Value: m.Spec.Selector.MatchLabels[k]},
		})
	}
	// Set based label matching. We have to map the valid relations
	// `In`, `NotIn`, `Exists`, and `DoesNotExist`, into relabeling rules.
	for _, exp := range m.Spec.Selector.MatchExpressions {
		switch exp.Operator {
		case metav1.LabelSelectorOpIn:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_service_label_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: strings.Join(exp.Values, "|")},
			})
		case metav1.LabelSelectorOpNotIn:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "drop"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_service_label_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: strings.Join(exp.Values, "|")},
			})
		case metav1.LabelSelectorOpExists:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_service_label_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: ".+"},
			})
		case metav1.LabelSelectorOpDoesNotExist:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "drop"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_service_label_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: ".+"},
			})
		}
	}

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

	if ep.RelabelConfigs != nil {
		for _, c := range ep.RelabelConfigs {
			relabelings = append(relabelings, generateRelabelConfig(c))
		}
	}
	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, m.Namespace, enforcedNamespaceLabel)
	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})

	if m.Spec.SampleLimit > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "sample_limit", Value: m.Spec.SampleLimit})
	}

	if ep.MetricRelabelConfigs != nil {
		var metricRelabelings []yaml.MapSlice
		for _, c := range ep.MetricRelabelConfigs {
			if c.TargetLabel != "" && enforcedNamespaceLabel != "" && c.TargetLabel == enforcedNamespaceLabel {
				continue
			}
			relabeling := generateRelabelConfig(c)

			metricRelabelings = append(metricRelabelings, relabeling)
		}
		cfg = append(cfg, yaml.MapItem{Key: "metric_relabel_configs", Value: metricRelabelings})
	}

	cfg = append(cfg, buildVMScrapeParams(m.Namespace, m.AsProxyKey(i), ep.VMScrapeParams, ssCache)...)
	if ep.OAuth2 != nil {
		r := buildOAuth2Config(m.AsMapKey(i), ep.OAuth2, ssCache.oauth2Secrets)
		if len(r) > 0 {
			cfg = append(cfg, yaml.MapItem{Key: "oauth2", Value: r})
		}
	}

	return cfg
}

func generateNodeScrapeConfig(
	cr *victoriametricsv1beta1.VMNodeScrape,
	i int,
	apiserverConfig *victoriametricsv1beta1.APIServerConfig,
	ssCache *scrapesSecretsCache,
	ignoreHonorLabels bool,
	overrideHonorTimestamps bool,
	enforcedNamespaceLabel string) yaml.MapSlice {

	nodeSpec := cr.Spec
	hl := honorLabels(nodeSpec.HonorLabels, ignoreHonorLabels)
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: fmt.Sprintf("%s/%s/%d", cr.Namespace, cr.Name, i),
		},
		{
			Key:   "honor_labels",
			Value: hl,
		},
	}
	cfg = honorTimestamps(cfg, nodeSpec.HonorTimestamps, overrideHonorTimestamps)

	cfg = append(cfg, generateK8SSDConfig(nil, apiserverConfig, ssCache.baSecrets, kubernetesSDRoleNode))

	if nodeSpec.ScrapeInterval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: nodeSpec.ScrapeInterval})
	} else if nodeSpec.Interval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: nodeSpec.Interval})
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

	if nodeSpec.BearerTokenSecret.Name != "" {
		if s, ok := ssCache.bearerTokens[cr.AsMapKey()]; ok {
			cfg = append(cfg, yaml.MapItem{Key: "bearer_token", Value: s})
		}
	}
	if cr.Spec.BasicAuth != nil {
		var bac yaml.MapSlice
		if s, ok := ssCache.baSecrets[cr.AsMapKey()]; ok {
			bac = append(bac,
				yaml.MapItem{Key: "username", Value: s.username},
				yaml.MapItem{Key: "password", Value: s.password},
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
				{Key: "source_labels", Value: []string{"__meta_kubernetes_node_label_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: ".+"},
			})
		case metav1.LabelSelectorOpDoesNotExist:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "drop"},
				{Key: "source_labels", Value: []string{"__meta_kubernetes_node_label_" + sanitizeLabelName(exp.Key)}},
				{Key: "regex", Value: ".+"},
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

	if nodeSpec.RelabelConfigs != nil {
		for _, c := range nodeSpec.RelabelConfigs {
			relabelings = append(relabelings, generateRelabelConfig(c))
		}
	}
	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, cr.Namespace, enforcedNamespaceLabel)
	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})

	if cr.Spec.SampleLimit > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "sample_limit", Value: cr.Spec.SampleLimit})
	}

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
	if nodeSpec.OAuth2 != nil {
		r := buildOAuth2Config(cr.AsMapKey(), nodeSpec.OAuth2, ssCache.oauth2Secrets)
		if len(r) > 0 {
			cfg = append(cfg, yaml.MapItem{Key: "oauth2", Value: r})
		}
	}
	return cfg
}

func addTLStoYaml(cfg yaml.MapSlice, namespace string, tls *victoriametricsv1beta1.TLSConfig, addDirect bool) yaml.MapSlice {
	if tls != nil {
		pathPrefix := path.Join(tlsAssetsDir, namespace)
		tlsConfig := yaml.MapSlice{
			{Key: "insecure_skip_verify", Value: tls.InsecureSkipVerify},
		}
		if tls.CAFile != "" {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "ca_file", Value: tls.CAFile})
		} else if tls.CA.Name() != "" {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "ca_file", Value: tls.BuildAssetPath(pathPrefix, tls.CA.Name(), tls.CA.Key())})
		}
		if tls.CertFile != "" {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "cert_file", Value: tls.CertFile})
		} else if tls.Cert.Name() != "" {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "cert_file", Value: tls.BuildAssetPath(pathPrefix, tls.Cert.Name(), tls.Cert.Key())})
		}
		if tls.KeyFile != "" {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "key_file", Value: tls.KeyFile})
		} else if tls.KeySecret != nil {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "key_file", Value: tls.BuildAssetPath(pathPrefix, tls.KeySecret.Name, tls.KeySecret.Key)})
		}
		if tls.ServerName != "" {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "server_name", Value: tls.ServerName})
		}
		if addDirect {
			cfg = append(cfg, tlsConfig...)
			return cfg
		}
		cfg = append(cfg, yaml.MapItem{Key: "tls_config", Value: tlsConfig})
	}
	return cfg
}

func addRelabelConfigs(dst []yaml.MapSlice, rcs []victoriametricsv1beta1.RelabelConfig) []yaml.MapSlice {
	for i := range rcs {
		rc := &rcs[i]
		if rc.IsEmpty() {
			continue
		}
		dst = append(dst, generateRelabelConfig(rc))
	}
	return dst
}

func generateRelabelConfig(rc *victoriametricsv1beta1.RelabelConfig) yaml.MapSlice {
	relabeling := yaml.MapSlice{}

	c := updateRelabelConfigWithUnderscores(rc)

	if len(c.SourceLabels) > 0 {
		relabeling = append(relabeling, yaml.MapItem{Key: "source_labels", Value: c.SourceLabels})
	}

	if c.Separator != "" {
		relabeling = append(relabeling, yaml.MapItem{Key: "separator", Value: c.Separator})
	}

	if c.TargetLabel != "" {
		relabeling = append(relabeling, yaml.MapItem{Key: "target_label", Value: c.TargetLabel})
	}

	if c.Regex != "" {
		relabeling = append(relabeling, yaml.MapItem{Key: "regex", Value: c.Regex})
	}

	if c.Modulus != uint64(0) {
		relabeling = append(relabeling, yaml.MapItem{Key: "modulus", Value: c.Modulus})
	}

	if c.Replacement != "" {
		relabeling = append(relabeling, yaml.MapItem{Key: "replacement", Value: c.Replacement})
	}

	if c.Action != "" {
		relabeling = append(relabeling, yaml.MapItem{Key: "action", Value: c.Action})
	}

	return relabeling
}

func volumeName(name string) string {
	return fmt.Sprintf("%s-db", prefixedName(name))
}

func prefixedName(name string) string {
	return fmt.Sprintf("vmalertmanager-%s", name)
}

// getNamespacesFromNamespaceSelector gets a list of namespaces to select based on
// the given namespace selector, the given default namespace, and whether to ignore namespace selectors
func getNamespacesFromNamespaceSelector(nsSelector *victoriametricsv1beta1.NamespaceSelector, namespace string, ignoreNamespaceSelectors bool) []string {
	switch {
	case ignoreNamespaceSelectors:
		return []string{namespace}
	case nsSelector.Any:
		return []string{}
	case len(nsSelector.MatchNames) == 0:
		return []string{namespace}
	default:
		return nsSelector.MatchNames
	}
}

func combineSelectorStr(kvs map[string]string) string {
	kvsSlice := make([]string, 0)
	for k, v := range kvs {
		kvsSlice = append(kvsSlice, fmt.Sprintf("%v=%v", k, v))
	}

	return strings.Join(kvsSlice, ",")
}

func generatePodK8SSDConfig(namespaces []string, labelSelector metav1.LabelSelector, apiserverConfig *victoriametricsv1beta1.APIServerConfig, basicAuthSecrets map[string]*BasicAuthCredentials, role string) yaml.MapItem {

	cfg := generateK8SSDConfig(namespaces, apiserverConfig, basicAuthSecrets, role)

	if len(labelSelector.MatchLabels) != 0 {
		k8sSDs, flag := cfg.Value.([]yaml.MapSlice)
		if !flag {
			log.Error(fmt.Errorf("type assert failed"), "cfg.Value is not []yaml.MapSlice")
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

func generateK8SSDConfig(namespaces []string, apiserverConfig *victoriametricsv1beta1.APIServerConfig, basicAuthSecrets map[string]*BasicAuthCredentials, role string) yaml.MapItem {
	k8sSDConfig := yaml.MapSlice{
		{
			Key:   "role",
			Value: role,
		},
	}

	if len(namespaces) != 0 {
		k8sSDConfig = append(k8sSDConfig, yaml.MapItem{
			Key: "namespaces",
			Value: yaml.MapSlice{
				{
					Key:   "names",
					Value: namespaces,
				},
			},
		})
	}

	if apiserverConfig != nil {
		k8sSDConfig = append(k8sSDConfig, yaml.MapItem{
			Key: "api_server", Value: apiserverConfig.Host,
		})

		if apiserverConfig.BasicAuth != nil && basicAuthSecrets != nil {
			if s, ok := basicAuthSecrets["apiserver"]; ok {
				k8sSDConfig = append(k8sSDConfig, yaml.MapItem{
					Key: "basic_auth", Value: yaml.MapSlice{
						{Key: "username", Value: s.username},
						{Key: "password", Value: s.password},
					},
				})
			}
		}

		if apiserverConfig.BearerToken != "" {
			k8sSDConfig = append(k8sSDConfig, yaml.MapItem{Key: "bearer_token", Value: apiserverConfig.BearerToken})
		}

		if apiserverConfig.BearerTokenFile != "" {
			k8sSDConfig = append(k8sSDConfig, yaml.MapItem{Key: "bearer_token_file", Value: apiserverConfig.BearerTokenFile})
		}

		// config as well, make sure to path the right namespace here.
		k8sSDConfig = addTLStoYaml(k8sSDConfig, "", apiserverConfig.TLSConfig, false)
	}

	return yaml.MapItem{
		Key: "kubernetes_sd_configs",
		Value: []yaml.MapSlice{
			k8sSDConfig,
		},
	}
}

func enforceNamespaceLabel(relabelings []yaml.MapSlice, namespace, enforcedNamespaceLabel string) []yaml.MapSlice {
	if enforcedNamespaceLabel == "" {
		return relabelings
	}
	return append(relabelings, yaml.MapSlice{
		{Key: "target_label", Value: enforcedNamespaceLabel},
		{Key: "replacement", Value: namespace}})
}

func buildExternalLabels(p *victoriametricsv1beta1.VMAgent) yaml.MapSlice {
	m := map[string]string{}

	// Use "prometheus" external label name by default if field is missing.
	// in case of migration from prometheus to vmagent, it helps to have same labels
	// Do not add external label if field is set to empty string.
	prometheusExternalLabelName := "prometheus"
	if p.Spec.VMAgentExternalLabelName != nil {
		if *p.Spec.VMAgentExternalLabelName != "" {
			prometheusExternalLabelName = *p.Spec.VMAgentExternalLabelName
		} else {
			prometheusExternalLabelName = ""
		}
	}

	if prometheusExternalLabelName != "" {
		m[prometheusExternalLabelName] = fmt.Sprintf("%s/%s", p.Namespace, p.Name)
	}

	for n, v := range p.Spec.ExternalLabels {
		m[n] = v
	}
	return stringMapToMapSlice(m)
}

// replaces targetLabel and sourceLabels with underscore version if possible.
func updateRelabelConfigWithUnderscores(c *victoriametricsv1beta1.RelabelConfig) *victoriametricsv1beta1.RelabelConfig {
	// copy to prevent side effects.
	rc := c.DeepCopy()
	if len(rc.SourceLabels) == 0 && len(rc.UnderScoreSourceLabels) > 0 {
		rc.SourceLabels = append(rc.SourceLabels, rc.UnderScoreSourceLabels...)
	}
	if rc.TargetLabel == "" && rc.UnderScoreTargetLabel != "" {
		rc.TargetLabel = rc.UnderScoreTargetLabel
	}
	return rc
}

func buildVMScrapeParams(namespace, cacheKey string, cfg *victoriametricsv1beta1.VMScrapeParams, ssCache *scrapesSecretsCache) yaml.MapSlice {
	var r yaml.MapSlice
	if cfg == nil {
		return r
	}
	toYaml := func(key string, src interface{}) {
		if src == nil || reflect.ValueOf(src).IsNil() {
			return
		}
		r = append(r, yaml.MapItem{Key: key, Value: src})
	}
	toYaml("scrape_align_interval", cfg.ScrapeAlignInterval)
	toYaml("stream_parse", cfg.StreamParse)
	toYaml("disable_compression", cfg.DisableCompression)
	toYaml("scrape_offset", cfg.ScrapeOffset)
	toYaml("disable_keep_alive", cfg.DisableKeepAlive)
	toYaml("relabel_debug", cfg.RelabelDebug)
	toYaml("metric_relabel_debug", cfg.MetricRelabelDebug)
	if cfg.ProxyClientConfig != nil {
		r = append(r, buildProxyAuthConfig(namespace, cacheKey, cfg.ProxyClientConfig, ssCache)...)
	}
	return r
}

func buildOAuth2Config(cacheKey string, cfg *victoriametricsv1beta1.OAuth2, oauth2Cache map[string]*oauthCreds) yaml.MapSlice {
	var r yaml.MapSlice
	cachedSecret := oauth2Cache[cacheKey]
	if cachedSecret == nil {
		return r
	}
	if len(cachedSecret.clientID) > 0 {
		r = append(r, yaml.MapItem{Key: "client_id", Value: cachedSecret.clientID})
	}

	if cfg.ClientSecret != nil {
		r = append(r, yaml.MapItem{Key: "client_secret", Value: cachedSecret.clientSecret})
	} else if cfg.ClientSecretFile != "" {
		r = append(r, yaml.MapItem{Key: "client_secret_file", Value: cfg.ClientSecretFile})
	}

	if len(cfg.Scopes) > 0 {
		r = append(r, yaml.MapItem{Key: "scopes", Value: cfg.Scopes})
	}
	if len(cfg.EndpointParams) > 0 {
		r = append(r, yaml.MapItem{Key: "endpoint_params", Value: cfg.EndpointParams})
	}
	if len(cfg.TokenURL) > 0 {
		r = append(r, yaml.MapItem{Key: "token_url", Value: cfg.TokenURL})
	}
	return r
}

func buildProxyAuthConfig(namespace, cacheKey string, proxyAuth *victoriametricsv1beta1.ProxyAuth, ssCache *scrapesSecretsCache) yaml.MapSlice {
	var r yaml.MapSlice
	if proxyAuth.BasicAuth != nil {
		var pa yaml.MapSlice
		if ba, ok := ssCache.baSecrets[cacheKey]; ok {
			pa = append(pa,
				yaml.MapItem{Key: "username", Value: ba.username},
				yaml.MapItem{Key: "password", Value: ba.password},
			)
		}
		if len(proxyAuth.BasicAuth.PasswordFile) > 0 {
			pa = append(pa, yaml.MapItem{Key: "password_file", Value: proxyAuth.BasicAuth.PasswordFile})
		}
		if len(pa) > 0 {
			r = append(r, yaml.MapItem{Key: "proxy_basic_auth", Value: pa})
		}
	}
	if proxyAuth.TLSConfig != nil {
		t := addTLStoYaml(yaml.MapSlice{}, namespace, proxyAuth.TLSConfig, true)
		if len(t) > 0 {
			r = append(r, yaml.MapItem{Key: "proxy_tls_config", Value: t})
		}
	}

	if proxyAuth.BearerToken != nil {
		if bt, ok := ssCache.bearerTokens[cacheKey]; ok {
			r = append(r, yaml.MapItem{Key: "proxy_bearer_token", Value: bt})
		}
	} else if len(proxyAuth.BearerTokenFile) > 0 {
		r = append(r, yaml.MapItem{Key: "proxy_bearer_token_file", Value: proxyAuth.BearerTokenFile})
	}
	return r
}
