package v1beta1

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	v1 "k8s.io/api/core/v1"
)

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
	// DisableCompression
	// +optional
	DisableCompression *bool `json:"disable_compression,omitempty"`
	// disable_keepalive allows disabling HTTP keep-alive when scraping targets.
	// By default, HTTP keep-alive is enabled, so TCP connections to scrape targets
	// could be reused.
	// See https://docs.victoriametrics.com/vmagent#scrape_config-enhancements
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
	// See feature description https://docs.victoriametrics.com/vmagent#scraping-targets-via-a-proxy
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
	ClientSecret *v1.SecretKeySelector `json:"client_secret,omitempty" yaml:"client_secret,omitempty"`
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
	Credentials *v1.SecretKeySelector `json:"credentials,omitempty"`
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
// More info: https://docs.victoriametrics.com/#relabeling
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
	Separator *string `json:"separator,omitempty" yaml:"separator,omitempty"`
	// Label to which the resulting value is written in a replace action.
	// It is mandatory for replace actions. Regex capture groups are available.
	// +optional
	TargetLabel string `json:"targetLabel,omitempty" yaml:"-"`
	// Regular expression against which the extracted value is matched. Default is '(.*)'
	// victoriaMetrics supports multiline regex joined with |
	// https://docs.victoriametrics.com/vmagent/#relabeling-enhancements
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
	SampleLimit uint64 `json:"sampleLimit,omitempty"`
	// SeriesLimit defines per-scrape limit on number of unique time series
	// a single target can expose during all the scrapes on the time window of 24h.
	// +optional
	SeriesLimit uint64 `json:"seriesLimit,omitempty"`
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
	BearerTokenSecret *v1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
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
