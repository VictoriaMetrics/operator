package build

import (
	"strings"

	v1 "k8s.io/api/core/v1"
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
func VMServiceScrapeForAlertmanager(service *v1.Service, amCR *vmv1beta1.VMAlertmanager) *vmv1beta1.VMServiceScrape {
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
func VMServiceScrapeForServiceWithSpec(service *v1.Service, builder serviceScrapeBuilder, additionalPortNames ...string) *vmv1beta1.VMServiceScrape {
	serviceScrapeSpec, extraArgs, metricPath := builder.GetServiceScrape(), builder.GetExtraArgs(), builder.GetMetricPath()
	return vmServiceScrapeForServiceWithSpec(service, serviceScrapeSpec, extraArgs, metricPath, additionalPortNames...)
}

// VMServiceScrapeForServiceWithSpec build VMServiceScrape for given service with optional spec
// optionally could filter out ports from service
func vmServiceScrapeForServiceWithSpec(service *v1.Service, serviceScrapeSpec *vmv1beta1.VMServiceScrapeSpec, extraArgs map[string]string, metricPath string, additionalPortNames ...string) *vmv1beta1.VMServiceScrape {
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
		scrapeSvc.Spec.Selector = metav1.LabelSelector{MatchLabels: service.Spec.Selector, MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: vmv1beta1.AdditionalServiceLabel, Operator: metav1.LabelSelectorOpDoesNotExist},
		}}
	}

	return scrapeSvc
}
