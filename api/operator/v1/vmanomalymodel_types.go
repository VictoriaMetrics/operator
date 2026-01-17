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
	"k8s.io/apimachinery/pkg/runtime"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VMAnomalyModelSpec defines the desired state of VMAnomalyModel.
type VMAnomalyModelSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// Class defines anomaly detection model class
	Class string `json:"class" yaml:"class"`
	// Params defines anomaly detection model params
	Params runtime.RawExtension `json:"params,omitempty" yaml:"params,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyModelSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyModelSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// VMAnomalyModelStatus defines the observed state of VMAnomalyModel.
type VMAnomalyModelStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VMAnomalyModel) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// AsKey returns unique key for object
func (cr *VMAnomalyModel) AsKey(_ bool) string {
	return cr.Namespace + "/" + cr.Name
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyModel is the Schema for the vmanomalymodels API.
type VMAnomalyModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyModelSpec   `json:"spec,omitempty"`
	Status VMAnomalyModelStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VMAnomalyModelList contains a list of VMAnomalyModel.
type VMAnomalyModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyModel `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VMAnomalyModel{}, &VMAnomalyModelList{})
}
