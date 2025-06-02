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

// VMAnomalyPeriodicSchedulerSpec defines the desired state of VMAnomalyPeriodicScheduler.
type VMAnomalyPeriodicSchedulerSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// What time range to use for training the models. Must be at least 1 second.
	FitWindow metav1.Duration `json:"fit_window"`
	// How often a model produce and write its anomaly scores on new datapoints. Must be at least 1 second.
	InferEvery metav1.Duration `json:"infer_every"`
	// How often to completely retrain the models. If not set, value of infer_every is used and retrain happens on every inference run.
	FitEvery metav1.Duration `json:"fit_every,omitempty"`
	// Specifies when to initiate the first fit_every call. Accepts either an ISO 8601 datetime or a time in HH:MM format. If the specified time is in the past, the next suitable time is calculated based on the fit_every interval. For the HH:MM format, if the time is in the past, it will be scheduled for the same time on the following day, respecting the tz argument if provided. By default, the timezone defaults to UTC.
	StartFrom Time `json:"start_from,omitempty"`
	// Defines the local timezone for the start_from parameter, if specified. Defaults to UTC if no timezone is provided.
	Timezone Location `json:"tz,omitempty"`
}

func (s *VMAnomalyPeriodicScheduler) Validate() error {
	return nil
}

// VMAnomalyPeriodicSchedulerStatus defines the observed state of VMAnomalyPeriodicScheduler.
type VMAnomalyPeriodicSchedulerStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyPeriodicScheduler) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyPeriodicScheduler is the Schema for the vmanomalyperiodicschedulers API.
type VMAnomalyPeriodicScheduler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyPeriodicSchedulerSpec   `json:"spec,omitempty"`
	Status VMAnomalyPeriodicSchedulerStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyPeriodicSchedulerSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyPeriodicSchedulerSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyPeriodicScheduler spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyPeriodicSchedulerList contains a list of VMAnomalyPeriodicScheduler.
type VMAnomalyPeriodicSchedulerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyPeriodicScheduler `json:"items"`
}

func (l *VMAnomalyPeriodicSchedulerList) ItemsList() []VMAnomalyPeriodicScheduler {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyPeriodicScheduler{}, &VMAnomalyPeriodicSchedulerList{})
}
