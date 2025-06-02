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

// VMAnomalyStdModelSpec defines the desired state of VMAnomalyStdModel.
type VMAnomalyStdModelSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// Standard score for calculation boundaries and anomaly score. Defaults to 2.5.
	Threshold float64 `json:"z_threshold,omitempty"`
	// Number of datapoints in one season.
	Period int `json:"period,omitempty"`
}

func (s *VMAnomalyStdModel) Validate() error {
	return nil
}

// VMAnomalyStdModelStatus defines the observed state of VMAnomalyStdModel.
type VMAnomalyStdModelStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyStdModel) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyStdModel is the Schema for the vmanomalystdmodels API.
type VMAnomalyStdModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyStdModelSpec   `json:"spec,omitempty"`
	Status VMAnomalyStdModelStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyStdModelSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyStdModelSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyStdModel spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyStdModelList contains a list of VMAnomalyStdModel.
type VMAnomalyStdModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyStdModel `json:"items"`
}

func (l *VMAnomalyStdModelList) ItemsList() []VMAnomalyStdModel {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyStdModel{}, &VMAnomalyStdModelList{})
}
