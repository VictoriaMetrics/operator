package v1beta1

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VMStaticScrapeSpec defines the desired state of VMStaticScrape.
type VMStaticScrapeSpec struct {
	// JobName name of job.
	JobName string `json:"jobName,omitempty"`
	// A list of target endpoints to scrape metrics from.
	TargetEndpoints []*TargetEndpoint `json:"targetEndpoints"`
	// SampleLimit defines per-scrape limit on number of scraped samples that will be accepted.
	// +optional
	SampleLimit uint64 `json:"sampleLimit,omitempty"`
}

// TargetEndpoint defines single static target endpoint.
type TargetEndpoint struct {
	// Targets static targets addresses in form of ["192.122.55.55:9100","some-name:9100"].
	// +kubebuilder:validation:MinItems=1
	Targets []string `json:"targets"`
	// Labels static labels for targets.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Default port for target.
	// +optional
	Port string `json:"port,omitempty"`
	// HTTP path to scrape for metrics.
	// +optional
	Path string `json:"path,omitempty"`
	// HTTP scheme to use for scraping.
	// +optional
	// +kubebuilder:validation:Enum=http;https
	Scheme string `json:"scheme,omitempty"`
	// Optional HTTP URL parameters
	// +optional
	Params map[string][]string `json:"params,omitempty"`
	// FollowRedirects controls redirects for scraping.
	// +optional
	FollowRedirects *bool `json:"follow_redirects,omitempty"`
	// SampleLimit defines per-scrape limit on number of scraped samples that will be accepted.
	// +optional
	SampleLimit uint64 `json:"sampleLimit,omitempty"`
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
	// OAuth2 defines auth configuration
	// +optional
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`
	// TLSConfig configuration to use when scraping the endpoint
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// File to read bearer token for scraping targets.
	// +optional
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// Secret to mount to read bearer token for scraping targets. The secret
	// needs to be in the same namespace as the service scrape and accessible by
	// the victoria-metrics operator.
	// +optional
	// +nullable
	BearerTokenSecret *v1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// More info: https://prometheus.io/docs/operating/configuration/#endpoints
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Authorization with http header Authorization
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"`
	// MetricRelabelConfigs to apply to samples before ingestion.
	// +optional
	MetricRelabelConfigs []*RelabelConfig `json:"metricRelabelConfigs,omitempty"`
	// RelabelConfigs to apply to samples before scraping.
	// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config
	// +optional
	RelabelConfigs []*RelabelConfig `json:"relabelConfigs,omitempty"`
	// ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint.
	// +optional
	ProxyURL *string `json:"proxyURL,omitempty"`
	// HonorLabels chooses the metric's labels on collisions with target labels.
	// +optional
	HonorLabels bool `json:"honorLabels,omitempty"`
	// HonorTimestamps controls whether vmagent respects the timestamps present in scraped data.
	// +optional
	HonorTimestamps *bool `json:"honorTimestamps,omitempty"`
	// VMScrapeParams defines VictoriaMetrics specific scrape parametrs
	// +optional
	VMScrapeParams *VMScrapeParams `json:"vm_scrape_params,omitempty"`
}

// VMStaticScrapeStatus defines the observed state of VMStaticScrape
type VMStaticScrapeStatus struct{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +genclient
// VMStaticScrape  defines static targets configuration for scraping.
type VMStaticScrape struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMStaticScrapeSpec   `json:"spec,omitempty"`
	Status VMStaticScrapeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VMStaticScrapeList contains a list of VMStaticScrape
type VMStaticScrapeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMStaticScrape `json:"items"`
}

// AsProxyKey builds key for proxy cache maps
func (cr VMStaticScrape) AsProxyKey(i int) string {
	return fmt.Sprintf("staticScrapeProxy/%s/%s/%d", cr.Namespace, cr.Name, i)
}

// AsMapKey builds key for cache secret map
func (cr VMStaticScrape) AsMapKey(i int) string {
	return fmt.Sprintf("staticScrape/%s/%s/%d", cr.Namespace, cr.Name, i)
}

func init() {
	SchemeBuilder.Register(&VMStaticScrape{}, &VMStaticScrapeList{})
}
