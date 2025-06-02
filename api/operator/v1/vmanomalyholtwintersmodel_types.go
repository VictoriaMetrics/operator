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

// VMAnomalyHoltWintersModelSpec defines the desired state of VMAnomalyHoltWintersModel.
type VMAnomalyHoltWintersModelSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// Frequency must be set equal to sampling_period. Model needs to know expected data-points frequency (e.g. ‘10m’).
	// If omitted, frequency is guessed during fitting as the median of intervals between fitting data timestamps.
	Frequency   metav1.Duration `json:"frequency,omitempty"`
	Seasonality metav1.Duration `json:"seasonality"`
	// Standard score for calculating boundaries to define anomaly score. Defaults to 2.5.
	Threshold float64 `json:"z_threshold,omitempty"`
	// Inner model args (key-value pairs). See accepted params in model documentation
	Args Args `json:"args,omitempty"`
}

func (s *VMAnomalyHoltWintersModel) Validate() error {
	return nil
}

// VMAnomalyHoltWintersModelStatus defines the observed state of VMAnomalyHoltWintersModel.
type VMAnomalyHoltWintersModelStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyHoltWintersModel) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyHoltWintersModel is the Schema for the vmanomalyholtwintersmodels API.
type VMAnomalyHoltWintersModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyHoltWintersModelSpec   `json:"spec,omitempty"`
	Status VMAnomalyHoltWintersModelStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyHoltWintersModelSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyHoltWintersModelSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyHoltWintersModel spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyHoltWintersModelList contains a list of VMAnomalyHoltWintersModel.
type VMAnomalyHoltWintersModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyHoltWintersModel `json:"items"`
}

func (l *VMAnomalyHoltWintersModelList) ItemsList() []VMAnomalyHoltWintersModel {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyHoltWintersModel{}, &VMAnomalyHoltWintersModelList{})
}
