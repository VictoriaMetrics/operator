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
	"github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VMAgentSpec defines the desired state of VMAgent
// +k8s:openapi-gen=true
type VMDistributedClusterSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// VMAuth points to the VMAuth object controlling traffic distribution between multiple VMClusters
	VMAuth *v1beta1.VMAuth `json:"vmAuth,omitempty"`
	// VMClusters is a list of VMCluster instances to update
	VMClusters []v1beta1.VMCluster `json:"vmClusters"`
	// ClusterVersion defines expected image tag for all components.
	ClusterVersion string `json:"clusterVersion,omitempty"`
}

// +k8s:openapi-gen=true
// VMDistributedClusterStatus defines the observed state of VMDistributedClusterStatus
type VMDistributedClusterStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
	// VMClusterGenerations is a list of VMCluster-generation pairs
	VMClusterGenerations []VMClusterGenerationPair `json:"vmClusterGenerations,omitempty"`
}

// +k8s:openapi-gen=true
// VMClusterGenerationPair is a pair of VMCluster and its generation
type VMClusterGenerationPair struct {
	VMCluster  v1beta1.VMCluster `json:"vmCluster"`
	Generation int64             `json:"generation"`
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
// +kubebuilder:resource:path=VMDistributedClusters,scope=Namespaced
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
