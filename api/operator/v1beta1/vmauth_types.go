package v1beta1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	v12 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var labelNameRegexp = regexp.MustCompile("^[a-zA-Z_:.][a-zA-Z0-9_:.]*$")

// VMAuthSpec defines the desired state of VMAuth
type VMAuthSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// PodMetadata configures Labels and Annotations which are propagated to the VMAuth pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty" yaml:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *ManagedObjectsMetadata `json:"managedMetadata,omitempty" yaml:"managedMetadata,omitempty"`
	// LogLevel for victoria metrics single to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty" yaml:"logLevel,omitempty"`
	// LogFormat for VMAuth to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty" yaml:"logFormat,omitempty"`
	// SelectAllByDefault changes default behavior for empty CRD selectors, such userSelector.
	// with selectAllByDefault: true and empty userSelector and userNamespaceSelector
	// Operator selects all exist users
	// with selectAllByDefault: false - selects nothing
	// +optional
	SelectAllByDefault bool `json:"selectAllByDefault,omitempty" yaml:"selectAllByDefault,omitempty"`
	// UserSelector defines VMUser to be selected for config file generation.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAuth namespace.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	UserSelector *metav1.LabelSelector `json:"userSelector,omitempty" yaml:"userSelector,omitempty"`
	// UserNamespaceSelector Namespaces to be selected for  VMAuth discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAuth namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	UserNamespaceSelector *metav1.LabelSelector `json:"userNamespaceSelector,omitempty" yaml:"userNamespaceSelector,omitempty"`

	// ServiceSpec that will be added to vmsingle service spec
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty" yaml:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmauth VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty" yaml:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty" yaml:"podDisruptionBudget,omitempty"`
	// Ingress enables ingress configuration for VMAuth.
	Ingress *EmbeddedIngress `json:"ingress,omitempty"`
	// LivenessProbe that will be added to VMAuth pod
	*EmbeddedProbes `json:",inline"`
	// UnauthorizedAccessConfig configures access for un authorized users
	//
	// Deprecated, use unauthorizedUserAccessSpec instead
	// will be removed at v1.0 release
	// +deprecated
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	UnauthorizedAccessConfig []UnauthorizedAccessConfigURLMap `json:"unauthorizedAccessConfig,omitempty" yaml:"unauthorizedAccessConfig,omitempty"`
	// UnauthorizedUserAccessSpec defines unauthorized_user config section of vmauth config
	// +optional
	UnauthorizedUserAccessSpec *VMAuthUnauthorizedUserAccessSpec `json:"unauthorizedUserAccessSpec,omitempty" yaml:"unauthorizedUserAccessSpec,omitempty"`
	// IPFilters global access ip filters
	// supported only with enterprise version of [vmauth](https://docs.victoriametrics.com/vmauth/#ip-filters)
	// +optional
	// will be added after removal of VMUserConfigOptions
	// currently it has collision with inlined fields
	// IPFilters VMUserIPFilters `json:"ip_filters,omitempty"`
	// will be removed at v1.0 release
	// +deprecated
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	VMUserConfigOptions `json:",inline" yaml:",inline"`
	// License allows to configure license key to be used for enterprise features.
	// Using license key is supported starting from VictoriaMetrics v1.94.0.
	// See [here](https://docs.victoriametrics.com/enterprise)
	// +optional
	License *License `json:"license,omitempty"`
	// ConfigSecret is the name of a Kubernetes Secret in the same namespace as the
	// VMAuth object, which contains auth configuration for vmauth,
	// configuration must be inside secret key: config.yaml.
	// It must be created and managed manually.
	// If it's defined, configuration for vmauth becomes unmanaged and operator'll not create any related secrets/config-reloaders
	// Deprecated, use externalConfig.secretRef instead
	ConfigSecret string `json:"configSecret,omitempty" yaml:"configSecret,omitempty"`
	// ExternalConfig defines a source of external VMAuth configuration.
	// If it's defined, configuration for vmauth becomes unmanaged and operator'll not create any related secrets/config-reloaders
	// +optional
	ExternalConfig `json:"externalConfig,omitempty" yaml:"externalConfig,omitempty"`

	*ServiceAccount `json:",inline,omitempty"`

	CommonDefaultableParams           `json:",inline,omitempty" yaml:",inline"`
	CommonConfigReloaderParams        `json:",inline,omitempty" yaml:",inline"`
	CommonApplicationDeploymentParams `json:",inline,omitempty" yaml:",inline"`
}

// VMAuthUnauthorizedUserAccessSpec defines unauthorized_user section configuration for vmauth
type VMAuthUnauthorizedUserAccessSpec struct {
	// URLPrefix defines prefix prefix for destination
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	URLPrefix StringOrArray                    `json:"url_prefix,omitempty" yaml:"url_prefix,omitempty"`
	URLMap    []UnauthorizedAccessConfigURLMap `json:"url_map,omitempty" yaml:"url_map,omitempty"`

	VMUserConfigOptions `json:",inline" yaml:",inline"`
	// MetricLabels - additional labels for metrics exported by vmauth for given user.
	// +optional
	MetricLabels map[string]string `json:"metric_labels,omitempty" yaml:"metric_labels"`
}

// Validate performs semantic syntax validation
func (r *VMAuthUnauthorizedUserAccessSpec) Validate() error {
	if len(r.URLMap) == 0 && len(r.URLPrefix) == 0 {
		return fmt.Errorf("at least one of `url_map` or `url_prefix` must be defined")
	}
	for idx, urlMap := range r.URLMap {
		if err := urlMap.validate(); err != nil {
			return fmt.Errorf("incorrect url_map at idx=%d: %w", idx, err)
		}
	}
	for _, urlPrefix := range r.URLPrefix {
		if err := validateURLPrefix(urlPrefix); err != nil {
			return err
		}
	}
	if r.TLSConfig != nil {
		if err := r.TLSConfig.Validate(); err != nil {
			return fmt.Errorf("incorrect tlsConfig for UnauthorizedUserAccess: %w", err)
		}
	}
	for k := range r.MetricLabels {
		if !labelNameRegexp.Match([]byte(k)) {
			return fmt.Errorf("incorrect metricLabelName=%q, must match pattern=%q", k, labelNameRegexp)
		}
	}
	if err := r.VMUserConfigOptions.validate(); err != nil {
		return fmt.Errorf("incorrect UnauthorizedUserAccess options: %w", err)
	}

	return nil
}

// UnauthorizedAccessConfigURLMap defines element of url_map routing configuration
// For UnauthorizedAccessConfig and VMAuthUnauthorizedUserAccessSpec.URLMap
type UnauthorizedAccessConfigURLMap struct {
	// SrcPaths is an optional list of regular expressions, which must match the request path.
	SrcPaths []string `json:"src_paths,omitempty" yaml:"src_paths,omitempty"`

	// SrcHosts is an optional list of regular expressions, which must match the request hostname.
	SrcHosts []string `json:"src_hosts,omitempty" yaml:"src_hosts,omitempty"`

	// UrlPrefix contains backend url prefixes for the proxied request url.
	// URLPrefix defines prefix prefix for destination
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	URLPrefix StringOrArray `json:"url_prefix,omitempty" yaml:"url_prefix,omitempty"`

	URLMapCommon `json:",omitempty" yaml:",inline"`
}

// Validate performs syntax logic validation
func (r *UnauthorizedAccessConfigURLMap) validate() error {
	if len(r.SrcPaths) == 0 && len(r.SrcHosts) == 0 && len(r.SrcQueryArgs) == 0 && len(r.URLMapCommon.SrcQueryArgs) == 0 {
		return fmt.Errorf("incorrect url_map config at least of one src_paths,src_hosts,src_query_args or src_headers must be defined")
	}
	if len(r.URLPrefix) == 0 {
		return fmt.Errorf("url_prefix cannot be empty for url_map")
	}
	for idx, urlPrefix := range r.URLPrefix {
		if err := validateURLPrefix(urlPrefix); err != nil {
			return fmt.Errorf("incorrect url_prefix=%q at idx: %d: %w", urlPrefix, idx, err)
		}
	}
	return nil
}

func validateURLPrefix(urlPrefixStr string) error {
	urlPrefix, err := url.Parse(urlPrefixStr)
	if err != nil {
		return err
	}
	// Validate urlPrefix
	if urlPrefix.Scheme != "http" && urlPrefix.Scheme != "https" {
		return fmt.Errorf("unsupported scheme for `url_prefix: %q`: %q; must be `http` or `https`", urlPrefix, urlPrefix.Scheme)
	}
	if urlPrefix.Host == "" {
		return fmt.Errorf("missing hostname in `url_prefix %q`", urlPrefix)
	}
	return nil
}

// URLMapCommon contains common fields for unauthorized user and user in vmuser
type URLMapCommon struct {
	// SrcQueryArgs is an optional list of query args, which must match request URL query args.
	SrcQueryArgs []string `json:"src_query_args,omitempty" yaml:"src_query_args,omitempty"`

	// SrcHeaders is an optional list of headers, which must match request headers.
	SrcHeaders []string `json:"src_headers,omitempty" yaml:"src_headers,omitempty"`

	// DiscoverBackendIPs instructs discovering URLPrefix backend IPs via DNS.
	DiscoverBackendIPs *bool `json:"discover_backend_ips,omitempty" yaml:"discover_backend_ips,omitempty"`

	// RequestHeaders represent additional http headers, that vmauth uses
	// in form of ["header_key: header_value"]
	// multiple values for header key:
	// ["header_key: value1,value2"]
	// it's available since 1.68.0 version of vmauth
	// +optional
	RequestHeaders []string `json:"headers,omitempty" yaml:"headers,omitempty"`
	// ResponseHeaders represent additional http headers, that vmauth adds for request response
	// in form of ["header_key: header_value"]
	// multiple values for header key:
	// ["header_key: value1,value2"]
	// it's available since 1.93.0 version of vmauth
	// +optional
	ResponseHeaders []string `json:"response_headers,omitempty" yaml:"response_headers,omitempty"`

	// RetryStatusCodes defines http status codes in numeric format for request retries
	// Can be defined per target or at VMUser.spec level
	// e.g. [429,503]
	// +optional
	RetryStatusCodes []int `json:"retry_status_codes,omitempty" yaml:"retry_status_codes,omitempty"`

	// LoadBalancingPolicy defines load balancing policy to use for backend urls.
	// Supported policies: least_loaded, first_available.
	// See [here](https://docs.victoriametrics.com/vmauth#load-balancing) for more details (default "least_loaded")
	// +optional
	// +kubebuilder:validation:Enum=least_loaded;first_available
	LoadBalancingPolicy *string `json:"load_balancing_policy,omitempty" yaml:"load_balancing_policy,omitempty"`

	// DropSrcPathPrefixParts is the number of `/`-delimited request path prefix parts to drop before proxying the request to backend.
	// See [here](https://docs.victoriametrics.com/vmauth#dropping-request-path-prefix) for more details.
	// +optional
	DropSrcPathPrefixParts *int `json:"drop_src_path_prefix_parts,omitempty" yaml:"drop_src_path_prefix_parts,omitempty"`
}

// VMUserConfigOptions defines configuration options for VMUser object
type VMUserConfigOptions struct {
	// DefaultURLs backend url for non-matching paths filter
	// usually used for default backend with error message
	DefaultURLs []string `json:"default_url,omitempty" yaml:"default_url,omitempty"`

	// TLSConfig defines tls configuration for the backend connection
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty" yaml:"tlsConfig,omitempty"`

	// IPFilters defines per target src ip filters
	// supported only with enterprise version of [vmauth](https://docs.victoriametrics.com/vmauth/#ip-filters)
	// +optional
	IPFilters VMUserIPFilters `json:"ip_filters,omitempty" yaml:"ip_filters,omitempty"`

	// DiscoverBackendIPs instructs discovering URLPrefix backend IPs via DNS.
	DiscoverBackendIPs *bool `json:"discover_backend_ips,omitempty" yaml:"discover_backend_ips,omitempty"`

	// Headers represent additional http headers, that vmauth uses
	// in form of ["header_key: header_value"]
	// multiple values for header key:
	// ["header_key: value1,value2"]
	// it's available since 1.68.0 version of vmauth
	// +optional
	Headers []string `json:"headers,omitempty"`
	// ResponseHeaders represent additional http headers, that vmauth adds for request response
	// in form of ["header_key: header_value"]
	// multiple values for header key:
	// ["header_key: value1,value2"]
	// it's available since 1.93.0 version of vmauth
	// +optional
	ResponseHeaders []string `json:"response_headers,omitempty" yaml:"response_headers,omitempty"`

	// RetryStatusCodes defines http status codes in numeric format for request retries
	// e.g. [429,503]
	// +optional
	RetryStatusCodes []int `json:"retry_status_codes,omitempty" yaml:"retry_status_codes,omitempty"`

	// MaxConcurrentRequests defines max concurrent requests per user
	// 300 is default value for vmauth
	// +optional
	MaxConcurrentRequests *int `json:"max_concurrent_requests,omitempty" yaml:"max_concurrent_requests,omitempty"`

	// LoadBalancingPolicy defines load balancing policy to use for backend urls.
	// Supported policies: least_loaded, first_available.
	// See [here](https://docs.victoriametrics.com/vmauth#load-balancing) for more details (default "least_loaded")
	// +optional
	// +kubebuilder:validation:Enum=least_loaded;first_available
	LoadBalancingPolicy *string `json:"load_balancing_policy,omitempty" yaml:"load_balancing_policy,omitempty"`

	// DropSrcPathPrefixParts is the number of `/`-delimited request path prefix parts to drop before proxying the request to backend.
	// See [here](https://docs.victoriametrics.com/vmauth#dropping-request-path-prefix) for more details.
	// +optional
	DropSrcPathPrefixParts *int `json:"drop_src_path_prefix_parts,omitempty" yaml:"drop_src_path_prefix_parts,omitempty"`

	// DumpRequestOnErrors instructs vmauth to return detailed request params to the client
	// if routing rules don't allow to forward request to the backends.
	// Useful for debugging `src_hosts` and `src_headers` based routing rules
	//
	// available since v1.107.0 vmauth version
	// +optional
	DumpRequestOnErrors *bool `json:"dump_request_on_errors,omitempty"`
}

// Validate performs semantic syntax validation
func (r *VMUserConfigOptions) validate() error {
	for _, durl := range r.DefaultURLs {
		if err := validateURLPrefix(durl); err != nil {
			return fmt.Errorf("unexpected spec.default_url=%q: %w", durl, err)
		}
	}
	if r.TLSConfig != nil {
		if err := r.TLSConfig.Validate(); err != nil {
			return err
		}
	}
	if err := validateHTTPHeaders(r.Headers); err != nil {
		return fmt.Errorf("incorrect 'headers' syntax: %w", err)
	}
	if err := validateHTTPHeaders(r.ResponseHeaders); err != nil {
		return fmt.Errorf("incorrect 'response_headers' syntax: %w", err)
	}
	return nil
}

func validateHTTPHeaders(headerValues []string) error {
	for _, headerValue := range headerValues {
		idx := strings.IndexByte(headerValue, ':')
		if idx <= 0 {
			return fmt.Errorf("expected colon separated header: value, got=%q", headerValue)
		}
	}

	return nil
}

func (r *VMAuth) setLastSpec(prevSpec VMAuthSpec) {
	r.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (r *VMAuth) UnmarshalJSON(src []byte) error {
	type pr VMAuth
	if err := json.Unmarshal(src, (*pr)(r)); err != nil {
		return err
	}
	if err := parseLastAppliedState(r); err != nil {
		return err
	}

	return nil
}

func (r *VMAuth) Validate() error {
	if mustSkipValidation(r) {
		return nil
	}
	if r.Spec.ServiceSpec != nil && r.Spec.ServiceSpec.Name == r.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", r.PrefixedName())
	}
	if r.Spec.Ingress != nil {
		// check ingress
		// TlsHosts and TlsSecretName are both needed if one of them is used
		ing := r.Spec.Ingress
		if len(ing.TlsHosts) > 0 && ing.TlsSecretName == "" {
			return fmt.Errorf("spec.ingress.tlsSecretName cannot be empty with non-empty spec.ingress.tlsHosts")
		}
		if ing.TlsSecretName != "" && len(ing.TlsHosts) == 0 {
			return fmt.Errorf("spec.ingress.tlsHosts cannot be empty with non-empty spec.ingress.tlsSecretName")
		}
	}
	if r.Spec.ConfigSecret != "" && r.Spec.ExternalConfig.SecretRef != nil {
		return fmt.Errorf("spec.configSecret and spec.externalConfig.secretRef cannot be used at the same time")
	}
	if r.Spec.ExternalConfig.SecretRef != nil && r.Spec.ExternalConfig.LocalPath != "" {
		return fmt.Errorf("at most one option can be used for externalConfig: spec.configSecret or spec.externalConfig.secretRef")
	}
	if r.Spec.ExternalConfig.SecretRef != nil {
		if r.Spec.ExternalConfig.SecretRef.Name == r.PrefixedName() {
			return fmt.Errorf("spec.externalConfig.secretRef cannot be equal to the vmauth-config-CR_NAME=%q, it's operator reserved value", r.ConfigSecretName())
		}
		if r.Spec.ExternalConfig.SecretRef.Name == "" || r.Spec.ExternalConfig.SecretRef.Key == "" {
			return fmt.Errorf("name=%q and key=%q fields must be non-empty for spec.externalConfig.secretRef",
				r.Spec.ExternalConfig.SecretRef.Name, r.Spec.ExternalConfig.SecretRef.Key)
		}
	}
	if len(r.Spec.UnauthorizedAccessConfig) > 0 && r.Spec.UnauthorizedUserAccessSpec != nil {
		return fmt.Errorf("at most one option can be used `spec.unauthorizedAccessConfig` or `spec.unauthorizedUserAccessSpec`, got both")
	}
	if len(r.Spec.UnauthorizedAccessConfig) > 0 {
		for _, urlMap := range r.Spec.UnauthorizedAccessConfig {
			if err := urlMap.validate(); err != nil {
				return fmt.Errorf("incorrect r.spec.UnauthorizedAccessConfig: %w", err)
			}
		}
		if err := r.Spec.VMUserConfigOptions.validate(); err != nil {
			return fmt.Errorf("incorrect r.spec UnauthorizedAccessConfig options: %w", err)
		}
	}

	if r.Spec.UnauthorizedUserAccessSpec != nil {
		if err := r.Spec.UnauthorizedUserAccessSpec.validate(); err != nil {
			return fmt.Errorf("incorrect r.spec.UnauthorizedUserAccess syntax: %w", err)
		}
	}

	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (r *VMAuthSpec) UnmarshalJSON(src []byte) error {
	type pr VMAuthSpec
	if err := json.Unmarshal(src, (*pr)(r)); err != nil {
		r.ParsingError = fmt.Sprintf("cannot parse vmauth spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// EmbeddedIngress describes ingress configuration options.
type EmbeddedIngress struct {
	// ClassName defines ingress class name for VMAuth
	// +optional
	ClassName *string `json:"class_name,omitempty" yaml:"class_name,omitempty"`
	//  EmbeddedObjectMetadata adds labels and annotations for object.
	EmbeddedObjectMetadata `json:",inline"`
	// TlsHosts configures TLS access for ingress, tlsSecretName must be defined for it.
	TlsHosts []string `json:"tlsHosts,omitempty" yaml:"tlsHosts,omitempty"`
	// TlsSecretName defines secretname at the VMAuth namespace with cert and key
	// https://kubernetes.io/docs/concepts/services-networking/ingress/#tls
	// +optional
	TlsSecretName string `json:"tlsSecretName,omitempty" yaml:"tlsSecretName,omitempty"`
	// ExtraRules - additional rules for ingress,
	// must be checked for correctness by user.
	// +optional
	ExtraRules []v12.IngressRule `json:"extraRules,omitempty" yaml:"extraRules,omitempty"`
	// ExtraTLS - additional TLS configuration for ingress
	// must be checked for correctness by user.
	// +optional
	ExtraTLS []v12.IngressTLS `json:"extraTls,omitempty" yaml:"extraTls,omitempty"`
	// Host defines ingress host parameter for default rule
	// It will be used, only if TlsHosts is empty
	// +optional
	Host string `json:"host,omitempty"`
}

// VMAuthStatus defines the observed state of VMAuth
type VMAuthStatus struct {
	StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (r *VMAuthStatus) GetStatusMetadata() *StatusMetadata {
	return &r.StatusMetadata
}

// VMAuth is the Schema for the vmauths API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of update rollout"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="ReplicaCount",type="integer",JSONPath=".spec.replicaCount",description="The desired replicas number of Alertmanagers"
type VMAuth struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VMAuthSpec `json:"spec,omitempty"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VMAuthSpec `json:"-" yaml:"-"`

	Status VMAuthStatus `json:"status,omitempty"`
}

func (r *VMAuth) Probe() *EmbeddedProbes {
	return r.Spec.EmbeddedProbes
}

func (r *VMAuth) ProbePath() string {
	return buildPathWithPrefixFlag(r.Spec.ExtraArgs, healthPath)
}

func (r *VMAuth) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(r.Spec.ExtraArgs))
}

func (r *VMAuth) ProbePort() string {
	return r.Spec.Port
}

func (r *VMAuth) ProbeNeedLiveness() bool {
	return true
}

// +kubebuilder:object:root=true

// VMAuthList contains a list of VMAuth
type VMAuthList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAuth `json:"items"`
}

// AsOwner returns owner references with current object as owner
func (r *VMAuth) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         r.APIVersion,
			Kind:               r.Kind,
			Name:               r.Name,
			UID:                r.UID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	}
}

func (r *VMAuth) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if r.Spec.PodMetadata != nil {
		for annotation, value := range r.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (r *VMAuth) AnnotationsFiltered() map[string]string {
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	dst := filterMapKeysByPrefixes(r.ObjectMeta.Annotations, annotationFilterPrefixes)
	if r.Spec.ManagedMetadata != nil {
		if dst == nil {
			dst = make(map[string]string)
		}
		for k, v := range r.Spec.ManagedMetadata.Annotations {
			dst[k] = v
		}
	}
	return dst
}

func (r *VMAuth) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmauth",
		"app.kubernetes.io/instance":  r.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (r *VMAuth) PodLabels() map[string]string {
	lbls := r.SelectorLabels()
	if r.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(r.Spec.PodMetadata.Labels, lbls)
}

func (r *VMAuth) AllLabels() map[string]string {
	selectorLabels := r.SelectorLabels()
	// fast path
	if r.ObjectMeta.Labels == nil && r.Spec.ManagedMetadata == nil {
		return selectorLabels
	}
	var result map[string]string
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	if r.ObjectMeta.Labels != nil {
		result = filterMapKeysByPrefixes(r.ObjectMeta.Labels, labelFilterPrefixes)
	}
	if r.Spec.ManagedMetadata != nil {
		result = labels.Merge(result, r.Spec.ManagedMetadata.Labels)
	}
	return labels.Merge(result, selectorLabels)
}

func (r *VMAuth) PrefixedName() string {
	return fmt.Sprintf("vmauth-%s", r.Name)
}

func (r *VMAuth) ConfigSecretName() string {
	return fmt.Sprintf("vmauth-config-%s", r.Name)
}

// GetMetricPath returns prefixed path for metric requests
func (r *VMAuth) GetMetricPath() string {
	return buildPathWithPrefixFlag(r.Spec.ExtraArgs, metricPath)
}

// GetExtraArgs returns additionally configured command-line arguments
func (r *VMAuth) GetExtraArgs() map[string]string {
	return r.Spec.ExtraArgs
}

// GetServiceScrape returns overrides for serviceScrape builder
func (r *VMAuth) GetServiceScrape() *VMServiceScrapeSpec {
	return r.Spec.ServiceScrapeSpec
}

func (r *VMAuth) GetServiceAccount() *ServiceAccount {
	sa := r.Spec.ServiceAccount
	if sa == nil {
		sa = &ServiceAccount{
			Name:           r.PrefixedName(),
			AutomountToken: true,
		}
	}
	return sa
}

func (r *VMAuth) IsOwnsServiceAccount() bool {
	if r.Spec.ServiceAccount != nil && r.Spec.ServiceAccount.Name != "" {
		return r.Spec.ServiceAccount.Name == ""
	}
	return false
}

// GetNSName implements build.builderOpts interface
func (r *VMAuth) GetNSName() string {
	return r.GetNamespace()
}

// AsCRDOwner implements interface
func (r *VMAuth) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(Auth)
}

// IsUnmanaged checks if object should managed any  config objects
func (r *VMAuth) IsUnmanaged() bool {
	return (!r.Spec.SelectAllByDefault && r.Spec.UserSelector == nil && r.Spec.UserNamespaceSelector == nil) ||
		r.Spec.ExternalConfig.SecretRef != nil ||
		r.Spec.ExternalConfig.LocalPath != ""
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (r *VMAuth) LastAppliedSpecAsPatch() (client.Patch, error) {
	return lastAppliedChangesAsPatch(r.ObjectMeta, r.Spec)
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (r *VMAuth) HasSpecChanges() (bool, error) {
	return hasStateChanges(r.ObjectMeta, r.Spec)
}

func (r *VMAuth) Paused() bool {
	return r.Spec.Paused
}

// SetStatusTo changes update status with optional reason of fail
func (r *VMAuth) SetUpdateStatusTo(ctx context.Context, c client.Client, status UpdateStatus, maybeErr error) error {
	return updateObjectStatus(ctx, c, &patchStatusOpts[*VMAuth, *VMAuthStatus]{
		actualStatus: status,
		r:            r,
		rStatus:      &r.Status,
		maybeErr:     maybeErr,
	})
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (r *VMAuth) GetAdditionalService() *AdditionalServiceSpec {
	return r.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VMAuth{}, &VMAuthList{})
}
