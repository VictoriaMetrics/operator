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

// VMAnomalyBacktestingSchedulerSpec defines the desired state of VMAnomalyBacktestingScheduler.
type VMAnomalyBacktestingSchedulerSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// What time range to use for training the models. Must be at least 1 second.
	FitWindow metav1.Duration `json:"fit_window"`
	// Start datetime to use for
	From Time `json:"from"`
	// End datetime to use for  Must be greater than from_start
	To Time `json:"to"`
	// What time range to use previously trained model to infer on new data until next retrain happens.
	FitEvery metav1.Duration `json:"fit_every"`
	// Allows proportionally faster (yet more resource-intensive) evaluations of a config on historical data. Default value is 1, that implies sequential execution. Introduced in v1.13.0
	Jobs int `json:"n_jobs,omitempty"`
	// Enables inference mode in which the window you specify via [from, to] (or [from_iso, to_iso]) is used solely for inference, and the corresponding training (“fit”) windows are determined automatically.
	InferenceOnly bool `json:"inference_only,omitempty"`
}

func (s *VMAnomalyBacktestingScheduler) Validate() error {
	spec := &s.Spec
	if spec.From.IsZero() || spec.To.IsZero() {
		return fmt.Errorf(`both "from" and "to" parameters are required`)
	}
	if !spec.To.After(spec.From.Time) {
		return fmt.Errorf(`"to" time should be greater than "from"`)
	}
	return nil
}

// VMAnomalyBacktestingSchedulerStatus defines the observed state of VMAnomalyBacktestingScheduler.
type VMAnomalyBacktestingSchedulerStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyBacktestingScheduler) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyBacktestingScheduler is the Schema for the vmanomalybacktestingschedulers API.
type VMAnomalyBacktestingScheduler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyBacktestingSchedulerSpec   `json:"spec,omitempty"`
	Status VMAnomalyBacktestingSchedulerStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyBacktestingSchedulerSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyBacktestingSchedulerSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyBacktestingScheduler spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyBacktestingSchedulerList contains a list of VMAnomalyBacktestingScheduler.
type VMAnomalyBacktestingSchedulerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyBacktestingScheduler `json:"items"`
}

func (l *VMAnomalyBacktestingSchedulerList) ItemsList() []VMAnomalyBacktestingScheduler {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyBacktestingScheduler{}, &VMAnomalyBacktestingSchedulerList{})
}
