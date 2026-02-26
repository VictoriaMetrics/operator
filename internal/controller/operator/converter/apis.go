package converter

import (
	"math"
	"strings"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

const (
	prometheusSecretDir    = "/etc/prometheus/secrets"
	prometheusConfigmapDir = "/etc/prometheus/configmaps"
)

var log = logf.Log.WithName("controller.PrometheusConverter")

// PrometheusRule creates VMRule from PrometheusRule
func PrometheusRule(prom *promv1.PrometheusRule, conf *config.BaseOperatorConf) *vmv1beta1.VMRule {
	ruleGroups := make([]vmv1beta1.RuleGroup, 0, len(prom.Spec.Groups))
	for _, promGroup := range prom.Spec.Groups {
		ruleItems := make([]vmv1beta1.Rule, 0, len(promGroup.Rules))
		for _, promRuleItem := range promGroup.Rules {
			trule := vmv1beta1.Rule{
				Labels:      promRuleItem.Labels,
				Annotations: promRuleItem.Annotations,
				Expr:        promRuleItem.Expr.String(),
				Record:      promRuleItem.Record,
				Alert:       promRuleItem.Alert,
			}
			if promRuleItem.For != nil {
				trule.For = string(*promRuleItem.For)
			}
			if promRuleItem.KeepFiringFor != nil {
				trule.KeepFiringFor = string(*promRuleItem.KeepFiringFor)
			}
			ruleItems = append(ruleItems, trule)
		}

		tgroup := vmv1beta1.RuleGroup{
			Name:   promGroup.Name,
			Rules:  ruleItems,
			Labels: promGroup.Labels,
		}
		if promGroup.QueryOffset != nil {
			tgroup.EvalOffset = string(*promGroup.QueryOffset)
		}
		if promGroup.Limit != nil {
			tgroup.Limit = *promGroup.Limit
		}
		if promGroup.Interval != nil {
			tgroup.Interval = string(*promGroup.Interval)
		}
		ruleGroups = append(ruleGroups, tgroup)
	}
	cr := &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   prom.Namespace,
			Name:        prom.Name,
			Labels:      FilterPrefixes(prom.Labels, conf.FilterPrometheusConverterLabelPrefixes),
			Annotations: FilterPrefixes(prom.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
		},
		Spec: vmv1beta1.VMRuleSpec{
			Groups: ruleGroups,
		},
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cr.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         promv1.SchemeGroupVersion.String(),
				Kind:               promv1.PrometheusRuleKind,
				Name:               prom.Name,
				UID:                prom.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
	}
	cr.Annotations = MaybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cr.Annotations)
	return cr
}

// MaybeAddArgoCDIgnoreAnnotations optionally adds ArgoCD annotations
func MaybeAddArgoCDIgnoreAnnotations(mustAdd bool, dst map[string]string) map[string]string {
	if !mustAdd {
		// fast path
		return dst
	}
	if dst == nil {
		dst = make(map[string]string)
	}
	dst["argocd.argoproj.io/compare-options"] = "IgnoreExtraneous"
	dst["argocd.argoproj.io/sync-options"] = "Prune=false"
	return dst
}

// ServiceMonitor create VMServiceScrape from ServiceMonitor
func ServiceMonitor(serviceMon *promv1.ServiceMonitor, conf *config.BaseOperatorConf) *vmv1beta1.VMServiceScrape {
	cs := &vmv1beta1.VMServiceScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceMon.Name,
			Namespace:   serviceMon.Namespace,
			Annotations: FilterPrefixes(serviceMon.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
			Labels:      FilterPrefixes(serviceMon.Labels, conf.FilterPrometheusConverterLabelPrefixes),
		},
		Spec: vmv1beta1.VMServiceScrapeSpec{
			JobLabel:        serviceMon.Spec.JobLabel,
			TargetLabels:    serviceMon.Spec.TargetLabels,
			PodTargetLabels: serviceMon.Spec.PodTargetLabels,
			Selector:        serviceMon.Spec.Selector,
			Endpoints:       convertEndpoint(serviceMon.Spec.Endpoints),
			NamespaceSelector: vmv1beta1.NamespaceSelector{
				Any:        serviceMon.Spec.NamespaceSelector.Any,
				MatchNames: serviceMon.Spec.NamespaceSelector.MatchNames,
			},
			ScrapeClassName: serviceMon.Spec.ScrapeClassName,
		},
	}
	if serviceMon.Spec.ServiceDiscoveryRole != nil {
		cs.Spec.DiscoveryRole = strings.ToLower(string(*serviceMon.Spec.ServiceDiscoveryRole))
	}
	if serviceMon.Spec.SampleLimit != nil && *serviceMon.Spec.SampleLimit <= uint64(math.MaxInt) {
		cs.Spec.SampleLimit = int(*serviceMon.Spec.SampleLimit)
	}
	if serviceMon.Spec.AttachMetadata != nil {
		cs.Spec.AttachMetadata = vmv1beta1.AttachMetadata{
			Node: serviceMon.Spec.AttachMetadata.Node,
		}
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cs.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         promv1.SchemeGroupVersion.String(),
				Kind:               promv1.ServiceMonitorsKind,
				Name:               serviceMon.Name,
				UID:                serviceMon.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
	}
	cs.Annotations = MaybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cs.Annotations)
	return cs
}

// ReplacePromDirPath replace prometheus directory path for config maps and secrets to VM one
func ReplacePromDirPath(origin string) string {
	if strings.HasPrefix(origin, prometheusSecretDir) {
		return strings.Replace(origin, prometheusSecretDir, vmv1beta1.SecretsDir, 1)
	}
	if strings.HasPrefix(origin, prometheusConfigmapDir) {
		return strings.Replace(origin, prometheusConfigmapDir, vmv1beta1.ConfigMapsDir, 1)
	}
	return origin
}

// ConvertOAuth converts prometheus OAuth config to VM one
func ConvertOAuth(src *promv1.OAuth2) *vmv1beta1.OAuth2 {
	if src == nil {
		return nil
	}

	o := vmv1beta1.OAuth2{
		ClientID:       convertSecretOrConfigmap(src.ClientID),
		ClientSecret:   &src.ClientSecret,
		Scopes:         src.Scopes,
		TokenURL:       src.TokenURL,
		EndpointParams: src.EndpointParams,
	}
	return &o
}

// ConvertAuthorization converts prometheus auth struct to VM one
func ConvertAuthorization(srcSafe *promv1.SafeAuthorization, src *promv1.Authorization) *vmv1beta1.Authorization {
	if srcSafe == nil && src == nil {
		return nil
	}
	if srcSafe != nil {
		return &vmv1beta1.Authorization{
			Type:        srcSafe.Type,
			Credentials: srcSafe.Credentials,
		}
	}
	return &vmv1beta1.Authorization{
		Type:            src.Type,
		Credentials:     src.Credentials,
		CredentialsFile: src.CredentialsFile,
	}
}

func convertBearerToken(src *corev1.SecretKeySelector) *corev1.SecretKeySelector {
	if src == nil || (src.Key == "" && src.Name == "") {
		return nil
	}
	return src
}

func convertEndpoint(promEndpoint []promv1.Endpoint) []vmv1beta1.Endpoint {
	endpoints := make([]vmv1beta1.Endpoint, 0, len(promEndpoint))
	for _, endpoint := range promEndpoint {
		ep := vmv1beta1.Endpoint{
			Port:       endpoint.Port,
			TargetPort: endpoint.TargetPort,
			EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
				Path:            endpoint.Path,
				Scheme:          endpoint.Scheme.String(),
				Params:          endpoint.Params,
				Interval:        string(endpoint.Interval),
				ScrapeTimeout:   string(endpoint.ScrapeTimeout),
				HonorLabels:     endpoint.HonorLabels,
				HonorTimestamps: endpoint.HonorTimestamps,
				ProxyURL:        endpoint.ProxyURL,
				FollowRedirects: endpoint.FollowRedirects,
			},
			EndpointAuth: vmv1beta1.EndpointAuth{
				BasicAuth:     ConvertBasicAuth(endpoint.BasicAuth),
				TLSConfig:     ConvertTLSConfig(endpoint.TLSConfig),
				OAuth2:        ConvertOAuth(endpoint.OAuth2),
				Authorization: ConvertAuthorization(endpoint.Authorization, nil),
				// Unless prometheus deletes BearerTokenFile, we have to support it for backward compatibility
				//nolint:staticcheck
				BearerTokenFile: ReplacePromDirPath(endpoint.BearerTokenFile),
				// Unless prometheus deletes BearerTokenSecret, we have to support it for backward compatibility
				//nolint:staticcheck
				BearerTokenSecret: convertBearerToken(endpoint.BearerTokenSecret),
			},
			EndpointRelabelings: vmv1beta1.EndpointRelabelings{
				MetricRelabelConfigs: ConvertRelabelConfig(endpoint.MetricRelabelConfigs),
				RelabelConfigs:       ConvertRelabelConfig(endpoint.RelabelConfigs),
			},
		}

		endpoints = append(endpoints, ep)
	}
	return endpoints
}

// ConvertBasicAuth converts Prometheus basic auth config to VM one
func ConvertBasicAuth(bAuth *promv1.BasicAuth) *vmv1beta1.BasicAuth {
	if bAuth == nil {
		return nil
	}
	return &vmv1beta1.BasicAuth{
		Username: bAuth.Username,
		Password: bAuth.Password,
	}
}

// ConvertTLSConfig converts Prometheus TLS config to VM one
func ConvertTLSConfig(tlsConf *promv1.TLSConfig) *vmv1beta1.TLSConfig {
	if tlsConf == nil {
		return nil
	}
	tc := &vmv1beta1.TLSConfig{
		CAFile:    ReplacePromDirPath(tlsConf.CAFile),
		CA:        convertSecretOrConfigmap(tlsConf.CA),
		CertFile:  ReplacePromDirPath(tlsConf.CertFile),
		Cert:      convertSecretOrConfigmap(tlsConf.Cert),
		KeyFile:   ReplacePromDirPath(tlsConf.KeyFile),
		KeySecret: tlsConf.KeySecret,
	}

	if tlsConf.InsecureSkipVerify != nil {
		tc.InsecureSkipVerify = *tlsConf.InsecureSkipVerify
	}
	if tlsConf.ServerName != nil {
		tc.ServerName = *tlsConf.ServerName
	}
	return tc
}

// ConvertSafeTLSConfig performs convert ConvertSafeTLSConfig to vm version
func ConvertSafeTLSConfig(tlsConf *promv1.SafeTLSConfig) *vmv1beta1.TLSConfig {
	if tlsConf == nil {
		return nil
	}
	tc := &vmv1beta1.TLSConfig{
		CA:        convertSecretOrConfigmap(tlsConf.CA),
		Cert:      convertSecretOrConfigmap(tlsConf.Cert),
		KeySecret: tlsConf.KeySecret,
	}
	if tlsConf.InsecureSkipVerify != nil {
		tc.InsecureSkipVerify = *tlsConf.InsecureSkipVerify
	}
	if tlsConf.ServerName != nil {
		tc.ServerName = *tlsConf.ServerName
	}

	return tc
}

func convertSecretOrConfigmap(promSCM promv1.SecretOrConfigMap) vmv1beta1.SecretOrConfigMap {
	return vmv1beta1.SecretOrConfigMap{
		Secret:    promSCM.Secret,
		ConfigMap: promSCM.ConfigMap,
	}
}

// ConvertRelabelConfig converts Prometheus relabel config to VM one
func ConvertRelabelConfig(promRelabelConfig []promv1.RelabelConfig) []*vmv1beta1.RelabelConfig {
	if promRelabelConfig == nil {
		return nil
	}
	relabelCfg := []*vmv1beta1.RelabelConfig{}
	sourceLabelsToStringSlice := func(src []promv1.LabelName) []string {
		if len(src) == 0 {
			return nil
		}
		res := make([]string, len(src))
		for i, v := range src {
			res[i] = string(v)
		}
		return res
	}
	for idx, relabel := range promRelabelConfig {
		relabelCfg = append(relabelCfg, &vmv1beta1.RelabelConfig{
			SourceLabels: sourceLabelsToStringSlice(relabel.SourceLabels),
			Separator:    relabel.Separator,
			TargetLabel:  relabel.TargetLabel,
			Modulus:      relabel.Modulus,
			Replacement:  relabel.Replacement,
			Action:       relabel.Action,
		})
		if len(relabel.Regex) > 0 {
			relabelCfg[idx].Regex = vmv1beta1.StringOrArray{relabel.Regex}
		}
	}
	return filterUnsupportedRelabelCfg(relabelCfg)
}

func convertPodEndpoints(promPodEnpoints []promv1.PodMetricsEndpoint) []vmv1beta1.PodMetricsEndpoint {
	if promPodEnpoints == nil {
		return nil
	}
	endPoints := make([]vmv1beta1.PodMetricsEndpoint, 0, len(promPodEnpoints))
	for _, promEndPoint := range promPodEnpoints {
		var safeTLS *promv1.SafeTLSConfig
		if promEndPoint.TLSConfig != nil {
			safeTLS = promEndPoint.TLSConfig
		}
		ep := vmv1beta1.PodMetricsEndpoint{
			Port:       promEndPoint.Port,
			PortNumber: promEndPoint.PortNumber,
			// Unless prometheus deletes TargetPort, we have to support it for backward compatibility
			//nolint:staticcheck
			TargetPort: promEndPoint.TargetPort,
			// Unless prometheus deletes BearerTokenSecret, we have to support it for backward compatibility
			//nolint:staticcheck
			EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
				Interval:        string(promEndPoint.Interval),
				Path:            promEndPoint.Path,
				Scheme:          promEndPoint.Scheme.String(),
				Params:          promEndPoint.Params,
				ScrapeTimeout:   string(promEndPoint.ScrapeTimeout),
				HonorLabels:     promEndPoint.HonorLabels,
				HonorTimestamps: promEndPoint.HonorTimestamps,
				ProxyURL:        promEndPoint.ProxyURL,
				FollowRedirects: promEndPoint.FollowRedirects,
			},
			EndpointRelabelings: vmv1beta1.EndpointRelabelings{
				RelabelConfigs:       ConvertRelabelConfig(promEndPoint.RelabelConfigs),
				MetricRelabelConfigs: ConvertRelabelConfig(promEndPoint.MetricRelabelConfigs),
			},

			EndpointAuth: vmv1beta1.EndpointAuth{
				BasicAuth: ConvertBasicAuth(promEndPoint.BasicAuth),
				//nolint:staticcheck
				BearerTokenSecret: convertBearerToken(promEndPoint.BearerTokenSecret),
				TLSConfig:         ConvertSafeTLSConfig(safeTLS),
				OAuth2:            ConvertOAuth(promEndPoint.OAuth2),
				Authorization:     ConvertAuthorization(promEndPoint.Authorization, nil),
			},
			FilterRunning: promEndPoint.FilterRunning,
		}
		endPoints = append(endPoints, ep)
	}
	return endPoints
}

// PodMonitor create VMPodScrape from PodMonitor
func PodMonitor(podMon *promv1.PodMonitor, conf *config.BaseOperatorConf) *vmv1beta1.VMPodScrape {
	cs := &vmv1beta1.VMPodScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podMon.Name,
			Namespace:   podMon.Namespace,
			Labels:      FilterPrefixes(podMon.Labels, conf.FilterPrometheusConverterLabelPrefixes),
			Annotations: FilterPrefixes(podMon.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
		},
		Spec: vmv1beta1.VMPodScrapeSpec{
			JobLabel:        podMon.Spec.JobLabel,
			PodTargetLabels: podMon.Spec.PodTargetLabels,
			Selector:        podMon.Spec.Selector,
			NamespaceSelector: vmv1beta1.NamespaceSelector{
				Any:        podMon.Spec.NamespaceSelector.Any,
				MatchNames: podMon.Spec.NamespaceSelector.MatchNames,
			},
			PodMetricsEndpoints: convertPodEndpoints(podMon.Spec.PodMetricsEndpoints),
			ScrapeClassName:     podMon.Spec.ScrapeClassName,
		},
	}
	if podMon.Spec.SampleLimit != nil && *podMon.Spec.SampleLimit <= uint64(math.MaxInt) {
		cs.Spec.SampleLimit = int(*podMon.Spec.SampleLimit)
	}
	if podMon.Spec.AttachMetadata != nil {
		cs.Spec.AttachMetadata = vmv1beta1.AttachMetadata{
			Node: podMon.Spec.AttachMetadata.Node,
		}
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cs.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         promv1.SchemeGroupVersion.String(),
				Kind:               promv1.PodMonitorsKind,
				Name:               podMon.Name,
				UID:                podMon.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
	}
	cs.Annotations = MaybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cs.Annotations)
	return cs
}

// Probe creates VMProbe from prometheus probe
func Probe(probe *promv1.Probe, conf *config.BaseOperatorConf) *vmv1beta1.VMProbe {
	var (
		k8sTargets    []*vmv1beta1.VMProbeTargetKubernetes
		staticTargets *vmv1beta1.VMProbeTargetStatic
	)
	if probe.Spec.Targets.Ingress != nil {
		k8sTargets = append(k8sTargets, &vmv1beta1.VMProbeTargetKubernetes{
			Role:     "ingress",
			Selector: probe.Spec.Targets.Ingress.Selector,
			NamespaceSelector: vmv1beta1.NamespaceSelector{
				Any:        probe.Spec.Targets.Ingress.NamespaceSelector.Any,
				MatchNames: probe.Spec.Targets.Ingress.NamespaceSelector.MatchNames,
			},
			RelabelConfigs: ConvertRelabelConfig(probe.Spec.Targets.Ingress.RelabelConfigs),
		})
	}
	if probe.Spec.Targets.StaticConfig != nil {
		staticTargets = &vmv1beta1.VMProbeTargetStatic{
			Targets:        probe.Spec.Targets.StaticConfig.Targets,
			Labels:         probe.Spec.Targets.StaticConfig.Labels,
			RelabelConfigs: ConvertRelabelConfig(probe.Spec.Targets.StaticConfig.RelabelConfigs),
		}
	}
	var safeTLS *promv1.SafeTLSConfig
	if probe.Spec.TLSConfig != nil {
		safeTLS = probe.Spec.TLSConfig
	}
	cp := &vmv1beta1.VMProbe{
		ObjectMeta: metav1.ObjectMeta{
			Name:        probe.Name,
			Namespace:   probe.Namespace,
			Labels:      FilterPrefixes(probe.Labels, conf.FilterPrometheusConverterLabelPrefixes),
			Annotations: FilterPrefixes(probe.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
		},
		Spec: vmv1beta1.VMProbeSpec{
			JobName: probe.Spec.JobName,
			VMProberSpec: vmv1beta1.VMProberSpec{
				URL:    probe.Spec.ProberSpec.URL,
				Scheme: probe.Spec.ProberSpec.Scheme.String(),
				Path:   probe.Spec.ProberSpec.Path,
			},
			Module: probe.Spec.Module,
			Targets: vmv1beta1.VMProbeTargets{
				Kubernetes: k8sTargets,
				Static:     staticTargets,
			},
			EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
				Interval:      string(probe.Spec.Interval),
				ScrapeTimeout: string(probe.Spec.ScrapeTimeout),
			},
			MetricRelabelConfigs: ConvertRelabelConfig(probe.Spec.MetricRelabelConfigs),
			EndpointAuth: vmv1beta1.EndpointAuth{
				BasicAuth:         ConvertBasicAuth(probe.Spec.BasicAuth),
				BearerTokenSecret: convertBearerToken(probe.Spec.BearerTokenSecret), //nolint:staticcheck
				TLSConfig:         ConvertSafeTLSConfig(safeTLS),
				OAuth2:            ConvertOAuth(probe.Spec.OAuth2),
				Authorization:     ConvertAuthorization(probe.Spec.Authorization, nil),
			},
			ScrapeClassName: probe.Spec.ScrapeClassName,
		},
	}
	if probe.Spec.ProberSpec.ProxyURL != nil {
		cp.Spec.ProxyURL = probe.Spec.ProberSpec.ProxyURL
	}
	if probe.Spec.SampleLimit != nil && *probe.Spec.SampleLimit <= uint64(math.MaxInt) {
		cp.Spec.SampleLimit = int(*probe.Spec.SampleLimit)
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cp.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         promv1.SchemeGroupVersion.String(),
				Kind:               promv1.ProbesKind,
				Name:               probe.Name,
				UID:                probe.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
	}
	cp.Annotations = MaybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cp.Annotations)
	return cp
}

func filterUnsupportedRelabelCfg(relabelCfgs []*vmv1beta1.RelabelConfig) []*vmv1beta1.RelabelConfig {
	newRelabelCfg := make([]*vmv1beta1.RelabelConfig, 0, len(relabelCfgs))
	for _, r := range relabelCfgs {
		switch r.Action {
		case "keep", "hashmod", "drop":
			if len(r.SourceLabels) == 0 {
				log.Info("filtering unsupported format of relabelConfig", "action", r.Action, "reason", "source labels are empty")
				continue
			}
		}
		newRelabelCfg = append(newRelabelCfg, r)
	}
	return newRelabelCfg
}

// FilterPrefixes filters given prefixes from src map
func FilterPrefixes(src map[string]string, filterPrefixes []string) map[string]string {
	if len(src) == 0 || len(filterPrefixes) == 0 {
		return src
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		ignoreKey := false
		for _, filterPref := range filterPrefixes {
			if ignoreKey = strings.HasPrefix(k, filterPref); ignoreKey {
				break
			}
		}
		if !ignoreKey {
			dst[k] = v
		}
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}
