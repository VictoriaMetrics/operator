package build

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// ScrapeBuilder is implemented by primary CRs and by sidecars (vmbackupmanager, config-reloader, ...).
type ScrapeBuilder interface {
	GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec
	GetMetricsPath() string
	Params(vmv1beta1.ParamsKind) *vmv1beta1.StandardAppsParams
}

type podScrapeBuilder interface {
	ScrapeBuilder
	GetNamespace() string
	PrefixedName() string
	SelectorLabels() map[string]string
	AsOwner() metav1.OwnerReference
}

// sidecarRelabelings builds the job-suffixing relabeling rule for a sidecar endpoint.
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

// scrapeEndpointTLS returns the Scheme/TLSConfig/Params fields for a scrape endpoint.
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

// VMServiceScrape builds a VMServiceScrape for service, scraping primary's own listeners plus
// every sidecar's listeners, addressed by TargetPort.
func VMServiceScrape(service *corev1.Service, primary ScrapeBuilder, sidecars ...ScrapeBuilder) *vmv1beta1.VMServiceScrape {
	params := primary.Params(vmv1beta1.ScrapeParamsKind)
	scrapeListeners := params.GetScrapeListeners()

	authKey := params.ExtraArgs[vmv1beta1.MetricsAuthKeyFlag]
	scrapeListenerTLS := func(name string) (bool, bool) {
		for _, l := range scrapeListeners {
			if l.Name == name {
				return ptr.Deref(l.TLS, false), true
			}
		}
		return false, false
	}

	var endpoints []vmv1beta1.Endpoint
	for _, servicePort := range service.Spec.Ports {
		useTLS, ok := scrapeListenerTLS(servicePort.Name)
		if !ok {
			continue
		}
		scheme, tlsConfig, epParams := scrapeEndpointTLS(useTLS, authKey)
		endpoints = append(endpoints, vmv1beta1.Endpoint{
			Port: servicePort.Name,
			EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
				Path:         primary.GetMetricsPath(),
				Scheme:       scheme,
				Params:       epParams,
				EndpointAuth: vmv1beta1.EndpointAuth{TLSConfig: tlsConfig},
			},
		})
	}

	serviceScrapeSpec := primary.GetServiceScrape()
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

	for _, sidecar := range sidecars {
		sidecarParams := sidecar.Params(vmv1beta1.ScrapeParamsKind)
		sidecarAuthKey := sidecarParams.ExtraArgs[vmv1beta1.MetricsAuthKeyFlag]
		for _, l := range sidecarParams.GetScrapeListeners() {
			scheme, tlsConfig, epParams := scrapeEndpointTLS(ptr.Deref(l.TLS, false), sidecarAuthKey)
			scrape.Spec.Endpoints = append(scrape.Spec.Endpoints, vmv1beta1.Endpoint{
				TargetPort:          ptr.To(intstr.Parse(l.AddrPort())),
				EndpointRelabelings: sidecarRelabelings(l.Name),
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					Path:         sidecar.GetMetricsPath(),
					Scheme:       scheme,
					Params:       epParams,
					EndpointAuth: vmv1beta1.EndpointAuth{TLSConfig: tlsConfig},
				},
			})
		}
	}

	if len(scrape.Spec.Endpoints) == 0 {
		return nil
	}
	for i := range scrape.Spec.Endpoints {
		addVictoriaMetricsAppRelabelConfig(&scrape.Spec.Endpoints[i].EndpointRelabelings)
	}

	return scrape
}

// VMPodScrape builds a VMPodScrape for b, scraping its own listeners plus every sidecar's
// listeners, addressed by PortNumber.
func VMPodScrape(b podScrapeBuilder, sidecars ...ScrapeBuilder) *vmv1beta1.VMPodScrape {
	params := b.Params(vmv1beta1.ScrapeParamsKind)
	scrapeListeners := params.GetScrapeListeners()

	authKey := params.ExtraArgs[vmv1beta1.MetricsAuthKeyFlag]
	var endpoints []vmv1beta1.PodMetricsEndpoint
	for _, l := range scrapeListeners {
		scheme, tlsConfig, epParams := scrapeEndpointTLS(ptr.Deref(l.TLS, false), authKey)
		endpoints = append(endpoints, vmv1beta1.PodMetricsEndpoint{
			Port: ptr.To(l.Name),
			EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
				Path:         b.GetMetricsPath(),
				Scheme:       scheme,
				Params:       epParams,
				EndpointAuth: vmv1beta1.EndpointAuth{TLSConfig: tlsConfig},
			},
		})
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

	for _, sidecar := range sidecars {
		sidecarParams := sidecar.Params(vmv1beta1.ScrapeParamsKind)
		sidecarAuthKey := sidecarParams.ExtraArgs[vmv1beta1.MetricsAuthKeyFlag]
		for _, l := range sidecarParams.GetScrapeListeners() {
			scheme, tlsConfig, epParams := scrapeEndpointTLS(ptr.Deref(l.TLS, false), sidecarAuthKey)
			scrape.Spec.PodMetricsEndpoints = append(scrape.Spec.PodMetricsEndpoints, vmv1beta1.PodMetricsEndpoint{
				PortNumber:          ptr.To(intstr.Parse(l.AddrPort()).IntVal),
				EndpointRelabelings: sidecarRelabelings(l.Name),
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					Path:         sidecar.GetMetricsPath(),
					Scheme:       scheme,
					Params:       epParams,
					EndpointAuth: vmv1beta1.EndpointAuth{TLSConfig: tlsConfig},
				},
			})
		}
	}

	if len(scrape.Spec.PodMetricsEndpoints) == 0 {
		return nil
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
