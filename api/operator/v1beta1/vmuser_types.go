package v1beta1

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var supportedCRDKinds = []string{
	"VMAgent", "VMAlert", "VMAlertmanager", "VMSingle", "VMCluster/vmselect", "VMCluster/vminsert", "VMCluster/vmstorage",
}

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

	VMUserConfigOptions `json:",inline"`

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
	Hosts []string `json:"hosts,omitempty"`

	URLMapCommon `json:",omitempty"`

	// TargetPathSuffix allows to add some suffix to the target path
	// It allows to hide tenant configuration from user with crd as ref.
	// it also may contain any url encoded params.
	// +optional
	TargetPathSuffix string `json:"target_path_suffix,omitempty"`
	// TargetRefBasicAuth allow an target endpoint to authenticate over basic authentication
	// +optional
	TargetRefBasicAuth *TargetRefBasicAuth `json:"targetRefBasicAuth,omitempty"`
}

// VMUserIPFilters defines filters for IP addresses
// supported only with enterprise version of [vmauth](https://docs.victoriametrics.com/vmauth#ip-filters)
type VMUserIPFilters struct {
	DenyList  []string `json:"deny_list,omitempty"`
	AllowList []string `json:"allow_list,omitempty"`
}

// CRDRef describe CRD target reference.
type CRDRef struct {
	// Kind one of:
	// VMAgent,VMAlert, VMSingle, VMCluster/vmselect, VMCluster/vmstorage,VMCluster/vminsert  or VMAlertManager
	// +kubebuilder:validation:Enum=VMAgent;VMAlert;VMSingle;VLogs;VMAlertManager;VMAlertmanager;VMCluster/vmselect;VMCluster/vmstorage;VMCluster/vminsert
	Kind string `json:"kind"`
	// Name target CRD object name
	Name string `json:"name"`
	// Namespace target CRD object namespace.
	Namespace string `json:"namespace"`
}

// AddRefToObj adds reference to given object and return it.
func (r *CRDRef) AddRefToObj(obj client.Object) client.Object {
	obj.SetName(r.Name)
	obj.SetNamespace(r.Namespace)
	return obj
}

func (r *CRDRef) AsKey() string {
	return fmt.Sprintf("%s/%s/%s", r.Kind, r.Namespace, r.Name)
}

// StaticRef - user-defined routing host address.
type StaticRef struct {
	// URL http url for given staticRef.
	URL string `json:"url,omitempty"`
	// URLs allows setting multiple urls for load-balancing at vmauth-side.
	// +optional
	URLs []string `json:"urls,omitempty"`
}

// TargetRefBasicAuth target basic authentication
type TargetRefBasicAuth struct {
	// The secret in the service scrape namespace that contains the username
	// for authentication.
	// It must be at them same namespace as CRD
	Username v1.SecretKeySelector `json:"username"`
	// The secret in the service scrape namespace that contains the password
	// for authentication.
	// It must be at them same namespace as CRD
	Password v1.SecretKeySelector `json:"password"`
}

// VMUserStatus defines the observed state of VMUser
type VMUserStatus struct {
	StatusMetadata `json:",inline"`
}

// VMUser is the Schema for the vmusers API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus"
// +kubebuilder:printcolumn:name="Sync Error",type="string",JSONPath=".status.reason"
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
func (r *VMUser) SecretName() string {
	return fmt.Sprintf("vmuser-%s", r.Name)
}

// PasswordRefAsKey - builds key for passwordRef cache
func (r *VMUser) PasswordRefAsKey() string {
	return fmt.Sprintf("%s/%s/%s", r.Namespace, r.Spec.PasswordRef.Name, r.Spec.PasswordRef.Key)
}

// TokenRefAsKey - builds key for passwordRef cache
func (r *VMUser) TokenRefAsKey() string {
	return fmt.Sprintf("%s/%s/%s", r.Namespace, r.Spec.TokenRef.Name, r.Spec.TokenRef.Key)
}

// AsOwner returns owner references with current object as owner
func (r *VMUser) AsOwner() []metav1.OwnerReference {
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

func (r VMUser) AnnotationsFiltered() map[string]string {
	annotations := make(map[string]string)
	for annotation, value := range r.Annotations {
		if !strings.HasPrefix(annotation, "kubectl.kubernetes.io/") {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (r VMUser) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmuser",
		"app.kubernetes.io/instance":  r.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// AllLabels returns combined labels for VMUser
func (r VMUser) AllLabels() map[string]string {
	labels := r.SelectorLabels()
	if r.Labels != nil {
		for label, value := range r.Labels {
			if _, ok := labels[label]; ok {
				// forbid changes for selector labels
				continue
			}
			labels[label] = value
		}
	}
	return labels
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (r *VMUser) GetStatusMetadata() *StatusMetadata {
	return &r.Status.StatusMetadata
}

func (r *VMUser) Validate() error {
	if mustSkipValidation(r) {
		return nil
	}
	if r.Spec.UserName != nil && r.Spec.BearerToken != nil {
		return fmt.Errorf("one of spec.username and spec.bearerToken must be defined for user, got both")
	}
	if r.Spec.PasswordRef != nil && r.Spec.Password != nil {
		return fmt.Errorf("one of spec.password or spec.passwordRef must be used for user, got both")
	}
	if len(r.Spec.TargetRefs) == 0 {
		return fmt.Errorf("at least 1 TargetRef must be provided for spec.targetRefs")
	}
	isRetryCodesSet := len(r.Spec.RetryStatusCodes) > 0
	for i := range r.Spec.TargetRefs {
		targetRef := r.Spec.TargetRefs[i]
		if targetRef.CRD != nil && targetRef.Static != nil {
			return fmt.Errorf("targetRef validation failed, one of `crd` or `static` must be configured, got both")
		}
		if targetRef.CRD == nil && targetRef.Static == nil {
			return fmt.Errorf("targetRef validation failed, one of `crd` or `static` must be configured, got none")
		}
		if targetRef.Static != nil {
			if targetRef.Static.URL == "" && len(targetRef.Static.URLs) == 0 {
				return fmt.Errorf("for targetRef.static url or urls option must be set at idx=%d", i)
			}
			if targetRef.Static.URL != "" {
				if err := validateURLPrefix(targetRef.Static.URL); err != nil {
					return fmt.Errorf("incorrect static.url: %w", err)
				}
			}
			for _, staticURL := range targetRef.Static.URLs {
				if err := validateURLPrefix(staticURL); err != nil {
					return fmt.Errorf("incorrect value at static.urls: %w", err)
				}
			}
		}
		if targetRef.CRD != nil {
			switch targetRef.CRD.Kind {
			case "VMAgent", "VMAlert", "VMAlertmanager", "VMSingle", "VMCluster/vmselect", "VMCluster/vminsert", "VMCluster/vmstorage":
			default:
				return fmt.Errorf("unsupported crd.kind for target ref, got: `%s`, want one of: `%s`", targetRef.CRD.Kind, strings.Join(supportedCRDKinds, ","))
			}
			if targetRef.CRD.Namespace == "" || targetRef.CRD.Name == "" {
				return fmt.Errorf("crd.name and crd.namespace cannot be empty")
			}
		}
		if err := validateHTTPHeaders(targetRef.ResponseHeaders); err != nil {
			return fmt.Errorf("failed to parse targetRef response headers :%w", err)
		}
		if err := validateHTTPHeaders(targetRef.RequestHeaders); err != nil {
			return fmt.Errorf("failed to parse targetRef headers :%w", err)
		}
		if isRetryCodesSet && len(targetRef.RetryStatusCodes) > 0 {
			return fmt.Errorf("retry_status_codes already set at VMUser.spec level")
		}
	}
	for k := range r.Spec.MetricLabels {
		if !labelNameRegexp.MatchString(k) {
			return fmt.Errorf("incorrect metricLabels key=%q, must match pattern=%q", k, labelNameRegexp)
		}
	}
	if err := validateHTTPHeaders(r.Spec.Headers); err != nil {
		return fmt.Errorf("failed to parse vmuser headers: %w", err)
	}
	if err := validateHTTPHeaders(r.Spec.ResponseHeaders); err != nil {
		return fmt.Errorf("failed to parse vmuser response headers: %w", err)
	}
	return nil
}

func init() {
	SchemeBuilder.Register(&VMUser{}, &VMUserList{})
}
