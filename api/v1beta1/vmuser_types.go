package v1beta1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VMUserSpec defines the desired state of VMUser
type VMUserSpec struct {
	// UserName basic auth user name for accessing protected endpoint,
	// metadata.name if missing.
	// +optional
	UserName *string `json:"userName,omitempty"`
	// Password basic auth password for accessing protected endpoint,
	// randomly generated and saved into secret with the same name
	// as VMUser into same namespace
	// +optional
	Password *string `json:"password,omitempty"`
	// BearerToken Authorization header value for accessing protected endpoint.
	// +optional
	BearerToken *string `json:"bearerToken,omitempty"`
	// TargetRefs - reference to endpoints, which user may access.
	TargetRefs []TargetRef `json:"targetRefs"`
}

// TargetRef describes target for user traffic forwarding.
type TargetRef struct {
	// CRD - one of operator crd targets
	CRD *CRDRef `json:"crd,omitempty"`
	// Static - user defined url for traffic forward.
	Static *StaticRef `json:"static,omitempty"`
	// Paths - matched path to route.
	Paths []string `json:"paths,omitempty"`
	// QueryParams - additional query params for target.
	QueryParams []string `json:"queryParams,omitempty"`
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

func (cr *CRDRef) AsKey() string {
	return fmt.Sprintf("%s/%s/%s", cr.Kind, cr.Namespace, cr.Name)
}

type StaticRef struct {
	URL string `json:"url"`
}

// VMUserStatus defines the observed state of VMUser
type VMUserStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMUser is the Schema for the vmusers API
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

func init() {
	SchemeBuilder.Register(&VMUser{}, &VMUserList{})
}
