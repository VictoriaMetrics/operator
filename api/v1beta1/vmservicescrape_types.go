package v1beta1

import (
	"encoding/json"
	"fmt"
	"reflect"

	v1 "k8s.io/api/core/v1"
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
	// SampleLimit defines per-endpoint limit on number of scraped samples that will be accepted.
	// +optional
	SampleLimit uint64 `json:"sampleLimit,omitempty"`
	// OAuth2 defines auth configuration
	// +optional
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`
	// Authorization with http header Authorization
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"`
	// TLSConfig configuration to use when scraping the endpoint
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// File to read bearer token for scraping targets.
	// +optional
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// Secret to mount to read bearer token for scraping targets. The secret
	// needs to be in the same namespace as the service scrape and accessible by
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
	// VMScrapeParams defines VictoriaMetrics specific scrape parametrs
	// +optional
	VMScrapeParams *VMScrapeParams `json:"vm_scrape_params,omitempty"`
	// AttachMetadata configures metadata attaching from service discovery
	// +optional
	AttachMetadata AttachMetadata `json:"attach_metadata,omitempty"`
}

// AttachMetadata configures metadata attachment
type AttachMetadata struct {
	// Node instructs vmagent to add node specific metadata from service discovery
	// Valid for roles: pod, endpoints, endpointslice.
	// +optional
	Node *bool `json:"node,omitempty"`
}

// VMScrapeParams defines scrape target configuration that compatible only with VictoriaMetrics scrapers
// VMAgent and VMSingle
type VMScrapeParams struct {
	// +optional
	RelabelDebug *bool `json:"relabel_debug,omitempty"`
	// +optional
	MetricRelabelDebug *bool `json:"metric_relabel_debug,omitempty"`
	// +optional
	DisableCompression *bool `json:"disable_compression,omitempty"`
	// +optional
	DisableKeepAlive *bool `json:"disable_keep_alive,omitempty"`
	// +optional
	DisableStaleMarkers *bool `json:"no_stale_markers,omitempty"`
	// +optional
	StreamParse *bool `json:"stream_parse,omitempty"`
	// +optional
	ScrapeAlignInterval *string `json:"scrape_align_interval,omitempty"`
	// +optional
	ScrapeOffset *string `json:"scrape_offset,omitempty"`
	// ProxyClientConfig configures proxy auth settings for scraping
	// See feature description https://docs.victoriametrics.com/vmagent.html#scraping-targets-via-a-proxy
	// +optional
	ProxyClientConfig *ProxyAuth `json:"proxy_client_config,omitempty"`
	// Headers allows sending custom headers to scrape targets
	// must be in of semicolon separated header with it's value
	// eg:
	// headerName: headerValue
	// vmagent supports since 1.79.0 version
	// +optional
	Headers []string `json:"headers,omitempty"`
}

// ProxyAuth represent proxy auth config
// Only VictoriaMetrics scrapers supports it.
// See https://github.com/VictoriaMetrics/VictoriaMetrics/commit/a6a71ef861444eb11fe8ec6d2387f0fc0c4aea87
type ProxyAuth struct {
	BasicAuth       *BasicAuth            `json:"basic_auth,omitempty"`
	BearerToken     *v1.SecretKeySelector `json:"bearer_token,omitempty"`
	BearerTokenFile string                `json:"bearer_token_file,omitempty"`
	TLSConfig       *TLSConfig            `json:"tls_config,omitempty"`
}

// OAuth2 defines OAuth2 configuration
type OAuth2 struct {
	// The secret or configmap containing the OAuth2 client id
	// +required
	ClientID SecretOrConfigMap `json:"client_id"`
	// The secret containing the OAuth2 client secret
	// +optional
	ClientSecret *v1.SecretKeySelector `json:"client_secret,omitempty"`
	// ClientSecretFile defines path for client secret file.
	// +optional
	ClientSecretFile string `json:"client_secret_file,omitempty"`
	// The URL to fetch the token from
	// +kubebuilder:validation:MinLength=1
	// +required
	TokenURL string `json:"token_url"`
	// OAuth2 scopes used for the token request
	// +optional
	Scopes []string `json:"scopes,omitempty"`
	// Parameters to append to the token URL
	// +optional
	EndpointParams map[string]string `json:"endpoint_params,omitempty"`
}

// Authorization configures generic authorization params
type Authorization struct {
	// Type of authorization, default to bearer
	// +optional
	Type string `json:"type,omitempty"`
	// Reference to the secret with value for authorization
	Credentials *v1.SecretKeySelector `json:"credentials,omitempty"`
	// File with value for authorization
	// +optional
	CredentialsFile string `json:"credentialsFile,omitempty"`
}

// TLSConfig specifies TLSConfig configuration parameters.
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

func (c *TLSConfig) AsArgs(args []string, prefix, pathPrefix string) []string {
	if c.CAFile != "" {
		args = append(args, fmt.Sprintf("-%s.tlsCAFile=%s", prefix, c.CAFile))
	} else if c.CA.Name() != "" {
		args = append(args, fmt.Sprintf("-%s.tlsCAFile=%s", prefix, c.BuildAssetPath(pathPrefix, c.CA.Name(), c.CA.Key())))
	}
	if c.CertFile != "" {
		args = append(args, fmt.Sprintf("-%s.tlsCertFile=%s", prefix, c.CertFile))
	} else if c.Cert.Name() != "" {
		args = append(args, fmt.Sprintf("-%s.tlsCertFile=%s", prefix, c.BuildAssetPath(pathPrefix, c.Cert.Name(), c.Cert.Key())))
	}
	if c.KeyFile != "" {
		args = append(args, fmt.Sprintf("-%s.tlsKeyFile=%s", prefix, c.KeyFile))
	} else if c.KeySecret != nil {
		args = append(args, fmt.Sprintf("-%s.tlsKeyFile=%s", prefix, c.BuildAssetPath(pathPrefix, c.KeySecret.Name, c.KeySecret.Key)))
	}
	if c.ServerName != "" {
		args = append(args, fmt.Sprintf("-%s.tlsServerName=%s", prefix, c.ServerName))
	}
	if c.InsecureSkipVerify {
		args = append(args, fmt.Sprintf("-%s.tlsInsecureSkipVerify=%v", prefix, c.InsecureSkipVerify))
	}
	return args
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
	// UnderScoreSourceLabels - additional form of source labels source_labels
	// for compatibility with original relabel config.
	// if set  both sourceLabels and source_labels, sourceLabels has priority.
	// for details https://github.com/VictoriaMetrics/operator/issues/131
	// +optional
	UnderScoreSourceLabels []string `json:"source_labels,omitempty" yaml:"source_labels,omitempty"`
	// UnderScoreTargetLabel - additional form of target label - target_label
	// for compatibility with original relabel config.
	// if set  both targetLabel and target_label, targetLabel has priority.
	// for details https://github.com/VictoriaMetrics/operator/issues/131
	// +optional
	UnderScoreTargetLabel string `json:"target_label,omitempty" yaml:"target_label,omitempty"`

	// The source labels select values from existing labels. Their content is concatenated
	// using the configured separator and matched against the configured regular expression
	// for the replace, keep, and drop actions.
	// +optional
	SourceLabels []string `json:"sourceLabels,omitempty" yaml:"-"`
	// Separator placed between concatenated source label values. default is ';'.
	// +optional
	Separator string `json:"separator,omitempty" yaml:"separator,omitempty"`
	// Label to which the resulting value is written in a replace action.
	// It is mandatory for replace actions. Regex capture groups are available.
	// +optional
	TargetLabel string `json:"targetLabel,omitempty" yaml:"-"`
	// Regular expression against which the extracted value is matched. Default is '(.*)'
	// +optional
	Regex string `json:"regex,omitempty" yaml:"regex,omitempty"`
	// Modulus to take of the hash of the source label values.
	// +optional
	Modulus uint64 `json:"modulus,omitempty" yaml:"modulus,omitempty"`
	// Replacement value against which a regex replace is performed if the
	// regular expression matches. Regex capture groups are available. Default is '$1'
	// +optional
	Replacement string `json:"replacement,omitempty" yaml:"replacement,omitempty"`
	// Action to perform based on regex matching. Default is 'replace'
	// +optional
	Action string `json:"action,omitempty" yaml:"action,omitempty"`
	// If represents metricsQL match expression (or list of expressions): '{__name__=~"foo_.*"}'
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	If StringOrArray `json:"if,omitempty" yaml:"if,omitempty"`
	// Match is used together with Labels for `action: graphite`
	// +optional
	Match string `json:"match,omitempty" yaml:"match,omitempty"`
	// Labels is used together with Match for `action: graphite`
	// +optional
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// UnmarshalJSON implements interface
// handles cases for snake and camel cases of json tags
func (rc *RelabelConfig) UnmarshalJSON(src []byte) error {
	type rcfg RelabelConfig
	if err := json.Unmarshal(src, (*rcfg)(rc)); err != nil {
		return fmt.Errorf("cannot parse relabelConfig: %w", err)
	}

	if len(rc.SourceLabels) == 0 && len(rc.UnderScoreSourceLabels) > 0 {
		rc.SourceLabels = append(rc.SourceLabels, rc.UnderScoreSourceLabels...)
	}
	if len(rc.UnderScoreSourceLabels) == 0 && len(rc.SourceLabels) > 0 {
		rc.UnderScoreSourceLabels = append(rc.UnderScoreSourceLabels, rc.SourceLabels...)
	}
	if rc.TargetLabel == "" && rc.UnderScoreTargetLabel != "" {
		rc.TargetLabel = rc.UnderScoreTargetLabel
	}
	if rc.UnderScoreTargetLabel == "" && rc.TargetLabel != "" {
		rc.UnderScoreTargetLabel = rc.TargetLabel
	}
	return nil
}

func (rc *RelabelConfig) IsEmpty() bool {
	if rc == nil {
		return true
	}
	return reflect.DeepEqual(*rc, RelabelConfig{})
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

func (c *SecretOrConfigMap) Name() string {
	if c.Secret != nil {
		return c.Secret.Name
	}
	if c.ConfigMap != nil {
		return c.ConfigMap.Name
	}
	return ""
}

func (c *SecretOrConfigMap) Key() string {
	if c.Secret != nil {
		return c.Secret.Key
	}
	if c.ConfigMap != nil {
		return c.ConfigMap.Key
	}
	return ""
}

func (c *TLSConfig) BuildAssetPath(prefix, name, key string) string {
	if name == "" || key == "" {
		return ""
	}
	return fmt.Sprintf("%s_%s_%s", prefix, name, key)
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
	// TLSConfig Config to use for accessing apiserver.
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"`
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
