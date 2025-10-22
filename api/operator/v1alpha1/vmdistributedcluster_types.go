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

package v1alpha1

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VMDistributedClusterSpec defines the desired state of VMDistributedClusterSpec
// +k8s:openapi-gen=true
type VMDistributedClusterSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// VMAgent points to the VMAgent object for collecting metrics from multiple VMClusters
	VMAgent corev1.LocalObjectReference `json:"vmAgent,omitempty"`
	// VMUsers is a list of VMUser objects controlling traffic distribution between multiple VMClusters
	VMUsers []corev1.LocalObjectReference `json:"vmUsers,omitempty"`
	// VMClusters is a list of VMCluster instances to update
	VMClusters []VMClusterRefOrSpec `json:"vmClusters,omitempty"`
	// ClusterVersion defines expected image tag for all components.
	ClusterVersion string `json:"clusterVersion,omitempty"`
	// Paused If set to true all actions on the underlying managed objects are not
	// going to be performed, except for delete actions.
	// +optional
	Paused bool `json:"paused,omitempty"`
}

// +k8s:openapi-gen=true
// VMClusterRefOrSpec is either a reference to existing VMCluster or a specification of a new VMCluster.
// +kubebuilder:validation:Xor=Ref,Spec
type VMClusterRefOrSpec struct {
	// Ref points to the VMCluster object.
	// +optional
	Ref *corev1.LocalObjectReference `json:"ref,omitempty"`
	// Spec defines the desired state of a new VMCluster.
	// +optional
	Spec *vmv1beta1.VMClusterSpec `json:"spec,omitempty"`
}

// +k8s:openapi-gen=true
// VMDistributedClusterStatus defines the observed state of VMDistributedClusterStatus
type VMDistributedClusterStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
	// VMClusterInfo is a list of VMCluster-generation pairs
	VMClusterInfo []VMClusterStatus `json:"vmClusterGenerations,omitempty"`
}

// +k8s:openapi-gen=true
// VMClusterStatus is a pair of VMCluster and its generation
type VMClusterStatus struct {
	VMClusterName string              `json:"vmClusterName"`
	TargetRef     vmv1beta1.TargetRef `json:"targetRef"`
	Generation    int64               `json:"generation"`
}

// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMDistributedCluster App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +kubebuilder:object:root=true
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmdistributedclusters,scope=Namespaced
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="current status of update rollout"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// VMDistributedClusterSpec is progressively rolling out updates to multiple VMClusters.
type VMDistributedCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of VMDistributedCluster
	// +required
	Spec VMDistributedClusterSpec `json:"spec"`

	// status defines the observed state of VMDistributedCluster
	// +optional
	Status VMDistributedClusterStatus `json:"status,omitempty,omitzero"`

	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VMDistributedClusterSpec `json:"-" yaml:"-"`
}

// +kubebuilder:object:root=true

// VMDistributedClusterList contains a list of VMDistributedCluster
type VMDistributedClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMDistributedCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VMDistributedCluster{}, &VMDistributedClusterList{})
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMDistributedCluster) GetStatus() *VMDistributedClusterStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMDistributedCluster) DefaultStatusFields(vs *VMDistributedClusterStatus) {
}

// GetStatusMetadata returns metadata for object status
func (cr *VMDistributedClusterStatus) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.StatusMetadata
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VMDistributedCluster) LastAppliedSpecAsPatch() (client.Patch, error) {
	return vmv1beta1.LastAppliedChangesAsPatch(cr.Spec)
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (cr *VMDistributedCluster) HasSpecChanges() (bool, error) {
	return vmv1beta1.HasStateChanges(cr.ObjectMeta, cr.Spec)
}

// Paused checks if resource reconcile should be paused
func (cr *VMDistributedCluster) Paused() bool {
	return cr.Spec.Paused
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMDistributedClusterSpec) UnmarshalJSON(src []byte) error {
	type pcr VMDistributedClusterSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vmdistributedcluster spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}
