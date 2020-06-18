package v1

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ServiceMonitorSpec contains specification parameters for a ServiceMonitor.
// +k8s:openapi-gen=true
type ServiceMonitorSpec struct {
	// The label to use to retrieve the job name from.
	JobLabel string `json:"jobLabel,omitempty"`
	// TargetLabels transfers labels on the Kubernetes Service onto the target.
	TargetLabels []string `json:"targetLabels,omitempty"`
	// PodTargetLabels transfers labels on the Kubernetes Pod onto the target.
	PodTargetLabels []string `json:"podTargetLabels,omitempty"`
	// A list of endpoints allowed as part of this ServiceMonitor.
	Endpoints []Endpoint `json:"endpoints"`
	// Selector to select Endpoints objects.
	Selector metav1.LabelSelector `json:"selector"`
	// Selector to select which namespaces the Endpoints objects are discovered from.
	NamespaceSelector NamespaceSelector `json:"namespaceSelector,omitempty"`
	// SampleLimit defines per-scrape limit on number of scraped samples that will be accepted.
	SampleLimit uint64 `json:"sampleLimit,omitempty"`
}

// ServiceMonitorStatus defines the observed state of ServiceMonitor
// +k8s:openapi-gen=true
type ServiceMonitorStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// ServiceMonitor is the Schema for the servicemonitors API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=servicemonitors,scope=Namespaced
// ServiceMonitor defines monitoring for a set of services.
// +genclient
// +k8s:openapi-gen=true
type ServiceMonitor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Specification of desired Service selection for target discovery by
	// Prometheus.
	Spec ServiceMonitorSpec `json:"spec"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServiceMonitorList contains a list of ServiceMonitor
type ServiceMonitorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceMonitor `json:"items"`
}

// NamespaceSelector is a selector for selecting either all namespaces or a
// list of namespaces.
// +k8s:openapi-gen=true
type NamespaceSelector struct {
	// Boolean describing whether all namespaces are selected in contrast to a
	// list restricting them.
	Any bool `json:"any,omitempty"`
	// List of namespace names.
	MatchNames []string `json:"matchNames,omitempty"`

	// TODO(fabxc): this should embed metav1.LabelSelector eventually.
	// Currently the selector is only used for namespaces which require more complex
	// implementation to support label selections.
}

// Endpoint defines a scrapeable endpoint serving Prometheus metrics.
// +k8s:openapi-gen=true
type Endpoint struct {
	// Name of the service port this endpoint refers to. Mutually exclusive with targetPort.
	Port string `json:"port,omitempty"`
	// Name or number of the pod port this endpoint refers to. Mutually exclusive with port.
	TargetPort *intstr.IntOrString `json:"targetPort,omitempty"`
	// HTTP path to scrape for metrics.
	Path string `json:"path,omitempty"`
	// HTTP scheme to use for scraping.
	Scheme string `json:"scheme,omitempty"`
	// Optional HTTP URL parameters
	Params map[string][]string `json:"params,omitempty"`
	// Interval at which metrics should be scraped
	Interval string `json:"interval,omitempty"`
	// Timeout after which the scrape is ended
	ScrapeTimeout string `json:"scrapeTimeout,omitempty"`
	// TLS configuration to use when scraping the endpoint
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// File to read bearer token for scraping targets.
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// Secret to mount to read bearer token for scraping targets. The secret
	// needs to be in the same namespace as the service monitor and accessible by
	// the Prometheus Operator.
	BearerTokenSecret v1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
	// HonorLabels chooses the metric's labels on collisions with target labels.
	HonorLabels bool `json:"honorLabels,omitempty"`
	// HonorTimestamps controls whether Prometheus respects the timestamps present in scraped data.
	HonorTimestamps *bool `json:"honorTimestamps,omitempty"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// More info: https://prometheus.io/docs/operating/configuration/#endpoints
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// MetricRelabelConfigs to apply to samples before ingestion.
	MetricRelabelConfigs []*RelabelConfig `json:"metricRelabelConfigs,omitempty"`
	// RelabelConfigs to apply to samples before scraping.
	// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config
	RelabelConfigs []*RelabelConfig `json:"relabelConfigs,omitempty"`
	// ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint.
	ProxyURL *string `json:"proxyURL,omitempty"`
}

// TLSConfig specifies TLS configuration parameters.
// +k8s:openapi-gen=true
type TLSConfig struct {
	// Path to the CA cert in the Prometheus container to use for the targets.
	CAFile string `json:"caFile,omitempty"`
	// Stuct containing the CA cert to use for the targets.
	CA SecretOrConfigMap `json:"ca,omitempty"`

	// Path to the client cert file in the Prometheus container for the targets.
	CertFile string `json:"certFile,omitempty"`
	// Struct containing the client cert file for the targets.
	Cert SecretOrConfigMap `json:"cert,omitempty"`

	// Path to the client key file in the Prometheus container for the targets.
	KeyFile string `json:"keyFile,omitempty"`
	// Secret containing the client key file for the targets.
	KeySecret *v1.SecretKeySelector `json:"keySecret,omitempty"`

	// Used to verify the hostname for the targets.
	ServerName string `json:"serverName,omitempty"`
	// Disable target certificate validation.
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// BasicAuth allow an endpoint to authenticate over basic authentication
// More info: https://prometheus.io/docs/operating/configuration/#endpoints
// +k8s:openapi-gen=true
type BasicAuth struct {
	// The secret in the service monitor namespace that contains the username
	// for authentication.
	Username v1.SecretKeySelector `json:"username,omitempty"`
	// The secret in the service monitor namespace that contains the password
	// for authentication.
	Password v1.SecretKeySelector `json:"password,omitempty"`
}

// SecretOrConfigMap allows to specify data as a Secret or ConfigMap. Fields are mutually exclusive.
type SecretOrConfigMap struct {
	// Secret containing data to use for the targets.
	Secret *v1.SecretKeySelector `json:"secret,omitempty"`
	// ConfigMap containing data to use for the targets.
	ConfigMap *v1.ConfigMapKeySelector `json:"configMap,omitempty"`
}

// RelabelConfig allows dynamic rewriting of the label set, being applied to samples before ingestion.
// It defines `<metric_relabel_configs>`-section of Prometheus configuration.
// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#metric_relabel_configs
// +k8s:openapi-gen=true
type RelabelConfig struct {
	//The source labels select values from existing labels. Their content is concatenated
	//using the configured separator and matched against the configured regular expression
	//for the replace, keep, and drop actions.
	SourceLabels []string `json:"sourceLabels,omitempty"`
	//Separator placed between concatenated source label values. default is ';'.
	Separator string `json:"separator,omitempty"`
	//Label to which the resulting value is written in a replace action.
	//It is mandatory for replace actions. Regex capture groups are available.
	TargetLabel string `json:"targetLabel,omitempty"`
	//Regular expression against which the extracted value is matched. Default is '(.*)'
	Regex string `json:"regex,omitempty"`
	// Modulus to take of the hash of the source label values.
	Modulus uint64 `json:"modulus,omitempty"`
	//Replacement value against which a regex replace is performed if the
	//regular expression matches. Regex capture groups are available. Default is '$1'
	Replacement string `json:"replacement,omitempty"`
	// Action to perform based on regex matching. Default is 'replace'
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

// RemoteWriteSpec defines the remote_write configuration for prometheus.
// +k8s:openapi-gen=true
type RemoteWriteSpec struct {
	// The URL of the endpoint to send samples to.
	URL string `json:"url"`
	// The name of the remote write queue, must be unique if specified. The
	// name is used in metrics and logging in order to differentiate queues.
	// Only valid in Prometheus versions 2.15.0 and newer.
	Name string `json:"name,omitempty"`
	// Timeout for requests to the remote write endpoint.
	RemoteTimeout string `json:"remoteTimeout,omitempty"`
	// The list of remote write relabel configurations.
	WriteRelabelConfigs []RelabelConfig `json:"writeRelabelConfigs,omitempty"`
	//BasicAuth for the URL.
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// File to read bearer token for remote write.
	BearerToken string `json:"bearerToken,omitempty"`
	// File to read bearer token for remote write.
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// TLS Config to use for remote write.
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// Optional ProxyURL
	ProxyURL string `json:"proxyURL,omitempty"`
	// QueueConfig allows tuning of the remote write queue parameters.
	QueueConfig *QueueConfig `json:"queueConfig,omitempty"`
}

// QueueConfig allows the tuning of remote_write queue_config parameters. This object
// is referenced in the RemoteWriteSpec object.
// +k8s:openapi-gen=true
type QueueConfig struct {
	// Capacity is the number of samples to buffer per shard before we start dropping them.
	Capacity int `json:"capacity,omitempty"`
	// MinShards is the minimum number of shards, i.e. amount of concurrency.
	MinShards int `json:"minShards,omitempty"`
	// MaxShards is the maximum number of shards, i.e. amount of concurrency.
	MaxShards int `json:"maxShards,omitempty"`
	// MaxSamplesPerSend is the maximum number of samples per send.
	MaxSamplesPerSend int `json:"maxSamplesPerSend,omitempty"`
	// BatchSendDeadline is the maximum time a sample will wait in buffer.
	BatchSendDeadline string `json:"batchSendDeadline,omitempty"`
	// MaxRetries is the maximum number of times to retry a batch on recoverable errors.
	MaxRetries int `json:"maxRetries,omitempty"`
	// MinBackoff is the initial retry delay. Gets doubled for every retry.
	MinBackoff string `json:"minBackoff,omitempty"`
	// MaxBackoff is the maximum retry delay.
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
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Bearer token for accessing apiserver.
	BearerToken string `json:"bearerToken,omitempty"`
	// File to read bearer token for accessing apiserver.
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// TLS Config to use for accessing apiserver.
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
}

// PodMonitor defines monitoring for a set of pods.
// +genclient
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:path=podmonitors,scope=Namespaced
type PodMonitor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Specification of desired Pod selection for target discovery by Prometheus.
	Spec PodMonitorSpec `json:"spec"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// PodMonitorList contains a list of PodMonitor
type PodMonitorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodMonitor `json:"items"`
}

// PodMonitorSpec contains specification parameters for a PodMonitor.
// +k8s:openapi-gen=true
type PodMonitorSpec struct {
	// The label to use to retrieve the job name from.
	JobLabel string `json:"jobLabel,omitempty"`
	// PodTargetLabels transfers labels on the Kubernetes Pod onto the target.
	PodTargetLabels []string `json:"podTargetLabels,omitempty"`
	// A list of endpoints allowed as part of this PodMonitor.
	PodMetricsEndpoints []PodMetricsEndpoint `json:"podMetricsEndpoints"`
	// Selector to select Pod objects.
	Selector metav1.LabelSelector `json:"selector"`
	// Selector to select which namespaces the Endpoints objects are discovered from.
	NamespaceSelector NamespaceSelector `json:"namespaceSelector,omitempty"`
	// SampleLimit defines per-scrape limit on number of scraped samples that will be accepted.
	SampleLimit uint64 `json:"sampleLimit,omitempty"`
}

// PodMetricsEndpoint defines a scrapeable endpoint of a Kubernetes Pod serving Prometheus metrics.
// +k8s:openapi-gen=true
type PodMetricsEndpoint struct {
	// Name of the pod port this endpoint refers to. Mutually exclusive with targetPort.
	Port string `json:"port,omitempty"`
	// Deprecated: Use 'port' instead.
	TargetPort *intstr.IntOrString `json:"targetPort,omitempty"`
	// HTTP path to scrape for metrics.
	Path string `json:"path,omitempty"`
	// HTTP scheme to use for scraping.
	Scheme string `json:"scheme,omitempty"`
	// Optional HTTP URL parameters
	Params map[string][]string `json:"params,omitempty"`
	// Interval at which metrics should be scraped
	Interval string `json:"interval,omitempty"`
	// Timeout after which the scrape is ended
	ScrapeTimeout string `json:"scrapeTimeout,omitempty"`
	// HonorLabels chooses the metric's labels on collisions with target labels.
	HonorLabels bool `json:"honorLabels,omitempty"`
	// HonorTimestamps controls whether Prometheus respects the timestamps present in scraped data.
	HonorTimestamps *bool `json:"honorTimestamps,omitempty"`
	// MetricRelabelConfigs to apply to samples before ingestion.
	MetricRelabelConfigs []*RelabelConfig `json:"metricRelabelConfigs,omitempty"`
	// RelabelConfigs to apply to samples before ingestion.
	// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config
	RelabelConfigs []*RelabelConfig `json:"relabelConfigs,omitempty"`
	// ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint.
	ProxyURL *string `json:"proxyURL,omitempty"`
}

// ArbitraryFSAccessThroughSMsConfig enables users to configure, whether
// a service monitor selected by the Prometheus instance is allowed to use
// arbitrary files on the file system of the Prometheus container. This is the case
// when e.g. a service monitor specifies a BearerTokenFile in an endpoint. A
// malicious user could create a service monitor selecting arbitrary secret files
// in the Prometheus container. Those secrets would then be sent with a scrape
// request by Prometheus to a malicious target. Denying the above would prevent the
// attack, users can instead use the BearerTokenSecret field.
type ArbitraryFSAccessThroughSMsConfig struct {
	Deny bool `json:"deny,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ServiceMonitor{}, &ServiceMonitorList{}, &PodMonitorList{}, &PodMonitor{})
}
