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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VMDistributedSpec defines the desired state of VMDistributedSpec
// +k8s:openapi-gen=true
type VMDistributedSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// ReadyDeadline is the deadline for the VMCluster to be ready.
	// +optional
	ReadyDeadline *metav1.Duration `json:"readyDeadline,omitempty"`
	// VMAgentFlushDeadline is the deadline for VMAgent to flush accumulated queue.
	// +optional
	VMAgentFlushDeadline *metav1.Duration `json:"vmAgentFlushDeadline,omitempty"`
	// ZoneUpdatePause is the time the operator should wait between zone updates to ensure a smooth transition.
	// +optional
	ZoneUpdatePause *metav1.Duration `json:"zoneUpdatePause,omitempty"`
	// VMAgent is the name and spec of the VM agent to balance traffic between VMClusters.
	VMAgent VMAgentNameAndSpec `json:"vmagent,omitempty"`
	// VMAuth is a VMAuth definition (name + optional spec) that acts as a proxy for the VMUsers created by the operator.
	// Use an inline spec to define a VMAuth object in-place or provide a name to reference an existing VMAuth.
	VMAuth VMAuthNameAndSpec `json:"vmauth,omitempty"`
	// Zones is a list of VMCluster instances to update. Each VMCluster in the list represents a "zone" within the distributed cluster.
	Zones ZoneSpec `json:"zones,omitempty"`
	// License configures license key for enterprise features. If not nil, it will be passed to VMAgent, VMAuth and VMClusters.
	// +optional
	License *vmv1beta1.License `json:"license,omitempty"`
	// ClusterVersion defines expected image tag for all components.

	// Paused If set to true all actions on the underlying managed objects are not
	// going to be performed, except for delete actions.
	// +optional
	Paused bool `json:"paused,omitempty"`
}

// +k8s:openapi-gen=true
// ZoneSpec is a list of VMCluster instances to update.
type ZoneSpec struct {
	// GlobalClusterSpec specifies an override to all VMClusters.
	// These overrides are applied to the referenced object if `ref` is specified.
	// +kubebuilder:validation:Type=object
	// +kubebuilder:validation:XPreserveUnknownFields
	// +optional
	GlobalClusterSpec *apiextensionsv1.JSON `json:"globalClusterSpec,omitempty"`

	// Each VMClusterObjOrRef is either defining a new inline VMCluster or referencing an existing one.
	VMClusters []VMClusterObjOrRef `json:"vmclusters,omitempty"`
}

func (s *ZoneSpec) validate() error {
	for idx, refOrSpec := range s.VMClusters {
		if err := refOrSpec.validate(idx); err != nil {
			return err
		}
	}
	return nil
}

// +k8s:openapi-gen=true
// VMClusterObjOrRef is either a reference to existing VMCluster or a specification of a new VMCluster.
// +kubebuilder:validation:Xor=Ref,Name
type VMClusterObjOrRef struct {
	// Ref points to the VMCluster object.
	// If Ref is specified, Name is ignored.
	// +optional
	Ref *corev1.LocalObjectReference `json:"ref,omitempty"`

	// Name specifies the static name to be used for the new VMCluster.
	// This field is ignored if `ref` is specified.
	// +optional
	Name string `json:"name,omitempty"`

	// Spec defines the desired state of a new or update spec for existing VMCluster.
	// +optional
	Spec *vmv1beta1.VMClusterSpec `json:"spec,omitempty"`
}

func (s *VMClusterObjOrRef) validate(idx int) error {
	// Check mutual exclusivity: either ref or name must be set, but not both
	if s.Ref != nil && len(s.Ref.Name) > 0 && len(s.Name) > 0 {
		return fmt.Errorf("vmclusters[%d] must specify either ref or name, not both", idx)
	}

	// Check that at least one of ref or name is set
	if s.Ref == nil || len(s.Ref.Name) == 0 {
		if len(s.Name) == 0 {
			return fmt.Errorf("vmclusters[%d] must have either ref or name set", idx)
		} else if s.Spec == nil {
			return fmt.Errorf("vmclusters[%d] must have spec if name is set", idx)
		}
	}

	return nil
}

// +k8s:openapi-gen=true
// VMAgentNameAndSpec is a name and a specification of a new VMAgent.
type VMAgentNameAndSpec struct {
	// Name specifies the static name to be used for the VMAgent when Spec is provided.
	// +optional
	Name string `json:"name,omitempty"`

	// LabelSelector specifies VMAgents to be selected for metrics check.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// Spec defines the desired state of a new VMAgent.
	// Note that RemoteWrite and RemoteWriteSettings are ignored as its managed by the operator.
	// +optional
	Spec *CustomVMAgentSpec `json:"spec,omitempty"`
}

// +k8s:openapi-gen=true
// CustomVMAgentSpec is a customized specification of a new VMAgent.
// It includes selected options from the original VMAgentSpec.
type CustomVMAgentSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// PodMetadata configures Labels and Annotations which are propagated to the vmagent pods.
	// +optional
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *vmv1beta1.ManagedObjectsMetadata `json:"managedMetadata,omitempty"`
	// LogLevel for VMAgent to be configured with.
	// INFO, WARN, ERROR, FATAL, PANIC
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VMAgent to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`

	// RemoteWrite list of victoria metrics /some other remote write system
	// for vm it must looks like: http://victoria-metrics-single:8428/api/v1/write
	// or for cluster different url
	// https://docs.victoriametrics.com/victoriametrics/vmagent/#splitting-data-streams-among-multiple-systems
	// +optional
	RemoteWrite []CustomVMAgentRemoteWriteSpec `json:"remoteWrite"`
	// RemoteWriteSettings defines global settings for all remoteWrite urls.
	// +optional
	RemoteWriteSettings *vmv1beta1.VMAgentRemoteWriteSettings `json:"remoteWriteSettings,omitempty"`
	// UpdateStrategy - overrides default update strategy.
	// works only for deployments, statefulset always use OnDelete.
	// +kubebuilder:validation:Enum=Recreate;RollingUpdate
	// +optional
	UpdateStrategy *appsv1.DeploymentStrategyType `json:"updateStrategy,omitempty"`
	// RollingUpdate - overrides deployment update params.
	// +optional
	RollingUpdate *appsv1.RollingUpdateDeployment `json:"rollingUpdate,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget       *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*vmv1beta1.EmbeddedProbes `json:",inline"`
	// StatefulMode enables StatefulSet for `VMAgent` instead of Deployment
	// it allows using persistent storage for vmagent's persistentQueue
	// +optional
	StatefulMode bool `json:"statefulMode,omitempty"`
	// StatefulStorage configures storage for StatefulSet
	// +optional
	StatefulStorage *vmv1beta1.StorageSpec `json:"statefulStorage,omitempty"`
	// StatefulRollingUpdateStrategy allows configuration for strategyType
	// set it to RollingUpdate for disabling operator statefulSet rollingUpdate
	// +optional
	StatefulRollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"statefulRollingUpdateStrategy,omitempty"`
	// PersistentVolumeClaimRetentionPolicy allows configuration of PVC retention policy
	// +optional
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`
	// ClaimTemplates allows adding additional VolumeClaimTemplates for VMAgent in StatefulMode
	ClaimTemplates []corev1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`

	// License allows to configure license key to be used for enterprise features.
	// Using license key is supported starting from VictoriaMetrics v1.94.0.
	// See [here](https://docs.victoriametrics.com/victoriametrics/enterprise/)
	// +optional
	License *vmv1beta1.License `json:"license,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	vmv1beta1.CommonDefaultableParams           `json:",inline,omitempty"`
	vmv1beta1.CommonApplicationDeploymentParams `json:",inline,omitempty"`
}

// CustomVMAgentRemoteWriteSpec is a copy of VMAgentRemoteWriteSpec, but allows empty URLs
// These urls will be overwritten by the controller
// +k8s:openapi-gen=true
type CustomVMAgentRemoteWriteSpec struct {
	// URL is the URL of the remote write system.
	// +optional
	URL string `json:"url,omitempty"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// +optional
	BasicAuth *vmv1beta1.BasicAuth `json:"basicAuth,omitempty"`
	// Optional bearer auth token to use for -remoteWrite.url
	// +optional
	BearerTokenSecret *corev1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`

	// OAuth2 defines auth configuration
	// +optional
	OAuth2 *vmv1beta1.OAuth2 `json:"oauth2,omitempty"`
	// TLSConfig describes tls configuration for remote write target
	// +optional
	TLSConfig *vmv1beta1.TLSConfig `json:"tlsConfig,omitempty"`
	// Timeout for sending a single block of data to -remoteWrite.url (default 1m0s)
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	SendTimeout *string `json:"sendTimeout,omitempty"`
	// Headers allow configuring custom http headers
	// Must be in form of semicolon separated header with value
	// e.g.
	// headerName: headerValue
	// vmagent supports since 1.79.0 version
	// +optional
	Headers []string `json:"headers,omitempty"`

	// MaxDiskUsage defines the maximum file-based buffer size in bytes for the given remoteWrite
	// It overrides global configuration defined at remoteWriteSettings.maxDiskUsagePerURL
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	MaxDiskUsage *vmv1beta1.BytesString `json:"maxDiskUsage,omitempty"`
	// ForceVMProto forces using VictoriaMetrics protocol for sending data to -remoteWrite.url
	// +optional
	ForceVMProto bool `json:"forceVMProto,omitempty"`
	// ProxyURL for -remoteWrite.url. Supported proxies: http, https, socks5. Example: socks5://proxy:1234
	// +optional
	ProxyURL *string `json:"proxyURL,omitempty"`
	// AWS describes params specific to AWS cloud
	AWS *vmv1beta1.AWS `json:"aws,omitempty"`
}

// +k8s:openapi-gen=true
// VMAuthNameAndSpec defines a VMAuth by name or inline spec
type VMAuthNameAndSpec struct {
	// Name specifies the static name to be used for the VMAuthNameAndSpec when Spec is provided.
	// +optional
	Name string `json:"name,omitempty"`
	// Spec defines the desired state of a new VMAuth.
	// +optional
	Spec *vmv1beta1.VMAuthSpec `json:"spec,omitempty"`
}

// +k8s:openapi-gen=true
// VMDistributedStatus defines the observed state of VMDistributedStatus
type VMDistributedStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
}

// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMDistributed App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +kubebuilder:object:root=true
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmdistributed,scope=Namespaced
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="current status of update rollout"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// VMDistributedSpec is progressively rolling out updates to multiple VMClusters.
type VMDistributed struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of VMDistributed
	// +required
	Spec VMDistributedSpec `json:"spec"`

	// status defines the observed state of VMDistributed
	// +optional
	Status VMDistributedStatus `json:"status,omitempty,omitzero"`
}

// SelectorLabels defines selector labels for given component kind
func (cr *VMDistributed) SelectorLabels(kind vmv1beta1.ClusterComponent) map[string]string {
	return vmv1beta1.ClusterSelectorLabels(kind, cr.Name, "vmd")
}

// PrefixedName returns prefixed name for the given component kind
func (cr *VMDistributed) PrefixedName(kind vmv1beta1.ClusterComponent) string {
	return vmv1beta1.ClusterPrefixedName(kind, cr.Name, "vmd", false)
}

// FinalLabels adds cluster labels to the base labels and filters by prefix if needed
func (cr *VMDistributed) FinalLabels(kind vmv1beta1.ClusterComponent) map[string]string {
	return vmv1beta1.AddClusterLabels(cr.SelectorLabels(kind), "vmd")
}

// AnnotationsFiltered returns global annotations to be applied by objects generate for vmcluster
func (cr *VMDistributed) AnnotationsFiltered() map[string]string {
	return map[string]string{}
}

// Owns returns error if owned by other CR
func (cr *VMDistributed) Owns(r client.Object) error {
	refs := r.GetOwnerReferences()
	for i := range refs {
		ref := &refs[i]
		if ref.APIVersion == cr.APIVersion && ref.Kind == cr.Kind {
			if ref.Name != cr.Name {
				return fmt.Errorf("%T %s/%s is owned by other distributed resource: %s, expected: %s", r, r.GetNamespace(), r.GetName(), ref.Name, cr.Name)
			}
		}
	}
	return nil
}

// AsOwner returns owner references with current object as owner
func (cr *VMDistributed) AsOwner() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         cr.APIVersion,
		Kind:               cr.Kind,
		Name:               cr.Name,
		UID:                cr.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
}

// PodLabels returns pod labels for given component kind
func (cr *VMDistributed) PodLabels(kind vmv1beta1.ClusterComponent) map[string]string {
	selectorLabels := cr.SelectorLabels(kind)
	podMetadata := cr.PodMetadata(kind)
	if podMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(podMetadata.Labels, selectorLabels)
}

func (cr *VMDistributed) GetVMAuthSpec() *vmv1beta1.VMAuthSpec {
	if cr == nil {
		return nil
	}
	spec := cr.Spec.VMAuth.Spec
	if spec == nil {
		spec = &vmv1beta1.VMAuthSpec{
			CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{},
		}
	}
	// Make a copy to avoid modifying the original spec
	specCopy := spec.DeepCopy()
	// If License is not set in VMAuth spec but is set in VMDistributed, use the VMDistributed License
	if specCopy.License == nil && cr.Spec.License != nil {
		specCopy.License = cr.Spec.License
	}
	return specCopy
}

// PodMetadata returns pod metadata for given component kind
func (cr *VMDistributed) PodMetadata(kind vmv1beta1.ClusterComponent) *vmv1beta1.EmbeddedObjectMetadata {
	return cr.GetVMAuthSpec().PodMetadata
}

// FinalAnnotations returns global annotations to be applied by objects generate for vmcluster
func (cr *VMDistributed) FinalAnnotations() map[string]string {
	return map[string]string{}
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMDistributed) GetAdditionalService(kind vmv1beta1.ClusterComponent) *vmv1beta1.AdditionalServiceSpec {
	return nil
}

// GetServiceAccountName returns service account name for all vmcluster components
func (cr *VMDistributed) GetServiceAccountName() string {
	return cr.PrefixedName(vmv1beta1.ClusterComponentBalancer)
}

// PodAnnotations returns pod annotations for given component kind
func (cr *VMDistributed) PodAnnotations(kind vmv1beta1.ClusterComponent) map[string]string {
	podMetadata := cr.PodMetadata(kind)
	if podMetadata == nil {
		return nil
	}
	return podMetadata.Annotations
}

func (cr *VMDistributed) IsOwnsServiceAccount() bool {
	return false
}

// PrefixedInternalName returns prefixed name for the given component kind
func (cr *VMDistributed) PrefixedInternalName(kind vmv1beta1.ClusterComponent) string {
	return vmv1beta1.ClusterPrefixedName(kind, cr.Name, "vmd", true)
}

// PrefixedInternalName returns prefixed name for the given component kind
func (cr *VMDistributed) AllLabels() map[string]string {
	return cr.SelectorLabels(vmv1beta1.ClusterComponentBalancer)
}

// +kubebuilder:object:root=true

// VMDistributedList contains a list of VMDistributed
type VMDistributedList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMDistributed `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VMDistributed{}, &VMDistributedList{})
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMDistributed) GetStatus() *VMDistributedStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMDistributed) DefaultStatusFields(vs *VMDistributedStatus) {
}

// GetStatusMetadata returns metadata for object status
func (cr *VMDistributedStatus) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.StatusMetadata
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VMDistributed) LastAppliedSpecAsPatch() (client.Patch, error) {
	return vmv1beta1.LastAppliedChangesAsPatch(cr.Spec)
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (cr *VMDistributed) HasSpecChanges() (bool, error) {
	return vmv1beta1.HasStateChanges(cr.ObjectMeta, cr.Spec)
}

// Paused checks if resource reconcile should be paused
func (cr *VMDistributed) Paused() bool {
	return cr.Spec.Paused
}

func (cr *VMDistributed) GetVMUserName() string {
	return fmt.Sprintf("%s-user", cr.Name)
}

// AutomountServiceAccountToken implements reloadable interface
func (cr *VMDistributed) AutomountServiceAccountToken() bool {
	return true
}

// GetReloaderParams implements reloadable interface
func (cr *VMDistributed) GetReloaderParams() *vmv1beta1.CommonConfigReloaderParams {
	return &cr.GetVMAuthSpec().CommonConfigReloaderParams
}

// UseProxyProtocol implements reloadable interface
func (cr *VMDistributed) UseProxyProtocol() bool {
	return false
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMDistributedSpec) UnmarshalJSON(src []byte) error {
	type pcr VMDistributedSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMDistributed spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// Validate validates the VMDistributed resource
func (cr *VMDistributed) Validate() error {
	spec := cr.Spec
	// Validate VMAuth
	if spec.VMAuth.Name == "" {
		return fmt.Errorf("VMAuth.Name must be set")
	}

	// VMAgent needs to specify either Name or LabelSelector
	if spec.VMAgent.Name != "" && spec.VMAgent.LabelSelector != nil {
		return fmt.Errorf("VMAgent.Name and LabelSelector cannot be set at the same time")
	}
	if spec.VMAgent.Spec != nil && spec.VMAgent.LabelSelector != nil {
		return fmt.Errorf("VMAgent.Spec and LabelSelector cannot be set at the same time")
	}
	return spec.Zones.validate()
}
