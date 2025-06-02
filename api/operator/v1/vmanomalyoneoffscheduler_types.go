/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VMAnomalyOneoffSchedulerSpec defines the desired state of VMAnomalyOneoffScheduler.
type VMAnomalyOneoffSchedulerSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// Start datetime to use for training a model. ISO string or UNIX time in seconds.
	InferStart Time `json:"infer_start"`
	// End datetime to use for training a model. Must be greater than fit_start. ISO string or UNIX time in seconds.
	InferEnd Time `json:"infer_end"`
	// Start datetime to use for a model inference. ISO string or UNIX time in seconds.
	FitStart Time `json:"fit_start"`
	// End datetime to use for a model inference. Must be greater than infer_start. ISO string or UNIX time in seconds.
	FitEnd Time `json:"fit_end"`
}

func (s *VMAnomalyOneoffScheduler) Validate() error {
	spec := &s.Spec
	if spec.InferStart.IsZero() || spec.InferEnd.IsZero() {
		return fmt.Errorf(`both "infer_start" and "infer_end" parameters are required`)
	}
	if !spec.InferEnd.After(spec.InferStart.Time) {
		return fmt.Errorf(`"infer_end" time should be greater than "infer_start"`)
	}
	if spec.FitStart.IsZero() || spec.FitEnd.IsZero() {
		return fmt.Errorf(`both "fit_start" and "fit_end" parameters are required`)
	}
	if !spec.FitEnd.After(spec.FitStart.Time) {
		return fmt.Errorf(`"fit_end" time should be greater than "fit_start"`)
	}
	return nil
}

// VMAnomalyOneoffSchedulerStatus defines the observed state of VMAnomalyOneoffScheduler.
type VMAnomalyOneoffSchedulerStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyOneoffScheduler) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyOneoffScheduler is the Schema for the vmanomalyoneoffschedulers API.
type VMAnomalyOneoffScheduler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyOneoffSchedulerSpec   `json:"spec,omitempty"`
	Status VMAnomalyOneoffSchedulerStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyOneoffSchedulerSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyOneoffSchedulerSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyOneoffScheduler spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyOneoffSchedulerList contains a list of VMAnomalyOneoffScheduler.
type VMAnomalyOneoffSchedulerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyOneoffScheduler `json:"items"`
}

func (l *VMAnomalyOneoffSchedulerList) ItemsList() []VMAnomalyOneoffScheduler {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyOneoffScheduler{}, &VMAnomalyOneoffSchedulerList{})
}
