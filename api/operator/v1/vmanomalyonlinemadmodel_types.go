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

// VMAnomalyOnlineMadModelSpec defines the desired state of VMAnomalyOnlineMadModel.
type VMAnomalyOnlineMadModelSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// The threshold multiplier for the MAD to determine anomalies.
	Threshold float64 `json:"threshold,omitempty"`
	// The minimum number of samples to be seen before computing the anomaly score.
	// Otherwise, the anomaly score will be 0, as there is not enough data to trust the model’s predictions. Defaults to 16
	MinSamplesSeen int `json:"min_n_samples_seen,omitempty"`
	// The compression parameter for underlying t-digest . Higher values mean higher accuracy but higher memory usage. By default 100.
	Compression int `json:"compression,omitempty"`
}

func (s *VMAnomalyOnlineMadModel) Validate() error {
	return nil
}

// VMAnomalyOnlineMadModelStatus defines the observed state of VMAnomalyOnlineMadModel.
type VMAnomalyOnlineMadModelStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyOnlineMadModel) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyOnlineMadModel is the Schema for the vmanomalyonlinemadmodels API.
type VMAnomalyOnlineMadModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyOnlineMadModelSpec   `json:"spec,omitempty"`
	Status VMAnomalyOnlineMadModelStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyOnlineMadModelSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyOnlineMadModelSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyOnlineMadModel spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyOnlineMadModelList contains a list of VMAnomalyOnlineMadModel.
type VMAnomalyOnlineMadModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyOnlineMadModel `json:"items"`
}

func (l *VMAnomalyOnlineMadModelList) ItemsList() []VMAnomalyOnlineMadModel {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyOnlineMadModel{}, &VMAnomalyOnlineMadModelList{})
}
