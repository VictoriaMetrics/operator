package v1beta1

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMUserSpec defines the desired state of VMUser
type VMUserSpec struct {
	// Name of the VMUser object.
	// +optional
	Name *string `json:"name,omitempty"`
	// UserName basic auth user name for accessing protected endpoint,
	// will be replaced with metadata.name of VMUser if omitted.
	// +optional
	UserName *string `json:"username,omitempty"`
	// Password basic auth password for accessing protected endpoint.
	// +optional
	Password *string `json:"password,omitempty"`
	// PasswordRef allows fetching password from user-create secret by its name and key.
	// +optional
	PasswordRef *v1.SecretKeySelector `json:"passwordRef,omitempty"`
	// TokenRef allows fetching token from user-created secrets by its name and key.
	// +optional
	TokenRef *v1.SecretKeySelector `json:"tokenRef,omitempty"`
	// GeneratePassword instructs operator to generate password for user
	// if spec.password if empty.
	// +optional
	GeneratePassword bool `json:"generatePassword,omitempty"`
	// BearerToken Authorization header value for accessing protected endpoint.
	// +optional
	BearerToken *string `json:"bearerToken,omitempty"`
	// TargetRefs - reference to endpoints, which user may access.
	TargetRefs []TargetRef `json:"targetRefs"`

	// DefaultURLs backend url for non-matching paths filter
	// usually used for default backend with error message
	// +optional
	DefaultURLs []string `json:"default_url,omitempty"`
	// IPFilters defines per target src ip filters
	// supported only with enterprise version of vmauth
	// https://docs.victoriametrics.com/vmauth.html#ip-filters
	// +optional
	IPFilters VMUserIPFilters `json:"ip_filters,omitempty"`

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
	// See https://docs.victoriametrics.com/vmauth.html#load-balancing for more details (default "least_loaded")
	// +optional
	// +kubebuilder:validation:Enum=least_loaded;first_available
	LoadBalancingPolicy *string `json:"load_balancing_policy,omitempty"`

	// DropSrcPathPrefixParts is the number of `/`-delimited request path prefix parts to drop before proxying the request to backend.
	// See https://docs.victoriametrics.com/vmauth.html#dropping-request-path-prefix for more details.
	// +optional
	DropSrcPathPrefixParts *int `json:"drop_src_path_prefix_parts,omitempty"`

	// TLSInsecureSkipVerify - whether to skip TLS verification when connecting to backend over HTTPS.
	// See https://docs.victoriametrics.com/vmauth.html#backend-tls-setup
	// +optional
	TLSInsecureSkipVerify bool `json:"tls_insecure_skip_verify,omitempty"`

	// MetricLabels - additional labels for metrics exported by vmauth for given user.
	// +optional
	MetricLabels map[string]string `json:"metric_labels,omitempty"`

	// DisableSecretCreation skips related secret creation for vmuser
	DisableSecretCreation bool `json:"disable_secret_creation,omitempty"`
}

// TargetRef describes target for user traffic forwarding.
// one of target types can be chosen:
// crd or static per targetRef.
// user can define multiple targetRefs with different ref Types.
type TargetRef struct {
	// CRD describes exist operator's CRD object,
	// operator generates access url based on CRD params.
	// +optional
	CRD *CRDRef `json:"crd,omitempty"`
	// Static - user defined url for traffic forward,
	// for instance http://vmsingle:8429
	// +optional
	Static *StaticRef `json:"static,omitempty"`
	// Paths - matched path to route.
	// +optional
	Paths []string `json:"paths,omitempty"`
	// QueryParams []string `json:"queryParams,omitempty"`
	// TargetPathSuffix allows to add some suffix to the target path
	// It allows to hide tenant configuration from user with crd as ref.
	// it also may contain any url encoded params.
	// +optional
	TargetPathSuffix string `json:"target_path_suffix,omitempty"`
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
	// Can be defined per target or at VMUser.spec level
	// e.g. [429,503]
	// +optional
	RetryStatusCodes []int `json:"retry_status_codes,omitempty"`
	// LoadBalancingPolicy defines load balancing policy to use for backend urls.
	// Supported policies: least_loaded, first_available.
	// See https://docs.victoriametrics.com/vmauth.html#load-balancing for more details (default "least_loaded")
	// +optional
	// +kubebuilder:validation:Enum=least_loaded;first_available
	LoadBalancingPolicy *string `json:"load_balancing_policy,omitempty"`
	// DropSrcPathPrefixParts is the number of `/`-delimited request path prefix parts to drop before proxying the request to backend.
	// See https://docs.victoriametrics.com/vmauth.html#dropping-request-path-prefix for more details.
	// +optional
	DropSrcPathPrefixParts *int `json:"drop_src_path_prefix_parts,omitempty"`
}

// VMUserIPFilters defines filters for IP addresses
// supported only with enterprise version of vmauth
// https://docs.victoriametrics.com/vmauth.html#ip-filters
type VMUserIPFilters struct {
	DenyList  []string `json:"deny_list,omitempty"`
	AllowList []string `json:"allow_list,omitempty"`
}

// CRDRef describe CRD target reference.
type CRDRef struct {
	// Kind one of:
	// VMAgent VMAlert VMCluster VMSingle or VMAlertManager
	Kind string `json:"kind"`
	// Name target CRD object name
	Name string `json:"name"`
	// Namespace target CRD object namespace.
	Namespace string `json:"namespace"`
}

// AddRefToObj adds reference to given object and return it.
func (cr *CRDRef) AddRefToObj(obj client.Object) client.Object {
	obj.SetName(cr.Name)
	obj.SetNamespace(cr.Namespace)
	return obj
}

func (cr *CRDRef) AsKey() string {
	return fmt.Sprintf("%s/%s/%s", cr.Kind, cr.Namespace, cr.Name)
}

// StaticRef - user-defined routing host address.
type StaticRef struct {
	// URL http url for given staticRef.
	URL string `json:"url,omitempty"`
	// URLs allows setting multiple urls for load-balancing at vmauth-side.
	// +optional
	URLs []string `json:"urls,omitempty"`
}

// VMUserStatus defines the observed state of VMUser
type VMUserStatus struct {
}

// VMUser is the Schema for the vmusers API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +genclient
type VMUser struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMUserSpec   `json:"spec,omitempty"`
	Status VMUserStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VMUserList contains a list of VMUser
type VMUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMUser `json:"items"`
}

// SecretName builds secret name for VMUser.
func (cr *VMUser) SecretName() string {
	return fmt.Sprintf("vmuser-%s", cr.Name)
}

// PasswordRefAsKey - builds key for passwordRef cache
func (cr *VMUser) PasswordRefAsKey() string {
	return fmt.Sprintf("%s/%s/%s", cr.Namespace, cr.Spec.PasswordRef.Name, cr.Spec.PasswordRef.Key)
}

// TokenRefAsKey - builds key for passwordRef cache
func (cr *VMUser) TokenRefAsKey() string {
	return fmt.Sprintf("%s/%s/%s", cr.Namespace, cr.Spec.TokenRef.Name, cr.Spec.TokenRef.Key)
}

func (cr *VMUser) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         cr.APIVersion,
			Kind:               cr.Kind,
			Name:               cr.Name,
			UID:                cr.UID,
			Controller:         pointer.Bool(true),
			BlockOwnerDeletion: pointer.Bool(true),
		},
	}
}

func (cr VMUser) AnnotationsFiltered() map[string]string {
	annotations := make(map[string]string)
	for annotation, value := range cr.ObjectMeta.Annotations {
		if !strings.HasPrefix(annotation, "kubectl.kubernetes.io/") {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr VMUser) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmuser",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VMUser) AllLabels() map[string]string {
	labels := cr.SelectorLabels()
	if cr.ObjectMeta.Labels != nil {
		for label, value := range cr.ObjectMeta.Labels {
			if _, ok := labels[label]; ok {
				// forbid changes for selector labels
				continue
			}
			labels[label] = value
		}
	}
	return labels
}

func init() {
	SchemeBuilder.Register(&VMUser{}, &VMUserList{})
}
