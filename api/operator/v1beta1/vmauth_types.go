package v1beta1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	v12 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMAuthSpec defines the desired state of VMAuth
type VMAuthSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// PodMetadata configures Labels and Annotations which are propagated to the VMAuth pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogLevel for victoria metrics single to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VMAuth to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// SelectAllByDefault changes default behavior for empty CRD selectors, such userSelector.
	// with selectAllByDefault: true and empty userSelector and userNamespaceSelector
	// Operator selects all exist users
	// with selectAllByDefault: false - selects nothing
	// +optional
	SelectAllByDefault bool `json:"selectAllByDefault,omitempty"`
	// UserSelector defines VMUser to be selected for config file generation.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAuth namespace.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	UserSelector *metav1.LabelSelector `json:"userSelector,omitempty"`
	// UserNamespaceSelector Namespaces to be selected for  VMAuth discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAuth namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	UserNamespaceSelector *metav1.LabelSelector `json:"userNamespaceSelector,omitempty"`

	// ServiceSpec that will be added to vmsingle service spec
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmauth VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// Ingress enables ingress configuration for VMAuth.
	Ingress *EmbeddedIngress `json:"ingress,omitempty"`
	// LivenessProbe that will be added to VMAuth pod
	*EmbeddedProbes `json:",inline"`
	// UnauthorizedAccessConfig configures access for un authorized users
	// +optional
	UnauthorizedAccessConfig []UnauthorizedAccessConfigURLMap `json:"unauthorizedAccessConfig,omitempty"`
	UserConfigOption         `json:",inline"`
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
	// +optional
	ConfigSecret string `json:"configSecret,omitempty"`

	CommonDefaultableParams           `json:",inline,omitempty"`
	CommonConfigReloaderParams        `json:",inline,omitempty"`
	CommonApplicationDeploymentParams `json:",inline,omitempty"`
}

type UnauthorizedAccessConfigURLMap struct {
	// SrcPaths is an optional list of regular expressions, which must match the request path.
	SrcPaths []string `json:"src_paths,omitempty"`

	// SrcHosts is an optional list of regular expressions, which must match the request hostname.
	SrcHosts []string `json:"src_hosts,omitempty"`

	// UrlPrefix contains backend url prefixes for the proxied request url.
	URLPrefix []string `json:"url_prefix,omitempty"`

	URLMapCommon `json:",omitempty"`
}

// URLMapCommon contains common fields for unauthorized user and user in vmuser
type URLMapCommon struct {
	// SrcQueryArgs is an optional list of query args, which must match request URL query args.
	SrcQueryArgs []string `json:"src_query_args,omitempty"`

	// SrcHeaders is an optional list of headers, which must match request headers.
	SrcHeaders []string `json:"src_headers,omitempty"`

	// DiscoverBackendIPs instructs discovering URLPrefix backend IPs via DNS.
	DiscoverBackendIPs *bool `json:"discover_backend_ips,omitempty"`

	// RequestHeaders represent additional http headers, that vmauth uses
	// in form of ["header_key: header_value"]
	// multiple values for header key:
	// ["header_key: value1,value2"]
	// it's available since 1.68.0 version of vmauth
	// +optional
	RequestHeaders []string `json:"headers,omitempty"`
	// ResponseHeaders represent additional http headers, that vmauth adds for request response
	// in form of ["header_key: header_value"]
	// multiple values for header key:
	// ["header_key: value1,value2"]
	// it's available since 1.93.0 version of vmauth
	// +optional
	ResponseHeaders []string `json:"response_headers,omitempty"`

	// RetryStatusCodes defines http status codes in numeric format for request retries
	// Can be defined per target or at VMUser.spec level
	// e.g. [429,503]
	// +optional
	RetryStatusCodes []int `json:"retry_status_codes,omitempty"`

	// LoadBalancingPolicy defines load balancing policy to use for backend urls.
	// Supported policies: least_loaded, first_available.
	// See [here](https://docs.victoriametrics.com/vmauth#load-balancing) for more details (default "least_loaded")
	// +optional
	// +kubebuilder:validation:Enum=least_loaded;first_available
	LoadBalancingPolicy *string `json:"load_balancing_policy,omitempty"`

	// DropSrcPathPrefixParts is the number of `/`-delimited request path prefix parts to drop before proxying the request to backend.
	// See [here](https://docs.victoriametrics.com/vmauth#dropping-request-path-prefix) for more details.
	// +optional
	DropSrcPathPrefixParts *int `json:"drop_src_path_prefix_parts,omitempty"`
}

type UserConfigOption struct {
	// DefaultURLs backend url for non-matching paths filter
	// usually used for default backend with error message
	DefaultURLs []string `json:"default_url,omitempty"`

	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`

	// IPFilters defines per target src ip filters
	// supported only with enterprise version of [vmauth](https://docs.victoriametrics.com/vmauth/#ip-filters)
	// +optional
	IPFilters VMUserIPFilters `json:"ip_filters,omitempty"`

	// DiscoverBackendIPs instructs discovering URLPrefix backend IPs via DNS.
	DiscoverBackendIPs *bool `json:"discover_backend_ips,omitempty"`

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
	ResponseHeaders []string `json:"response_headers,omitempty"`

	// RetryStatusCodes defines http status codes in numeric format for request retries
	// e.g. [429,503]
	// +optional
	RetryStatusCodes []int `json:"retry_status_codes,omitempty"`

	// MaxConcurrentRequests defines max concurrent requests per user
	// 300 is default value for vmauth
	// +optional
	MaxConcurrentRequests *int `json:"max_concurrent_requests,omitempty"`

	// LoadBalancingPolicy defines load balancing policy to use for backend urls.
	// Supported policies: least_loaded, first_available.
	// See [here](https://docs.victoriametrics.com/vmauth#load-balancing) for more details (default "least_loaded")
	// +optional
	// +kubebuilder:validation:Enum=least_loaded;first_available
	LoadBalancingPolicy *string `json:"load_balancing_policy,omitempty"`

	// DropSrcPathPrefixParts is the number of `/`-delimited request path prefix parts to drop before proxying the request to backend.
	// See [here](https://docs.victoriametrics.com/vmauth#dropping-request-path-prefix) for more details.
	// +optional
	DropSrcPathPrefixParts *int `json:"drop_src_path_prefix_parts,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAuth) UnmarshalJSON(src []byte) error {
	type pcr VMAuth
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	prev, err := parseLastAppliedSpec[VMAuthSpec](cr)
	if err != nil {
		return err
	}
	cr.ParsedLastAppliedSpec = prev
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
	ClassName *string `json:"class_name,omitempty"`
	//  EmbeddedObjectMetadata adds labels and annotations for object.
	EmbeddedObjectMetadata `json:",inline"`
	// TlsHosts configures TLS access for ingress, tlsSecretName must be defined for it.
	TlsHosts []string `json:"tlsHosts,omitempty"`
	// TlsSecretName defines secretname at the VMAuth namespace with cert and key
	// https://kubernetes.io/docs/concepts/services-networking/ingress/#tls
	// +optional
	TlsSecretName string `json:"tlsSecretName,omitempty"`
	// ExtraRules - additional rules for ingress,
	// must be checked for correctness by user.
	// +optional
	ExtraRules []v12.IngressRule `json:"extraRules,omitempty"`
	// ExtraTLS - additional TLS configuration for ingress
	// must be checked for correctness by user.
	// +optional
	ExtraTLS []v12.IngressTLS `json:"extraTls,omitempty"`
	// Host defines ingress host parameter for default rule
	// It will be used, only if TlsHosts is empty
	// +optional
	Host string `json:"host,omitempty"`
}

// VMAuthStatus defines the observed state of VMAuth
type VMAuthStatus struct {
	// UpdateStatus defines a status for update rollout, effective only for statefulMode
	UpdateStatus UpdateStatus `json:"updateStatus,omitempty"`
	// Reason defines fail reason for update process, effective only for statefulMode
	Reason string `json:"reason,omitempty"`
}

// VMAuth is the Schema for the vmauths API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of update rollout"
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

func (cr VMAuth) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr VMAuth) AnnotationsFiltered() map[string]string {
	return filterMapKeysByPrefixes(cr.ObjectMeta.Annotations, annotationFilterPrefixes)
}

func (cr VMAuth) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmauth",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VMAuth) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

func (cr VMAuth) AllLabels() map[string]string {
	selectorLabels := cr.SelectorLabels()
	// fast path
	if cr.ObjectMeta.Labels == nil {
		return selectorLabels
	}
	crLabels := filterMapKeysByPrefixes(cr.ObjectMeta.Labels, labelFilterPrefixes)
	return labels.Merge(crLabels, selectorLabels)
}

func (cr VMAuth) PrefixedName() string {
	return fmt.Sprintf("vmauth-%s", cr.Name)
}

func (cr VMAuth) ConfigSecretName() string {
	return fmt.Sprintf("vmauth-config-%s", cr.Name)
}

// GetMetricPath returns prefixed path for metric requests
func (cr VMAuth) GetMetricPath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

// GetExtraArgs returns additionally configured command-line arguments
func (cr VMAuth) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// GetServiceScrape returns overrides for serviceScrape builder
func (cr VMAuth) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

func (cr VMAuth) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr VMAuth) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

func (cr VMAuth) GetNSName() string {
	return cr.GetNamespace()
}

// AsCRDOwner implements interface
func (cr *VMAuth) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(Auth)
}

// IsUnmanaged checks if object should managed any  config objects
func (cr *VMAuth) IsUnmanaged() bool {
	return (!cr.Spec.SelectAllByDefault && cr.Spec.UserSelector == nil && cr.Spec.UserNamespaceSelector == nil) || cr.Spec.ConfigSecret != ""
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VMAuth) LastAppliedSpecAsPatch() (client.Patch, error) {
	data, err := json.Marshal(cr.Spec)
	if err != nil {
		return nil, fmt.Errorf("possible bug, cannot serialize specification as json :%w", err)
	}
	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q: %q}}}`, lastAppliedSpecAnnotationName, data)
	return client.RawPatch(types.MergePatchType, []byte(patch)), nil
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (cr *VMAuth) HasSpecChanges() (bool, error) {
	lastAppliedClusterJSON := cr.Annotations[lastAppliedSpecAnnotationName]
	if len(lastAppliedClusterJSON) == 0 {
		return true, nil
	}

	instanceSpecData, err := json.Marshal(cr.Spec)
	if err != nil {
		return true, err
	}
	return !bytes.Equal([]byte(lastAppliedClusterJSON), instanceSpecData), nil
}

func (cr *VMAuth) Paused() bool {
	return cr.Spec.Paused
}

// SetStatusTo changes update status with optional reason of fail
func (cr *VMAuth) SetUpdateStatusTo(ctx context.Context, r client.Client, status UpdateStatus, maybeErr error) error {
	currentStatus := cr.Status.UpdateStatus
	prevStatus := cr.Status.DeepCopy()
	switch status {
	case UpdateStatusExpanding:
	case UpdateStatusFailed:
		if maybeErr != nil {
			cr.Status.Reason = maybeErr.Error()
		}
	case UpdateStatusOperational:
		cr.Status.Reason = ""
	case UpdateStatusPaused:
		if currentStatus == status {
			return nil
		}
	default:
		panic(fmt.Sprintf("BUG: not expected status=%q", status))
	}
	if equality.Semantic.DeepEqual(&cr.Status, prevStatus) && status == currentStatus {
		return nil
	}
	cr.Status.UpdateStatus = status
	return statusPatch(ctx, r, cr.DeepCopy(), cr.Status)
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMAuth) GetAdditionalService() *AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VMAuth{}, &VMAuthList{})
}
