package v1beta1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ json.Unmarshaler = (*VMStaticScrape)(nil)

// VMStaticScrapeSpec defines the desired state of VMStaticScrape.
type VMStaticScrapeSpec struct {
	// JobName name of job.
	JobName string `json:"jobName,omitempty"`
	// A list of target endpoints to scrape metrics from.
	TargetEndpoints []*TargetEndpoint `json:"targetEndpoints"`
	// SampleLimit defines per-scrape limit on number of scraped samples that will be accepted.
	// +optional
	SampleLimit int `json:"sampleLimit,omitempty"`
	// SeriesLimit defines per-scrape limit on number of unique time series
	// a single target can expose during all the scrapes on the time window of 24h.
	// +optional
	SeriesLimit int `json:"seriesLimit,omitempty"`
	// ScrapeClass defined scrape class to apply
	// +optional
	ScrapeClassName *string `json:"scrapeClass,omitempty"`
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

// Validate returns error if CR is invalid
func (cr *VMStaticScrape) Validate() error {
	if MustSkipCRValidation(cr) {
		return nil
	}
	for _, endpoint := range cr.Spec.TargetEndpoints {
		if err := endpoint.validate(); err != nil {
			return err
		}
	}
	return nil
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMStaticScrape) GetStatusMetadata() *StatusMetadata {
	return &cr.Status.StatusMetadata
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMStaticScrape) GetStatus() *ScrapeObjectStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMStaticScrape) DefaultStatusFields(vs *ScrapeObjectStatus) {}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMStaticScrape) UnmarshalJSON(src []byte) error {
	type pcr VMStaticScrape
	type shadow struct {
		*pcr
		Spec json.RawMessage `json:"spec"`
	}
	s := shadow{pcr: (*pcr)(cr)}
	if err := json.Unmarshal(src, &s); err != nil {
		return err
	}
	if len(s.Spec) > 0 {
		if err := UnmarshalSpecStrict(s.Spec, &cr.Spec); err != nil {
			cr.Status.ParsingSpecError = fmt.Sprintf("cannot parse VMStaticScrapeSpec: %s, err: %s", string(s.Spec), err)
		}
	}
	return nil
}

// AsKey returns unique key for object
func (cr *VMStaticScrape) AsKey(_ bool) string {
	return cr.Namespace + "/" + cr.Name
}
