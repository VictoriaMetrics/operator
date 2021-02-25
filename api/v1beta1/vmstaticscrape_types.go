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
	Scheme string `json:"scheme,omitempty"`
	// Optional HTTP URL parameters
	// +optional
	Params map[string][]string `json:"params,omitempty"`
	// Interval at which metrics should be scraped
	// +optional
	Interval string `json:"interval,omitempty"`
	// Timeout after which the scrape is ended
	// +optional
	ScrapeTimeout string `json:"scrapeTimeout,omitempty"`
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
	BearerTokenSecret v1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// More info: https://prometheus.io/docs/operating/configuration/#endpoints
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
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
}

// AsKey represent CR as map key.
func (cr *VMStaticScrape) AsKey(pos int) string {
	return fmt.Sprintf("TargetEndpoint/%s/%s/%d", cr.Namespace, cr.Name, pos)
}

// VMStaticScrapeStatus defines the observed state of VMStaticScrape
type VMStaticScrapeStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMStaticScrape is the Schema for the vmstaticscrapes API,
// which defines static targets configuration for scraping.
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

func init() {
	SchemeBuilder.Register(&VMStaticScrape{}, &VMStaticScrapeList{})
}
