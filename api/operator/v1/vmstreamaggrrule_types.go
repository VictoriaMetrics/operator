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

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VMStreamAggrRuleSpec defines the desired state of VMStreamAggrRule.
// +k8s:openapi-gen=true
type VMStreamAggrRuleSpec struct {
	vmv1beta1.StreamAggrRule `json:",inline"`
}

// VMStreamAggrRuleStatus defines the observed state of VMStreamAggrRule.
// +k8s:openapi-gen=true
type VMStreamAggrRuleStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMStreamAggrRule) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// Validate performs semantic validation of object
func (cr *VMStreamAggrRule) Validate() error {
	if vmv1beta1.MustSkipCRValidation(cr) {
		return nil
	}
	return nil
}

// VMStreamAggrRule is the Schema for the vmstreamaggrrules API.
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMStreamAggrRule"
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmstreamaggrrules,scope=Namespaced
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus"
// +kubebuilder:printcolumn:name="Sync Error",type="string",JSONPath=".status.reason"
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VMStreamAggrRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMStreamAggrRuleSpec   `json:"spec,omitempty"`
	Status VMStreamAggrRuleStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VMStreamAggrRuleList contains a list of VMStreamAggrRule.
type VMStreamAggrRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMStreamAggrRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VMStreamAggrRule{}, &VMStreamAggrRuleList{})
}
