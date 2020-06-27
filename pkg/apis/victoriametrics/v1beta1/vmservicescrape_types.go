package v1beta1

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// VMServiceScrapeSpec defines the desired state of VMServiceScrape
type VMServiceScrapeSpec struct {
	// The label to use to retrieve the job name from.
	// +optional
	JobLabel string `json:"jobLabel,omitempty"`
	// TargetLabels transfers labels on the Kubernetes Service onto the target.
	// +optional
	TargetLabels []string `json:"targetLabels,omitempty"`
	// PodTargetLabels transfers labels on the Kubernetes Pod onto the target.
	// +optional
	PodTargetLabels []string `json:"podTargetLabels,omitempty"`
	// A list of endpoints allowed as part of this ServiceMonitor.
	Endpoints []Endpoint `json:"endpoints"`
	// Selector to select Endpoints objects.
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="Service selector"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:selector:"
	Selector metav1.LabelSelector `json:"selector"`
	// Selector to select which namespaces the Endpoints objects are discovered from.
	// +optional
	NamespaceSelector NamespaceSelector `json:"namespaceSelector,omitempty"`
	// SampleLimit defines per-scrape limit on number of scraped samples that will be accepted.
	// +optional
	SampleLimit uint64 `json:"sampleLimit,omitempty"`
}

// VMServiceScrapeStatus defines the observed state of VMServiceScrape
type VMServiceScrapeStatus struct {
}

// VMServiceScrape is the Schema for the servicescrapes API
// it generates scrape configuration for vmagent based on selectors
// result config will scrape service endpoints
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMServiceScrape"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
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

// Endpoint defines a scrapeable endpoint serving Prometheus metrics.
// +k8s:openapi-gen=true
type Endpoint struct {
	// Name of the service port this endpoint refers to. Mutually exclusive with targetPort.
	// +optional
	Port string `json:"port,omitempty"`
	// Name or number of the pod port this endpoint refers to. Mutually exclusive with port.
	// +optional
	TargetPort *intstr.IntOrString `json:"targetPort,omitempty"`
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
	// TLS configuration to use when scraping the endpoint
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// File to read bearer token for scraping targets.
	// +optional
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// Secret to mount to read bearer token for scraping targets. The secret
	// needs to be in the same namespace as the service monitor and accessible by
	// the victoria-metrics operator.
	// +optional
	BearerTokenSecret v1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
	// HonorLabels chooses the metric's labels on collisions with target labels.
	// +optional
	HonorLabels bool `json:"honorLabels,omitempty"`
	// HonorTimestamps controls whether vmagent respects the timestamps present in scraped data.
	// +optional
	HonorTimestamps *bool `json:"honorTimestamps,omitempty"`
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

// TLSConfig specifies TLS configuration parameters.
// +k8s:openapi-gen=true
type TLSConfig struct {
	// Path to the CA cert in the container to use for the targets.
	// +optional
	CAFile string `json:"caFile,omitempty"`
	// Stuct containing the CA cert to use for the targets.
	// +optional
	CA SecretOrConfigMap `json:"ca,omitempty"`

	// Path to the client cert file in the container for the targets.
	// +optional
	CertFile string `json:"certFile,omitempty"`
	// Struct containing the client cert file for the targets.
	// +optional
	Cert SecretOrConfigMap `json:"cert,omitempty"`

	// Path to the client key file in the container for the targets.
	// +optional
	KeyFile string `json:"keyFile,omitempty"`
	// Secret containing the client key file for the targets.
	// +optional
	KeySecret *v1.SecretKeySelector `json:"keySecret,omitempty"`

	// Used to verify the hostname for the targets.
	// +optional
	ServerName string `json:"serverName,omitempty"`
	// Disable target certificate validation.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// BasicAuth allow an endpoint to authenticate over basic authentication
// More info: https://prometheus.io/docs/operating/configuration/#endpoints
// +k8s:openapi-gen=true
type BasicAuth struct {
	// The secret in the service monitor namespace that contains the username
	// for authentication.
	// +optional
	Username v1.SecretKeySelector `json:"username,omitempty"`
	// The secret in the service monitor namespace that contains the password
	// for authentication.
	// +optional
	Password v1.SecretKeySelector `json:"password,omitempty"`
}

// SecretOrConfigMap allows to specify data as a Secret or ConfigMap. Fields are mutually exclusive.
type SecretOrConfigMap struct {
	// Secret containing data to use for the targets.
	// +optional
	Secret *v1.SecretKeySelector `json:"secret,omitempty"`
	// ConfigMap containing data to use for the targets.
	// +optional
	ConfigMap *v1.ConfigMapKeySelector `json:"configMap,omitempty"`
}

// RelabelConfig allows dynamic rewriting of the label set, being applied to samples before ingestion.
// It defines `<metric_relabel_configs>`-section of configuration.
// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#metric_relabel_configs
// +k8s:openapi-gen=true
type RelabelConfig struct {
	//The source labels select values from existing labels. Their content is concatenated
	//using the configured separator and matched against the configured regular expression
	//for the replace, keep, and drop actions.
	// +optional
	SourceLabels []string `json:"sourceLabels,omitempty"`
	//Separator placed between concatenated source label values. default is ';'.
	// +optional
	Separator string `json:"separator,omitempty"`
	//Label to which the resulting value is written in a replace action.
	//It is mandatory for replace actions. Regex capture groups are available.
	// +optional
	TargetLabel string `json:"targetLabel,omitempty"`
	//Regular expression against which the extracted value is matched. Default is '(.*)'
	// +optional
	Regex string `json:"regex,omitempty"`
	// Modulus to take of the hash of the source label values.
	// +optional
	Modulus uint64 `json:"modulus,omitempty"`
	//Replacement value against which a regex replace is performed if the
	//regular expression matches. Regex capture groups are available. Default is '$1'
	// +optional
	Replacement string `json:"replacement,omitempty"`
	// Action to perform based on regex matching. Default is 'replace'
	// +optional
	Action string `json:"action,omitempty"`
}

// TLSConfigValidationError is returned by TLSConfig.Validate() on semantically
// invalid tls configurations.
// +k8s:openapi-gen=false
type TLSConfigValidationError struct {
	err string
}

func (e *TLSConfigValidationError) Error() string {
	return e.err
}

// Validate semantically validates the given TLSConfig.
func (c *TLSConfig) Validate() error {
	if c.CA != (SecretOrConfigMap{}) {
		if c.CAFile != "" {
			return &TLSConfigValidationError{"tls config can not both specify CAFile and CA"}
		}
		if err := c.CA.Validate(); err != nil {
			return err
		}
	}

	if c.Cert != (SecretOrConfigMap{}) {
		if c.CertFile != "" {
			return &TLSConfigValidationError{"tls config can not both specify CertFile and Cert"}
		}
		if err := c.Cert.Validate(); err != nil {
			return err
		}
	}

	if c.KeyFile != "" && c.KeySecret != nil {
		return &TLSConfigValidationError{"tls config can not both specify KeyFile and KeySecret"}
	}

	return nil
}

// SecretOrConfigMapValidationError is returned by SecretOrConfigMap.Validate()
// on semantically invalid configurations.
// +k8s:openapi-gen=false
type SecretOrConfigMapValidationError struct {
	err string
}

func (e *SecretOrConfigMapValidationError) Error() string {
	return e.err
}

// Validate semantically validates the given TLSConfig.
func (c *SecretOrConfigMap) Validate() error {
	if c.Secret != nil && c.ConfigMap != nil {
		return &SecretOrConfigMapValidationError{"SecretOrConfigMap can not specify both Secret and ConfigMap"}
	}

	return nil
}

func (c *SecretOrConfigMap) BuildSelectorWithPrefix(prefix string) string {
	if c.Secret != nil {
		return fmt.Sprintf("%s%s/%s", prefix, c.Secret.Name, c.Secret.Key)

	}
	if c.ConfigMap != nil {
		return fmt.Sprintf("%s%s/%s", prefix, c.ConfigMap.Name, c.ConfigMap.Key)
	}
	return ""
}

func (c *TLSConfig) BuildAssetPath(prefix, name, key string) string {
	return fmt.Sprintf("%s_%s_%s", prefix, name, key)
}

// RemoteWriteSpec defines the remote_write configuration.
// +k8s:openapi-gen=true
type RemoteWriteSpec struct {
	// The URL of the endpoint to send samples to.
	URL string `json:"url"`
	// The name of the remote write queue, must be unique if specified. The
	// name is used in metrics and logging in order to differentiate queues.
	// +optional
	Name string `json:"name,omitempty"`
	// Timeout for requests to the remote write endpoint.
	// +optional
	RemoteTimeout string `json:"remoteTimeout,omitempty"`
	// The list of remote write relabel configurations.
	// +optional
	WriteRelabelConfigs []RelabelConfig `json:"writeRelabelConfigs,omitempty"`
	//BasicAuth for the URL.
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// File to read bearer token for remote write.
	// +optional
	BearerToken string `json:"bearerToken,omitempty"`
	// File to read bearer token for remote write.
	// +optional
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// TLS Config to use for remote write.
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// Optional ProxyURL
	// +optional
	ProxyURL string `json:"proxyURL,omitempty"`
	// QueueConfig allows tuning of the remote write queue parameters.
	// +optional
	QueueConfig *QueueConfig `json:"queueConfig,omitempty"`
}

// QueueConfig allows the tuning of remote_write queue_config parameters. This object
// is referenced in the RemoteWriteSpec object.
// +k8s:openapi-gen=true
type QueueConfig struct {
	// Capacity is the number of samples to buffer per shard before we start dropping them.
	// +optional
	Capacity int `json:"capacity,omitempty"`
	// MinShards is the minimum number of shards, i.e. amount of concurrency.
	// +optional
	MinShards int `json:"minShards,omitempty"`
	// MaxShards is the maximum number of shards, i.e. amount of concurrency.
	// +optional
	MaxShards int `json:"maxShards,omitempty"`
	// MaxSamplesPerSend is the maximum number of samples per send.
	// +optional
	MaxSamplesPerSend int `json:"maxSamplesPerSend,omitempty"`
	// BatchSendDeadline is the maximum time a sample will wait in buffer.
	// +optional
	BatchSendDeadline string `json:"batchSendDeadline,omitempty"`
	// MaxRetries is the maximum number of times to retry a batch on recoverable errors.
	// +optional
	MaxRetries int `json:"maxRetries,omitempty"`
	// MinBackoff is the initial retry delay. Gets doubled for every retry.
	// +optional
	MinBackoff string `json:"minBackoff,omitempty"`
	// MaxBackoff is the maximum retry delay.
	// +optional
	MaxBackoff string `json:"maxBackoff,omitempty"`
}

// APIServerConfig defines a host and auth methods to access apiserver.
// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#kubernetes_sd_config
// +k8s:openapi-gen=true
type APIServerConfig struct {
	// Host of apiserver.
	// A valid string consisting of a hostname or IP followed by an optional port number
	Host string `json:"host"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Bearer token for accessing apiserver.
	// +optional
	BearerToken string `json:"bearerToken,omitempty"`
	// File to read bearer token for accessing apiserver.
	// +optional
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// TLS Config to use for accessing apiserver.
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
}

func init() {
	SchemeBuilder.Register(&VMServiceScrape{}, &VMServiceScrapeList{})
}
