/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VMScrapeConfig specifies a set of targets and parameters describing how to scrape them.
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMScrapeConfig"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmscrapeconfigs,scope=Namespaced
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus"
// +kubebuilder:printcolumn:name="Sync Error",type="string",JSONPath=".status.reason"
// +genclient
type VMScrapeConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMScrapeConfigSpec `json:"spec,omitempty"`
	Status ScrapeObjectStatus `json:"status,omitempty"`
}

// VMScrapeConfigSpec defines the desired state of VMScrapeConfig
type VMScrapeConfigSpec struct {
	// StaticConfigs defines a list of static targets with a common label set.
	// +optional
	StaticConfigs []StaticConfig `json:"staticConfigs,omitempty"`
	// FileSDConfigs defines a list of file service discovery configurations.
	// +optional
	FileSDConfigs []FileSDConfig `json:"fileSDConfigs,omitempty"`
	// HTTPSDConfigs defines a list of HTTP service discovery configurations.
	// +optional
	HTTPSDConfigs []HTTPSDConfig `json:"httpSDConfigs,omitempty"`
	// KubernetesSDConfigs defines a list of Kubernetes service discovery configurations.
	// +optional
	KubernetesSDConfigs []KubernetesSDConfig `json:"kubernetesSDConfigs,omitempty"`
	// ConsulSDConfigs defines a list of Consul service discovery configurations.
	// +optional
	ConsulSDConfigs []ConsulSDConfig `json:"consulSDConfigs,omitempty"`
	// DNSSDConfigs defines a list of DNS service discovery configurations.
	// +optional
	DNSSDConfigs []DNSSDConfig `json:"dnsSDConfigs,omitempty"`
	// EC2SDConfigs defines a list of EC2 service discovery configurations.
	// +optional
	EC2SDConfigs []EC2SDConfig `json:"ec2SDConfigs,omitempty"`
	// AzureSDConfigs defines a list of Azure service discovery configurations.
	// +optional
	AzureSDConfigs []AzureSDConfig `json:"azureSDConfigs,omitempty"`
	// GCESDConfigs defines a list of GCE service discovery configurations.
	// +optional
	GCESDConfigs []GCESDConfig `json:"gceSDConfigs,omitempty"`
	// OpenStackSDConfigs defines a list of OpenStack service discovery configurations.
	// +optional
	OpenStackSDConfigs []OpenStackSDConfig `json:"openstackSDConfigs,omitempty"`
	// DigitalOceanSDConfigs defines a list of DigitalOcean service discovery configurations.
	// +optional
	DigitalOceanSDConfigs []DigitalOceanSDConfig `json:"digitalOceanSDConfigs,omitempty"`
	EndpointScrapeParams  `json:",inline"`
	EndpointRelabelings   `json:",inline"`
	EndpointAuth          `json:",inline"`
}

// StaticConfig defines a static configuration.
// See [here](https://docs.victoriametrics.com/sd_configs#static_configs)
type StaticConfig struct {
	// List of targets for this static configuration.
	// +optional
	Targets []string `json:"targets,omitempty"`
	// Labels assigned to all metrics scraped from the targets.
	// +mapType:=atomic
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// FileSDConfig defines a file service discovery configuration.
// See [here](https://docs.victoriametrics.com/sd_configs#file_sd_configs)
type FileSDConfig struct {
	// List of files to be used for file discovery.
	// +kubebuilder:validation:MinItems:=1
	Files []string `json:"files"`
}

// HTTPSDConfig defines a HTTP service discovery configuration.
// See [here](https://docs.victoriametrics.com/sd_configs#http_sd_configs)
type HTTPSDConfig struct {
	// URL from which the targets are fetched.
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:Pattern:="^http(s)?://.+$"
	URL string `json:"url"`
	// BasicAuth information to use on every scrape request.
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Authorization header to use on every scrape request.
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"`
	// TLS configuration to use on every scrape request
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint.
	// +optional
	ProxyURL *string `json:"proxyURL,omitempty"`
	// ProxyClientConfig configures proxy auth settings for scraping
	// See [feature description](https://docs.victoriametrics.com/vmagent#scraping-targets-via-a-proxy)
	// +optional
	ProxyClientConfig *ProxyAuth `json:"proxy_client_config,omitempty"`
}

// KubernetesSDConfig allows retrieving scrape targets from Kubernetes' REST API.
// See [here](https://docs.victoriametrics.com/sd_configs#kubernetes_sd_configs)
// +k8s:openapi-gen=true
type KubernetesSDConfig struct {
	// The API server address consisting of a hostname or IP address followed
	// by an optional port number.
	// If left empty, assuming process is running inside
	// of the cluster. It will discover API servers automatically and use the pod's
	// CA certificate and bearer token file at /var/run/secrets/kubernetes.io/serviceaccount/.
	// +optional
	APIServer *string `json:"apiServer,omitempty"`
	// Role of the Kubernetes entities that should be discovered.
	// +required
	Role string `json:"role"`
	// BasicAuth information to use on every scrape request.
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Authorization header to use on every scrape request.
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"`
	// TLS configuration to use on every scrape request
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// OAuth2 defines auth configuration
	// +optional
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`
	// ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint.
	// +optional
	ProxyURL *string `json:"proxyURL,omitempty"`
	// ProxyClientConfig configures proxy auth settings for scraping
	// See [feature description](https://docs.victoriametrics.com/vmagent#scraping-targets-via-a-proxy)
	// +optional
	ProxyClientConfig *ProxyAuth `json:"proxy_client_config,omitempty"`
	// Configure whether HTTP requests follow HTTP 3xx redirects.
	// +optional
	FollowRedirects *bool `json:"followRedirects,omitempty"`
	// Optional namespace discovery. If omitted, discover targets across all namespaces.
	// +optional
	Namespaces *NamespaceDiscovery `json:"namespaces,omitempty"`
	// AttachMetadata configures metadata attaching from service discovery
	// +optional
	AttachMetadata AttachMetadata `json:"attach_metadata,omitempty"`
	// Selector to select objects.
	// +optional
	// +listType=map
	// +listMapKey=role
	Selectors []K8SSelectorConfig `json:"selectors,omitempty"`
}

// K8SSelectorConfig is Kubernetes Selector Config
type K8SSelectorConfig struct {
	// +kubebuilder:validation:Required
	Role  string `json:"role"`
	Label string `json:"label,omitempty"`
	Field string `json:"field,omitempty"`
}

// NamespaceDiscovery is the configuration for discovering
// Kubernetes namespaces.
type NamespaceDiscovery struct {
	// Includes the namespace in which the pod exists to the list of watched namespaces.
	// +optional
	IncludeOwnNamespace *bool `json:"ownNamespace,omitempty"`
	// List of namespaces where to watch for resources.
	// If empty and `ownNamespace` isn't true, watch for resources in all namespaces.
	// +optional
	Names []string `json:"names,omitempty"`
}

// ConsulSDConfig defines a Consul service discovery configuration.
// See [here](https://docs.victoriametrics.com/sd_configs/#consul_sd_configs)
// +k8s:openapi-gen=true
type ConsulSDConfig struct {
	// A valid string consisting of a hostname or IP followed by an optional port number.
	// +kubebuilder:validation:MinLength=1
	// +required
	Server string `json:"server"`
	// Consul ACL TokenRef, if not provided it will use the ACL from the local Consul Agent.
	// +optional
	TokenRef *corev1.SecretKeySelector `json:"tokenRef,omitempty"`
	// Consul Datacenter name, if not provided it will use the local Consul Agent Datacenter.
	// +optional
	Datacenter *string `json:"datacenter,omitempty"`
	// Namespaces are only supported in Consul Enterprise.
	// +optional
	Namespace *string `json:"namespace,omitempty"`
	// Admin Partitions are only supported in Consul Enterprise.
	// +optional
	Partition *string `json:"partition,omitempty"`
	// HTTP Scheme default "http"
	// +kubebuilder:validation:Enum=HTTP;HTTPS
	// +optional
	Scheme *string `json:"scheme,omitempty"`
	// A list of services for which targets are retrieved. If omitted, all services are scraped.
	// +listType:=atomic
	// +optional
	Services []string `json:"services,omitempty"`
	// An optional list of tags used to filter nodes for a given service. Services must contain all tags in the list.
	//+listType:=atomic
	// +optional
	Tags []string `json:"tags,omitempty"`
	// The string by which Consul tags are joined into the tag label.
	// If unset, use its default value.
	// +optional
	TagSeparator *string `json:"tagSeparator,omitempty"`
	// Node metadata key/value pairs to filter nodes for a given service.
	// +mapType:=atomic
	// +optional
	NodeMeta map[string]string `json:"nodeMeta,omitempty"`
	// Allow stale Consul results (see https://developer.hashicorp.com/consul/api-docs/features/consistency). Will reduce load on Consul.
	// If unset, use its default value.
	// +optional
	AllowStale *bool `json:"allowStale,omitempty"`
	// Filter defines filter for /v1/catalog/services requests
	// See https://developer.hashicorp.com/consul/api-docs/features/filtering
	// +optional
	Filter string `json:"filter,omitempty"`
	// BasicAuth information to use on every scrape request.
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Authorization header to use on every scrape request.
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"`
	// OAuth2 defines auth configuration
	// +optional
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`
	// ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint.
	// +optional
	ProxyURL *string `json:"proxyURL,omitempty"`
	// ProxyClientConfig configures proxy auth settings for scraping
	// See [feature description](https://docs.victoriametrics.com/vmagent#scraping-targets-via-a-proxy)
	// +optional
	ProxyClientConfig *ProxyAuth `json:"proxy_client_config,omitempty"`
	// Configure whether HTTP requests follow HTTP 3xx redirects.
	// If unset, use its default value.
	// +optional
	FollowRedirects *bool `json:"followRedirects,omitempty"`
	// TLS configuration to use on every scrape request
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
}

// DNSSDConfig allows specifying a set of DNS domain names which are periodically queried to discover a list of targets.
// The DNS servers to be contacted are read from /etc/resolv.conf.
// See [here](https://docs.victoriametrics.com/sd_configs#dns_sd_configs)
// +k8s:openapi-gen=true
type DNSSDConfig struct {
	// A list of DNS domain names to be queried.
	// +kubebuilder:validation:MinItems:=1
	Names []string `json:"names"`
	// The type of DNS query to perform.
	// Supported values are: SRV, A, AAAA or MX.
	// By default, SRV is used.
	// +kubebuilder:validation:Enum=SRV;A;AAAA;MX
	// +optional

	Type *string `json:"type"`
	// The port number used if the query type is not SRV
	// Ignored for SRV records
	// +optional
	Port *int `json:"port"`
}

// EC2SDConfig allow retrieving scrape targets from AWS EC2 instances.
// The private IP address is used by default, but may be changed to the public IP address with relabeling.
// The IAM credentials used must have the ec2:DescribeInstances permission to discover scrape targets.
// See [here](https://docs.victoriametrics.com/sd_configs#ec2_sd_configs)
// +k8s:openapi-gen=true
type EC2SDConfig struct {
	// The AWS region
	// +optional
	Region *string `json:"region"`
	// AccessKey is the AWS API key.
	// +optional
	AccessKey *corev1.SecretKeySelector `json:"accessKey,omitempty"`
	// SecretKey is the AWS API secret.
	// +optional
	SecretKey *corev1.SecretKeySelector `json:"secretKey,omitempty"`
	// AWS Role ARN, an alternative to using AWS API keys.
	// +optional
	RoleARN *string `json:"roleARN,omitempty"`
	// The port to scrape metrics from. If using the public IP address, this must
	// instead be specified in the relabeling rule.
	// +optional
	Port *int `json:"port"`
	// Filters can be used optionally to filter the instance list by other criteria.
	// Available filter criteria can be found here:
	// https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_DescribeInstances.html
	// Filter API documentation: https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_Filter.html
	// +optional
	Filters []*EC2Filter `json:"filters"`
}

// EC2Filter is the configuration for filtering EC2 instances.
type EC2Filter struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// AzureSDConfig allow retrieving scrape targets from Azure VMs.
// See [here](https://docs.victoriametrics.com/sd_configs#azure_sd_configs)
// +k8s:openapi-gen=true
type AzureSDConfig struct {
	// The Azure environment.
	// +optional
	Environment *string `json:"environment,omitempty"`
	// # The authentication method, either OAuth or ManagedIdentity.
	// See https://docs.microsoft.com/en-us/azure/active-directory/managed-identities-azure-resources/overview
	// +kubebuilder:validation:Enum=OAuth;ManagedIdentity
	// +optional
	AuthenticationMethod *string `json:"authenticationMethod,omitempty"`
	// The subscription ID. Always required.
	// +kubebuilder:validation:MinLength=1
	// +required
	SubscriptionID string `json:"subscriptionID"`
	// Optional tenant ID. Only required with the OAuth authentication method.
	// +optional
	TenantID *string `json:"tenantID,omitempty"`
	// Optional client ID. Only required with the OAuth authentication method.
	// +optional
	ClientID *string `json:"clientID,omitempty"`
	// Optional client secret. Only required with the OAuth authentication method.
	// +optional
	ClientSecret *corev1.SecretKeySelector `json:"clientSecret,omitempty"`
	// Optional resource group name. Limits discovery to this resource group.
	// +optional
	ResourceGroup *string `json:"resourceGroup,omitempty"`
	// The port to scrape metrics from. If using the public IP address, this must
	// instead be specified in the relabeling rule.
	// +optional
	Port *int `json:"port"`
}

// GCESDConfig configures scrape targets from GCP GCE instances.
// The private IP address is used by default, but may be changed to
// the public IP address with relabeling.
// See [here](https://docs.victoriametrics.com/sd_configs#gce_sd_configs)
//
// The GCE service discovery will load the Google Cloud credentials
// from the file specified by the GOOGLE_APPLICATION_CREDENTIALS environment variable.
// See https://cloud.google.com/kubernetes-engine/docs/tutorials/authenticating-to-cloud-platform
// +k8s:openapi-gen=true
type GCESDConfig struct {
	// The Google Cloud Project ID
	// +kubebuilder:validation:MinLength:=1
	// +required
	Project string `json:"project"`
	// The zone of the scrape targets. If you need multiple zones use multiple GCESDConfigs.
	// +required
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	Zone StringOrArray `json:"zone"`
	// Filter can be used optionally to filter the instance list by other criteria
	// Syntax of this filter is described in the filter query parameter section:
	// https://cloud.google.com/compute/docs/reference/latest/instances/list
	// +optional
	Filter *string `json:"filter,omitempty"`
	// The port to scrape metrics from. If using the public IP address, this must
	// instead be specified in the relabeling rule.
	// +optional
	Port *int `json:"port"`
	// The tag separator is used to separate the tags on concatenation
	// +optional
	TagSeparator *string `json:"tagSeparator,omitempty"`
}

// OpenStackSDConfig allow retrieving scrape targets from OpenStack Nova instances.
// See [here](https://docs.victoriametrics.com/sd_configs#openstack_sd_configs)
// +k8s:openapi-gen=true
type OpenStackSDConfig struct {
	// The OpenStack role of entities that should be discovered.
	// +kubebuilder:validation:Enum=Instance;instance;Hypervisor;hypervisor
	// +required
	Role string `json:"role"`
	// The OpenStack Region.
	// +kubebuilder:validation:MinLength:=1
	// +required
	Region string `json:"region"`
	// IdentityEndpoint specifies the HTTP endpoint that is required to work with
	// the Identity API of the appropriate version.
	// +optional
	IdentityEndpoint *string `json:"identityEndpoint,omitempty"`
	// Username is required if using Identity V2 API. Consult with your provider's
	// control panel to discover your account's username.
	// In Identity V3, either userid or a combination of username
	// and domainId or domainName are needed
	// +optional
	Username *string `json:"username,omitempty"`
	// UserID
	// +optional
	UserID *string `json:"userid,omitempty"`
	// Password for the Identity V2 and V3 APIs. Consult with your provider's
	// control panel to discover your account's preferred method of authentication.
	// +optional
	Password *corev1.SecretKeySelector `json:"password,omitempty"`
	// At most one of domainId and domainName must be provided if using username
	// with Identity V3. Otherwise, either are optional.
	// +optional
	DomainName *string `json:"domainName,omitempty"`
	// DomainID
	// +optional
	DomainID *string `json:"domainID,omitempty"`
	// The ProjectId and ProjectName fields are optional for the Identity V2 API.
	// Some providers allow you to specify a ProjectName instead of the ProjectId.
	// Some require both. Your provider's authentication policies will determine
	// how these fields influence authentication.
	// +optional
	ProjectName *string `json:"projectName,omitempty"`
	//  ProjectID
	// +optional
	ProjectID *string `json:"projectID,omitempty"`
	// The ApplicationCredentialID or ApplicationCredentialName fields are
	// required if using an application credential to authenticate. Some providers
	// allow you to create an application credential to authenticate rather than a
	// password.
	// +optional
	ApplicationCredentialName *string `json:"applicationCredentialName,omitempty"`
	// ApplicationCredentialID
	// +optional
	ApplicationCredentialID *string `json:"applicationCredentialId,omitempty"`
	// The applicationCredentialSecret field is required if using an application
	// credential to authenticate.
	// +optional
	ApplicationCredentialSecret *corev1.SecretKeySelector `json:"applicationCredentialSecret,omitempty"`
	// Whether the service discovery should list all instances for all projects.
	// It is only relevant for the 'instance' role and usually requires admin permissions.
	// +optional
	AllTenants *bool `json:"allTenants,omitempty"`
	// The port to scrape metrics from. If using the public IP address, this must
	// instead be specified in the relabeling rule.
	// +optional
	Port *int `json:"port"`
	// Availability of the endpoint to connect to.
	// +kubebuilder:validation:Enum=Public;public;Admin;admin;Internal;internal
	// +optional
	Availability *string `json:"availability,omitempty"`
	// TLS configuration to use on every scrape request
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
}

// DigitalOceanSDConfig allow retrieving scrape targets from DigitalOcean's Droplets API.
// This service discovery uses the public IPv4 address by default, by that can be changed with relabeling.
// See [here](https://docs.victoriametrics.com/sd_configs#digitalocean_sd_configs)
// +k8s:openapi-gen=true
type DigitalOceanSDConfig struct {
	// Authorization header to use on every scrape request.
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"`
	// OAuth2 defines auth configuration
	// +optional
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`
	// ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint.
	// +optional
	ProxyURL *string `json:"proxyURL,omitempty"`
	// ProxyClientConfig configures proxy auth settings for scraping
	// See [feature description](https://docs.victoriametrics.com/vmagent#scraping-targets-via-a-proxy)
	// +optional
	ProxyClientConfig *ProxyAuth `json:"proxy_client_config,omitempty"`
	// Configure whether HTTP requests follow HTTP 3xx redirects.
	// +optional
	FollowRedirects *bool `json:"followRedirects,omitempty"`
	// TLS configuration to use on every scrape request
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// The port to scrape metrics from.
	// +optional
	Port *int `json:"port,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VMScrapeConfigList contains a list of VMScrapeConfig
type VMScrapeConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMScrapeConfig `json:"items"`
}

// AsProxyKey builds key for proxy cache maps
func (cr *VMScrapeConfig) AsProxyKey(prefix string, i int) string {
	return fmt.Sprintf("scrapeConfigProxy/%s/%s/%s/%d", cr.Namespace, cr.Name, prefix, i)
}

// AsMapKey - returns cr name with suffix for token/auth maps.
func (cr *VMScrapeConfig) AsMapKey(prefix string, i int) string {
	return fmt.Sprintf("scrapeConfig/%s/%s/%s/%d", cr.Namespace, cr.Name, prefix, i)
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMScrapeConfig) GetStatusMetadata() *StatusMetadata {
	return &cr.Status.StatusMetadata
}

func init() {
	SchemeBuilder.Register(&VMScrapeConfig{}, &VMScrapeConfigList{})
}
