package v1beta1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// VMServiceScrapeSpec defines the desired state of VMServiceScrape
type VMServiceScrapeSpec struct {
	// DiscoveryRole - defines kubernetes_sd role for objects discovery.
	// by default, its endpoints.
	// can be changed to service or endpointslices.
	// note, that with service setting, you have to use port: "name"
	// and cannot use targetPort for endpoints.
	// +optional
	// +kubebuilder:validation:Enum=endpoints;service;endpointslices
	DiscoveryRole string `json:"discoveryRole,omitempty"`
	// The label to use to retrieve the job name from.
	// +optional
	JobLabel string `json:"jobLabel,omitempty"`
	// TargetLabels transfers labels on the Kubernetes Service onto the target.
	// +optional
	TargetLabels []string `json:"targetLabels,omitempty"`
	// PodTargetLabels transfers labels on the Kubernetes Pod onto the target.
	// +optional
	PodTargetLabels []string `json:"podTargetLabels,omitempty"`
	// A list of endpoints allowed as part of this ServiceScrape.
	Endpoints []Endpoint `json:"endpoints"`
	// Selector to select Endpoints objects by corresponding Service labels.
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Service selector"
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

// VMServiceScrapeStatus defines the observed state of VMServiceScrape
type VMServiceScrapeStatus struct{}

// VMServiceScrape is scrape configuration for endpoints associated with
// kubernetes service,
// it generates scrape configuration for vmagent based on selectors.
// result config will scrape service endpoints
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMServiceScrape"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmservicescrapes,scope=Namespaced
// +genclient
type VMServiceScrape struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMServiceScrapeSpec   `json:"spec"`
	Status VMServiceScrapeStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VMServiceScrapeList contains a list of VMServiceScrape
type VMServiceScrapeList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMServiceScrape `json:"items"`
}

// NamespaceSelector is a selector for selecting either all namespaces or a
// list of namespaces.
// +k8s:openapi-gen=true
type NamespaceSelector struct {
	// Boolean describing whether all namespaces are selected in contrast to a
	// list restricting them.
	// +optional
	Any bool `json:"any,omitempty"`
	// List of namespace names.
	// +optional
	MatchNames []string `json:"matchNames,omitempty"`
}

type nsMatcher interface {
	GetNamespace() string
}

func (ns *NamespaceSelector) IsMatch(item nsMatcher) bool {
	if ns.Any {
		return true
	}
	for _, n := range ns.MatchNames {
		if item.GetNamespace() == n {
			return true
		}
	}
	return false
}

// Endpoint defines a scrapeable endpoint serving metrics.
// +k8s:openapi-gen=true
type Endpoint struct {
	// Name of the port exposed at Service.
	// +optional
	Port string `json:"port,omitempty"`

	// TargetPort
	// Name or number of the pod port this endpoint refers to. Mutually exclusive with port.
	// +optional
	TargetPort *intstr.IntOrString `json:"targetPort,omitempty"`

	EndpointRelabelings  `json:",inline"`
	EndpointAuth         `json:",inline"`
	EndpointScrapeParams `json:",inline"`

	// AttachMetadata configures metadata attaching from service discovery
	// +optional
	AttachMetadata AttachMetadata `json:"attach_metadata,omitempty"`
}

// AsProxyKey builds key for proxy cache maps
func (cr VMServiceScrape) AsProxyKey(i int) string {
	return fmt.Sprintf("serviceScrapeProxy/%s/%s/%d", cr.Namespace, cr.Name, i)
}

// AsMapKey - returns cr name with suffix for token/auth maps.
func (cr VMServiceScrape) AsMapKey(i int) string {
	return fmt.Sprintf("serviceScrape/%s/%s/%d", cr.Namespace, cr.Name, i)
}

func init() {
	SchemeBuilder.Register(&VMServiceScrape{}, &VMServiceScrapeList{})
}
