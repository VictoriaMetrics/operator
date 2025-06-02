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

// VMAnomalyRollingQuantileModelSpec defines the desired state of VMAnomalyRollingQuantileModel.
type VMAnomalyRollingQuantileModelSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// Quantile value, from 0.5 to 1.0. This constraint is implied by 2-sided confidence interval.
	Quantile float64 `json:"quantile,omitempty"`
	// Size of the moving window.
	WindowSteps int `json:"window_steps,omitempty"`
}

func (s *VMAnomalyRollingQuantileModel) Validate() error {
	return nil
}

// VMAnomalyRollingQuantileModelStatus defines the observed state of VMAnomalyRollingQuantileModel.
type VMAnomalyRollingQuantileModelStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyRollingQuantileModel) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyRollingQuantileModel is the Schema for the vmanomalyrollingquantilemodels API.
type VMAnomalyRollingQuantileModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyRollingQuantileModelSpec   `json:"spec,omitempty"`
	Status VMAnomalyRollingQuantileModelStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyRollingQuantileModelSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyRollingQuantileModelSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyRollingQuantileModel spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyRollingQuantileModelList contains a list of VMAnomalyRollingQuantileModel.
type VMAnomalyRollingQuantileModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyRollingQuantileModel `json:"items"`
}

func (l *VMAnomalyRollingQuantileModelList) ItemsList() []VMAnomalyRollingQuantileModel {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyRollingQuantileModel{}, &VMAnomalyRollingQuantileModelList{})
}
