package converter

import (
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"

	v1beta1vm "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory"
	v1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	prometheusSecretDir    = "/etc/prometheus/secrets"
	prometheusConfigmapDir = "/etc/prometheus/configmaps"
)

var log = ctrl.Log.WithValues("controller", "prometheus.converter")

func ConvertPromRule(prom *v1.PrometheusRule) *v1beta1vm.VMRule {

	ruleGroups := []v1beta1vm.RuleGroup{}
	for _, promGroup := range prom.Spec.Groups {
		ruleItems := []v1beta1vm.Rule{}
		for _, promRuleItem := range promGroup.Rules {
			ruleItems = append(ruleItems, v1beta1vm.Rule{
				Labels:      promRuleItem.Labels,
				Annotations: promRuleItem.Annotations,
				Expr:        promRuleItem.Expr,
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
			Labels:      prom.Labels,
			Annotations: prom.Annotations,
		},
		Spec: v1beta1vm.VMRuleSpec{
			Groups: ruleGroups,
		},
	}
	return cr
}

func ConvertServiceMonitor(serviceMon *v1.ServiceMonitor) *v1beta1vm.VMServiceScrape {
	return &v1beta1vm.VMServiceScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceMon.Name,
			Namespace:   serviceMon.Namespace,
			Annotations: serviceMon.Annotations,
			Labels:      serviceMon.Labels,
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

func ConvertEndpoint(promEndpoint []v1.Endpoint) []v1beta1vm.Endpoint {
	endpoints := []v1beta1vm.Endpoint{}
	for _, endpoint := range promEndpoint {
		endpoints = append(endpoints, v1beta1vm.Endpoint{
			Port:                 endpoint.Port,
			TargetPort:           endpoint.TargetPort,
			Path:                 endpoint.Path,
			Scheme:               endpoint.Scheme,
			Params:               endpoint.Params,
			Interval:             endpoint.Interval,
			ScrapeTimeout:        endpoint.ScrapeTimeout,
			BearerTokenFile:      replacePromDirPath(endpoint.BearerTokenFile),
			BearerTokenSecret:    endpoint.BearerTokenSecret,
			HonorLabels:          endpoint.HonorLabels,
			BasicAuth:            ConvertBasicAuth(endpoint.BasicAuth),
			TLSConfig:            ConvertTlsConfig(endpoint.TLSConfig),
			MetricRelabelConfigs: ConvertRelabelConfig(endpoint.MetricRelabelConfigs),
			RelabelConfigs:       ConvertRelabelConfig(endpoint.RelabelConfigs),
			ProxyURL:             endpoint.ProxyURL,
		})
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
	for _, relabel := range promRelabelConfig {
		relabelCfg = append(relabelCfg, &v1beta1vm.RelabelConfig{
			SourceLabels: relabel.SourceLabels,
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
	endPoints := []v1beta1vm.PodMetricsEndpoint{}
	for _, promEndPoint := range promPodEnpoints {
		endPoints = append(endPoints, v1beta1vm.PodMetricsEndpoint{
			Port:                 promEndPoint.Port,
			Interval:             promEndPoint.Interval,
			Path:                 promEndPoint.Path,
			Scheme:               promEndPoint.Scheme,
			Params:               promEndPoint.Params,
			ScrapeTimeout:        promEndPoint.ScrapeTimeout,
			HonorLabels:          promEndPoint.HonorLabels,
			HonorTimestamps:      promEndPoint.HonorTimestamps,
			ProxyURL:             promEndPoint.ProxyURL,
			RelabelConfigs:       ConvertRelabelConfig(promEndPoint.RelabelConfigs),
			MetricRelabelConfigs: ConvertRelabelConfig(promEndPoint.MetricRelabelConfigs),
		})
	}
	return endPoints
}

func ConvertPodMonitor(podMon *v1.PodMonitor) *v1beta1vm.VMPodScrape {

	return &v1beta1vm.VMPodScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podMon.Name,
			Namespace:   podMon.Namespace,
			Labels:      podMon.Labels,
			Annotations: podMon.Annotations,
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
}

func ConvertProbe(probe *v1.Probe) *v1beta1vm.VMProbe {
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
	return &v1beta1vm.VMProbe{
		ObjectMeta: metav1.ObjectMeta{
			Name:      probe.Name,
			Namespace: probe.Namespace,
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
			Interval:      probe.Spec.Interval,
			ScrapeTimeout: probe.Spec.ScrapeTimeout,
		},
	}
}

func filterUnsupportedRelabelCfg(relabelCfgs []*v1beta1vm.RelabelConfig) []*v1beta1vm.RelabelConfig {
	newRelabelCfg := make([]*v1beta1vm.RelabelConfig, 0, len(relabelCfgs))
	for _, r := range relabelCfgs {
		switch r.Action {
		case "keep", "hashmod", "drop":
			if len(r.SourceLabels) == 0 {
				log.Info("filtering unsupported relabelConfig", "action", r.Action, "reason", "source labels empty")
				continue
			}
		}
		newRelabelCfg = append(newRelabelCfg, r)
	}
	return newRelabelCfg
}
