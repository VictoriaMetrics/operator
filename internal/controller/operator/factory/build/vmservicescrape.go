package build

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type serviceScrapeBuilder interface {
	GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec
	GetExtraArgs() map[string]string
	GetMetricPath() string
}

// VMServiceScrapeForServiceWithSpec build VMServiceScrape for VMAlertmanager
func VMServiceScrapeForAlertmanager(service *corev1.Service, amCR *vmv1beta1.VMAlertmanager) *vmv1beta1.VMServiceScrape {
	var extraArgs map[string]string

	isTLS := amCR.Spec.WebConfig != nil && amCR.Spec.WebConfig.TLSServerConfig != nil

	// use hack to emulate enabled tls in generic way with vm components
	if isTLS {
		extraArgs = map[string]string{
			"tls": "true",
		}
	}
	return vmServiceScrapeForServiceWithSpec(service, amCR.GetServiceScrape(), extraArgs, amCR.GetMetricPath())
}

// VMServiceScrapeForServiceWithSpec creates corresponding object with `http` port endpoint obtained from given service
// add additionalPortNames to the monitoring if needed
func VMServiceScrapeForServiceWithSpec(service *corev1.Service, builder serviceScrapeBuilder, additionalPortNames ...string) *vmv1beta1.VMServiceScrape {
	serviceScrapeSpec, extraArgs, metricPath := builder.GetServiceScrape(), builder.GetExtraArgs(), builder.GetMetricPath()
	return vmServiceScrapeForServiceWithSpec(service, serviceScrapeSpec, extraArgs, metricPath, additionalPortNames...)
}

// VMServiceScrapeForServiceWithSpec build VMServiceScrape for given service with optional spec
// optionally could filter out ports from service
func vmServiceScrapeForServiceWithSpec(service *corev1.Service, serviceScrapeSpec *vmv1beta1.VMServiceScrapeSpec, extraArgs map[string]string, metricPath string, additionalPortNames ...string) *vmv1beta1.VMServiceScrape {
	var endPoints []vmv1beta1.Endpoint
	var isTLS bool
	v, ok := extraArgs["tls"]
	if ok {
		// tls is array flag type at VictoriaMetrics components
		// use first value
		firstIdx := strings.IndexByte(v, ',')
		if firstIdx > 0 {
			v = v[:firstIdx]
		}
		isTLS = strings.ToLower(v) == "true"
	}
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

		ep := vmv1beta1.Endpoint{
			Port:                servicePort.Name,
			EndpointRelabelings: extraRelabelingRules,
			EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
				Path: metricPath,
			},
		}
		if isTLS {
			ep.Scheme = "https"
			// add insecure by default
			// if needed user will override it with direct config
			ep.TLSConfig = &vmv1beta1.TLSConfig{
				InsecureSkipVerify: true,
			}
		}
		if len(authKey) > 0 {
			ep.Params = map[string][]string{
				"authKey": {authKey},
			}
		}
		endPoints = append(endPoints, ep)
	}

	if serviceScrapeSpec == nil {
		serviceScrapeSpec = &vmv1beta1.VMServiceScrapeSpec{}
	}
	scrapeSvc := &vmv1beta1.VMServiceScrape{
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
	for _, generatedEP := range endPoints {
		var found bool
		for idx := range scrapeSvc.Spec.Endpoints {
			eps := &scrapeSvc.Spec.Endpoints[idx]
			if eps.Port == generatedEP.Port {
				found = true
				if eps.Path == "" {
					eps.Path = generatedEP.Path
				}
			}
		}
		if !found {
			scrapeSvc.Spec.Endpoints = append(scrapeSvc.Spec.Endpoints, generatedEP)
		}
	}
	// allow to manually define selectors
	// in some cases it may be useful
	// for instance when additional service created with extra pod ports
	if scrapeSvc.Spec.Selector.MatchLabels == nil && scrapeSvc.Spec.Selector.MatchExpressions == nil {
		scrapeSvc.Spec.Selector = metav1.LabelSelector{
			MatchLabels: service.Labels,
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: vmv1beta1.AdditionalServiceLabel, Operator: metav1.LabelSelectorOpDoesNotExist},
			},
		}
	}

	return scrapeSvc
}

type podScrapeBuilder interface {
	GetNamespace() string
	PrefixedName() string
	ProbePort() string
	SelectorLabels() map[string]string
	GetMetricPath() string
	AsOwner() metav1.OwnerReference
}

// VMPodScrapeForObjectWithSpec build VMPodScrape for given podScrapeBuilder with provided args
// optionally could filter out ports from service
func VMPodScrapeForObjectWithSpec(psb podScrapeBuilder, serviceScrapeSpec *vmv1beta1.VMServiceScrapeSpec, extraArgs map[string]string) *vmv1beta1.VMPodScrape {
	var isTLS bool
	v, ok := extraArgs["tls"]
	if ok {
		// tls is array flag type at VictoriaMetrics components
		// use first value
		firstIdx := strings.IndexByte(v, ',')
		if firstIdx > 0 {
			v = v[:firstIdx]
		}
		isTLS = strings.ToLower(v) == "true"
	}
	authKey := extraArgs["metricsAuthKey"]

	defaultEP := vmv1beta1.PodMetricsEndpoint{
		Port: ptr.To("http"),
		EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
			Path: psb.GetMetricPath(),
		},
	}
	if isTLS {
		defaultEP.Scheme = "https"
		// add insecure by default
		// if needed user will override it with direct config
		defaultEP.TLSConfig = &vmv1beta1.TLSConfig{
			InsecureSkipVerify: true,
		}
	}
	if len(authKey) > 0 {
		defaultEP.Params = map[string][]string{
			"authKey": {authKey},
		}
	}

	selectorLabels := psb.SelectorLabels()
	podScrape := &vmv1beta1.VMPodScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:            psb.PrefixedName(),
			Namespace:       psb.GetNamespace(),
			Labels:          selectorLabels,
			OwnerReferences: []metav1.OwnerReference{psb.AsOwner()},
		},
		Spec: vmv1beta1.VMPodScrapeSpec{
			Selector: *metav1.SetAsLabelSelector(selectorLabels),
			PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{
				defaultEP,
			},
		},
	}
	if serviceScrapeSpec != nil {
		for _, ssEP := range serviceScrapeSpec.Endpoints {
			if ssEP.Port == *defaultEP.Port {
				defaultEP.EndpointAuth = ssEP.EndpointAuth
				defaultEP.EndpointScrapeParams = ssEP.EndpointScrapeParams
				defaultEP.EndpointRelabelings = ssEP.EndpointRelabelings
				continue
			}
			podScrape.Spec.PodMetricsEndpoints = append(podScrape.Spec.PodMetricsEndpoints, vmv1beta1.PodMetricsEndpoint{
				Port:                 ptr.To(ssEP.Port),
				EndpointAuth:         ssEP.EndpointAuth,
				EndpointRelabelings:  ssEP.EndpointRelabelings,
				EndpointScrapeParams: ssEP.EndpointScrapeParams,
			})
		}
		podScrape.Spec.PodTargetLabels = serviceScrapeSpec.PodTargetLabels
		podScrape.Spec.SampleLimit = serviceScrapeSpec.SampleLimit
		podScrape.Spec.SeriesLimit = serviceScrapeSpec.SeriesLimit
		podScrape.Spec.AttachMetadata = serviceScrapeSpec.AttachMetadata
	}
	return podScrape
}
