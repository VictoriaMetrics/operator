package v1beta1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ json.Unmarshaler = (*VMNodeScrapeSpec)(nil)

// VMNodeScrapeSpec defines specification for VMNodeScrape.
type VMNodeScrapeSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// The label to use to retrieve the job name from.
	// +optional
	JobLabel string `json:"jobLabel,omitempty"`
	// TargetLabels transfers labels on the Kubernetes Node onto the target.
	// +optional
	TargetLabels []string `json:"targetLabels,omitempty"`
	// Name of the port exposed at Node.
	// +optional
	Port                 string `json:"port,omitempty"`
	EndpointRelabelings  `json:",inline"`
	EndpointAuth         `json:",inline"`
	EndpointScrapeParams `json:",inline"`

	// Selector to select kubernetes Nodes.
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Service selector"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:selector:"
	// +optional
	Selector metav1.LabelSelector `json:"selector,omitempty"`
	// ScrapeClass defined scrape class to apply
	// +optional
	ScrapeClassName *string `json:"scrapeClass,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMNodeScrapeSpec) UnmarshalJSON(src []byte) error {
	type pcr VMNodeScrapeSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// VMNodeScrape defines discovery for targets placed on kubernetes nodes,
// usually its node-exporters and other host services.
// InternalIP is used as __address__ for scraping.
// +kubebuilder:object:root=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus"
// +kubebuilder:printcolumn:name="Sync Error",type="string",JSONPath=".status.reason"
// +genclient
type VMNodeScrape struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMNodeScrapeSpec   `json:"spec,omitempty"`
	Status ScrapeObjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VMNodeScrapeList contains a list of VMNodeScrape
type VMNodeScrapeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMNodeScrape `json:"items"`
}

// Validate returns error if CR is invalid
func (cr *VMNodeScrape) Validate() error {
	if MustSkipCRValidation(cr) {
		return nil
	}
	return cr.Spec.validate()
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMNodeScrape) GetStatusMetadata() *StatusMetadata {
	return &cr.Status.StatusMetadata
}

// AsKey returns unique key for object
func (cr *VMNodeScrape) AsKey(_ bool) string {
	return cr.Namespace + "/" + cr.Name
}

func init() {
	SchemeBuilder.Register(&VMNodeScrape{}, &VMNodeScrapeList{})
}
