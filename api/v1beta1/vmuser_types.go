package v1beta1

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMUserSpec defines the desired state of VMUser
type VMUserSpec struct {
	// UserName basic auth user name for accessing protected endpoint,
	// metadata.name if missing.
	// +optional
	UserName *string `json:"username,omitempty"`
	// Password basic auth password for accessing protected endpoint,
	// randomly generated and saved into secret with the same name
	// as VMUser into same namespace
	// +optional
	Password *string `json:"password,omitempty"`
	// GeneratePassword instructs operator to generate password for user
	// if spec.password if empty.
	// +optional
	GeneratePassword bool `json:"generatePassword,omitempty"`
	// BearerToken Authorization header value for accessing protected endpoint.
	// +optional
	BearerToken *string `json:"bearerToken,omitempty"`
	// TargetRefs - reference to endpoints, which user may access.
	TargetRefs []TargetRef `json:"targetRefs"`
}

// TargetRef describes target for user traffic forwarding.
type TargetRef struct {
	// CRD - one of operator crd targets
	// one of crd or static can be configured per targetRef.
	// +optional
	CRD *CRDRef `json:"crd,omitempty"`
	// Static - user defined url for traffic forward.
	// one of crd or static can be configured per targetRef.
	// +optional
	Static *StaticRef `json:"static,omitempty"`
	// Paths - matched path to route.
	// +optional
	Paths []string `json:"paths,omitempty"`
	// todo enable it if needed.
	// QueryParams - additional query params for target.
	// +optional
	// QueryParams []string `json:"queryParams,omitempty"`
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

func (cr *VMUser) SecretName() string {
	return fmt.Sprintf("vmuser-%s", cr.Name)
}

func (cr *VMUser) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         cr.APIVersion,
			Kind:               cr.Kind,
			Name:               cr.Name,
			UID:                cr.UID,
			Controller:         pointer.BoolPtr(true),
			BlockOwnerDeletion: pointer.BoolPtr(true),
		},
	}
}

func (cr VMUser) Annotations() map[string]string {
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

func (cr VMUser) Labels() map[string]string {
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
