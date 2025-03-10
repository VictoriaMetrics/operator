package v1beta1

import (
	"fmt"

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
	// SeriesLimit defines per-scrape limit on number of unique time series
	// a single target can expose during all the scrapes on the time window of 24h.
	// +optional
	SeriesLimit uint64 `json:"seriesLimit,omitempty"`
}

// TargetEndpoint defines single static target endpoint.
type TargetEndpoint struct {
	// Targets static targets addresses in form of ["192.122.55.55:9100","some-name:9100"].
	// +kubebuilder:validation:MinItems=1
	Targets []string `json:"targets"`
	// Labels static labels for targets.
	// +optional
	Labels               map[string]string `json:"labels,omitempty"`
	EndpointRelabelings  `json:",inline"`
	EndpointAuth         `json:",inline"`
	EndpointScrapeParams `json:",inline"`
}

// VMStaticScrape  defines static targets configuration for scraping.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus"
// +kubebuilder:printcolumn:name="Sync Error",type="string",JSONPath=".status.reason"
// +genclient
type VMStaticScrape struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMStaticScrapeSpec `json:"spec,omitempty"`
	Status ScrapeObjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// VMStaticScrapeList contains a list of VMStaticScrape
type VMStaticScrapeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMStaticScrape `json:"items"`
}

// AsProxyKey builds key for proxy cache maps
func (cr *VMStaticScrape) AsProxyKey(i int) string {
	return fmt.Sprintf("staticScrapeProxy/%s/%s/%d", cr.Namespace, cr.Name, i)
}

// AsMapKey builds key for cache secret map
func (cr *VMStaticScrape) AsMapKey(i int) string {
	return fmt.Sprintf("staticScrape/%s/%s/%d", cr.Namespace, cr.Name, i)
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMStaticScrape) GetStatusMetadata() *StatusMetadata {
	return &cr.Status.StatusMetadata
}

func init() {
	SchemeBuilder.Register(&VMStaticScrape{}, &VMStaticScrapeList{})
}
