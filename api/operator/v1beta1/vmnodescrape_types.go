package v1beta1

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VMNodeScrapeSpec defines specification for VMNodeScrape.
type VMNodeScrapeSpec struct {
	// The label to use to retrieve the job name from.
	// +optional
	JobLabel string `json:"jobLabel,omitempty"`
	// TargetLabels transfers labels on the Kubernetes Node onto the target.
	// +optional
	TargetLabels []string `json:"targetLabels,omitempty"`
	// Name of the port exposed at Node.
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
	// Authorization with http header Authorization
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"` // TLSConfig configuration to use when scraping the node
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// File to read bearer token for scraping targets.
	// +optional
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// Secret to mount to read bearer token for scraping targets. The secret
	// needs to be  accessible by
	// the victoria-metrics operator.
	// +optional
	// +nullable
	BearerTokenSecret *v1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
	// HonorLabels chooses the metric's labels on collisions with target labels.
	// +optional
	HonorLabels bool `json:"honorLabels,omitempty"`
	// HonorTimestamps controls whether vmagent respects the timestamps present in scraped data.
	// +optional
	HonorTimestamps *bool `json:"honorTimestamps,omitempty"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// MetricRelabelConfigs to apply to samples after scrapping.
	// +optional
	MetricRelabelConfigs []*RelabelConfig `json:"metricRelabelConfigs,omitempty"`
	// RelabelConfigs to apply to samples during service discovery.
	// +optional
	RelabelConfigs []*RelabelConfig `json:"relabelConfigs,omitempty"`
	// ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint.
	// +optional
	ProxyURL *string `json:"proxyURL,omitempty"`
	// Selector to select kubernetes Nodes.
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Service selector"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:selector:"
	// +optional
	Selector metav1.LabelSelector `json:"selector,omitempty"`
	// SampleLimit defines per-scrape limit on number of scraped samples that will be accepted.
	// +optional
	SampleLimit uint64 `json:"sampleLimit,omitempty"`
	// SeriesLimit defines per-scrape limit on number of unique time series
	// a single target can expose during all the scrapes on the time window of 24h.
	// +optional
	SeriesLimit uint64 `json:"seriesLimit,omitempty"`
	// VMScrapeParams defines VictoriaMetrics specific scrape parameters
	// +optional
	VMScrapeParams *VMScrapeParams `json:"vm_scrape_params,omitempty"`
}

// VMNodeScrapeStatus defines the observed state of VMNodeScrape
type VMNodeScrapeStatus struct{}

// VMNodeScrape defines discovery for targets placed on kubernetes nodes,
// usually its node-exporters and other host services.
// InternalIP is used as __address__ for scraping.
// +kubebuilder:object:root=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +genclient
type VMNodeScrape struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMNodeScrapeSpec   `json:"spec,omitempty"`
	Status VMNodeScrapeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VMNodeScrapeList contains a list of VMNodeScrape
type VMNodeScrapeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMNodeScrape `json:"items"`
}

// AsProxyKey builds key for proxy cache maps
func (cr VMNodeScrape) AsProxyKey() string {
	return fmt.Sprintf("nodeScrapeProxy/%s/%s", cr.Namespace, cr.Name)
}

// AsMapKey - returns cr name with suffix for token/auth maps.
func (cr VMNodeScrape) AsMapKey() string {
	return fmt.Sprintf("nodeScrape/%s/%s", cr.Namespace, cr.Name)
}

func init() {
	SchemeBuilder.Register(&VMNodeScrape{}, &VMNodeScrapeList{})
}
