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

// VMAnomalyMadModelSpec defines the desired state of VMAnomalyMadModel.
type VMAnomalyMadModelSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// The threshold multiplier for the MAD to determine anomalies.
	Threshold float64 `json:"threshold,omitempty"`
}

func (s *VMAnomalyMadModel) Validate() error {
	return nil
}

// VMAnomalyMadModelStatus defines the observed state of VMAnomalyMadModel.
type VMAnomalyMadModelStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyMadModel) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyMadModel is the Schema for the vmanomalymadmodels API.
type VMAnomalyMadModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyMadModelSpec   `json:"spec,omitempty"`
	Status VMAnomalyMadModelStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyMadModelSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyMadModelSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyMadModel spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyMadModelList contains a list of VMAnomalyMadModel.
type VMAnomalyMadModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyMadModel `json:"items"`
}

func (l *VMAnomalyMadModelList) ItemsList() []VMAnomalyMadModel {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyMadModel{}, &VMAnomalyMadModelList{})
}
