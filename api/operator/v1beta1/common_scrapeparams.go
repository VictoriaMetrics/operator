package v1beta1

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AttachMetadata configures metadata attachment
type AttachMetadata struct {
	// Node instructs vmagent to add node specific metadata from service discovery
	// Valid for roles: pod, endpoints, endpointslice.
	// +optional
	Node *bool `json:"node,omitempty"`

	// Namespace instructs vmagent to add namespace specific metadata from service discovery
	// Valid for roles: pod, service, endpoints, endpointslice, ingress.
	Namespace *bool `json:"namespace,omitempty"`
}

// VMScrapeParams defines scrape target configuration that compatible only with VictoriaMetrics scrapers
// VMAgent and VMSingle
type VMScrapeParams struct {
	// DisableCompression
	// +optional
	DisableCompression *bool `json:"disable_compression,omitempty"`
	// disable_keepalive allows disabling HTTP keep-alive when scraping targets.
	// By default, HTTP keep-alive is enabled, so TCP connections to scrape targets
	// could be reused.
	// See https://docs.victoriametrics.com/victoriametrics/vmagent/#scrape_config-enhancements
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
	// See feature description https://docs.victoriametrics.com/victoriametrics/vmagent/#scraping-targets-via-a-proxy
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
	BasicAuth       *BasicAuth                `json:"basic_auth,omitempty"`
	BearerToken     *corev1.SecretKeySelector `json:"bearer_token,omitempty"`
	BearerTokenFile string                    `json:"bearer_token_file,omitempty"`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	TLSConfig *TLSConfig `json:"tls_config,omitempty"`
}

// OAuth2 defines OAuth2 configuration
type OAuth2 struct {
	// The secret or configmap containing the OAuth2 client id
	// +required
	ClientID SecretOrConfigMap `json:"client_id" yaml:"client_id,omitempty"`
	// The secret containing the OAuth2 client secret
	// +optional
	ClientSecret *corev1.SecretKeySelector `json:"client_secret,omitempty" yaml:"client_secret,omitempty"`
	// ClientSecretFile defines path for client secret file.
	// +optional
	ClientSecretFile string `json:"client_secret_file,omitempty" yaml:"client_secret_file,omitempty"`
	// The URL to fetch the token from
	// +kubebuilder:validation:MinLength=1
	// +required
	TokenURL string `json:"token_url" yaml:"token_url"`
	// OAuth2 scopes used for the token request
	// +optional
	Scopes []string `json:"scopes,omitempty"`
	// Parameters to append to the token URL
	// +optional
	EndpointParams map[string]string `json:"endpoint_params,omitempty" yaml:"endpoint_params"`

	// The proxy URL for token_url connection
	// ( available from v0.55.0).
	// Is only supported by Scrape objects family
	// +optional
	ProxyURL string `json:"proxy_url,omitempty"`
	// TLSConfig for token_url connection
	// ( available from v0.55.0).
	// Is only supported by Scrape objects family
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	TLSConfig *TLSConfig `json:"tls_config,omitempty"`
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
		if err := o.validate(); err != nil {
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
	CredentialsFile string `json:"credentialsFile,omitempty" yaml:"credentials_file,omitempty"`
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
type RelabelConfig struct {
	// UnderScoreSourceLabels - additional form of source labels source_labels
	// for compatibility with original relabel config.
	// if set both sourceLabels and source_labels, sourceLabels has priority.
	// for details https://github.com/VictoriaMetrics/operator/issues/131
	// +optional
	UnderScoreSourceLabels []string `json:"source_labels,omitempty" yaml:"source_labels,omitempty"`
	// UnderScoreTargetLabel - additional form of target label - target_label
	// for compatibility with original relabel config.
	// if set both targetLabel and target_label, targetLabel has priority.
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
	Separator *string `json:"separator,omitempty" yaml:"separator,omitempty"`
	// Label to which the resulting value is written in a replace action.
	// It is mandatory for replace actions. Regex capture groups are available.
	// +optional
	TargetLabel string `json:"targetLabel,omitempty" yaml:"-"`
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

// IsEmpty checks if given relabelConfig has only empty values
func (rc *RelabelConfig) IsEmpty() bool {
	if rc == nil {
		return true
	}
	return reflect.DeepEqual(*rc, RelabelConfig{})
}

// ScrapeTargetParams defines common configuration params for all scrape endpoint targets
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
	FollowRedirects *bool `json:"follow_redirects,omitempty"`
	// SampleLimit defines per-scrape limit on number of scraped samples that will be accepted.
	// +optional
	SampleLimit int `json:"sampleLimit,omitempty"`
	// SeriesLimit defines per-scrape limit on number of unique time series
	// a single target can expose during all the scrapes on the time window of 24h.
	// +optional
	SeriesLimit int `json:"seriesLimit,omitempty"`
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
	// ProxyURL eg http://proxyserver:2195 Directs scrapes to proxy through this endpoint.
	// +optional
	ProxyURL *string `json:"proxyURL,omitempty"`
	// HonorLabels chooses the metric's labels on collisions with target labels.
	// +optional
	HonorLabels bool `json:"honorLabels,omitempty"`
	// HonorTimestamps controls whether vmagent respects the timestamps present in scraped data.
	// +optional
	HonorTimestamps *bool `json:"honorTimestamps,omitempty"`
	// MaxScrapeSize defines a maximum size of scraped data for a job
	// +optional
	MaxScrapeSize string `json:"max_scrape_size,omitempty"`
	// VMScrapeParams defines VictoriaMetrics specific scrape parameters
	// +optional
	VMScrapeParams *VMScrapeParams `json:"vm_scrape_params,omitempty"`
}

// EndpointAuth defines target endpoint authorization options for scrapping
type EndpointAuth struct {
	// OAuth2 defines auth configuration
	// +optional
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`
	// TLSConfig configuration to use when scraping the endpoint
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// File to read bearer token for scraping targets.
	// +optional
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// Secret to mount to read bearer token for scraping targets. The secret
	// needs to be in the same namespace as the scrape object and accessible by
	// the victoria-metrics operator.
	// +optional
	// +nullable
	BearerTokenSecret *corev1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Authorization with http header Authorization
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"`
}

// EndpointRelabelings defines service discovery and metrics relabeling configuration for endpoints
type EndpointRelabelings struct {
	// MetricRelabelConfigs to apply to samples after scrapping.
	// +optional
	MetricRelabelConfigs []*RelabelConfig `json:"metricRelabelConfigs,omitempty"`
	// RelabelConfigs to apply to samples during service discovery.
	// +optional
	RelabelConfigs []*RelabelConfig `json:"relabelConfigs,omitempty"`
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
	OverrideHonorLabels bool `json:"overrideHonorLabels,omitempty"`
	// OverrideHonorTimestamps allows to globally enforce honoring timestamps in all scrape configs.
	// +optional
	OverrideHonorTimestamps bool `json:"overrideHonorTimestamps,omitempty"`
	// IgnoreNamespaceSelectors if set to true will ignore NamespaceSelector settings from
	// scrape objects, and they will only discover endpoints
	// within their current namespace. Defaults to false.
	// +optional
	IgnoreNamespaceSelectors bool `json:"ignoreNamespaceSelectors,omitempty"`
	// EnforcedNamespaceLabel enforces adding a namespace label of origin for each alert
	// and metric that is user created. The label value will always be the namespace of the object that is
	// being created.
	// +optional
	EnforcedNamespaceLabel string `json:"enforcedNamespaceLabel,omitempty"`
	// ArbitraryFSAccessThroughSMs configures whether configuration
	// based on EndpointAuth can access arbitrary files on the file system
	// of the VMAgent container e.g. bearer token files, basic auth, tls certs
	// +optional
	ArbitraryFSAccessThroughSMs ArbitraryFSAccessThroughSMsConfig `json:"arbitraryFSAccessThroughSMs,omitempty"`
}

type CommonScrapeParams struct {
	// GlobalScrapeMetricRelabelConfigs is a global metric relabel configuration, which is applied to each scrape job.
	// +optional
	GlobalScrapeMetricRelabelConfigs []*RelabelConfig `json:"globalScrapeMetricRelabelConfigs,omitempty"`
	// GlobalScrapeRelabelConfigs is a global relabel configuration, which is applied to each samples of each scrape job during service discovery.
	// +optional
	GlobalScrapeRelabelConfigs []*RelabelConfig `json:"globalScrapeRelabelConfigs,omitempty"`
	// ScrapeInterval defines how often scrape targets by default
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	ScrapeInterval string `json:"scrapeInterval,omitempty"`
	// ScrapeTimeout defines global timeout for targets scrape
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	ScrapeTimeout string `json:"scrapeTimeout,omitempty"`
	// SampleLimit defines global per target limit of scraped samples
	// +optional
	SampleLimit int `json:"sampleLimit,omitempty"`
	// SelectAllByDefault changes default behavior for empty CRD selectors, such ServiceScrapeSelector.
	// with selectAllByDefault: true and empty serviceScrapeSelector and ServiceScrapeNamespaceSelector
	// Operator selects all exist serviceScrapes
	// with selectAllByDefault: false - selects nothing
	// +optional
	SelectAllByDefault bool `json:"selectAllByDefault,omitempty"`
	// ServiceScrapeSelector defines ServiceScrapes to be selected for target discovery.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ServiceScrapeSelector *metav1.LabelSelector `json:"serviceScrapeSelector,omitempty"`
	// ServiceScrapeNamespaceSelector Namespaces to be selected for VMServiceScrape discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ServiceScrapeNamespaceSelector *metav1.LabelSelector `json:"serviceScrapeNamespaceSelector,omitempty"`
	// PodScrapeSelector defines PodScrapes to be selected for target discovery.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	PodScrapeSelector *metav1.LabelSelector `json:"podScrapeSelector,omitempty"`
	// PodScrapeNamespaceSelector defines Namespaces to be selected for VMPodScrape discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	PodScrapeNamespaceSelector *metav1.LabelSelector `json:"podScrapeNamespaceSelector,omitempty"`
	// ProbeSelector defines VMProbe to be selected for target probing.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ProbeSelector *metav1.LabelSelector `json:"probeSelector,omitempty"`
	// ProbeNamespaceSelector defines Namespaces to be selected for VMProbe discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ProbeNamespaceSelector *metav1.LabelSelector `json:"probeNamespaceSelector,omitempty"`
	// NodeScrapeSelector defines VMNodeScrape to be selected for scraping.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	NodeScrapeSelector *metav1.LabelSelector `json:"nodeScrapeSelector,omitempty"`
	// NodeScrapeNamespaceSelector defines Namespaces to be selected for VMNodeScrape discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	NodeScrapeNamespaceSelector *metav1.LabelSelector `json:"nodeScrapeNamespaceSelector,omitempty"`
	// StaticScrapeSelector defines VMStaticScrape to be selected for target discovery.
	// Works in combination with NamespaceSelector.
	// If both nil - match everything.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// +optional
	StaticScrapeSelector *metav1.LabelSelector `json:"staticScrapeSelector,omitempty"`
	// StaticScrapeNamespaceSelector defines Namespaces to be selected for VMStaticScrape discovery.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	StaticScrapeNamespaceSelector *metav1.LabelSelector `json:"staticScrapeNamespaceSelector,omitempty"`
	// ScrapeConfigSelector defines VMScrapeConfig to be selected for target discovery.
	// Works in combination with NamespaceSelector.
	// +optional
	ScrapeConfigSelector *metav1.LabelSelector `json:"scrapeConfigSelector,omitempty"`
	// ScrapeConfigNamespaceSelector defines Namespaces to be selected for VMScrapeConfig discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ScrapeConfigNamespaceSelector *metav1.LabelSelector `json:"scrapeConfigNamespaceSelector,omitempty"`
	// InlineScrapeConfig As scrape configs are appended, the user is responsible to make sure it
	// is valid. Note that using this feature may expose the possibility to
	// break upgrades of VMAgent. It is advised to review VMAgent release
	// notes to ensure that no incompatible scrape configs are going to break
	// VMAgent after the upgrade.
	// it should be defined as single yaml file.
	// inlineScrapeConfig: |
	//     - job_name: "prometheus"
	//       static_configs:
	//       - targets: ["localhost:9090"]
	// +optional
	InlineScrapeConfig string `json:"inlineScrapeConfig,omitempty"`
	// AdditionalScrapeConfigs As scrape configs are appended, the user is responsible to make sure it
	// is valid. Note that using this feature may expose the possibility to
	// break upgrades of VMAgent. It is advised to review VMAgent release
	// notes to ensure that no incompatible scrape configs are going to break
	// VMAgent after the upgrade.
	// +optional
	AdditionalScrapeConfigs *corev1.SecretKeySelector `json:"additionalScrapeConfigs,omitempty"`
	// ServiceScrapeRelabelTemplate defines relabel config, that will be added to each VMServiceScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	ServiceScrapeRelabelTemplate []*RelabelConfig `json:"serviceScrapeRelabelTemplate,omitempty"`
	// PodScrapeRelabelTemplate defines relabel config, that will be added to each VMPodScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	PodScrapeRelabelTemplate []*RelabelConfig `json:"podScrapeRelabelTemplate,omitempty"`
	// NodeScrapeRelabelTemplate defines relabel config, that will be added to each VMNodeScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	NodeScrapeRelabelTemplate []*RelabelConfig `json:"nodeScrapeRelabelTemplate,omitempty"`
	// StaticScrapeRelabelTemplate defines relabel config, that will be added to each VMStaticScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	StaticScrapeRelabelTemplate []*RelabelConfig `json:"staticScrapeRelabelTemplate,omitempty"`
	// ProbeScrapeRelabelTemplate defines relabel config, that will be added to each VMProbeScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	ProbeScrapeRelabelTemplate []*RelabelConfig `json:"probeScrapeRelabelTemplate,omitempty"`
	// ScrapeConfigRelabelTemplate defines relabel config, that will be added to each VMScrapeConfig.
	// it's useful for adding specific labels to all targets
	// +optional
	ScrapeConfigRelabelTemplate []*RelabelConfig `json:"scrapeConfigRelabelTemplate,omitempty"`
	// MinScrapeInterval allows limiting minimal scrape interval for VMServiceScrape, VMPodScrape and other scrapes
	// If interval is lower than defined limit, `minScrapeInterval` will be used.
	MinScrapeInterval *string `json:"minScrapeInterval,omitempty"`
	// ScrapeClasses defines the list of scrape classes to expose to scraping objects such as
	// PodScrapes, ServiceScrapes, Probes and ScrapeConfigs.
	// +listType=map
	// +listMapKey=name
	// +optional
	ScrapeClasses []ScrapeClass `json:"scrapeClasses,omitempty"`
	// MaxScrapeInterval allows limiting maximum scrape interval for VMServiceScrape, VMPodScrape and other scrapes
	// If interval is higher than defined limit, `maxScrapeInterval` will be used.
	MaxScrapeInterval *string `json:"maxScrapeInterval,omitempty"`
	// VMAgentExternalLabelName Name of vmAgent external label used to denote vmAgent instance
	// name. Defaults to the value of `prometheus`. External label will
	// _not_ be added when value is set to empty string (`""`).
	// +deprecated={deprecated_in: "v0.67.0", removed_in: "v0.69.0", replacements: {externalLabelName}}
	// +optional
	VMAgentExternalLabelName *string `json:"vmAgentExternalLabelName,omitempty"`
	// ExternalLabelName Name of external label used to denote scraping agent instance
	// name. Defaults to the value of `prometheus`. External label will
	// _not_ be added when value is set to empty string (`""`).
	// +optional
	ExternalLabelName *string `json:"externalLabelName,omitempty"`
	// ExternalLabels The labels to add to any time series scraped by vmagent.
	// it doesn't affect metrics ingested directly by push API's
	// +optional
	ExternalLabels map[string]string `json:"externalLabels,omitempty"`
	// IngestOnlyMode switches vmagent into unmanaged mode
	// it disables any config generation for scraping
	// Currently it prevents vmagent from managing tls and auth options for remote write
	// +optional
	IngestOnlyMode *bool `json:"ingestOnlyMode,omitempty"`
	// EnableKubernetesAPISelectors instructs vmagent to use CRD scrape objects spec.selectors for
	// Kubernetes API list and watch requests.
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#list-and-watch-filtering
	// It could be useful to reduce Kubernetes API server resource usage for serving less than 100 CRD scrape objects in total.
	// +optional
	EnableKubernetesAPISelectors     bool `json:"enableKubernetesAPISelectors,omitempty"`
	CommonScrapeSecurityEnforcements `json:",inline,omitempty"`
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
