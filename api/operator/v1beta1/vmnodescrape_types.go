package v1beta1

import (
	"fmt"

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

// AsProxyKey builds key for proxy cache maps
func (cr *VMNodeScrape) AsProxyKey() string {
	return fmt.Sprintf("nodeScrapeProxy/%s/%s", cr.Namespace, cr.Name)
}

// AsMapKey - returns cr name with suffix for token/auth maps.
func (cr *VMNodeScrape) AsMapKey() string {
	return fmt.Sprintf("nodeScrape/%s/%s", cr.Namespace, cr.Name)
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMNodeScrape) GetStatusMetadata() *StatusMetadata {
	return &cr.Status.StatusMetadata
}

func init() {
	SchemeBuilder.Register(&VMNodeScrape{}, &VMNodeScrapeList{})
}
