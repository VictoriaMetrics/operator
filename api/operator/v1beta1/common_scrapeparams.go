package v1beta1

import (
	"fmt"
	"reflect"
	"strings"

	jsonv2 "github.com/go-json-experiment/json"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ScrapeClass struct {
	// name of the scrape class.
	//
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// default defines that the scrape applies to all scrape objects that
	// don't configure an explicit scrape class name.
	//
	// Only one scrape class can be set as the default.
	//
	// +optional
	Default *bool `json:"default,omitempty"`

	EndpointAuth        `json:",inline"`
	EndpointRelabelings `json:",inline"`

	// AttachMetadata defines additional metadata to the discovered targets.
	// When the scrape object defines its own configuration, it takes
	// precedence over the scrape class configuration.
	// +optional
	AttachMetadata *AttachMetadata `json:"attachMetadata,omitempty,case:ignore"`
}

// AttachMetadata configures metadata attachment
type AttachMetadata struct {
	// Node instructs vmagent or vmsingle to add node specific metadata from service discovery
	// Valid for roles: pod, endpoints, endpointslice.
	// +optional
	Node *bool `json:"node,omitempty"`

	// Namespace instructs vmagent or vmsingle to add namespace specific metadata from service discovery
	// Valid for roles: pod, service, endpoints, endpointslice, ingress.
	Namespace *bool `json:"namespace,omitempty"`
}

// VMScrapeParams defines scrape target configuration that compatible only with VictoriaMetrics scrapers
// VMAgent and VMSingle
type VMScrapeParams struct {
	// DisableCompression
	// +optional
	DisableCompression *bool `json:"disable_compression,omitempty,case:ignore"`
	// disable_keepalive allows disabling HTTP keep-alive when scraping targets.
	// By default, HTTP keep-alive is enabled, so TCP connections to scrape targets
	// could be reused.
	// See https://docs.victoriametrics.com/victoriametrics/vmagent/#scrape_config-enhancements
	// +optional
	DisableKeepAlive *bool `json:"disable_keep_alive,omitempty,case:ignore"`
	// +optional
	DisableStaleMarkers *bool `json:"no_stale_markers,omitempty,case:ignore"`
	// +optional
	StreamParse *bool `json:"stream_parse,omitempty,case:ignore"`
	// +optional
	ScrapeAlignInterval *string `json:"scrape_align_interval,omitempty,case:ignore"`
	// +optional
	ScrapeOffset *string `json:"scrape_offset,omitempty,case:ignore"`
	// ProxyClientConfig configures proxy auth settings for scraping
	// See feature description https://docs.victoriametrics.com/victoriametrics/vmagent/#scraping-targets-via-a-proxy
	// +optional
	ProxyClientConfig *ProxyClientConfig `json:"proxy_client_config,omitempty,case:ignore"`
	// Headers allows sending custom headers to scrape targets
	// must be in of semicolon separated header with it's value
	// eg:
	// headerName: headerValue
	// vmagent and vmsingle support since 1.79.0 version
	// +optional
	Headers []string `json:"headers,omitempty"`
}

// ProxyClientConfig represent proxy client config
type ProxyClientConfig struct {
	// OAuth2 defines auth configuration
	// +optional
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`
	// BasicAuth allows proxy to authenticate over basic authentication
	// +optional
	BasicAuth *BasicAuth `json:"basic_auth,omitempty,case:ignore"`
	// Secret to mount to read bearer token for scraping targets proxy auth. The secret
	// needs to be in the same namespace as the scrape object and accessible by
	// the victoria-metrics operator.
	// +optional
	// +nullable
	BearerToken *corev1.SecretKeySelector `json:"bearer_token,omitempty,case:ignore"`
	// BearerTokenFile defines file to read bearer token from for proxy auth.
	// +optional
	BearerTokenFile string `json:"bearer_token_file,omitempty,case:ignore"`
	// TLSConfig configuration to use when scraping the endpoint
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	TLSConfig *TLSConfig `json:"tls_config,omitempty,case:ignore"`
	// Authorization with http header Authorization
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"`
}

func (c *ProxyClientConfig) validateArbitraryFSAccess() error {
	if c == nil {
		return nil
	}
	var props []string
	if c.BearerTokenFile != "" {
		props = append(props, "bearer_token_file")
	}
	if c.BasicAuth != nil && c.BasicAuth.PasswordFile != "" {
		props = append(props, "basic_auth.passwordFile")
	}
	if c.OAuth2 != nil && c.OAuth2.ClientSecretFile != "" {
		props = append(props, "oauth2.clientSecretFile")
	}
	if c.Authorization != nil && c.Authorization.CredentialsFile != "" {
		props = append(props, "authorization.credentialsFile")
	}
	props = c.TLSConfig.appendForbiddenProperties(props)
	if len(props) > 0 {
		return fmt.Errorf("%s are prohibited", strings.Join(props, ", "))
	}
	return nil
}

// OAuth2 defines OAuth2 configuration
type OAuth2 struct {
	// The secret or configmap containing the OAuth2 client id
	// +required
	ClientID SecretOrConfigMap `json:"client_id,case:ignore" yaml:"client_id,omitempty"`
	// The secret containing the OAuth2 client secret
	// +optional
	ClientSecret *corev1.SecretKeySelector `json:"client_secret,omitempty,case:ignore" yaml:"client_secret,omitempty"`
	// ClientSecretFile defines path for client secret file.
	// +optional
	ClientSecretFile string `json:"client_secret_file,omitempty,case:ignore" yaml:"client_secret_file,omitempty"`
	// The URL to fetch the token from
	// +kubebuilder:validation:MinLength=1
	// +required
	TokenURL string `json:"token_url,case:ignore" yaml:"token_url"`
	// OAuth2 scopes used for the token request
	// +optional
	Scopes []string `json:"scopes,omitempty"`
	// Parameters to append to the token URL
	// +optional
	EndpointParams map[string]string `json:"endpoint_params,omitempty,case:ignore" yaml:"endpoint_params"`

	// The proxy URL for token_url connection
	// ( available from v0.55.0).
	// Is only supported by Scrape objects family
	// +optional
	ProxyURL string `json:"proxy_url,omitempty,case:ignore"`
	// TLSConfig for token_url connection
	// ( available from v0.55.0).
	// Is only supported by Scrape objects family
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	TLSConfig *TLSConfig `json:"tls_config,omitempty,case:ignore"`
}

func (o *OAuth2) validate() error {
	if o == nil {
		return nil
	}
	if o.TokenURL == "" {
		return fmt.Errorf("token_url field for oauth2 config must be set")
	}

	if o.ClientID == (SecretOrConfigMap{}) {
		return fmt.Errorf("client_id field must be set")
	}

	if o.ClientID.Secret != nil && o.ClientID.ConfigMap != nil {
		return fmt.Errorf("cannot specify both Secret and ConfigMap for client_id field")
	}
	if o.TLSConfig != nil {
		if err := o.TLSConfig.Validate(); err != nil {
			return fmt.Errorf("invalid tls_config: %w", err)
		}
	}
	return nil
}

// Authorization configures generic authorization params
type Authorization struct {
	// Type of authorization, default to bearer
	// +optional
	Type string `json:"type,omitempty"`
	// Reference to the secret with value for authorization
	Credentials *corev1.SecretKeySelector `json:"credentials,omitempty"`
	// File with value for authorization
	// +optional
	CredentialsFile string `json:"credentialsFile,omitempty,case:ignore" yaml:"credentials_file,omitempty"`
}

func (ac *Authorization) validate() error {
	if ac == nil {
		return nil
	}

	if strings.ToLower(strings.TrimSpace(ac.Type)) == "basic" {
		return fmt.Errorf("Authorization type cannot be set to 'basic', use 'basic_auth' instead`")
	}

	if ac.Credentials == nil && len(ac.CredentialsFile) == 0 {
		return fmt.Errorf("at least `credentials` or `credentials_file` must be set")
	}
	return nil
}

// RelabelConfig allows dynamic rewriting of the label set
// More info: https://docs.victoriametrics.com/victoriametrics/#relabeling
// +k8s:openapi-gen=true
// +kubebuilder:pruning:PreserveUnknownFields
type RelabelConfig struct {
	// The source labels select values from existing labels. Their content is concatenated
	// using the configured separator and matched against the configured regular expression
	// for the replace, keep, and drop actions.
	// +optional
	SourceLabels []string `json:"sourceLabels,omitempty,case:ignore" yaml:"source_labels,omitempty"`
	// Separator placed between concatenated source label values. default is ';'.
	// +optional
	Separator *string `json:"separator,omitempty" yaml:"separator,omitempty"`
	// Label to which the resulting value is written in a replace action.
	// It is mandatory for replace actions. Regex capture groups are available.
	// +optional
	TargetLabel string `json:"targetLabel,omitempty,case:ignore" yaml:"target_label,omitempty"`
	// Regular expression against which the extracted value is matched. Default is '(.*)'
	// victoriaMetrics supports multiline regex joined with |
	// https://docs.victoriametrics.com/victoriametrics/vmagent/#relabeling-enhancements
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	Regex StringOrArray `json:"regex,omitempty" yaml:"regex,omitempty"`
	// Modulus to take of the hash of the source label values.
	// +optional
	Modulus uint64 `json:"modulus,omitempty" yaml:"modulus,omitempty"`
	// Replacement value against which a regex replace is performed if the
	// regular expression matches. Regex capture groups are available. Default is '$1'
	// +optional
	Replacement *string `json:"replacement,omitempty" yaml:"replacement,omitempty"`
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

// UnmarshalJSON implements json.Unmarshaler.
// Both snake_case (source_labels, target_label) and camelCase (sourceLabels, targetLabel)
// field names are accepted, thanks to the case:ignore json tag option.
func (rc *RelabelConfig) UnmarshalJSON(src []byte) error {
	type rcfg RelabelConfig
	if err := jsonv2.Unmarshal(src, (*rcfg)(rc), jsonv2.MatchCaseInsensitiveNames(true)); err != nil {
		return fmt.Errorf("cannot parse relabelConfig: %w", err)
	}
	return nil
}

// IsEmpty checks if given relabelConfig has only empty values
func (rc *RelabelConfig) IsEmpty() bool {
	if rc == nil {
		return true
	}
	return reflect.DeepEqual(*rc, RelabelConfig{})
}

// EndpointScrapeParams defines common configuration params for all scrape endpoint targets
type EndpointScrapeParams struct {
	// HTTP path to scrape for metrics.
	// +optional
	Path string `json:"path,omitempty"`
	// HTTP scheme to use for scraping.
	// +optional
	// +kubebuilder:validation:Enum=http;https;HTTPS;HTTP
	Scheme string `json:"scheme,omitempty"`
	// Optional HTTP URL parameters
	// +optional
	Params map[string][]string `json:"params,omitempty"`
	// FollowRedirects controls redirects for scraping.
	// +optional
	FollowRedirects *bool `json:"follow_redirects,omitempty,case:ignore"`
	// SampleLimit defines per-scrape limit on number of scraped samples that will be accepted.
	// +optional
	SampleLimit int `json:"sampleLimit,omitempty,case:ignore"`
	// SeriesLimit defines per-scrape limit on number of unique time series
	// a single target can expose during all the scrapes on the time window of 24h.
	// +optional
	SeriesLimit int `json:"seriesLimit,omitempty,case:ignore"`
	// Interval at which metrics should be scraped
	// +optional
	Interval string `json:"interval,omitempty"`
	// ScrapeInterval is the same as Interval and has priority over it.
	// one of scrape_interval or interval can be used
	// +optional
	ScrapeInterval string `json:"scrape_interval,omitempty,case:ignore"`
	// Timeout after which the scrape is ended
	// +optional
	ScrapeTimeout string `json:"scrapeTimeout,omitempty,case:ignore"`
	// ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint.
	// +optional
	ProxyURL *string `json:"proxyURL,omitempty,case:ignore"`
	// HonorLabels chooses the metric's labels on collisions with target labels.
	// +optional
	HonorLabels bool `json:"honorLabels,omitempty,case:ignore"`
	// HonorTimestamps controls whether vmagent or vmsingle respects the timestamps present in scraped data.
	// +optional
	HonorTimestamps *bool `json:"honorTimestamps,omitempty,case:ignore"`
	// MaxScrapeSize defines a maximum size of scraped data for a job
	// +optional
	MaxScrapeSize string `json:"max_scrape_size,omitempty,case:ignore"`
	// VMScrapeParams defines VictoriaMetrics specific scrape parameters
	// +optional
	VMScrapeParams *VMScrapeParams `json:"vm_scrape_params,omitempty,case:ignore"`
	EndpointAuth   `json:",inline"`
}

func (p *EndpointScrapeParams) ValidateArbitraryFSAccess() error {
	if err := p.validateArbitraryFSAccess(); err != nil {
		return fmt.Errorf("endpoint auth contains prohibited properties for arbitrary filesystem access mode: %w", err)
	}
	if p.VMScrapeParams != nil {
		if err := p.VMScrapeParams.ProxyClientConfig.validateArbitraryFSAccess(); err != nil {
			return fmt.Errorf("endpoint proxy auth contains prohibited properties for arbitrary filesystem access mode: %w", err)
		}
	}
	return nil
}

// EndpointAuth defines target endpoint authorization options for scrapping
type EndpointAuth struct {
	// OAuth2 defines auth configuration
	// +optional
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`
	// TLSConfig configuration to use when scraping the endpoint
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty,case:ignore"`
	// File to read bearer token for scraping targets.
	// +optional
	BearerTokenFile string `json:"bearerTokenFile,omitempty,case:ignore"`
	// Secret to mount to read bearer token for scraping targets. The secret
	// needs to be in the same namespace as the scrape object and accessible by
	// the victoria-metrics operator.
	// +optional
	// +nullable
	BearerTokenSecret *corev1.SecretKeySelector `json:"bearerTokenSecret,omitempty,case:ignore"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty,case:ignore"`
	// Authorization with http header Authorization
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"`
}

func (a *EndpointAuth) validateArbitraryFSAccess() error {
	var props []string
	if a.BearerTokenFile != "" {
		props = append(props, "bearerTokenFile")
	}
	if a.BasicAuth != nil && a.BasicAuth.PasswordFile != "" {
		props = append(props, "basicAuth.passwordFile")
	}
	if a.OAuth2 != nil && a.OAuth2.ClientSecretFile != "" {
		props = append(props, "oauth2.clientSecretFile")
	}
	if a.Authorization != nil && a.Authorization.CredentialsFile != "" {
		props = append(props, "authorization.credentialsFile")
	}
	if a.TLSConfig != nil {
		tls := a.TLSConfig
		if err := tls.Validate(); err != nil {
			return err
		}
		if tls.CAFile != "" {
			props = append(props, "tlsConfig.caFile")
		}
		if tls.CertFile != "" {
			props = append(props, "tlsConfig.certFile")
		}
		if tls.KeyFile != "" {
			props = append(props, "tlsConfig.keyFile")
		}
	}
	if len(props) > 0 {
		return fmt.Errorf("%s are prohibited", strings.Join(props, ", "))
	}
	return nil
}

// EndpointRelabelings defines service discovery and metrics relabeling configuration for endpoints
type EndpointRelabelings struct {
	// MetricRelabelConfigs to apply to samples after scrapping.
	// +optional
	MetricRelabelConfigs []*RelabelConfig `json:"metricRelabelConfigs,omitempty,case:ignore"`
	// RelabelConfigs to apply to samples during service discovery.
	// +optional
	RelabelConfigs []*RelabelConfig `json:"relabelConfigs,omitempty,case:ignore"`
}

func (r *EndpointRelabelings) validate() error {
	if err := checkRelabelConfigs(r.RelabelConfigs); err != nil {
		return fmt.Errorf("invalid relabelConfigs: %w", err)
	}
	if err := checkRelabelConfigs(r.MetricRelabelConfigs); err != nil {
		return fmt.Errorf("invalid metricRelabelConfigs: %w", err)
	}
	return nil
}

// CommonScrapeSecurityEnforcements defines security configuration for endpoint scrapping
type CommonScrapeSecurityEnforcements struct {
	// OverrideHonorLabels if set to true overrides all user configured honor_labels.
	// If HonorLabels is set in scrape objects to true, this overrides honor_labels to false.
	// +optional
	OverrideHonorLabels bool `json:"overrideHonorLabels,omitempty,case:ignore"`
	// OverrideHonorTimestamps allows to globally enforce honoring timestamps in all scrape configs.
	// +optional
	OverrideHonorTimestamps bool `json:"overrideHonorTimestamps,omitempty,case:ignore"`
	// IgnoreNamespaceSelectors if set to true will ignore NamespaceSelector settings from
	// scrape objects, and they will only discover endpoints
	// within their current namespace. Defaults to false.
	// +optional
	IgnoreNamespaceSelectors bool `json:"ignoreNamespaceSelectors,omitempty,case:ignore"`
	// EnforcedNamespaceLabel enforces adding a namespace label of origin for each alert
	// and metric that is user created. The label value will always be the namespace of the object that is
	// being created.
	// +optional
	EnforcedNamespaceLabel string `json:"enforcedNamespaceLabel,omitempty,case:ignore"`
	// ArbitraryFSAccessThroughSMs configures whether configuration
	// based on EndpointAuth can access arbitrary files on the file system
	// of the VMAgent or VMSingle container e.g. bearer token files, basic auth, tls certs
	// +optional
	ArbitraryFSAccessThroughSMs ArbitraryFSAccessThroughSMsConfig `json:"arbitraryFSAccessThroughSMs,omitempty,case:ignore"`
}

type CommonScrapeParams struct {
	// GlobalScrapeMetricRelabelConfigs is a global metric relabel configuration, which is applied to each scrape job.
	// +optional
	GlobalScrapeMetricRelabelConfigs []*RelabelConfig `json:"globalScrapeMetricRelabelConfigs,omitempty,case:ignore"`
	// GlobalScrapeRelabelConfigs is a global relabel configuration, which is applied to each samples of each scrape job during service discovery.
	// +optional
	GlobalScrapeRelabelConfigs []*RelabelConfig `json:"globalScrapeRelabelConfigs,omitempty,case:ignore"`
	// ScrapeInterval defines how often scrape targets by default
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	ScrapeInterval string `json:"scrapeInterval,omitempty,case:ignore"`
	// ScrapeTimeout defines global timeout for targets scrape
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	ScrapeTimeout string `json:"scrapeTimeout,omitempty,case:ignore"`
	// SampleLimit defines global per target limit of scraped samples
	// +optional
	SampleLimit int `json:"sampleLimit,omitempty,case:ignore"`
	// SelectAllByDefault changes default behavior for empty CRD selectors, such ServiceScrapeSelector.
	// with selectAllByDefault: true and empty serviceScrapeSelector and ServiceScrapeNamespaceSelector
	// Operator selects all exist serviceScrapes
	// with selectAllByDefault: false - selects nothing
	// +optional
	SelectAllByDefault bool `json:"selectAllByDefault,omitempty,case:ignore"`
	// ServiceScrapeSelector defines ServiceScrapes to be selected for target discovery.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent or VMSingle namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ServiceScrapeSelector *metav1.LabelSelector `json:"serviceScrapeSelector,omitempty,case:ignore"`
	// ServiceScrapeNamespaceSelector Namespaces to be selected for VMServiceScrape discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent or VMSingle namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ServiceScrapeNamespaceSelector *metav1.LabelSelector `json:"serviceScrapeNamespaceSelector,omitempty,case:ignore"`
	// PodScrapeSelector defines PodScrapes to be selected for target discovery.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent or VMSingle namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	PodScrapeSelector *metav1.LabelSelector `json:"podScrapeSelector,omitempty,case:ignore"`
	// PodScrapeNamespaceSelector defines Namespaces to be selected for VMPodScrape discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent or VMSingle namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	PodScrapeNamespaceSelector *metav1.LabelSelector `json:"podScrapeNamespaceSelector,omitempty,case:ignore"`
	// ProbeSelector defines VMProbe to be selected for target probing.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent or VMSingle namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ProbeSelector *metav1.LabelSelector `json:"probeSelector,omitempty,case:ignore"`
	// ProbeNamespaceSelector defines Namespaces to be selected for VMProbe discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent or VMSingle namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ProbeNamespaceSelector *metav1.LabelSelector `json:"probeNamespaceSelector,omitempty,case:ignore"`
	// NodeScrapeSelector defines VMNodeScrape to be selected for scraping.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent or VMSingle namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	NodeScrapeSelector *metav1.LabelSelector `json:"nodeScrapeSelector,omitempty,case:ignore"`
	// NodeScrapeNamespaceSelector defines Namespaces to be selected for VMNodeScrape discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent or VMSingle namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	NodeScrapeNamespaceSelector *metav1.LabelSelector `json:"nodeScrapeNamespaceSelector,omitempty,case:ignore"`
	// StaticScrapeSelector defines VMStaticScrape to be selected for target discovery.
	// Works in combination with NamespaceSelector.
	// If both nil - match everything.
	// NamespaceSelector nil - only objects at VMAgent or VMSingle namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// +optional
	StaticScrapeSelector *metav1.LabelSelector `json:"staticScrapeSelector,omitempty,case:ignore"`
	// StaticScrapeNamespaceSelector defines Namespaces to be selected for VMStaticScrape discovery.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent or VMSingle namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	StaticScrapeNamespaceSelector *metav1.LabelSelector `json:"staticScrapeNamespaceSelector,omitempty,case:ignore"`
	// ScrapeConfigSelector defines VMScrapeConfig to be selected for target discovery.
	// Works in combination with NamespaceSelector.
	// +optional
	ScrapeConfigSelector *metav1.LabelSelector `json:"scrapeConfigSelector,omitempty,case:ignore"`
	// ScrapeConfigNamespaceSelector defines Namespaces to be selected for VMScrapeConfig discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent or VMSingle namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ScrapeConfigNamespaceSelector *metav1.LabelSelector `json:"scrapeConfigNamespaceSelector,omitempty,case:ignore"`
	// InlineScrapeConfig As scrape configs are appended, the user is responsible to make sure it
	// is valid. Note that using this feature may expose the possibility to
	// break upgrades of VMAgent or VMSingle. It is advised to review VMAgent or VMSingle release
	// notes to ensure that no incompatible scrape configs are going to break
	// VMAgent or VMSingle after the upgrade.
	// it should be defined as single yaml file.
	// inlineScrapeConfig: |
	//     - job_name: "prometheus"
	//       static_configs:
	//       - targets: ["localhost:9090"]
	// +optional
	InlineScrapeConfig string `json:"inlineScrapeConfig,omitempty,case:ignore"`
	// AdditionalScrapeConfigs As scrape configs are appended, the user is responsible to make sure it
	// is valid. Note that using this feature may expose the possibility to
	// break upgrades of VMAgent or VMSingle. It is advised to review VMAgent or VMSingle release
	// notes to ensure that no incompatible scrape configs are going to break
	// VMAgent or VMSingle after the upgrade.
	// +optional
	AdditionalScrapeConfigs *corev1.SecretKeySelector `json:"additionalScrapeConfigs,omitempty,case:ignore"`
	// ServiceScrapeRelabelTemplate defines relabel config, that will be added to each VMServiceScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	ServiceScrapeRelabelTemplate []*RelabelConfig `json:"serviceScrapeRelabelTemplate,omitempty,case:ignore"`
	// PodScrapeRelabelTemplate defines relabel config, that will be added to each VMPodScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	PodScrapeRelabelTemplate []*RelabelConfig `json:"podScrapeRelabelTemplate,omitempty,case:ignore"`
	// NodeScrapeRelabelTemplate defines relabel config, that will be added to each VMNodeScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	NodeScrapeRelabelTemplate []*RelabelConfig `json:"nodeScrapeRelabelTemplate,omitempty,case:ignore"`
	// StaticScrapeRelabelTemplate defines relabel config, that will be added to each VMStaticScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	StaticScrapeRelabelTemplate []*RelabelConfig `json:"staticScrapeRelabelTemplate,omitempty,case:ignore"`
	// ProbeScrapeRelabelTemplate defines relabel config, that will be added to each VMProbeScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	ProbeScrapeRelabelTemplate []*RelabelConfig `json:"probeScrapeRelabelTemplate,omitempty,case:ignore"`
	// ScrapeConfigRelabelTemplate defines relabel config, that will be added to each VMScrapeConfig.
	// it's useful for adding specific labels to all targets
	// +optional
	ScrapeConfigRelabelTemplate []*RelabelConfig `json:"scrapeConfigRelabelTemplate,omitempty,case:ignore"`
	// MinScrapeInterval allows limiting minimal scrape interval for VMServiceScrape, VMPodScrape and other scrapes
	// If interval is lower than defined limit, `minScrapeInterval` will be used.
	MinScrapeInterval *string `json:"minScrapeInterval,omitempty,case:ignore"`
	// ScrapeClasses defines the list of scrape classes to expose to scraping objects such as
	// PodScrapes, ServiceScrapes, Probes and ScrapeConfigs.
	// +listType=map
	// +listMapKey=name
	// +optional
	ScrapeClasses []ScrapeClass `json:"scrapeClasses,omitempty,case:ignore"`
	// MaxScrapeInterval allows limiting maximum scrape interval for VMServiceScrape, VMPodScrape and other scrapes
	// If interval is higher than defined limit, `maxScrapeInterval` will be used.
	MaxScrapeInterval *string `json:"maxScrapeInterval,omitempty,case:ignore"`
	// VMAgentExternalLabelName Name of vmAgent external label used to denote vmAgent instance
	// name. Defaults to the value of `prometheus`. External label will
	// _not_ be added when value is set to empty string (`""`).
	// +deprecated={deprecated_in: "v0.67.0", removed_in: "v0.69.0", replacements: {externalLabelName}}
	// +optional
	VMAgentExternalLabelName *string `json:"vmAgentExternalLabelName,omitempty,case:ignore"`
	// ExternalLabelName Name of external label used to denote scraping agent instance
	// name. Defaults to the value of `prometheus`. External label will
	// _not_ be added when value is set to empty string (`""`).
	// +optional
	ExternalLabelName *string `json:"externalLabelName,omitempty,case:ignore"`
	// ExternalLabels The labels to add to any time series scraped by vmagent or vmsingle.
	// it doesn't affect metrics ingested directly by push API's
	// +optional
	ExternalLabels map[string]string `json:"externalLabels,omitempty,case:ignore"`
	// IngestOnlyMode switches vmagent or vmsingle into unmanaged mode
	// it disables any config generation for scraping
	// Currently it prevents vmagent or vmsingle from managing tls and auth options for remote write
	// +optional
	IngestOnlyMode *bool `json:"ingestOnlyMode,omitempty,case:ignore"`
	// EnableKubernetesAPISelectors instructs vmagent or vmsingle to use CRD scrape objects spec.selectors for
	// Kubernetes API list and watch requests.
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#list-and-watch-filtering
	// It could be useful to reduce Kubernetes API server resource usage for serving less than 100 CRD scrape objects in total.
	// +optional
	EnableKubernetesAPISelectors     bool `json:"enableKubernetesAPISelectors,omitempty,case:ignore"`
	CommonScrapeSecurityEnforcements `json:",inline"`
}

func (cr *CommonScrapeParams) externalLabelName() string {
	// Use "prometheus" external label name by default if field is missing.
	// in case of migration from prometheus to vmagent, it helps to have same labels
	// Do not add external label if field is set to empty string.
	if cr.ExternalLabelName != nil {
		return *cr.ExternalLabelName
	}
	if cr.VMAgentExternalLabelName != nil {
		return *cr.VMAgentExternalLabelName
	}
	return "prometheus"
}

func (cr *CommonScrapeParams) externalLabels(defaultLabelValue string) map[string]string {
	m := map[string]string{}

	prometheusExternalLabelName := cr.externalLabelName()
	if len(prometheusExternalLabelName) > 0 {
		m[prometheusExternalLabelName] = defaultLabelValue
	}

	for n, v := range cr.ExternalLabels {
		m[n] = v
	}
	return m
}

// ScrapeSelectors gets object and namespace sepectors
func (cr *CommonScrapeParams) ScrapeSelectors(scrape client.Object) (*metav1.LabelSelector, *metav1.LabelSelector) {
	switch s := scrape.(type) {
	case *VMNodeScrape:
		return cr.NodeScrapeSelector, cr.NodeScrapeNamespaceSelector
	case *VMServiceScrape:
		return cr.ServiceScrapeSelector, cr.ServiceScrapeNamespaceSelector
	case *VMPodScrape:
		return cr.PodScrapeSelector, cr.PodScrapeNamespaceSelector
	case *VMProbe:
		return cr.ProbeSelector, cr.ProbeNamespaceSelector
	case *VMStaticScrape:
		return cr.StaticScrapeSelector, cr.StaticScrapeNamespaceSelector
	case *VMScrapeConfig:
		return cr.ScrapeConfigSelector, cr.ScrapeConfigNamespaceSelector
	default:
		panic(fmt.Sprintf("BUG: scrape kind %T is not supported", s))
	}
}

// isUnmanaged checks if object should managed any config objects
func (cr *CommonScrapeParams) isUnmanaged() bool {
	if ptr.Deref(cr.IngestOnlyMode, false) {
		return true
	}
	return !cr.SelectAllByDefault &&
		cr.NodeScrapeSelector == nil && cr.NodeScrapeNamespaceSelector == nil &&
		cr.ServiceScrapeSelector == nil && cr.ServiceScrapeNamespaceSelector == nil &&
		cr.PodScrapeSelector == nil && cr.PodScrapeNamespaceSelector == nil &&
		cr.ProbeSelector == nil && cr.ProbeNamespaceSelector == nil &&
		cr.StaticScrapeSelector == nil && cr.StaticScrapeNamespaceSelector == nil &&
		cr.ScrapeConfigSelector == nil && cr.ScrapeConfigNamespaceSelector == nil
}

// isNodeScrapeUnmanaged checks if scraping agent should managed any VMNodeScrape objects
func (cr *CommonScrapeParams) isNodeScrapeUnmanaged() bool {
	if ptr.Deref(cr.IngestOnlyMode, false) {
		return true
	}
	return !cr.SelectAllByDefault &&
		cr.NodeScrapeSelector == nil && cr.NodeScrapeNamespaceSelector == nil
}

// isServiceScrapeUnmanaged checks if scraping agent should managed any VMServiceScrape objects
func (cr *CommonScrapeParams) isServiceScrapeUnmanaged() bool {
	if ptr.Deref(cr.IngestOnlyMode, false) {
		return true
	}
	return !cr.SelectAllByDefault &&
		cr.ServiceScrapeSelector == nil && cr.ServiceScrapeNamespaceSelector == nil
}

// isUnmanaged checks if scraping agent should managed any VMPodScrape objects
func (cr *CommonScrapeParams) isPodScrapeUnmanaged() bool {
	if ptr.Deref(cr.IngestOnlyMode, false) {
		return true
	}
	return !cr.SelectAllByDefault &&
		cr.PodScrapeSelector == nil && cr.PodScrapeNamespaceSelector == nil
}

// isProbeUnmanaged checks if scraping agent should managed any VMProbe objects
func (cr *CommonScrapeParams) isProbeUnmanaged() bool {
	if ptr.Deref(cr.IngestOnlyMode, false) {
		return true
	}
	return !cr.SelectAllByDefault &&
		cr.ProbeSelector == nil && cr.ProbeNamespaceSelector == nil
}

// isStaticScrapeUnmanaged checks if scraping agent should managed any VMStaticScrape objects
func (cr *CommonScrapeParams) isStaticScrapeUnmanaged() bool {
	if ptr.Deref(cr.IngestOnlyMode, false) {
		return true
	}
	return !cr.SelectAllByDefault &&
		cr.StaticScrapeSelector == nil && cr.StaticScrapeNamespaceSelector == nil
}

// isScrapeConfigUnmanaged checks if scraping agent should managed any VMScrapeConfig objects
func (cr *CommonScrapeParams) isScrapeConfigUnmanaged() bool {
	if ptr.Deref(cr.IngestOnlyMode, false) {
		return true
	}
	return !cr.SelectAllByDefault &&
		cr.ScrapeConfigSelector == nil && cr.ScrapeConfigNamespaceSelector == nil
}
