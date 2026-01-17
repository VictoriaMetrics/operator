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

// VMAnomalySchedulerSpec defines the desired state of VMAnomalyScheduler.
type VMAnomalySchedulerSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// Class defines anomaly detection scheduler class
	Class string `json:"class" yaml:"class"`
	// Params defines anomaly detection scheduler params
	Params runtime.RawExtension `json:"params,omitempty" yaml:"params,omitempty"`
}

// VMAnomalySchedulerStatus defines the observed state of VMAnomalyScheduler.
type VMAnomalySchedulerStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VMAnomalyScheduler) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// AsKey returns unique key for object
func (cr *VMAnomalyScheduler) AsKey(_ bool) string {
	return cr.Namespace + "/" + cr.Name
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalySchedulerSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalySchedulerSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyScheduler is the Schema for the vmanomalyschedulers API.
type VMAnomalyScheduler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalySchedulerSpec   `json:"spec,omitempty"`
	Status VMAnomalySchedulerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VMAnomalySchedulerList contains a list of VMAnomalyScheduler.
type VMAnomalySchedulerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyScheduler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VMAnomalyScheduler{}, &VMAnomalySchedulerList{})
}
