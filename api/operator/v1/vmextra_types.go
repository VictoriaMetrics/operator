package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
