package v1beta1

import (
	"fmt"

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
	// +optional
	Selector metav1.LabelSelector `json:"selector,omitempty"`
	// Selector to select which namespaces the Endpoints objects are discovered from.
	// +optional
	NamespaceSelector NamespaceSelector `json:"namespaceSelector,omitempty"`
	// SampleLimit defines per-scrape limit on number of scraped samples that will be accepted.
	// +optional
	SampleLimit uint64 `json:"sampleLimit,omitempty"`
	// SeriesLimit defines per-scrape limit on number of unique time series
	// a single target can expose during all the scrapes on the time window of 24h.
	// +optional
	SeriesLimit uint64 `json:"seriesLimit,omitempty"`
	// AttachMetadata configures metadata attaching from service discovery
	// +optional
	AttachMetadata AttachMetadata `json:"attach_metadata,omitempty"`
}

// VMPodScrape is scrape configuration for pods,
// it generates vmagent's config for scraping pod targets
// based on selectors.
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMPodScrape"
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmpodscrapes,scope=Namespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus"
// +kubebuilder:printcolumn:name="Sync Error",type="string",JSONPath=".status.reason"
// +genclient
type VMPodScrape struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMPodScrapeSpec    `json:"spec,omitempty"`
	Status ScrapeObjectStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VMPodScrapeList contains a list of VMPodScrape
type VMPodScrapeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMPodScrape `json:"items"`
}

// PodMetricsEndpoint defines a scrapeable endpoint of a Kubernetes Pod serving metrics.
// +k8s:openapi-gen=true
type PodMetricsEndpoint struct {
	// Name of the port exposed at Pod.
	// +optional
	Port string `json:"port,omitempty"`
	// TargetPort
	// Name or number of the pod port this endpoint refers to. Mutually exclusive with port.
	// +optional
	TargetPort           *intstr.IntOrString `json:"targetPort,omitempty"`
	EndpointRelabelings  `json:",inline"`
	EndpointAuth         `json:",inline"`
	EndpointScrapeParams `json:",inline"`
	// AttachMetadata configures metadata attaching from service discovery
	// +optional
	AttachMetadata AttachMetadata `json:"attach_metadata,omitempty"`
	// FilterRunning applies filter with pod status == running
	// it prevents from scrapping metrics at failed or succeed state pods.
	// enabled by default
	// +optional
	FilterRunning *bool `json:"filterRunning,omitempty"`
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

// AsProxyKey builds key for proxy cache maps
func (cr VMPodScrape) AsProxyKey(i int) string {
	return fmt.Sprintf("podScrapeProxy/%s/%s/%d", cr.Namespace, cr.Name, i)
}

// AsMapKey builds key for cache secret map
func (cr *VMPodScrape) AsMapKey(i int) string {
	return fmt.Sprintf("podScrape/%s/%s/%d", cr.Namespace, cr.Name, i)
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMPodScrape) GetStatusMetadata() *StatusMetadata {
	return &cr.Status.StatusMetadata
}

func init() {
	SchemeBuilder.Register(&VMPodScrape{}, &VMPodScrapeList{})
}
