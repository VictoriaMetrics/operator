package v1

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

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

type Time struct {
	time.Time
}

// UnmarshalJSON implements json.Unmarshaller interface
func (t *Time) UnmarshalJSON(data []byte) error {
	if len(data) == 4 && string(data) == "null" {
		t.Time = time.Time{}
		return nil
	}

	var str string
	err := json.Unmarshal(data, &str)
	if err != nil {
		return err
	}

	ts, err := strconv.ParseInt(str, 10, 64)
	if err == nil {
		t.Time = time.Unix(ts, 0).Local()
		return nil
	}
	pt, err := time.Parse(time.RFC3339, str)
	if err == nil {
		t.Time = pt.Local()
		return nil
	}
	return fmt.Errorf("failed to parse %q value neither as Unix time nor in ISO8601 format", str)
}

func (t *Time) DeepCopyInto(out *Time) {
	*out = *t
}

type Location struct {
	time.Location
}

func (l *Location) DeepCopyInto(out *Location) {
	*out = *l
}

// UnmarshalJSON implements json.Unmarshaller interface
func (l *Location) UnmarshalJSON(data []byte) error {
	var str string
	err := json.Unmarshal(data, &str)
	if err != nil {
		return err
	}
	location, err := time.LoadLocation(str)
	if err != nil {
		return err
	}
	l.Location = *location
	return nil
}
