package converter

import (
	"encoding/json"
	"fmt"
	"github.com/VictoriaMetrics/operator/internal/config"
	alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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
				For:         string(promRuleItem.For),
				Record:      promRuleItem.Record,
				Alert:       promRuleItem.Alert,
			})
		}

		ruleGroups = append(ruleGroups, v1beta1vm.RuleGroup{
			Name:     promGroup.Name,
			Interval: string(promGroup.Interval),
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
	cr.Annotations = maybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cr.Annotations)
	return cr
}

func maybeAddArgoCDIgnoreAnnotations(mustAdd bool, dst map[string]string) map[string]string {
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

func convertMatchers(promMatchers []alpha1.Matcher) []string {
	if promMatchers == nil {
		return nil
	}
	r := make([]string, 0, len(promMatchers))
	for _, pm := range promMatchers {
		if pm.Regex && pm.MatchType == "" {
			pm.MatchType = "=~"
		}
		if pm.MatchType == "" {
			pm.MatchType = "="
		}
		r = append(r, pm.String())
	}
	return r
}

func convertRoute(promRoute *alpha1.Route) (*v1beta1vm.Route, error) {
	if promRoute == nil {
		return nil, nil
	}
	r := v1beta1vm.Route{
		Receiver:            promRoute.Receiver,
		Continue:            promRoute.Continue,
		GroupBy:             promRoute.GroupBy,
		GroupWait:           promRoute.GroupWait,
		GroupInterval:       promRoute.GroupInterval,
		RepeatInterval:      promRoute.RepeatInterval,
		Matchers:            convertMatchers(promRoute.Matchers),
		MuteTimeIntervals:   promRoute.MuteTimeIntervals,
		ActiveTimeIntervals: promRoute.ActiveTimeIntervals,
	}
	for _, route := range promRoute.Routes {
		var promRoute alpha1.Route
		if err := json.Unmarshal(route.Raw, &promRoute); err != nil {
			return nil, fmt.Errorf("cannot parse raw prom route: %s, err: %w", string(route.Raw), err)
		}
		vmRoute, err := convertRoute(&promRoute)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(vmRoute)
		if err != nil {
			return nil, fmt.Errorf("cannot serialize vm route for alertmanager config: %w", err)
		}
		r.RawRoutes = append(r.RawRoutes, apiextensionsv1.JSON{Raw: data})
	}
	return &r, nil
}

func convertInhibitRules(promIRs []alpha1.InhibitRule) []v1beta1vm.InhibitRule {
	if promIRs == nil {
		return nil
	}
	vmIRs := make([]v1beta1vm.InhibitRule, 0, len(promIRs))
	for _, promIR := range promIRs {
		ir := v1beta1vm.InhibitRule{
			TargetMatchers: convertMatchers(promIR.TargetMatch),
			SourceMatchers: convertMatchers(promIR.SourceMatch),
			Equal:          promIR.Equal,
		}
		vmIRs = append(vmIRs, ir)
	}
	return vmIRs
}

func convertMuteIntervals(promMIs []alpha1.MuteTimeInterval) []v1beta1vm.MuteTimeInterval {
	if promMIs == nil {
		return nil
	}

	vmMIs := make([]v1beta1vm.MuteTimeInterval, 0, len(promMIs))
	for _, promMI := range promMIs {
		vmMI := v1beta1vm.MuteTimeInterval{
			Name:          promMI.Name,
			TimeIntervals: make([]v1beta1vm.TimeInterval, 0, len(promMI.TimeIntervals)),
		}
		for _, tis := range promMI.TimeIntervals {
			var vmTIs v1beta1vm.TimeInterval
			for _, t := range tis.Times {
				vmTIs.Times = append(vmTIs.Times, v1beta1vm.TimeRange{EndTime: string(t.EndTime), StartTime: string(t.StartTime)})
			}
			for _, dm := range tis.DaysOfMonth {
				vmTIs.DaysOfMonth = append(vmTIs.DaysOfMonth, fmt.Sprintf("%d:%d", dm.Start, dm.End))
			}
			for _, wm := range tis.Weekdays {
				vmTIs.Weekdays = append(vmTIs.Weekdays, string(wm))
			}
			for _, y := range tis.Years {
				vmTIs.Years = append(vmTIs.Years, string(y))
			}
			for _, m := range tis.Months {
				vmTIs.Months = append(vmTIs.Months, string(m))
			}
			vmMI.TimeIntervals = append(vmMI.TimeIntervals, vmTIs)
		}
		vmMIs = append(vmMIs, vmMI)
	}
	return vmMIs
}

func convertReceivers(promReceivers []alpha1.Receiver) ([]v1beta1vm.Receiver, error) {
	// yaml instead of json is used by purpose
	// prometheus-operator objects has different field tags
	marshaledRcvs, err := yaml.Marshal(promReceivers)
	if err != nil {
		return nil, fmt.Errorf("possible bug, cannot serialize prometheus receivers, err: %w", err)
	}
	var vmReceivers []v1beta1vm.Receiver
	if err := yaml.Unmarshal(marshaledRcvs, &vmReceivers); err != nil {
		return nil, fmt.Errorf("cannot parse serialized prometheus receievers: %s, err: %w", string(marshaledRcvs), err)
	}
	return vmReceivers, nil
}

func ConvertAlertmanagerConfig(promAMCfg *alpha1.AlertmanagerConfig, conf *config.BaseOperatorConf) (*v1beta1vm.VMAlertmanagerConfig, error) {
	vamc := &v1beta1vm.VMAlertmanagerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:        promAMCfg.Name,
			Namespace:   promAMCfg.Namespace,
			Annotations: filterPrefixes(promAMCfg.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
			Labels:      filterPrefixes(promAMCfg.Labels, conf.FilterPrometheusConverterLabelPrefixes),
		},
		Spec: v1beta1vm.VMAlertmanagerConfigSpec{
			InhibitRules:     convertInhibitRules(promAMCfg.Spec.InhibitRules),
			MutTimeIntervals: convertMuteIntervals(promAMCfg.Spec.MuteTimeIntervals),
		},
	}
	convertedRoute, err := convertRoute(promAMCfg.Spec.Route)
	if err != nil {
		return nil, fmt.Errorf("cannot convert prometheus alertmanager config: %s into vm, err: %w", promAMCfg.Name, err)
	}
	vamc.Spec.Route = convertedRoute
	convertedReceivers, err := convertReceivers(promAMCfg.Spec.Receivers)
	if err != nil {
		return nil, fmt.Errorf("cannot convert prometheus alertmanager config: %s into vm, err: %w", promAMCfg.Name, err)
	}
	vamc.Spec.Receivers = convertedReceivers
	if conf.EnabledPrometheusConverterOwnerReferences {
		vamc.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         alpha1.SchemeGroupVersion.String(),
				Kind:               alpha1.AlertmanagerConfigKind,
				Name:               promAMCfg.Name,
				UID:                promAMCfg.UID,
				Controller:         pointer.BoolPtr(true),
				BlockOwnerDeletion: pointer.BoolPtr(true),
			},
		}
	}
	vamc.Annotations = maybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, vamc.Annotations)
	return vamc, nil
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
	cs.Annotations = maybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cs.Annotations)
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
			FilterRunning:        promEndPoint.FilterRunning,
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
	cs.Annotations = maybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cs.Annotations)
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
			Targets:        probe.Spec.Targets.StaticConfig.Targets,
			Labels:         probe.Spec.Targets.StaticConfig.Labels,
			RelabelConfigs: ConvertRelabelConfig(probe.Spec.Targets.StaticConfig.RelabelConfigs),
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
	cp.Annotations = maybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cp.Annotations)
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
