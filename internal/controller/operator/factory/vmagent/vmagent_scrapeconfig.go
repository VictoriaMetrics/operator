package vmagent

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/VictoriaMetrics/metricsql"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// CreateOrUpdateScrapeConfig builds scrape configuration for VMAgent
func CreateOrUpdateScrapeConfig(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent, childObject client.Object) error {
	var prevCR *vmv1beta1.VMAgent
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	ac := getAssetsCache(ctx, rclient, cr)
	if err := createOrUpdateScrapeConfig(ctx, rclient, cr, prevCR, childObject, ac); err != nil {
		return err
	}
	return nil
}

func createOrUpdateScrapeConfig(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, childObject client.Object, ac *build.AssetsCache) error {
	if ptr.Deref(cr.Spec.IngestOnlyMode, false) {
		return nil
	}
	// HACK: newPodSpec could load content into ac and it must be called
	// before secret config reconcile
	//
	// TODO: @f41gh7 rewrite this section with VLAgent secret assets injection pattern
	if _, err := newPodSpec(cr, ac); err != nil {
		return err
	}

	pos := &parsedObjects{
		Namespace:                cr.Namespace,
		APIServerConfig:          cr.Spec.APIServerConfig,
		MustUseNodeSelector:      cr.Spec.DaemonSetMode,
		HasClusterWideAccess:     config.IsClusterWideAccessAllowed() || !cr.IsOwnsServiceAccount(),
		ExternalLabels:           cr.ExternalLabels(),
		IgnoreNamespaceSelectors: cr.Spec.IgnoreNamespaceSelectors,
	}
	if !pos.HasClusterWideAccess {
		logger.WithContext(ctx).Info("setting discovery for the single namespace only." +
			"Since operator launched with set WATCH_NAMESPACE param. " +
			"Set custom ServiceAccountName property for VMAgent if needed.")
		pos.IgnoreNamespaceSelectors = true
	}
	sp := &cr.Spec.CommonScrapeParams
	if err := pos.init(ctx, rclient, sp); err != nil {
		return err
	}

	pos.validateObjects(sp)

	// Update secret based on the most recent configuration.
	generatedConfig, err := pos.generateConfig(
		ctx,
		sp,
		ac,
	)
	if err != nil {
		return fmt.Errorf("generating config for vmagent failed: %w", err)
	}

	for kind, secret := range ac.GetOutput() {
		var prevSecretMeta *metav1.ObjectMeta
		if prevCR != nil {
			prevSecretMeta = ptr.To(build.ResourceMeta(kind, prevCR))
		}
		if kind == build.SecretConfigResourceKind {
			// Compress config to avoid 1mb secret limit for a while
			data, err := build.GzipConfig(generatedConfig)
			if err != nil {
				return fmt.Errorf("cannot gzip config for vmagent: %w", err)
			}
			secret.Data[scrapeGzippedFilename] = data
		}
		secret.ObjectMeta = build.ResourceMeta(kind, cr)
		secret.Annotations = map[string]string{
			"generated": "true",
		}
		if err := reconcile.Secret(ctx, rclient, &secret, prevSecretMeta); err != nil {
			return err
		}
	}

	parentName := fmt.Sprintf("%s.%s.vmagent", cr.Name, cr.Namespace)
	if err := pos.updateStatusesForScrapeObjects(ctx, rclient, parentName, childObject); err != nil {
		return err
	}

	return nil
}

// TODO: @f41gh7 validate VMScrapeParams
func testForArbitraryFSAccess(e vmv1beta1.EndpointAuth) error {
	if e.BearerTokenFile != "" {
		return fmt.Errorf("it accesses file system via bearer token file which VMAgent specification prohibits")
	}
	if e.BasicAuth != nil && e.BasicAuth.PasswordFile != "" {
		return fmt.Errorf("it accesses file system via basicAuth password file which VMAgent specification prohibits")
	}

	if e.OAuth2 != nil && e.OAuth2.ClientSecretFile != "" {
		return fmt.Errorf("it accesses file system via oauth2 client secret file which VMAgent specification prohibits")
	}

	tlsConf := e.TLSConfig
	if tlsConf == nil {
		return nil
	}

	if err := e.TLSConfig.Validate(); err != nil {
		return err
	}

	if tlsConf.CAFile != "" || tlsConf.CertFile != "" || tlsConf.KeyFile != "" {
		return fmt.Errorf("it accesses file system via tls config which VMAgent specification prohibits")
	}

	return nil
}

func setScrapeIntervalToWithLimit(ctx context.Context, dst *vmv1beta1.EndpointScrapeParams, sp *vmv1beta1.CommonScrapeParams) {
	if dst.ScrapeInterval == "" {
		dst.ScrapeInterval = dst.Interval
	}

	originInterval, minIntervalStr, maxIntervalStr := dst.ScrapeInterval, sp.MinScrapeInterval, sp.MaxScrapeInterval
	if originInterval == "" || (minIntervalStr == nil && maxIntervalStr == nil) {
		// fast path
		return
	}
	originDurationMs, err := metricsql.DurationValue(originInterval, 0)
	if err != nil {
		logger.WithContext(ctx).Error(err, fmt.Sprintf("cannot parse duration value during limiting interval, using original value: %s", originInterval))
		return
	}

	if minIntervalStr != nil {
		parsedMinMs, err := metricsql.DurationValue(*minIntervalStr, 0)
		if err != nil {
			logger.WithContext(ctx).Error(err, fmt.Sprintf("cannot parse minScrapeInterval: %s, using original value: %s", *minIntervalStr, originInterval))
			return
		}
		if parsedMinMs >= originDurationMs {
			dst.ScrapeInterval = *minIntervalStr
			return
		}
	}
	if maxIntervalStr != nil {
		parsedMaxMs, err := metricsql.DurationValue(*maxIntervalStr, 0)
		if err != nil {
			logger.WithContext(ctx).Error(err, fmt.Sprintf("cannot parse maxScrapeInterval: %s, using origin value: %s", *maxIntervalStr, originInterval))
			return
		}
		if parsedMaxMs < originDurationMs {
			dst.ScrapeInterval = *maxIntervalStr
			return
		}
	}
}

const (
	defaultScrapeInterval  = "30s"
	k8sSDRoleEndpoints     = "endpoints"
	k8sSDRoleService       = "service"
	k8sSDRoleEndpointslice = "endpointslice"
	k8sSDRolePod           = "pod"
	k8sSDRoleIngress       = "ingress"
	k8sSDRoleNode          = "node"

	// before 0.67.0 endpointslice was called endpointslices. keeping old name for backward compatibility. will be removed in 0.70.0
	k8sSDRoleLegacyEndpointslices = "endpointslices"
)

var invalidLabelCharRE = regexp.MustCompile(`[^a-zA-Z0-9_]`)

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

// honorLabels determines the value of honor_labels.
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

func addAttachMetadata(dst yaml.MapSlice, am *vmv1beta1.AttachMetadata, role string) yaml.MapSlice {
	if am == nil {
		return dst
	}
	var items yaml.MapSlice
	if am.Node != nil && *am.Node {
		switch role {
		case k8sSDRolePod, k8sSDRoleEndpoints, k8sSDRoleEndpointslice, k8sSDRoleLegacyEndpointslices:
			items = append(items, yaml.MapItem{
				Key:   "node",
				Value: true,
			})
		}
	}
	if am.Namespace != nil && *am.Namespace {
		switch role {
		case k8sSDRolePod, k8sSDRoleService, k8sSDRoleEndpoints, k8sSDRoleEndpointslice, k8sSDRoleIngress, k8sSDRoleLegacyEndpointslices:
			items = append(items, yaml.MapItem{
				Key:   "namespace",
				Value: true,
			})
		}
	}
	if len(items) > 0 {
		dst = append(dst, yaml.MapItem{
			Key:   "attach_metadata",
			Value: items,
		})
	}
	return dst
}

func addRelabelConfigs(dst []yaml.MapSlice, rcs []*vmv1beta1.RelabelConfig) []yaml.MapSlice {
	for i := range rcs {
		rc := rcs[i]
		if rc.IsEmpty() {
			continue
		}
		dst = append(dst, generateRelabelConfig(rc))
	}
	return dst
}

func generateRelabelConfig(rc *vmv1beta1.RelabelConfig) yaml.MapSlice {
	relabeling := yaml.MapSlice{}

	if len(rc.SourceLabels) > 0 {
		relabeling = append(relabeling, yaml.MapItem{Key: "source_labels", Value: rc.SourceLabels})
	}

	if rc.Separator != nil {
		relabeling = append(relabeling, yaml.MapItem{Key: "separator", Value: *rc.Separator})
	}

	if rc.TargetLabel != "" {
		relabeling = append(relabeling, yaml.MapItem{Key: "target_label", Value: rc.TargetLabel})
	}

	if len(rc.Regex) > 0 {
		// dirty hack to properly format regex
		if len(rc.Regex) == 1 {
			relabeling = append(relabeling, yaml.MapItem{Key: "regex", Value: rc.Regex[0]})
		} else {
			relabeling = append(relabeling, yaml.MapItem{Key: "regex", Value: rc.Regex})
		}
	}

	if rc.Modulus != uint64(0) {
		relabeling = append(relabeling, yaml.MapItem{Key: "modulus", Value: rc.Modulus})
	}

	if rc.Replacement != nil {
		relabeling = append(relabeling, yaml.MapItem{Key: "replacement", Value: *rc.Replacement})
	}

	if rc.Action != "" {
		relabeling = append(relabeling, yaml.MapItem{Key: "action", Value: rc.Action})
	}
	if len(rc.If) != 0 {
		relabeling = append(relabeling, yaml.MapItem{Key: "if", Value: rc.If})
	}
	if rc.Match != "" {
		relabeling = append(relabeling, yaml.MapItem{Key: "match", Value: rc.Match})
	}
	if len(rc.Labels) > 0 {
		sortKeys := make([]string, 0, len(rc.Labels))
		labels := make(yaml.MapSlice, 0, len(rc.Labels))
		for key := range rc.Labels {
			sortKeys = append(sortKeys, key)
		}
		sort.Strings(sortKeys)
		for idx := range sortKeys {
			key := sortKeys[idx]
			labels = append(labels, yaml.MapItem{Key: key, Value: rc.Labels[key]})
		}
		relabeling = append(relabeling, yaml.MapItem{Key: "labels", Value: labels})
	}

	return relabeling
}

// getNamespacesFromNamespaceSelector gets a list of namespaces to select based on
// the given namespace selector, the given default namespace, and whether to ignore namespace selectors
func getNamespacesFromNamespaceSelector(nsSelector *vmv1beta1.NamespaceSelector, namespace string, ignoreNamespaceSelectors bool) []string {
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

type generateK8SSDConfigOptions struct {
	namespaces          []string
	apiServerConfig     *vmv1beta1.APIServerConfig
	role                string
	attachMetadata      *vmv1beta1.AttachMetadata
	shouldAddSelectors  bool
	selectors           metav1.LabelSelector
	mustUseNodeSelector bool
	namespace           string
}

func generateK8SSDConfig(ac *build.AssetsCache, opts generateK8SSDConfigOptions) (yaml.MapSlice, error) {
	k8sSDConfig := yaml.MapSlice{
		{
			Key:   "role",
			Value: opts.role,
		},
	}
	k8sSDConfig = addAttachMetadata(k8sSDConfig, opts.attachMetadata, opts.role)
	if len(opts.namespaces) != 0 {
		k8sSDConfig = append(k8sSDConfig, yaml.MapItem{
			Key: "namespaces",
			Value: yaml.MapSlice{
				{
					Key:   "names",
					Value: opts.namespaces,
				},
			},
		})
	}

	if opts.apiServerConfig != nil {
		apiserverConfig := opts.apiServerConfig
		k8sSDConfig = append(k8sSDConfig, yaml.MapItem{
			Key: "api_server", Value: apiserverConfig.Host,
		})

		if apiserverConfig.BasicAuth != nil {
			cfg, err := ac.BasicAuthToYAML(opts.namespace, apiserverConfig.BasicAuth)
			if err != nil {
				return nil, fmt.Errorf("could not generate basicAuth for apiserver config: %w", err)
			}
			if len(cfg) > 0 {
				k8sSDConfig = append(k8sSDConfig, yaml.MapItem{Key: "basic_auth", Value: cfg})
			}
		}

		if apiserverConfig.BearerTokenFile != "" {
			k8sSDConfig = append(k8sSDConfig, yaml.MapItem{Key: "bearer_token_file", Value: apiserverConfig.BearerTokenFile})
		} else if apiserverConfig.BearerToken != "" {
			k8sSDConfig = append(k8sSDConfig, yaml.MapItem{Key: "bearer_token", Value: apiserverConfig.BearerToken})
		}

		if apiserverConfig.Authorization != nil {
			cfg, err := ac.AuthorizationToYAML(opts.namespace, apiserverConfig.Authorization)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch authorization secret for apiserver config: %w", err)
			}
			if len(cfg) > 0 {
				k8sSDConfig = append(k8sSDConfig, cfg...)
			}
		}

		// config as well, make sure to path the right namespace here.
		if apiserverConfig.TLSConfig != nil {
			cfg, err := ac.TLSToYAML(opts.namespace, "", apiserverConfig.TLSConfig)
			if err != nil {
				return nil, fmt.Errorf("cannot add tls asset for apiServerConfig: %w", err)
			}
			if len(cfg) > 0 {
				k8sSDConfig = append(k8sSDConfig, yaml.MapItem{Key: "tls_config", Value: cfg})
			}
		}
	}

	var selectors []yaml.MapSlice

	isEmptySelectors := len(opts.selectors.MatchLabels)+len(opts.selectors.MatchExpressions) == 0
	switch {
	case opts.mustUseNodeSelector:
		var selector yaml.MapSlice
		selector = append(selector, yaml.MapItem{
			Key:   "role",
			Value: k8sSDRolePod,
		})
		selector = append(selector, yaml.MapItem{
			Key:   "field",
			Value: "spec.nodeName=" + kubeNodeEnvTemplate,
		})
		selectors = append(selectors, selector)

	case opts.shouldAddSelectors && !isEmptySelectors:
		var selector yaml.MapSlice
		selector = append(selector, yaml.MapItem{
			Key:   "role",
			Value: opts.role,
		})
		labelSelector, err := metav1.LabelSelectorAsSelector(&opts.selectors)
		if err != nil {
			panic(fmt.Sprintf("BUG: unexpected error, selectors must be already validated: %q: %s", opts.selectors.String(), err))
		}
		selector = append(selector, yaml.MapItem{
			Key:   "label",
			Value: labelSelector.String(),
		})
		selectors = append(selectors, selector)

		// special case, given roles create additional watchers for
		// pod and services roles
		if opts.role == k8sSDRoleEndpoints || opts.role == k8sSDRoleEndpointslice || opts.role == k8sSDRoleLegacyEndpointslices {
			for _, role := range []string{k8sSDRolePod, k8sSDRoleService} {
				selectors = append(selectors, yaml.MapSlice{
					{
						Key:   "role",
						Value: role,
					},
					{
						Key:   "label",
						Value: labelSelector.String(),
					},
				})
			}
		}
	}
	if len(selectors) > 0 {
		k8sSDConfig = append(k8sSDConfig, yaml.MapItem{
			Key:   "selectors",
			Value: selectors,
		})
	}

	return yaml.MapSlice{
		{
			Key: "kubernetes_sd_configs",
			Value: []yaml.MapSlice{
				k8sSDConfig,
			},
		},
	}, nil
}

func enforceNamespaceLabel(relabelings []yaml.MapSlice, namespace, enforcedNamespaceLabel string) []yaml.MapSlice {
	if enforcedNamespaceLabel == "" {
		return relabelings
	}
	return append(relabelings, yaml.MapSlice{
		{Key: "target_label", Value: enforcedNamespaceLabel},
		{Key: "replacement", Value: namespace},
	})
}

func buildVMScrapeParams(namespace string, cfg *vmv1beta1.VMScrapeParams, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var r yaml.MapSlice
	if cfg == nil {
		return r, nil
	}
	toYaml := func(key string, src any) {
		if src == nil || reflect.ValueOf(src).IsNil() {
			return
		}
		r = append(r, yaml.MapItem{Key: key, Value: src})
	}
	toYaml("scrape_align_interval", cfg.ScrapeAlignInterval)
	toYaml("stream_parse", cfg.StreamParse)
	toYaml("disable_compression", cfg.DisableCompression)
	toYaml("scrape_offset", cfg.ScrapeOffset)
	toYaml("no_stale_markers", cfg.DisableStaleMarkers)
	toYaml("disable_keepalive", cfg.DisableKeepAlive)
	if len(cfg.Headers) > 0 {
		r = append(r, yaml.MapItem{Key: "headers", Value: cfg.Headers})
	}
	if cfg.ProxyClientConfig != nil {
		if c, err := ac.ProxyAuthToYAML(namespace, cfg.ProxyClientConfig); err != nil {
			return nil, err
		} else if len(c) > 0 {
			r = append(r, c...)
		}
	}
	return r, nil
}

func addSelectorToRelabelingFor(relabelings []yaml.MapSlice, typeName string, selector metav1.LabelSelector, mustSkipAdd bool) []yaml.MapSlice {
	if mustSkipAdd {
		return relabelings
	}
	// Exact label matches.
	var labelKeys []string
	for k := range selector.MatchLabels {
		labelKeys = append(labelKeys, k)
	}
	sort.Strings(labelKeys)

	for _, k := range labelKeys {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "action", Value: "keep"},
			{Key: "source_labels", Value: []string{fmt.Sprintf("__meta_kubernetes_%s_label_%s", typeName, sanitizeLabelName(k))}},
			{Key: "regex", Value: selector.MatchLabels[k]},
		})
	}
	// Set based label matching. We have to map the valid relations
	// `In`, `NotIn`, `Exists`, and `DoesNotExist`, into relabeling rules.
	for _, exp := range selector.MatchExpressions {
		switch exp.Operator {
		case metav1.LabelSelectorOpIn:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{fmt.Sprintf("__meta_kubernetes_%s_label_%s", typeName, sanitizeLabelName(exp.Key))}},
				{Key: "regex", Value: strings.Join(exp.Values, "|")},
			})
		case metav1.LabelSelectorOpNotIn:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "drop"},
				{Key: "source_labels", Value: []string{fmt.Sprintf("__meta_kubernetes_%s_label_%s", typeName, sanitizeLabelName(exp.Key))}},
				{Key: "regex", Value: strings.Join(exp.Values, "|")},
			})
		case metav1.LabelSelectorOpExists:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{fmt.Sprintf("__meta_kubernetes_%s_labelpresent_%s", typeName, sanitizeLabelName(exp.Key))}},
				{Key: "regex", Value: "true"},
			})
		case metav1.LabelSelectorOpDoesNotExist:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "drop"},
				{Key: "source_labels", Value: []string{fmt.Sprintf("__meta_kubernetes_%s_labelpresent_%s", typeName, sanitizeLabelName(exp.Key))}},
				{Key: "regex", Value: "true"},
			})
		}
	}
	return relabelings
}

func addCommonScrapeParamsTo(cfg yaml.MapSlice, cs vmv1beta1.EndpointScrapeParams, se *vmv1beta1.CommonScrapeSecurityEnforcements) yaml.MapSlice {
	hl := honorLabels(cs.HonorLabels, se.OverrideHonorLabels)
	cfg = append(cfg, yaml.MapItem{
		Key:   "honor_labels",
		Value: hl,
	})

	cfg = honorTimestamps(cfg, cs.HonorTimestamps, se.OverrideHonorTimestamps)

	if cs.ScrapeInterval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: cs.ScrapeInterval})
	}
	if cs.ScrapeTimeout != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_timeout", Value: cs.ScrapeTimeout})
	}
	if cs.Path != "" {
		cfg = append(cfg, yaml.MapItem{Key: "metrics_path", Value: cs.Path})
	}
	if cs.ProxyURL != nil {
		cfg = append(cfg, yaml.MapItem{Key: "proxy_url", Value: cs.ProxyURL})
	}
	if cs.FollowRedirects != nil {
		cfg = append(cfg, yaml.MapItem{Key: "follow_redirects", Value: cs.FollowRedirects})
	}
	if len(cs.Params) > 0 {
		params := make(yaml.MapSlice, 0, len(cs.Params))
		paramIdxes := make([]string, len(cs.Params))
		var idxCnt int
		for k := range cs.Params {
			paramIdxes[idxCnt] = k
			idxCnt++
		}
		sort.Strings(paramIdxes)
		for _, k := range paramIdxes {
			params = append(params, yaml.MapItem{Key: k, Value: cs.Params[k]})
		}
		cfg = append(cfg, yaml.MapItem{Key: "params", Value: params})
	}
	if cs.Scheme != "" {
		// scheme may have uppercase format to be compatible with prometheus-operator objects
		// vmagent expects lower case format only
		cfg = append(cfg, yaml.MapItem{Key: "scheme", Value: strings.ToLower(cs.Scheme)})
	}
	if cs.MaxScrapeSize != "" {
		cfg = append(cfg, yaml.MapItem{Key: "max_scrape_size", Value: cs.MaxScrapeSize})
	}
	if cs.SampleLimit > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "sample_limit", Value: cs.SampleLimit})
	}
	if cs.SeriesLimit > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "series_limit", Value: cs.SeriesLimit})
	}
	return cfg
}

func addMetricRelabelingsTo(cfg yaml.MapSlice, src []*vmv1beta1.RelabelConfig, se *vmv1beta1.CommonScrapeSecurityEnforcements) yaml.MapSlice {
	if len(src) == 0 {
		return cfg
	}
	var metricRelabelings []yaml.MapSlice
	for _, c := range src {
		if c.TargetLabel != "" && se.EnforcedNamespaceLabel != "" && c.TargetLabel == se.EnforcedNamespaceLabel {
			continue
		}
		relabeling := generateRelabelConfig(c)

		metricRelabelings = append(metricRelabelings, relabeling)
	}
	if len(metricRelabelings) == 0 {
		return cfg
	}
	cfg = append(cfg, yaml.MapItem{Key: "metric_relabel_configs", Value: metricRelabelings})
	return cfg
}

func addEndpointAuthTo(cfg yaml.MapSlice, ea *vmv1beta1.EndpointAuth, namespace string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	if c, err := ac.TLSToYAML(namespace, "", ea.TLSConfig); err != nil {
		return nil, err
	} else if len(c) > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "tls_config", Value: c})
	}
	if ea.BearerTokenFile != "" {
		cfg = append(cfg, yaml.MapItem{Key: "bearer_token_file", Value: ea.BearerTokenFile})
	} else if ea.BearerTokenSecret != nil && ea.BearerTokenSecret.Name != "" {
		if secret, err := ac.LoadKeyFromSecret(namespace, ea.BearerTokenSecret); err != nil {
			return nil, err
		} else {
			cfg = append(cfg, yaml.MapItem{Key: "bearer_token", Value: secret})
		}
	}
	if ea.BasicAuth != nil {
		if c, err := ac.BasicAuthToYAML(namespace, ea.BasicAuth); err != nil {
			return nil, err
		} else if len(c) > 0 {
			cfg = append(cfg, yaml.MapItem{Key: "basic_auth", Value: c})
		}
	}
	if c, err := ac.OAuth2ToYAML(namespace, ea.OAuth2); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}
	if c, err := ac.AuthorizationToYAML(namespace, ea.Authorization); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}
	return cfg, nil
}

func getAssetsCache(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) *build.AssetsCache {
	cfg := map[build.ResourceKind]*build.ResourceCfg{
		build.SecretConfigResourceKind: {
			MountDir:   vmAgentConfDir,
			SecretName: build.ResourceName(build.SecretConfigResourceKind, cr),
		},
		build.TLSAssetsResourceKind: {
			MountDir:   tlsAssetsDir,
			SecretName: build.ResourceName(build.TLSAssetsResourceKind, cr),
		},
	}
	return build.NewAssetsCache(ctx, rclient, cfg)
}

func validateScrapeClassExists(scrapeClassName *string, sp *vmv1beta1.CommonScrapeParams) error {
	if scrapeClassName == nil {
		return nil
	}
	for _, sc := range sp.ScrapeClasses {
		if sc.Name == *scrapeClassName {
			return nil
		}
	}
	return fmt.Errorf("scrape class %q not supported", *scrapeClassName)
}

func mergeEndpointAuthWithScrapeClass(authz *vmv1beta1.EndpointAuth, scrapeClass *vmv1beta1.ScrapeClass) {
	if authz == nil {
		panic("BUG: authz cannot be nil")
	}
	if scrapeClass == nil {
		return
	}

	authz.Authorization = mergeAuthorizationWithScrapeClass(authz.Authorization, scrapeClass)
	authz.BasicAuth = mergeBasicAuthWithScrapeClass(authz.BasicAuth, scrapeClass)
	authz.TLSConfig = mergeTLSConfigs(authz.TLSConfig, scrapeClass.TLSConfig)
	authz.OAuth2 = mergeOAuth2WithScrapeClass(authz.OAuth2, scrapeClass)
	if len(authz.BearerTokenFile) == 0 {
		authz.BearerTokenFile = scrapeClass.BearerTokenFile
	}

	if authz.BearerTokenSecret == nil {
		authz.BearerTokenSecret = scrapeClass.BearerTokenSecret
	}
}

func mergeEndpointRelabelingsWithScrapeClass(ers *vmv1beta1.EndpointRelabelings, scrapeClass *vmv1beta1.ScrapeClass) {
	if ers == nil {
		panic("BUG: ers cannot be nil")
	}
	ers.RelabelConfigs = append(ers.RelabelConfigs, scrapeClass.RelabelConfigs...)
	ers.MetricRelabelConfigs = append(ers.MetricRelabelConfigs, scrapeClass.MetricRelabelConfigs...)
}

func mergeAuthorizationWithScrapeClass(authz *vmv1beta1.Authorization, scrapeClass *vmv1beta1.ScrapeClass) *vmv1beta1.Authorization {
	if scrapeClass.Authorization == nil {
		return authz
	}
	if authz == nil {
		return scrapeClass.Authorization
	}
	if authz.Credentials == nil {
		authz.Credentials = scrapeClass.Authorization.Credentials
	}

	if authz.Credentials == nil && authz.CredentialsFile == "" {
		authz.Credentials = scrapeClass.Authorization.Credentials
		authz.CredentialsFile = scrapeClass.Authorization.CredentialsFile
	}

	return authz
}

func mergeBasicAuthWithScrapeClass(ba *vmv1beta1.BasicAuth, scrapeClass *vmv1beta1.ScrapeClass) *vmv1beta1.BasicAuth {
	if scrapeClass.BasicAuth == nil {
		return ba
	}
	if ba == nil {
		return scrapeClass.BasicAuth
	}
	if ba.Username.Name == "" {
		ba.Username = scrapeClass.BasicAuth.Username
	}
	if ba.Password.Name == "" && ba.PasswordFile == "" {
		ba.Password = scrapeClass.BasicAuth.Password
		ba.PasswordFile = scrapeClass.BasicAuth.PasswordFile
	}
	return ba
}

func mergeOAuth2WithScrapeClass(oauth2 *vmv1beta1.OAuth2, scrapeClass *vmv1beta1.ScrapeClass) *vmv1beta1.OAuth2 {
	if scrapeClass.OAuth2 == nil {
		return oauth2
	}
	if oauth2 == nil {
		return scrapeClass.OAuth2
	}

	oauth2.TLSConfig = mergeTLSConfigs(oauth2.TLSConfig, scrapeClass.OAuth2.TLSConfig)

	if oauth2.ClientSecret == nil && oauth2.ClientSecretFile == "" {
		oauth2.ClientSecret = scrapeClass.OAuth2.ClientSecret
		oauth2.ClientSecretFile = scrapeClass.OAuth2.ClientSecretFile
	}
	if oauth2.ClientID == (vmv1beta1.SecretOrConfigMap{}) {
		oauth2.ClientID = scrapeClass.OAuth2.ClientID
	}
	if len(oauth2.EndpointParams) == 0 {
		oauth2.EndpointParams = scrapeClass.OAuth2.EndpointParams
	}
	if len(oauth2.Scopes) == 0 {
		oauth2.Scopes = scrapeClass.OAuth2.Scopes
	}
	if oauth2.TokenURL == "" {
		oauth2.TokenURL = scrapeClass.OAuth2.TokenURL
	}
	if oauth2.ProxyURL == "" {
		oauth2.ProxyURL = scrapeClass.OAuth2.ProxyURL
	}
	return oauth2
}

func mergeAttachMetadataWithScrapeClass(am *vmv1beta1.AttachMetadata, scrapeClass *vmv1beta1.ScrapeClass) {
	if am == nil {
		panic("BUG: am cannot be nil")
	}
	if scrapeClass.AttachMetadata == nil {
		return
	}

	if am.Node == nil {
		am.Node = scrapeClass.AttachMetadata.Node
	}
	if am.Namespace == nil {
		am.Namespace = scrapeClass.AttachMetadata.Namespace
	}
}

func mergeTLSConfigs(left, right *vmv1beta1.TLSConfig) *vmv1beta1.TLSConfig {
	if right == nil {
		return left
	}
	if left == nil {
		return right
	}

	if left.CAFile == "" && left.CA == (vmv1beta1.SecretOrConfigMap{}) {
		left.CAFile = right.CAFile
		left.CA = right.CA
	}

	if left.CertFile == "" && left.Cert == (vmv1beta1.SecretOrConfigMap{}) {
		left.CertFile = right.CertFile
		left.Cert = right.Cert
	}

	if left.KeyFile == "" && left.KeySecret == nil {
		left.KeyFile = right.KeyFile
		left.KeySecret = right.KeySecret
	}

	if left.ServerName == "" {
		left.ServerName = right.ServerName
	}

	return left
}

func getScrapeClass(name *string, sp *vmv1beta1.CommonScrapeParams) *vmv1beta1.ScrapeClass {
	var defaultClass *vmv1beta1.ScrapeClass
	for _, scrapeClass := range sp.ScrapeClasses {
		if ptr.Deref(name, "") == scrapeClass.Name {
			return &scrapeClass
		}
		if ptr.Deref(scrapeClass.Default, false) {
			defaultClass = &scrapeClass
		}
	}

	return defaultClass
}
