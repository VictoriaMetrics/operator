package build

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type scrapeBuilder interface {
	GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec
	GetExtraArgs() map[string]string
	GetMetricsPath() string
	UseTLS() bool
}

type podScrapeBuilder interface {
	scrapeBuilder
	GetNamespace() string
	PrefixedName() string
	SelectorLabels() map[string]string
	AsOwner() metav1.OwnerReference
}

// VMServiceScrape creates corresponding object with `http` port endpoint obtained from given service
// add additionalPortNames to the monitoring if needed
func VMServiceScrape(service *corev1.Service, b scrapeBuilder, additionalPortNames ...string) *vmv1beta1.VMServiceScrape {
	var endpoints []vmv1beta1.Endpoint

	extraArgs := b.GetExtraArgs()
	authKey := extraArgs["metricsAuthKey"]

	const defaultPortName = "http"
	for _, servicePort := range service.Spec.Ports {
		// fast path - filter all unmatched ports
		if servicePort.Name != defaultPortName && len(additionalPortNames) == 0 {
			continue
		}

		var extraRelabelingRules vmv1beta1.EndpointRelabelings
		if servicePort.Name != defaultPortName {
			// check service for extra ports
			var nameMatched bool
			for _, filter := range additionalPortNames {
				if servicePort.Name == filter {
					nameMatched = true
					// add a relabeling rule to avoid job collision
					extraRelabelingRules.RelabelConfigs = []*vmv1beta1.RelabelConfig{
						{
							SourceLabels: []string{"job"},
							TargetLabel:  "job",
							Regex:        vmv1beta1.StringOrArray{"(.+)"},
							Replacement:  ptr.To("${1}-" + filter),
						},
					}
					break
				}
			}
			if !nameMatched {
				continue
			}
		}

		endpoint := vmv1beta1.Endpoint{
			Port:                servicePort.Name,
			EndpointRelabelings: extraRelabelingRules,
			EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
				Path: b.GetMetricsPath(),
			},
		}
		if b.UseTLS() {
			endpoint.Scheme = "https"
			// add insecure by default
			// if needed user will override it with direct config
			endpoint.TLSConfig = &vmv1beta1.TLSConfig{
				InsecureSkipVerify: true,
			}
		}
		if len(authKey) > 0 {
			endpoint.Params = map[string][]string{
				"authKey": {authKey},
			}
		}
		endpoints = append(endpoints, endpoint)
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
	// merge generated endpoints into user defined values by Port name
	// assume, that it must be unique.
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
	// allow to manually define selectors
	// in some cases it may be useful
	// for instance when additional service created with extra pod ports
	if scrape.Spec.Selector.MatchLabels == nil && scrape.Spec.Selector.MatchExpressions == nil {
		scrape.Spec.Selector = metav1.LabelSelector{
			MatchLabels: service.Labels,
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: vmv1beta1.AdditionalServiceLabel, Operator: metav1.LabelSelectorOpDoesNotExist},
			},
		}
	}

	return scrape
}

// VMPodScrapeForObjectWithSpec build VMPodScrape for given podScrapeBuilder
func VMPodScrape(b podScrapeBuilder) *vmv1beta1.VMPodScrape {
	extraArgs := b.GetExtraArgs()
	authKey := extraArgs["metricsAuthKey"]

	endpoint := vmv1beta1.PodMetricsEndpoint{
		Port: ptr.To("monitoring-http"),
		EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
			Path: b.GetMetricsPath(),
		},
	}
	if b.UseTLS() {
		endpoint.Scheme = "https"
		// add insecure by default
		// if needed user will override it with direct config
		endpoint.TLSConfig = &vmv1beta1.TLSConfig{
			InsecureSkipVerify: true,
		}
	}
	if len(authKey) > 0 {
		endpoint.Params = map[string][]string{
			"authKey": {authKey},
		}
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
			Selector: *metav1.SetAsLabelSelector(selectorLabels),
			PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{
				endpoint,
			},
		},
	}
	serviceScrapeSpec := b.GetServiceScrape()
	if serviceScrapeSpec != nil {
		for _, e := range serviceScrapeSpec.Endpoints {
			if e.Port == *endpoint.Port {
				endpoint.EndpointAuth = e.EndpointAuth
				endpoint.EndpointScrapeParams = e.EndpointScrapeParams
				endpoint.EndpointRelabelings = e.EndpointRelabelings
				continue
			}
			scrape.Spec.PodMetricsEndpoints = append(scrape.Spec.PodMetricsEndpoints, vmv1beta1.PodMetricsEndpoint{
				Port:                 ptr.To(e.Port),
				EndpointAuth:         e.EndpointAuth,
				EndpointRelabelings:  e.EndpointRelabelings,
				EndpointScrapeParams: e.EndpointScrapeParams,
			})
		}
		scrape.Spec.PodTargetLabels = serviceScrapeSpec.PodTargetLabels
		scrape.Spec.SampleLimit = serviceScrapeSpec.SampleLimit
		scrape.Spec.SeriesLimit = serviceScrapeSpec.SeriesLimit
		scrape.Spec.AttachMetadata = serviceScrapeSpec.AttachMetadata
	}
	return scrape
}
