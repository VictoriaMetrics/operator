package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:object:generate=false
// +kubebuilder:pruning:PreserveUnknownFields
// +kubebuilder:validation:Schemaless
type Args map[string]any

// DeepCopyInto is an deepcopy function, copying the receiver, writing into out.
// In must be non-nil.
// We need to specify this manyually, as the generator does not support `any`.
func (in *Args) DeepCopyInto(out *Args) {
	if in == nil {
		*out = nil
	} else {
		*out = runtime.DeepCopyJSON(*in)
	}
}

// DeepCopy is an deepcopy function, copying the receiver, creating a new
// Args.
// We need to specify this manyually, as the generator does not support `any`.
func (in *Args) DeepCopy() *Args {
	if in == nil {
		return nil
	}
	out := new(Args)
	in.DeepCopyInto(out)
	return out
}

// EntitySelectors namespace and object selector
type EntitySelectors struct {
	// Object defines object selector
	// Works in combination with Namespace selector.
	// Namespace selector nil - only objects at Owner namespace.
	// Object selector nil - only objects at Namespace selector namespaces.
	// +optional
	Object *metav1.LabelSelector `json:"object,omitempty"`
	// Namespace defines selector for namespaces to be selected for object discovery
	// Works in combination with Object selector
	// Namespace selector nil - only objects at Owner namespace.
	// Selector nil - only objects at Namespace selector namespaces.
	// +optional
	Namespace *metav1.LabelSelector `json:"namespace,omitempty"`
}
