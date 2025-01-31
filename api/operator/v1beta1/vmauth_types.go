package v1beta1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	v12 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty" yaml:"serviceAccountName,omitempty"`

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
func (vmuua *VMAuthUnauthorizedUserAccessSpec) Validate() error {

	if len(vmuua.URLMap) == 0 && len(vmuua.URLPrefix) == 0 {
		return fmt.Errorf("at least one of `url_map` or `url_prefix` must be defined")
	}
	for idx, urlMap := range vmuua.URLMap {
		if err := urlMap.Validate(); err != nil {
			return fmt.Errorf("incorrect url_map at idx=%d: %w", idx, err)
		}
	}
	for _, urlPrefix := range vmuua.URLPrefix {
		if err := validateURLPrefix(urlPrefix); err != nil {
			return err
		}
	}
	if vmuua.TLSConfig != nil {
		if err := vmuua.TLSConfig.Validate(); err != nil {
			return fmt.Errorf("incorrect tlsConfig for UnauthorizedUserAccess: %w", err)
		}
	}
	for k := range vmuua.MetricLabels {
		if !labelNameRegexp.Match([]byte(k)) {
			return fmt.Errorf("incorrect metricLabelName=%q, must match pattern=%q", k, labelNameRegexp)
		}
	}
	if err := vmuua.VMUserConfigOptions.Validate(); err != nil {
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
func (uac *UnauthorizedAccessConfigURLMap) Validate() error {
	if len(uac.SrcPaths) == 0 && len(uac.SrcHosts) == 0 && len(uac.SrcQueryArgs) == 0 && len(uac.URLMapCommon.SrcQueryArgs) == 0 {
		return fmt.Errorf("incorrect url_map config at least of one src_paths,src_hosts,src_query_args or src_headers must be defined")
	}
	if len(uac.URLPrefix) == 0 {
		return fmt.Errorf("url_prefix cannot be empty for url_map")
	}
	for idx, urlPrefix := range uac.URLPrefix {
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
func (vuopts *VMUserConfigOptions) Validate() error {
	for _, durl := range vuopts.DefaultURLs {
		if err := validateURLPrefix(durl); err != nil {
			return fmt.Errorf("unexpected spec.default_url=%q: %w", durl, err)
		}
	}
	if vuopts.TLSConfig != nil {
		if err := vuopts.TLSConfig.Validate(); err != nil {
			return err
		}
	}
	if err := validateHTTPHeaders(vuopts.Headers); err != nil {
		return fmt.Errorf("incorrect 'headers' syntax: %w", err)
	}
	if err := validateHTTPHeaders(vuopts.ResponseHeaders); err != nil {
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

func (cr *VMAuth) setLastSpec(prevSpec VMAuthSpec) {
	cr.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAuth) UnmarshalJSON(src []byte) error {
	type pcr VMAuth
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	if err := parseLastAppliedState(cr); err != nil {
		return err
	}

	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAuthSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAuthSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vmauth spec: %s, err: %s", string(src), err)
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
func (cr *VMAuthStatus) GetStatusMetadata() *StatusMetadata {
	return &cr.StatusMetadata
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

func (cr *VMAuth) Probe() *EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

func (cr *VMAuth) ProbePath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

func (cr *VMAuth) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(cr.Spec.ExtraArgs))
}

func (cr *VMAuth) ProbePort() string {
	return cr.Spec.Port
}

func (cr *VMAuth) ProbeNeedLiveness() bool {
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
func (cr *VMAuth) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         cr.APIVersion,
			Kind:               cr.Kind,
			Name:               cr.Name,
			UID:                cr.UID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	}
}

func (cr *VMAuth) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr *VMAuth) AnnotationsFiltered() map[string]string {
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	dst := filterMapKeysByPrefixes(cr.ObjectMeta.Annotations, annotationFilterPrefixes)
	if cr.Spec.ManagedMetadata != nil {
		if dst == nil {
			dst = make(map[string]string)
		}
		for k, v := range cr.Spec.ManagedMetadata.Annotations {
			dst[k] = v
		}
	}
	return dst
}

func (cr *VMAuth) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmauth",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr *VMAuth) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

func (cr *VMAuth) AllLabels() map[string]string {
	selectorLabels := cr.SelectorLabels()
	// fast path
	if cr.ObjectMeta.Labels == nil && cr.Spec.ManagedMetadata == nil {
		return selectorLabels
	}
	var result map[string]string
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	if cr.ObjectMeta.Labels != nil {
		result = filterMapKeysByPrefixes(cr.ObjectMeta.Labels, labelFilterPrefixes)
	}
	if cr.Spec.ManagedMetadata != nil {
		result = labels.Merge(result, cr.Spec.ManagedMetadata.Labels)
	}
	return labels.Merge(result, selectorLabels)
}

func (cr *VMAuth) PrefixedName() string {
	return fmt.Sprintf("vmauth-%s", cr.Name)
}

func (cr *VMAuth) ConfigSecretName() string {
	return fmt.Sprintf("vmauth-config-%s", cr.Name)
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VMAuth) GetMetricPath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

// GetExtraArgs returns additionally configured command-line arguments
func (cr *VMAuth) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// GetServiceScrape returns overrides for serviceScrape builder
func (cr *VMAuth) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

func (cr *VMAuth) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr *VMAuth) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

// GetNSName implements build.builderOpts interface
func (cr *VMAuth) GetNSName() string {
	return cr.GetNamespace()
}

// AsCRDOwner implements interface
func (cr *VMAuth) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(Auth)
}

// IsUnmanaged checks if object should managed any  config objects
func (cr *VMAuth) IsUnmanaged() bool {
	return (!cr.Spec.SelectAllByDefault && cr.Spec.UserSelector == nil && cr.Spec.UserNamespaceSelector == nil) ||
		cr.Spec.ExternalConfig.SecretRef != nil ||
		cr.Spec.ExternalConfig.LocalPath != ""
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VMAuth) LastAppliedSpecAsPatch() (client.Patch, error) {
	return lastAppliedChangesAsPatch(cr.ObjectMeta, cr.Spec)
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (cr *VMAuth) HasSpecChanges() (bool, error) {
	return hasStateChanges(cr.ObjectMeta, cr.Spec)
}

func (cr *VMAuth) Paused() bool {
	return cr.Spec.Paused
}

// SetStatusTo changes update status with optional reason of fail
func (cr *VMAuth) SetUpdateStatusTo(ctx context.Context, r client.Client, status UpdateStatus, maybeErr error) error {
	return updateObjectStatus(ctx, r, &patchStatusOpts[*VMAuth, *VMAuthStatus]{
		actualStatus: status,
		cr:           cr,
		crStatus:     &cr.Status,
		maybeErr:     maybeErr,
	})
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMAuth) GetAdditionalService() *AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VMAuth{}, &VMAuthList{})
}
