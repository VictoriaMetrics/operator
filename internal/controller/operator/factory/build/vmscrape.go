package build

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type scrapeBuilder interface {
	GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec
	GetMetricsPath() string
	Params() *vmv1beta1.StandardAppsParams
}

type podScrapeBuilder interface {
	scrapeBuilder
	GetNamespace() string
	PrefixedName() string
	SelectorLabels() map[string]string
	AsOwner() metav1.OwnerReference
}

// sidecarRelabelings builds the job-suffixing relabeling rule shared by every sidecar
// endpoint (config-reloader, vmbackupmanager, ...), keyed by its port name.
func sidecarRelabelings(portName string) vmv1beta1.EndpointRelabelings {
	return vmv1beta1.EndpointRelabelings{
		RelabelConfigs: []*vmv1beta1.RelabelConfig{
			{
				SourceLabels: []string{"job"},
				TargetLabel:  "job",
				Regex:        vmv1beta1.StringOrArray{"(.+)"},
				Replacement:  ptr.To("${1}-" + portName),
			},
		},
	}
}

// scrapeEndpointTLS returns the Scheme/TLSConfig/Params fields shared by every scrape
// endpoint, VMServiceScrape or VMPodScrape.
func scrapeEndpointTLS(useTLS bool, authKey string) (scheme string, tlsConfig *vmv1beta1.TLSConfig, params map[string][]string) {
	if useTLS {
		scheme = "https"
		tlsConfig = &vmv1beta1.TLSConfig{InsecureSkipVerify: true}
	}
	if len(authKey) > 0 {
		params = map[string][]string{"authKey": {authKey}}
	}
	return
}

// VMServiceScrape creates a VMServiceScrape for service, with a single endpoint for b's own
// /metrics handler (see StandardAppsParams.GetScrapeListener) plus one endpoint for each
// name in additionalPortNames (e.g. sidecar metrics ports) that's actually present on service.
func VMServiceScrape(service *corev1.Service, b scrapeBuilder, additionalPortNames ...string) *vmv1beta1.VMServiceScrape {
	params := b.Params()
	authKey := params.ExtraArgs[vmv1beta1.MetricsAuthKeyFlag]
	primary := params.GetScrapeListener(params.PrimaryPortName())

	buildEndpoint := func(name, path string, useTLS bool, relabelings vmv1beta1.EndpointRelabelings) vmv1beta1.Endpoint {
		scheme, tlsConfig, epParams := scrapeEndpointTLS(useTLS, authKey)
		return vmv1beta1.Endpoint{
			Port:                name,
			EndpointRelabelings: relabelings,
			EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
				Path:         path,
				Scheme:       scheme,
				Params:       epParams,
				EndpointAuth: vmv1beta1.EndpointAuth{TLSConfig: tlsConfig},
			},
		}
	}

	var endpoints []vmv1beta1.Endpoint
	for _, servicePort := range service.Spec.Ports {
		if primary != nil && servicePort.Name == primary.Name {
			endpoints = append(endpoints, buildEndpoint(servicePort.Name, b.GetMetricsPath(), *primary.TLS, vmv1beta1.EndpointRelabelings{}))
			continue
		}
		for _, filter := range additionalPortNames {
			if servicePort.Name != filter {
				continue
			}
			endpoints = append(endpoints, buildEndpoint(servicePort.Name, "/metrics", params.UseTLS(), sidecarRelabelings(filter)))
			break
		}
	}

	serviceScrapeSpec := b.GetServiceScrape()
	if serviceScrapeSpec == nil {
		serviceScrapeSpec = &vmv1beta1.VMServiceScrapeSpec{}
	}
	scrape := &vmv1beta1.VMServiceScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:            service.Name,
			Namespace:       service.Namespace,
			OwnerReferences: service.OwnerReferences,
			Labels:          service.Labels,
			Annotations:     service.Annotations,
		},
		Spec: *serviceScrapeSpec,
	}
	for _, e := range endpoints {
		var found bool
		for idx := range scrape.Spec.Endpoints {
			eps := &scrape.Spec.Endpoints[idx]
			if eps.Port == e.Port {
				found = true
				if eps.Path == "" {
					eps.Path = e.Path
				}
			}
		}
		if !found {
			scrape.Spec.Endpoints = append(scrape.Spec.Endpoints, e)
		}
	}
	if scrape.Spec.Selector.MatchLabels == nil && scrape.Spec.Selector.MatchExpressions == nil {
		scrape.Spec.Selector = metav1.LabelSelector{
			MatchLabels: service.Labels,
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: vmv1beta1.AdditionalServiceLabel, Operator: metav1.LabelSelectorOpDoesNotExist},
			},
		}
	}
	for i := range scrape.Spec.Endpoints {
		addVictoriaMetricsAppRelabelConfig(&scrape.Spec.Endpoints[i].EndpointRelabelings)
	}

	return scrape
}

// VMPodScrape builds a VMPodScrape for given podScrapeBuilder, with a single endpoint for b's
// own /metrics handler (see StandardAppsParams.GetScrapeListener, falling back to portName)
// plus one endpoint for each name in additionalPortNames (e.g. sidecar metrics ports).
func VMPodScrape(b podScrapeBuilder, portName string, additionalPortNames ...string) *vmv1beta1.VMPodScrape {
	params := b.Params()
	authKey := params.ExtraArgs[vmv1beta1.MetricsAuthKeyFlag]
	primary := params.GetScrapeListener(portName)
	buildEndpoint := func(name, path string, useTLS bool, relabelings vmv1beta1.EndpointRelabelings) vmv1beta1.PodMetricsEndpoint {
		scheme, tlsConfig, epParams := scrapeEndpointTLS(useTLS, authKey)
		return vmv1beta1.PodMetricsEndpoint{
			Port:                ptr.To(name),
			EndpointRelabelings: relabelings,
			EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
				Path:         path,
				Scheme:       scheme,
				Params:       epParams,
				EndpointAuth: vmv1beta1.EndpointAuth{TLSConfig: tlsConfig},
			},
		}
	}

	var endpoints []vmv1beta1.PodMetricsEndpoint
	if primary != nil {
		endpoints = append(endpoints, buildEndpoint(primary.Name, b.GetMetricsPath(), *primary.TLS, vmv1beta1.EndpointRelabelings{}))
	}
	for _, name := range additionalPortNames {
		endpoints = append(endpoints, buildEndpoint(name, "/metrics", params.UseTLS(), sidecarRelabelings(name)))
	}

	selectorLabels := b.SelectorLabels()
	scrape := &vmv1beta1.VMPodScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:            b.PrefixedName(),
			Namespace:       b.GetNamespace(),
			Labels:          selectorLabels,
			OwnerReferences: []metav1.OwnerReference{b.AsOwner()},
		},
		Spec: vmv1beta1.VMPodScrapeSpec{
			Selector:            *metav1.SetAsLabelSelector(selectorLabels),
			PodMetricsEndpoints: endpoints,
		},
	}
	serviceScrapeSpec := b.GetServiceScrape()
	if serviceScrapeSpec != nil {
		for _, e := range serviceScrapeSpec.Endpoints {
			var found bool
			for idx := range scrape.Spec.PodMetricsEndpoints {
				pep := &scrape.Spec.PodMetricsEndpoints[idx]
				if pep.Port != nil && *pep.Port == e.Port {
					found = true
					pep.EndpointScrapeParams = e.EndpointScrapeParams
					pep.EndpointRelabelings = e.EndpointRelabelings
					break
				}
			}
			if !found {
				scrape.Spec.PodMetricsEndpoints = append(scrape.Spec.PodMetricsEndpoints, vmv1beta1.PodMetricsEndpoint{
					Port:                 ptr.To(e.Port),
					EndpointRelabelings:  e.EndpointRelabelings,
					EndpointScrapeParams: e.EndpointScrapeParams,
				})
			}
		}
		scrape.Spec.PodTargetLabels = serviceScrapeSpec.PodTargetLabels
		scrape.Spec.SampleLimit = serviceScrapeSpec.SampleLimit
		scrape.Spec.SeriesLimit = serviceScrapeSpec.SeriesLimit
		scrape.Spec.AttachMetadata = serviceScrapeSpec.AttachMetadata
	}
	for i := range scrape.Spec.PodMetricsEndpoints {
		addVictoriaMetricsAppRelabelConfig(&scrape.Spec.PodMetricsEndpoints[i].EndpointRelabelings)
	}
	return scrape
}

func addVictoriaMetricsAppRelabelConfig(relabelings *vmv1beta1.EndpointRelabelings) {
	for _, rc := range relabelings.RelabelConfigs {
		if rc != nil && (rc.TargetLabel == "victoriametrics_app" || rc.UnderScoreTargetLabel == "victoriametrics_app") {
			return
		}
	}
	relabelings.RelabelConfigs = append(relabelings.RelabelConfigs, victoriaMetricsAppRelabelConfig())
}

func victoriaMetricsAppRelabelConfig() *vmv1beta1.RelabelConfig {
	return &vmv1beta1.RelabelConfig{
		TargetLabel: "victoriametrics_app",
		Replacement: ptr.To("true"),
	}
}
