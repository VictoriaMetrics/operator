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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VMAnomalyConfigStatus defines the observed state of VMAnomalyConfig.
type VMAnomalyConfigStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VMAnomalyConfig) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMAnomalyConfig) GetStatus() *VMAnomalyConfigStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMAnomalyConfig) DefaultStatusFields(vs *VMAnomalyConfigStatus) {}

// AsKey returns unique key for object
func (cr *VMAnomalyConfig) AsKey(_ bool) string {
	return cr.Namespace + "/" + cr.Name
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyConfig is the Schema for the vmanomalyconfigs API.
type VMAnomalyConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   runtime.RawExtension  `json:"spec"`
	Status VMAnomalyConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VMAnomalyConfigList contains a list of VMAnomalyConfig.
type VMAnomalyConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VMAnomalyConfig{}, &VMAnomalyConfigList{})
}
