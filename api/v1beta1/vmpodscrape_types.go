package v1beta1

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// VMPodScrapeSpec defines the desired state of VMPodScrape
type VMPodScrapeSpec struct {
	// The label to use to retrieve the job name from.
	// +optional
	JobLabel string `json:"jobLabel,omitempty"`
	// PodTargetLabels transfers labels on the Kubernetes Pod onto the target.
	// +optional
	PodTargetLabels []string `json:"podTargetLabels,omitempty"`
	// A list of endpoints allowed as part of this PodMonitor.
	PodMetricsEndpoints []PodMetricsEndpoint `json:"podMetricsEndpoints"`
	// Selector to select Pod objects.
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Pod selector"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:selector:"
	Selector metav1.LabelSelector `json:"selector"`
	// Selector to select which namespaces the Endpoints objects are discovered from.
	// +optional
	NamespaceSelector NamespaceSelector `json:"namespaceSelector,omitempty"`
	// SampleLimit defines per-scrape limit on number of scraped samples that will be accepted.
	// +optional
	SampleLimit uint64 `json:"sampleLimit,omitempty"`
}

// VMPodScrapeStatus defines the observed state of VMPodScrape
type VMPodScrapeStatus struct {
}

// VMPodScrape is scrape configuration for pods,
// it generates vmagent's config for scraping pod targets
// based on selectors.
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMPodScrape"
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmpodscrapes,scope=Namespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
type VMPodScrape struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VMPodScrapeSpec `json:"spec,omitempty"`
	// +optional
	Status VMPodScrapeStatus `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VMPodScrapeList contains a list of VMPodScrape
type VMPodScrapeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMPodScrape `json:"items"`
}

// PodMetricsEndpoint defines a scrapeable endpoint of a Kubernetes Pod serving Prometheus metrics.
// +k8s:openapi-gen=true
type PodMetricsEndpoint struct {
	// Name of the pod port this endpoint refers to. Mutually exclusive with targetPort.
	// +optional
	Port string `json:"port,omitempty"`
	// Deprecated: Use 'port' instead.
	// +optional
	TargetPort *intstr.IntOrString `json:"targetPort,omitempty"`
	// HTTP path to scrape for metrics.
	// +optional
	Path string `json:"path,omitempty"`
	// HTTP scheme to use for scraping.
	// +optional
	Scheme string `json:"scheme,omitempty"`
	// Optional HTTP URL parameters
	// +optional
	Params map[string][]string `json:"params,omitempty"`
	// Interval at which metrics should be scraped
	// +optional
	Interval string `json:"interval,omitempty"`
	// ScrapeInterval is the same as Interval and has priority over it.
	// one of scrape_interval or interval can be used
	// +optional
	ScrapeInterval string `json:"scrape_interval,omitempty"`
	// Timeout after which the scrape is ended
	// +optional
	ScrapeTimeout string `json:"scrapeTimeout,omitempty"`
	// HonorLabels chooses the metric's labels on collisions with target labels.
	// +optional
	HonorLabels bool `json:"honorLabels,omitempty"`
	// HonorTimestamps controls whether vmagent respects the timestamps present in scraped data.
	// +optional
	HonorTimestamps *bool `json:"honorTimestamps,omitempty"`
	// MetricRelabelConfigs to apply to samples before ingestion.
	// +optional
	MetricRelabelConfigs []*RelabelConfig `json:"metricRelabelConfigs,omitempty"`
	// RelabelConfigs to apply to samples before ingestion.
	// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config
	// +optional
	RelabelConfigs []*RelabelConfig `json:"relabelConfigs,omitempty"`
	// ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint.
	// +optional
	ProxyURL *string `json:"proxyURL,omitempty"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// More info: https://prometheus.io/docs/operating/configuration/#endpoints
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// File to read bearer token for scraping targets.
	// +optional
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// Secret to mount to read bearer token for scraping targets. The secret
	// needs to be in the same namespace as the service scrape and accessible by
	// the victoria-metrics operator.
	// +optional
	BearerTokenSecret v1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
	// TLSConfig configuration to use when scraping the endpoint
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// OAuth2 defines auth configuration
	// +optional
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`
	// VMScrapeParams defines VictoriaMetrics specific scrape parametrs
	// +optional
	VMScrapeParams *VMScrapeParams `json:"vm_scrape_params,omitempty"`
}

// ArbitraryFSAccessThroughSMsConfig enables users to configure, whether
// a service scrape selected by the vmagent instance is allowed to use
// arbitrary files on the file system of the vmagent container. This is the case
// when e.g. a service scrape specifies a BearerTokenFile in an endpoint. A
// malicious user could create a service scrape selecting arbitrary secret files
// in the vmagent container. Those secrets would then be sent with a scrape
// request by vmagent to a malicious target. Denying the above would prevent the
// attack, users can instead use the BearerTokenSecret field.
type ArbitraryFSAccessThroughSMsConfig struct {
	Deny bool `json:"deny,omitempty"`
}

// AsMapKey builds key for cache secret map
func (cr *VMPodScrape) AsMapKey(i int) string {
	return fmt.Sprintf("podScrape/%s/%s/%d", cr.Namespace, cr.Name, i)
}

func init() {
	SchemeBuilder.Register(&VMPodScrape{}, &VMPodScrapeList{})
}
