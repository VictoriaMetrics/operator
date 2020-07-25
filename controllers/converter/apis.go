package converter

import (
	v1beta1vm "github.com/VictoriaMetrics/operator/api/v1beta1"
	v1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
			Namespace: prom.Namespace,
			Name:      prom.Name,
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
			Name:      serviceMon.Name,
			Namespace: serviceMon.Namespace,
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
			BearerTokenFile:      endpoint.BearerTokenFile,
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
		CAFile:             tlsConf.CAFile,
		CA:                 ConvertSecretOrConfigmap(tlsConf.CA),
		CertFile:           tlsConf.CertFile,
		Cert:               ConvertSecretOrConfigmap(tlsConf.Cert),
		KeyFile:            tlsConf.KeyFile,
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
	relalbelConf := []*v1beta1vm.RelabelConfig{}
	for _, relabel := range promRelabelConfig {
		relalbelConf = append(relalbelConf, &v1beta1vm.RelabelConfig{
			SourceLabels: relabel.SourceLabels,
			Separator:    relabel.Separator,
			TargetLabel:  relabel.TargetLabel,
			Regex:        relabel.Regex,
			Modulus:      relabel.Modulus,
			Replacement:  relabel.Replacement,
			Action:       relabel.Action,
		})
	}
	return relalbelConf

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
			Name:      podMon.Name,
			Namespace: podMon.Namespace,
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
