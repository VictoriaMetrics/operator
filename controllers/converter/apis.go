package converter

import (
	"github.com/VictoriaMetrics/operator/internal/config"
	corev1 "k8s.io/api/core/v1"
	"strings"

	v1beta1vm "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory"
	v1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	prometheusSecretDir    = "/etc/prometheus/secrets"
	prometheusConfigmapDir = "/etc/prometheus/configmaps"
)

var log = ctrl.Log.WithValues("controller", "prometheus.converter")

func ConvertPromRule(prom *v1.PrometheusRule, conf *config.BaseOperatorConf) *v1beta1vm.VMRule {
	ruleGroups := make([]v1beta1vm.RuleGroup, 0, len(prom.Spec.Groups))
	for _, promGroup := range prom.Spec.Groups {
		ruleItems := make([]v1beta1vm.Rule, 0, len(promGroup.Rules))
		for _, promRuleItem := range promGroup.Rules {
			ruleItems = append(ruleItems, v1beta1vm.Rule{
				Labels:      promRuleItem.Labels,
				Annotations: promRuleItem.Annotations,
				Expr:        promRuleItem.Expr.String(),
				For:         promRuleItem.For,
				Record:      promRuleItem.Record,
				Alert:       promRuleItem.Alert,
			})
		}

		ruleGroups = append(ruleGroups, v1beta1vm.RuleGroup{
			Name:     promGroup.Name,
			Interval: promGroup.Interval,
			Rules:    ruleItems,
		})
	}
	cr := &v1beta1vm.VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   prom.Namespace,
			Name:        prom.Name,
			Labels:      filterPrefixes(prom.Labels, conf.FilterPrometheusConverterLabelPrefixes),
			Annotations: filterPrefixes(prom.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
		},
		Spec: v1beta1vm.VMRuleSpec{
			Groups: ruleGroups,
		},
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cr.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         v1.SchemeGroupVersion.String(),
				Kind:               v1.PrometheusRuleKind,
				Name:               prom.Name,
				UID:                prom.UID,
				Controller:         pointer.BoolPtr(true),
				BlockOwnerDeletion: pointer.BoolPtr(true),
			},
		}
	}
	return cr
}

func ConvertServiceMonitor(serviceMon *v1.ServiceMonitor, conf *config.BaseOperatorConf) *v1beta1vm.VMServiceScrape {
	cs := &v1beta1vm.VMServiceScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceMon.Name,
			Namespace:   serviceMon.Namespace,
			Annotations: filterPrefixes(serviceMon.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
			Labels:      filterPrefixes(serviceMon.Labels, conf.FilterPrometheusConverterLabelPrefixes),
		},
		Spec: v1beta1vm.VMServiceScrapeSpec{
			JobLabel:        serviceMon.Spec.JobLabel,
			TargetLabels:    serviceMon.Spec.TargetLabels,
			PodTargetLabels: serviceMon.Spec.PodTargetLabels,
			SampleLimit:     serviceMon.Spec.SampleLimit,
			Selector:        serviceMon.Spec.Selector,
			Endpoints:       ConvertEndpoint(serviceMon.Spec.Endpoints),
			NamespaceSelector: v1beta1vm.NamespaceSelector{
				Any:        serviceMon.Spec.NamespaceSelector.Any,
				MatchNames: serviceMon.Spec.NamespaceSelector.MatchNames,
			},
		},
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cs.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         v1.SchemeGroupVersion.String(),
				Kind:               v1.ServiceMonitorsKind,
				Name:               serviceMon.Name,
				UID:                serviceMon.UID,
				Controller:         pointer.BoolPtr(true),
				BlockOwnerDeletion: pointer.BoolPtr(true),
			},
		}
	}
	return cs
}

func replacePromDirPath(origin string) string {
	if strings.HasPrefix(origin, prometheusSecretDir) {
		return strings.Replace(origin, prometheusSecretDir, factory.SecretsDir, 1)
	}
	if strings.HasPrefix(origin, prometheusConfigmapDir) {
		return strings.Replace(origin, prometheusConfigmapDir, factory.ConfigMapsDir, 1)
	}
	return origin
}

func convertOAuth(src *v1.OAuth2) *v1beta1vm.OAuth2 {
	if src == nil {
		return nil
	}

	o := v1beta1vm.OAuth2{
		ClientID:       ConvertSecretOrConfigmap(src.ClientID),
		ClientSecret:   &src.ClientSecret,
		Scopes:         src.Scopes,
		TokenURL:       src.TokenURL,
		EndpointParams: src.EndpointParams,
	}
	return &o
}

func convertAuthorization(srcSafe *v1.SafeAuthorization, src *v1.Authorization) *v1beta1vm.Authorization {
	if srcSafe == nil && src == nil {
		return nil
	}
	if srcSafe != nil {
		return &v1beta1vm.Authorization{
			Type:        srcSafe.Type,
			Credentials: srcSafe.Credentials,
		}
	}
	return &v1beta1vm.Authorization{
		Type:            src.Type,
		Credentials:     src.Credentials,
		CredentialsFile: src.CredentialsFile,
	}
}

func convertBearerToken(src corev1.SecretKeySelector) *corev1.SecretKeySelector {
	if src.Key == "" && src.Name == "" {
		return nil
	}
	return &src
}

func ConvertEndpoint(promEndpoint []v1.Endpoint) []v1beta1vm.Endpoint {
	endpoints := make([]v1beta1vm.Endpoint, 0, len(promEndpoint))
	for _, endpoint := range promEndpoint {
		ep := v1beta1vm.Endpoint{
			Port:                 endpoint.Port,
			TargetPort:           endpoint.TargetPort,
			Path:                 endpoint.Path,
			Scheme:               endpoint.Scheme,
			Params:               endpoint.Params,
			Interval:             string(endpoint.Interval),
			ScrapeTimeout:        string(endpoint.ScrapeTimeout),
			BearerTokenFile:      replacePromDirPath(endpoint.BearerTokenFile),
			HonorLabels:          endpoint.HonorLabels,
			HonorTimestamps:      endpoint.HonorTimestamps,
			BasicAuth:            ConvertBasicAuth(endpoint.BasicAuth),
			TLSConfig:            ConvertTlsConfig(endpoint.TLSConfig),
			MetricRelabelConfigs: ConvertRelabelConfig(endpoint.MetricRelabelConfigs),
			RelabelConfigs:       ConvertRelabelConfig(endpoint.RelabelConfigs),
			ProxyURL:             endpoint.ProxyURL,
			BearerTokenSecret:    convertBearerToken(endpoint.BearerTokenSecret),
			OAuth2:               convertOAuth(endpoint.OAuth2),
			FollowRedirects:      endpoint.FollowRedirects,
			Authorization:        convertAuthorization(endpoint.Authorization, nil),
		}

		endpoints = append(endpoints, ep)
	}
	return endpoints

}

func ConvertBasicAuth(bAuth *v1.BasicAuth) *v1beta1vm.BasicAuth {
	if bAuth == nil {
		return nil
	}
	return &v1beta1vm.BasicAuth{
		Username: bAuth.Username,
		Password: bAuth.Password,
	}
}

func ConvertTlsConfig(tlsConf *v1.TLSConfig) *v1beta1vm.TLSConfig {
	if tlsConf == nil {
		return nil
	}
	return &v1beta1vm.TLSConfig{
		CAFile:             replacePromDirPath(tlsConf.CAFile),
		CA:                 ConvertSecretOrConfigmap(tlsConf.CA),
		CertFile:           replacePromDirPath(tlsConf.CertFile),
		Cert:               ConvertSecretOrConfigmap(tlsConf.Cert),
		KeyFile:            replacePromDirPath(tlsConf.KeyFile),
		KeySecret:          tlsConf.KeySecret,
		InsecureSkipVerify: tlsConf.InsecureSkipVerify,
		ServerName:         tlsConf.ServerName,
	}
}

func ConvertSafeTlsConfig(tlsConf *v1.SafeTLSConfig) *v1beta1vm.TLSConfig {
	if tlsConf == nil {
		return nil
	}
	return &v1beta1vm.TLSConfig{
		CA:                 ConvertSecretOrConfigmap(tlsConf.CA),
		Cert:               ConvertSecretOrConfigmap(tlsConf.Cert),
		KeySecret:          tlsConf.KeySecret,
		InsecureSkipVerify: tlsConf.InsecureSkipVerify,
		ServerName:         tlsConf.ServerName,
	}
}

func ConvertSecretOrConfigmap(promSCM v1.SecretOrConfigMap) v1beta1vm.SecretOrConfigMap {
	return v1beta1vm.SecretOrConfigMap{
		Secret:    promSCM.Secret,
		ConfigMap: promSCM.ConfigMap,
	}
}

func ConvertRelabelConfig(promRelabelConfig []*v1.RelabelConfig) []*v1beta1vm.RelabelConfig {
	if promRelabelConfig == nil {
		return nil
	}
	relabelCfg := []*v1beta1vm.RelabelConfig{}
	sourceLabelsToStringSlice := func(src []v1.LabelName) []string {
		res := make([]string, len(src))
		for i, v := range src {
			res[i] = string(v)
		}
		return res
	}
	for _, relabel := range promRelabelConfig {
		relabelCfg = append(relabelCfg, &v1beta1vm.RelabelConfig{
			SourceLabels: sourceLabelsToStringSlice(relabel.SourceLabels),
			Separator:    relabel.Separator,
			TargetLabel:  relabel.TargetLabel,
			Regex:        relabel.Regex,
			Modulus:      relabel.Modulus,
			Replacement:  relabel.Replacement,
			Action:       relabel.Action,
		})
	}
	return filterUnsupportedRelabelCfg(relabelCfg)

}

func ConvertPodEndpoints(promPodEnpoints []v1.PodMetricsEndpoint) []v1beta1vm.PodMetricsEndpoint {
	if promPodEnpoints == nil {
		return nil
	}
	endPoints := make([]v1beta1vm.PodMetricsEndpoint, 0, len(promPodEnpoints))
	for _, promEndPoint := range promPodEnpoints {
		if promEndPoint.Authorization != nil {

		}
		var safeTls *v1.SafeTLSConfig
		if promEndPoint.TLSConfig != nil {
			safeTls = &promEndPoint.TLSConfig.SafeTLSConfig
		}
		ep := v1beta1vm.PodMetricsEndpoint{
			TargetPort:           promEndPoint.TargetPort,
			Port:                 promEndPoint.Port,
			Interval:             string(promEndPoint.Interval),
			Path:                 promEndPoint.Path,
			Scheme:               promEndPoint.Scheme,
			Params:               promEndPoint.Params,
			ScrapeTimeout:        string(promEndPoint.ScrapeTimeout),
			HonorLabels:          promEndPoint.HonorLabels,
			HonorTimestamps:      promEndPoint.HonorTimestamps,
			ProxyURL:             promEndPoint.ProxyURL,
			RelabelConfigs:       ConvertRelabelConfig(promEndPoint.RelabelConfigs),
			MetricRelabelConfigs: ConvertRelabelConfig(promEndPoint.MetricRelabelConfigs),
			BasicAuth:            ConvertBasicAuth(promEndPoint.BasicAuth),
			TLSConfig:            ConvertSafeTlsConfig(safeTls),
			OAuth2:               convertOAuth(promEndPoint.OAuth2),
			FollowRedirects:      promEndPoint.FollowRedirects,
			BearerTokenSecret:    convertBearerToken(promEndPoint.BearerTokenSecret),
			Authorization:        convertAuthorization(promEndPoint.Authorization, nil),
		}
		endPoints = append(endPoints, ep)
	}
	return endPoints
}

func ConvertPodMonitor(podMon *v1.PodMonitor, conf *config.BaseOperatorConf) *v1beta1vm.VMPodScrape {
	cs := &v1beta1vm.VMPodScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podMon.Name,
			Namespace:   podMon.Namespace,
			Labels:      filterPrefixes(podMon.Labels, conf.FilterPrometheusConverterLabelPrefixes),
			Annotations: filterPrefixes(podMon.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
		},
		Spec: v1beta1vm.VMPodScrapeSpec{
			JobLabel:        podMon.Spec.JobLabel,
			PodTargetLabels: podMon.Spec.PodTargetLabels,
			Selector:        podMon.Spec.Selector,
			NamespaceSelector: v1beta1vm.NamespaceSelector{
				Any:        podMon.Spec.NamespaceSelector.Any,
				MatchNames: podMon.Spec.NamespaceSelector.MatchNames,
			},
			SampleLimit:         podMon.Spec.SampleLimit,
			PodMetricsEndpoints: ConvertPodEndpoints(podMon.Spec.PodMetricsEndpoints),
		},
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cs.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         v1.SchemeGroupVersion.String(),
				Kind:               v1.PodMonitorsKind,
				Name:               podMon.Name,
				UID:                podMon.UID,
				Controller:         pointer.BoolPtr(true),
				BlockOwnerDeletion: pointer.BoolPtr(true),
			},
		}
	}
	return cs
}

func ConvertProbe(probe *v1.Probe, conf *config.BaseOperatorConf) *v1beta1vm.VMProbe {
	var (
		ingressTarget *v1beta1vm.ProbeTargetIngress
		staticTargets *v1beta1vm.VMProbeTargetStaticConfig
	)
	if probe.Spec.Targets.Ingress != nil {
		ingressTarget = &v1beta1vm.ProbeTargetIngress{
			Selector: probe.Spec.Targets.Ingress.Selector,
			NamespaceSelector: v1beta1vm.NamespaceSelector{
				Any:        probe.Spec.Targets.Ingress.NamespaceSelector.Any,
				MatchNames: probe.Spec.Targets.Ingress.NamespaceSelector.MatchNames,
			},
			RelabelConfigs: ConvertRelabelConfig(probe.Spec.Targets.Ingress.RelabelConfigs),
		}
	}
	if probe.Spec.Targets.StaticConfig != nil {
		staticTargets = &v1beta1vm.VMProbeTargetStaticConfig{
			Targets: probe.Spec.Targets.StaticConfig.Targets,
			Labels:  probe.Spec.Targets.StaticConfig.Labels,
		}
	}
	var safeTls *v1.SafeTLSConfig
	if probe.Spec.TLSConfig != nil {
		safeTls = &probe.Spec.TLSConfig.SafeTLSConfig
	}
	cp := &v1beta1vm.VMProbe{
		ObjectMeta: metav1.ObjectMeta{
			Name:        probe.Name,
			Namespace:   probe.Namespace,
			Labels:      filterPrefixes(probe.Labels, conf.FilterPrometheusConverterLabelPrefixes),
			Annotations: filterPrefixes(probe.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
		},
		Spec: v1beta1vm.VMProbeSpec{
			JobName: probe.Spec.JobName,
			VMProberSpec: v1beta1vm.VMProberSpec{
				URL:    probe.Spec.ProberSpec.URL,
				Scheme: probe.Spec.ProberSpec.Scheme,
				Path:   probe.Spec.ProberSpec.Path,
			},
			Module: probe.Spec.Module,
			Targets: v1beta1vm.VMProbeTargets{
				Ingress:      ingressTarget,
				StaticConfig: staticTargets,
			},
			Interval:          string(probe.Spec.Interval),
			ScrapeTimeout:     string(probe.Spec.ScrapeTimeout),
			BasicAuth:         ConvertBasicAuth(probe.Spec.BasicAuth),
			TLSConfig:         ConvertSafeTlsConfig(safeTls),
			BearerTokenSecret: convertBearerToken(probe.Spec.BearerTokenSecret),
			OAuth2:            convertOAuth(probe.Spec.OAuth2),
			SampleLimit:       probe.Spec.SampleLimit,
			Authorization:     convertAuthorization(probe.Spec.Authorization, nil),
		},
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cp.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         v1.SchemeGroupVersion.String(),
				Kind:               v1.ProbesKind,
				Name:               probe.Name,
				UID:                probe.UID,
				Controller:         pointer.BoolPtr(true),
				BlockOwnerDeletion: pointer.BoolPtr(true),
			},
		}
	}
	return cp
}

func filterUnsupportedRelabelCfg(relabelCfgs []*v1beta1vm.RelabelConfig) []*v1beta1vm.RelabelConfig {
	newRelabelCfg := make([]*v1beta1vm.RelabelConfig, 0, len(relabelCfgs))
	for _, r := range relabelCfgs {
		switch r.Action {
		case "keep", "hashmod", "drop":
			if len(r.SourceLabels) == 0 {
				log.Info("filtering unsupported relabelConfig", "action", r.Action, "reason", "source labels are empty")
				continue
			}
		}
		newRelabelCfg = append(newRelabelCfg, r)
	}
	return newRelabelCfg
}

// filterPrefixes filters given prefixes from src map
func filterPrefixes(src map[string]string, filterPrefixes []string) map[string]string {
	if len(src) == 0 || len(filterPrefixes) == 0 {
		return src
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		for _, filterPref := range filterPrefixes {
			if strings.HasPrefix(k, filterPref) {
				continue
			}
			dst[k] = v
		}
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}
