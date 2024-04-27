package converter

import (
	"encoding/json"
	"fmt"
	"strings"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	v1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	prometheusSecretDir    = "/etc/prometheus/secrets"
	prometheusConfigmapDir = "/etc/prometheus/configmaps"
)

var log = ctrl.Log.WithValues("controller", "prometheus.converter")

// ConvertPromRule creates VMRule from PrometheusRule
func ConvertPromRule(prom *v1.PrometheusRule, conf *config.BaseOperatorConf) *victoriametricsv1beta1.VMRule {
	ruleGroups := make([]victoriametricsv1beta1.RuleGroup, 0, len(prom.Spec.Groups))
	for _, promGroup := range prom.Spec.Groups {
		ruleItems := make([]victoriametricsv1beta1.Rule, 0, len(promGroup.Rules))
		for _, promRuleItem := range promGroup.Rules {
			trule := victoriametricsv1beta1.Rule{
				Labels:      promRuleItem.Labels,
				Annotations: promRuleItem.Annotations,
				Expr:        promRuleItem.Expr.String(),
				Record:      promRuleItem.Record,
				Alert:       promRuleItem.Alert,
			}
			if promRuleItem.For != nil {
				trule.For = string(*promRuleItem.For)
			}
			ruleItems = append(ruleItems, trule)
		}

		tgroup := victoriametricsv1beta1.RuleGroup{
			Name:  promGroup.Name,
			Rules: ruleItems,
		}
		if promGroup.Interval != nil {
			tgroup.Interval = string(*promGroup.Interval)
		}
		ruleGroups = append(ruleGroups, tgroup)
	}
	cr := &victoriametricsv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   prom.Namespace,
			Name:        prom.Name,
			Labels:      filterPrefixes(prom.Labels, conf.FilterPrometheusConverterLabelPrefixes),
			Annotations: filterPrefixes(prom.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
		},
		Spec: victoriametricsv1beta1.VMRuleSpec{
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
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
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

func convertMatchers(promMatchers []monitoringv1alpha1.Matcher) []string {
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

func convertRoute(promRoute *monitoringv1alpha1.Route) (*victoriametricsv1beta1.Route, error) {
	if promRoute == nil {
		return nil, nil
	}
	r := victoriametricsv1beta1.Route{
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
		var promRoute monitoringv1alpha1.Route
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

func convertInhibitRules(promIRs []monitoringv1alpha1.InhibitRule) []victoriametricsv1beta1.InhibitRule {
	if promIRs == nil {
		return nil
	}
	vmIRs := make([]victoriametricsv1beta1.InhibitRule, 0, len(promIRs))
	for _, promIR := range promIRs {
		ir := victoriametricsv1beta1.InhibitRule{
			TargetMatchers: convertMatchers(promIR.TargetMatch),
			SourceMatchers: convertMatchers(promIR.SourceMatch),
			Equal:          promIR.Equal,
		}
		vmIRs = append(vmIRs, ir)
	}
	return vmIRs
}

func convertMuteIntervals(promMIs []monitoringv1alpha1.MuteTimeInterval) []victoriametricsv1beta1.MuteTimeInterval {
	if promMIs == nil {
		return nil
	}

	vmMIs := make([]victoriametricsv1beta1.MuteTimeInterval, 0, len(promMIs))
	for _, promMI := range promMIs {
		vmMI := victoriametricsv1beta1.MuteTimeInterval{
			Name:          promMI.Name,
			TimeIntervals: make([]victoriametricsv1beta1.TimeInterval, 0, len(promMI.TimeIntervals)),
		}
		for _, tis := range promMI.TimeIntervals {
			var vmTIs victoriametricsv1beta1.TimeInterval
			for _, t := range tis.Times {
				vmTIs.Times = append(vmTIs.Times, victoriametricsv1beta1.TimeRange{EndTime: string(t.EndTime), StartTime: string(t.StartTime)})
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

func convertReceivers(promReceivers []monitoringv1alpha1.Receiver) ([]victoriametricsv1beta1.Receiver, error) {
	// yaml instead of json is used by purpose
	// prometheus-operator objects has different field tags
	marshaledRcvs, err := yaml.Marshal(promReceivers)
	if err != nil {
		return nil, fmt.Errorf("possible bug, cannot serialize prometheus receivers, err: %w", err)
	}
	var vmReceivers []victoriametricsv1beta1.Receiver
	if err := yaml.Unmarshal(marshaledRcvs, &vmReceivers); err != nil {
		return nil, fmt.Errorf("cannot parse serialized prometheus receievers: %s, err: %w", string(marshaledRcvs), err)
	}
	return vmReceivers, nil
}

// ConvertAlertmanagerConfig creates VMAlertmanagerConfig from prometheus alertmanagerConfig
func ConvertAlertmanagerConfig(promAMCfg *monitoringv1alpha1.AlertmanagerConfig, conf *config.BaseOperatorConf) (*victoriametricsv1beta1.VMAlertmanagerConfig, error) {
	vamc := &victoriametricsv1beta1.VMAlertmanagerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:        promAMCfg.Name,
			Namespace:   promAMCfg.Namespace,
			Annotations: filterPrefixes(promAMCfg.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
			Labels:      filterPrefixes(promAMCfg.Labels, conf.FilterPrometheusConverterLabelPrefixes),
		},
		Spec: victoriametricsv1beta1.VMAlertmanagerConfigSpec{
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
				APIVersion:         monitoringv1alpha1.SchemeGroupVersion.String(),
				Kind:               monitoringv1alpha1.AlertmanagerConfigKind,
				Name:               promAMCfg.Name,
				UID:                promAMCfg.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
	}
	vamc.Annotations = maybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, vamc.Annotations)
	return vamc, nil
}

// ConvertServiceMonitor create VMServiceScrape from ServiceMonitor
func ConvertServiceMonitor(serviceMon *v1.ServiceMonitor, conf *config.BaseOperatorConf) *victoriametricsv1beta1.VMServiceScrape {
	cs := &victoriametricsv1beta1.VMServiceScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceMon.Name,
			Namespace:   serviceMon.Namespace,
			Annotations: filterPrefixes(serviceMon.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
			Labels:      filterPrefixes(serviceMon.Labels, conf.FilterPrometheusConverterLabelPrefixes),
		},
		Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
			JobLabel:        serviceMon.Spec.JobLabel,
			TargetLabels:    serviceMon.Spec.TargetLabels,
			PodTargetLabels: serviceMon.Spec.PodTargetLabels,
			Selector:        serviceMon.Spec.Selector,
			Endpoints:       convertEndpoint(serviceMon.Spec.Endpoints),
			NamespaceSelector: victoriametricsv1beta1.NamespaceSelector{
				Any:        serviceMon.Spec.NamespaceSelector.Any,
				MatchNames: serviceMon.Spec.NamespaceSelector.MatchNames,
			},
		},
	}
	if serviceMon.Spec.SampleLimit != nil {
		cs.Spec.SampleLimit = *serviceMon.Spec.SampleLimit
	}
	if serviceMon.Spec.AttachMetadata != nil {
		cs.Spec.AttachMetadata = victoriametricsv1beta1.AttachMetadata{
			Node: serviceMon.Spec.AttachMetadata.Node,
		}
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cs.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         v1.SchemeGroupVersion.String(),
				Kind:               v1.ServiceMonitorsKind,
				Name:               serviceMon.Name,
				UID:                serviceMon.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
	}
	cs.Annotations = maybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cs.Annotations)
	return cs
}

func replacePromDirPath(origin string) string {
	if strings.HasPrefix(origin, prometheusSecretDir) {
		return strings.Replace(origin, prometheusSecretDir, victoriametricsv1beta1.SecretsDir, 1)
	}
	if strings.HasPrefix(origin, prometheusConfigmapDir) {
		return strings.Replace(origin, prometheusConfigmapDir, victoriametricsv1beta1.ConfigMapsDir, 1)
	}
	return origin
}

func convertOAuth(src *v1.OAuth2) *victoriametricsv1beta1.OAuth2 {
	if src == nil {
		return nil
	}

	o := victoriametricsv1beta1.OAuth2{
		ClientID:       convertSecretOrConfigmap(src.ClientID),
		ClientSecret:   &src.ClientSecret,
		Scopes:         src.Scopes,
		TokenURL:       src.TokenURL,
		EndpointParams: src.EndpointParams,
	}
	return &o
}

func convertAuthorization(srcSafe *v1.SafeAuthorization, src *v1.Authorization) *victoriametricsv1beta1.Authorization {
	if srcSafe == nil && src == nil {
		return nil
	}
	if srcSafe != nil {
		return &victoriametricsv1beta1.Authorization{
			Type:        srcSafe.Type,
			Credentials: srcSafe.Credentials,
		}
	}
	return &victoriametricsv1beta1.Authorization{
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

func convertEndpoint(promEndpoint []v1.Endpoint) []victoriametricsv1beta1.Endpoint {
	endpoints := make([]victoriametricsv1beta1.Endpoint, 0, len(promEndpoint))
	for _, endpoint := range promEndpoint {
		ep := victoriametricsv1beta1.Endpoint{
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
			BasicAuth:            convertBasicAuth(endpoint.BasicAuth),
			TLSConfig:            convertTLSConfig(endpoint.TLSConfig),
			MetricRelabelConfigs: convertRelabelConfig(endpoint.MetricRelabelConfigs),
			RelabelConfigs:       convertRelabelConfig(endpoint.RelabelConfigs),
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

func convertBasicAuth(bAuth *v1.BasicAuth) *victoriametricsv1beta1.BasicAuth {
	if bAuth == nil {
		return nil
	}
	return &victoriametricsv1beta1.BasicAuth{
		Username: bAuth.Username,
		Password: bAuth.Password,
	}
}

func convertTLSConfig(tlsConf *v1.TLSConfig) *victoriametricsv1beta1.TLSConfig {
	if tlsConf == nil {
		return nil
	}
	return &victoriametricsv1beta1.TLSConfig{
		CAFile:             replacePromDirPath(tlsConf.CAFile),
		CA:                 convertSecretOrConfigmap(tlsConf.CA),
		CertFile:           replacePromDirPath(tlsConf.CertFile),
		Cert:               convertSecretOrConfigmap(tlsConf.Cert),
		KeyFile:            replacePromDirPath(tlsConf.KeyFile),
		KeySecret:          tlsConf.KeySecret,
		InsecureSkipVerify: tlsConf.InsecureSkipVerify,
		ServerName:         tlsConf.ServerName,
	}
}

func convertSafeTLSConfig(tlsConf *v1.SafeTLSConfig) *victoriametricsv1beta1.TLSConfig {
	if tlsConf == nil {
		return nil
	}
	return &victoriametricsv1beta1.TLSConfig{
		CA:                 convertSecretOrConfigmap(tlsConf.CA),
		Cert:               convertSecretOrConfigmap(tlsConf.Cert),
		KeySecret:          tlsConf.KeySecret,
		InsecureSkipVerify: tlsConf.InsecureSkipVerify,
		ServerName:         tlsConf.ServerName,
	}
}

func convertSecretOrConfigmap(promSCM v1.SecretOrConfigMap) victoriametricsv1beta1.SecretOrConfigMap {
	return victoriametricsv1beta1.SecretOrConfigMap{
		Secret:    promSCM.Secret,
		ConfigMap: promSCM.ConfigMap,
	}
}

func convertRelabelConfig(promRelabelConfig []*v1.RelabelConfig) []*victoriametricsv1beta1.RelabelConfig {
	if promRelabelConfig == nil {
		return nil
	}
	relabelCfg := []*victoriametricsv1beta1.RelabelConfig{}
	sourceLabelsToStringSlice := func(src []v1.LabelName) []string {
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
		relabelCfg = append(relabelCfg, &victoriametricsv1beta1.RelabelConfig{
			SourceLabels: sourceLabelsToStringSlice(relabel.SourceLabels),
			Separator:    relabel.Separator,
			TargetLabel:  relabel.TargetLabel,
			Modulus:      relabel.Modulus,
			Replacement:  relabel.Replacement,
			Action:       relabel.Action,
		})
		if len(relabel.Regex) > 0 {
			relabelCfg[idx].Regex = victoriametricsv1beta1.StringOrArray{relabel.Regex}
		}
	}
	return filterUnsupportedRelabelCfg(relabelCfg)
}

func convertPodEndpoints(promPodEnpoints []v1.PodMetricsEndpoint) []victoriametricsv1beta1.PodMetricsEndpoint {
	if promPodEnpoints == nil {
		return nil
	}
	endPoints := make([]victoriametricsv1beta1.PodMetricsEndpoint, 0, len(promPodEnpoints))
	for _, promEndPoint := range promPodEnpoints {
		var safeTLS *v1.SafeTLSConfig
		if promEndPoint.TLSConfig != nil {
			safeTLS = &promEndPoint.TLSConfig.SafeTLSConfig
		}
		ep := victoriametricsv1beta1.PodMetricsEndpoint{
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
			RelabelConfigs:       convertRelabelConfig(promEndPoint.RelabelConfigs),
			BearerTokenSecret:    convertBearerToken(&promEndPoint.BearerTokenSecret),
			MetricRelabelConfigs: convertRelabelConfig(promEndPoint.MetricRelabelConfigs),
			BasicAuth:            convertBasicAuth(promEndPoint.BasicAuth),
			TLSConfig:            convertSafeTLSConfig(safeTLS),
			OAuth2:               convertOAuth(promEndPoint.OAuth2),
			FollowRedirects:      promEndPoint.FollowRedirects,
			Authorization:        convertAuthorization(promEndPoint.Authorization, nil),
			FilterRunning:        promEndPoint.FilterRunning,
		}
		endPoints = append(endPoints, ep)
	}
	return endPoints
}

// ConvertPodMonitor create VMPodScrape from PodMonitor
func ConvertPodMonitor(podMon *v1.PodMonitor, conf *config.BaseOperatorConf) *victoriametricsv1beta1.VMPodScrape {
	cs := &victoriametricsv1beta1.VMPodScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podMon.Name,
			Namespace:   podMon.Namespace,
			Labels:      filterPrefixes(podMon.Labels, conf.FilterPrometheusConverterLabelPrefixes),
			Annotations: filterPrefixes(podMon.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
		},
		Spec: victoriametricsv1beta1.VMPodScrapeSpec{
			JobLabel:        podMon.Spec.JobLabel,
			PodTargetLabels: podMon.Spec.PodTargetLabels,
			Selector:        podMon.Spec.Selector,
			NamespaceSelector: victoriametricsv1beta1.NamespaceSelector{
				Any:        podMon.Spec.NamespaceSelector.Any,
				MatchNames: podMon.Spec.NamespaceSelector.MatchNames,
			},
			PodMetricsEndpoints: convertPodEndpoints(podMon.Spec.PodMetricsEndpoints),
		},
	}
	if podMon.Spec.SampleLimit != nil {
		cs.Spec.SampleLimit = *podMon.Spec.SampleLimit
	}
	if podMon.Spec.AttachMetadata != nil {
		cs.Spec.AttachMetadata = victoriametricsv1beta1.AttachMetadata{
			Node: podMon.Spec.AttachMetadata.Node,
		}
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cs.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         v1.SchemeGroupVersion.String(),
				Kind:               v1.PodMonitorsKind,
				Name:               podMon.Name,
				UID:                podMon.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
	}
	cs.Annotations = maybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cs.Annotations)
	return cs
}

// ConvertProbe creates VMProbe from prometheus probe
func ConvertProbe(probe *v1.Probe, conf *config.BaseOperatorConf) *victoriametricsv1beta1.VMProbe {
	var (
		ingressTarget *victoriametricsv1beta1.ProbeTargetIngress
		staticTargets *victoriametricsv1beta1.VMProbeTargetStaticConfig
	)
	if probe.Spec.Targets.Ingress != nil {
		ingressTarget = &victoriametricsv1beta1.ProbeTargetIngress{
			Selector: probe.Spec.Targets.Ingress.Selector,
			NamespaceSelector: victoriametricsv1beta1.NamespaceSelector{
				Any:        probe.Spec.Targets.Ingress.NamespaceSelector.Any,
				MatchNames: probe.Spec.Targets.Ingress.NamespaceSelector.MatchNames,
			},
			RelabelConfigs: convertRelabelConfig(probe.Spec.Targets.Ingress.RelabelConfigs),
		}
	}
	if probe.Spec.Targets.StaticConfig != nil {
		staticTargets = &victoriametricsv1beta1.VMProbeTargetStaticConfig{
			Targets:        probe.Spec.Targets.StaticConfig.Targets,
			Labels:         probe.Spec.Targets.StaticConfig.Labels,
			RelabelConfigs: convertRelabelConfig(probe.Spec.Targets.StaticConfig.RelabelConfigs),
		}
	}
	var safeTLS *v1.SafeTLSConfig
	if probe.Spec.TLSConfig != nil {
		safeTLS = &probe.Spec.TLSConfig.SafeTLSConfig
	}
	cp := &victoriametricsv1beta1.VMProbe{
		ObjectMeta: metav1.ObjectMeta{
			Name:        probe.Name,
			Namespace:   probe.Namespace,
			Labels:      filterPrefixes(probe.Labels, conf.FilterPrometheusConverterLabelPrefixes),
			Annotations: filterPrefixes(probe.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
		},
		Spec: victoriametricsv1beta1.VMProbeSpec{
			JobName: probe.Spec.JobName,
			VMProberSpec: victoriametricsv1beta1.VMProberSpec{
				URL:    probe.Spec.ProberSpec.URL,
				Scheme: probe.Spec.ProberSpec.Scheme,
				Path:   probe.Spec.ProberSpec.Path,
			},
			Module: probe.Spec.Module,
			Targets: victoriametricsv1beta1.VMProbeTargets{
				Ingress:      ingressTarget,
				StaticConfig: staticTargets,
			},
			Interval:          string(probe.Spec.Interval),
			ScrapeTimeout:     string(probe.Spec.ScrapeTimeout),
			BasicAuth:         convertBasicAuth(probe.Spec.BasicAuth),
			TLSConfig:         convertSafeTLSConfig(safeTLS),
			BearerTokenSecret: convertBearerToken(&probe.Spec.BearerTokenSecret),
			OAuth2:            convertOAuth(probe.Spec.OAuth2),
			Authorization:     convertAuthorization(probe.Spec.Authorization, nil),
		},
	}
	if probe.Spec.ProberSpec.ProxyURL != "" {
		cp.Spec.ProxyURL = &probe.Spec.ProberSpec.ProxyURL
	}
	if probe.Spec.SampleLimit != nil {
		cp.Spec.SampleLimit = *probe.Spec.SampleLimit
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cp.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         v1.SchemeGroupVersion.String(),
				Kind:               v1.ProbesKind,
				Name:               probe.Name,
				UID:                probe.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
	}
	cp.Annotations = maybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cp.Annotations)
	return cp
}

func filterUnsupportedRelabelCfg(relabelCfgs []*victoriametricsv1beta1.RelabelConfig) []*victoriametricsv1beta1.RelabelConfig {
	newRelabelCfg := make([]*victoriametricsv1beta1.RelabelConfig, 0, len(relabelCfgs))
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

// ConvertScrapeConfig creates VMScrapeConfig from prometheus scrapeConfig
func ConvertScrapeConfig(promscrapeConfig *monitoringv1alpha1.ScrapeConfig, conf *config.BaseOperatorConf) *victoriametricsv1beta1.VMScrapeConfig {
	cs := &victoriametricsv1beta1.VMScrapeConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:        promscrapeConfig.Name,
			Namespace:   promscrapeConfig.Namespace,
			Labels:      filterPrefixes(promscrapeConfig.Labels, conf.FilterPrometheusConverterLabelPrefixes),
			Annotations: filterPrefixes(promscrapeConfig.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
		},
	}
	data, err := json.Marshal(promscrapeConfig.Spec)
	if err != nil {
		log.Error(err, "POSSIBLE BUG: failed to marshal prometheus scrapeconfig for converting", "name", promscrapeConfig.Name, "namespace", promscrapeConfig.Namespace)
		return cs
	}
	err = json.Unmarshal(data, &cs.Spec)
	if err != nil {
		log.Error(err, "POSSIBLE BUG: failed to convert prometheus scrapeconfig to VMScrapeConfig", "name", promscrapeConfig.Name, "namespace", promscrapeConfig.Namespace)
		return cs
	}
	cs.Labels = filterPrefixes(promscrapeConfig.Labels, conf.FilterPrometheusConverterLabelPrefixes)
	cs.Annotations = filterPrefixes(promscrapeConfig.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes)
	cs.Spec.RelabelConfigs = convertRelabelConfig(promscrapeConfig.Spec.RelabelConfigs)
	cs.Spec.MetricRelabelConfigs = convertRelabelConfig(promscrapeConfig.Spec.MetricRelabelConfigs)

	if promscrapeConfig.Spec.EnableCompression != nil {
		cs.Spec.VMScrapeParams = &victoriametricsv1beta1.VMScrapeParams{
			DisableCompression: ptr.To(!*promscrapeConfig.Spec.EnableCompression),
		}
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cs.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         v1.SchemeGroupVersion.String(),
				Kind:               v1.ServiceMonitorsKind,
				Name:               promscrapeConfig.Name,
				UID:                promscrapeConfig.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
	}
	cs.Annotations = maybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cs.Annotations)
	return cs
}
