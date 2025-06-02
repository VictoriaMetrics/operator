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

// VMAnomalyZScoreModelSpec defines the desired state of VMAnomalyZScoreModel.
type VMAnomalyZScoreModelSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// Standard score for calculation boundaries and anomaly score. Defaults to 2.5.
	Threshold float64 `json:"z_threshold,omitempty"`
}

func (s *VMAnomalyZScoreModel) Validate() error {
	return nil
}

// VMAnomalyZScoreModelStatus defines the observed state of VMAnomalyZScoreModel.
type VMAnomalyZScoreModelStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyZScoreModel) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyZScoreModel is the Schema for the vmanomalyzscoremodels API.
type VMAnomalyZScoreModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyZScoreModelSpec   `json:"spec,omitempty"`
	Status VMAnomalyZScoreModelStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyZScoreModelSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyZScoreModelSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyZScoreModel spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyZScoreModelList contains a list of VMAnomalyZScoreModel.
type VMAnomalyZScoreModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyZScoreModel `json:"items"`
}

func (l *VMAnomalyZScoreModelList) ItemsList() []VMAnomalyZScoreModel {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyZScoreModel{}, &VMAnomalyZScoreModelList{})
}
