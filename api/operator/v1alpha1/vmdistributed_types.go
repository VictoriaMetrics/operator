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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

const (
	ZonePlaceholder = "%ZONE%"
)

// VMDistributedSpec defines configurable parameters for VMDistributed CR
// +k8s:openapi-gen=true
type VMDistributedSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// VMAuth is a VMAuth definition (name + optional spec) that acts as a proxy for the VMUsers created by the operator.
	// Use an inline spec to define a VMAuth object in-place or provide a name to reference an existing VMAuth.
	VMAuth VMDistributedAuth `json:"vmauth,omitempty"`
	// Zones is a list of zones to update. Each item in the list represents a "zone" within the distributed setup.
	Zones []VMDistributedZone `json:"zones,omitempty"`
	// ZoneCommon defines common properties for all zones
	// +optional
	ZoneCommon VMDistributedZoneCommon `json:"zoneCommon,omitempty"`
	// License configures license key for enterprise features. If not nil, it will be passed to VMAgent, VMAuth and VMClusters.
	// +optional
	License *vmv1beta1.License `json:"license,omitempty"`
	// Paused If set to true all actions on the underlying managed objects are not
	// going to be performed, except for delete actions.
	// +optional
	Paused bool `json:"paused,omitempty"`
	// Retain keeps resources in case of VMDistributed removal
	// +optional
	Retain bool `json:"retain,omitempty"`
}

// +k8s:openapi-gen=true
// VMDistributedZoneCommon defines items, that are common for all zones
type VMDistributedZoneCommon struct {
	// VMCluster defines VictoriaMetrics cluster database
	// +optional
	VMCluster VMDistributedZoneCluster `json:"vmcluster,omitempty"`
	// VMAgent defines VMAgent to balance incoming traffic between VMClusters.
	// +optional
	VMAgent VMDistributedZoneAgent `json:"vmagent,omitempty"`
	// RemoteWrite defines VMAgent remote write settings for given zone
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	RemoteWrite VMDistributedZoneRemoteWriteSpec `json:"remoteWrite,omitempty"`
	// ReadyTimeout is the readiness timeout for each zone update.
	// +optional
	ReadyTimeout *metav1.Duration `json:"readyTimeout,omitempty"`
	// UpdatePause is the time the operator should wait between zone updates to ensure a smooth transition.
	// +optional
	UpdatePause *metav1.Duration `json:"updatePause,omitempty"`
}

// +k8s:openapi-gen=true
// VMDistributedZone defines items within a single zone to update.
type VMDistributedZone struct {
	// Name defines a name of zone, which can be used in zoneCommon spec as %ZONE%
	Name string `json:"name"`
	// VMCluster defines a new inline or referencing existing one VMCluster
	// +optional
	VMCluster VMDistributedZoneCluster `json:"vmcluster,omitempty"`
	// VMAgent defines VMAgent to balance incoming traffic between VMClusters.
	// +optional
	VMAgent VMDistributedZoneAgent `json:"vmagent,omitempty"`
	// RemoteWrite defines VMAgent remote write settings for given zone
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	RemoteWrite VMDistributedZoneRemoteWriteSpec `json:"remoteWrite,omitempty"`
}

// VMAgentName returns the name of the VMAgent for zone.
func (z *VMDistributedZone) VMAgentName(cr *VMDistributed) string {
	switch {
	case len(z.VMAgent.Name) > 0:
		return z.VMAgent.Name
	case len(cr.Spec.ZoneCommon.VMAgent.Name) > 0:
		return strings.ReplaceAll(cr.Spec.ZoneCommon.VMAgent.Name, ZonePlaceholder, z.Name)
	default:
		return fmt.Sprintf("%s-%s", cr.Name, z.Name)
	}
}

// VMClusterName return cluster name for zone
func (z *VMDistributedZone) VMClusterName(cr *VMDistributed) string {
	switch {
	case len(z.VMCluster.Name) > 0:
		return z.VMCluster.Name
	case len(cr.Spec.ZoneCommon.VMCluster.Name) > 0:
		return strings.ReplaceAll(cr.Spec.ZoneCommon.VMCluster.Name, ZonePlaceholder, z.Name)
	default:
		return fmt.Sprintf("%s-%s", cr.Name, z.Name)
	}
}

// +k8s:openapi-gen=true
// VMDistributedZoneCluster defines the name and specification of a VMCluster to be created or updated.
type VMDistributedZoneCluster struct {
	// Name specifies the static name to be used for the new VMCluster.
	// +optional
	Name string `json:"name,omitempty"`

	// Spec defines the desired state of a new or update spec for existing VMCluster.
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	Spec vmv1beta1.VMClusterSpec `json:"spec"`
}

// +k8s:openapi-gen=true
// VMDistributedZoneAgent is a name and a specification of a new VMAgent.
type VMDistributedZoneAgent struct {
	// Name specifies the static name to be used for the VMAgent when Spec is provided.
	// +optional
	Name string `json:"name,omitempty"`

	// Spec defines the desired state of a new VMAgent.
	// +optional
	Spec VMDistributedZoneAgentSpec `json:"spec,omitempty"`
}

// +k8s:openapi-gen=true
// VMDistributedZoneAgentSpec is a customized specification of a new VMAgent.
// It includes selected options from the original VMAgentSpec.
type VMDistributedZoneAgentSpec struct {
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

func (s *VMDistributedZoneAgentSpec) ToVMAgentSpec() (*vmv1beta1.VMAgentSpec, error) {
	vmAgentSpecData, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal: %w", err)
	}
	var vmAgentSpec vmv1beta1.VMAgentSpec
	if err := json.Unmarshal(vmAgentSpecData, &vmAgentSpec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal: %w", err)
	}
	return &vmAgentSpec, nil
}

// VMDistributedZoneRemoteWriteSpec is a copy of VMAgentRemoteWriteSpec, which allows empty URLs and has no relabeling or stream aggregation
// These urls will be overwritten by the controller
// +k8s:openapi-gen=true
type VMDistributedZoneRemoteWriteSpec struct {
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

func (s *VMDistributedZoneRemoteWriteSpec) ToVMAgentRemoteWriteSpec() (*vmv1beta1.VMAgentRemoteWriteSpec, error) {
	remoteWriteData, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal remote write: %w", err)
	}
	var remoteWrite vmv1beta1.VMAgentRemoteWriteSpec
	if err := json.Unmarshal(remoteWriteData, &remoteWrite); err != nil {
		return nil, fmt.Errorf("failed to unmarshal remote write: %w", err)
	}
	return &remoteWrite, nil
}

// +k8s:openapi-gen=true
// VMDistributedAuth defines a VMAuth by name or inline spec
type VMDistributedAuth struct {
	// Name specifies the static name to be used for the VMDistributedAuth when Spec is provided.
	// +optional
	Name string `json:"name,omitempty"`
	// Spec defines the desired state of a new VMAuth.
	// +optional
	Spec vmv1beta1.VMAuthSpec `json:"spec,omitempty"`
}

// +k8s:openapi-gen=true
// VMDistributedStatus defines the observed state of VMDistributedStatus
type VMDistributedStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	LastAppliedSpec *VMDistributedSpec `json:"lastAppliedSpec,omitempty"`
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
// VMDistributed is progressively rolling out updates to multiple zone components.
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

// VMAuthName returns the name of the VMAuth resource to be created/managed.
func (cr *VMDistributed) VMAuthName() string {
	switch {
	case len(cr.Spec.VMAuth.Name) > 0:
		return cr.Spec.VMAuth.Name
	default:
		return cr.Name
	}
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

// LastSpecUpdated compares spec with last applied spec stored, replaces old spec and returns true if it's updated
func (cr *VMDistributed) LastSpecUpdated() bool {
	updated := cr.Status.LastAppliedSpec == nil || !equality.Semantic.DeepEqual(&cr.Spec, cr.Status.LastAppliedSpec)
	cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
	return updated
}

// Paused checks if resource reconcile should be paused
func (cr *VMDistributed) Paused() bool {
	return cr.Spec.Paused
}

// SelectorLabels returns selector labels for distributed resources
func (cr *VMDistributed) SelectorLabels() map[string]string {
	return nil
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
	zones := make(map[string]struct{})
	clusters := make(map[string]struct{})
	agents := make(map[string]struct{})
	spec := cr.Spec
	hasCommonVMInsert := cr.Spec.ZoneCommon.VMCluster.Spec.VMInsert != nil
	hasCommonVMSelect := cr.Spec.ZoneCommon.VMCluster.Spec.VMSelect != nil
	for i := range spec.Zones {
		zone := &spec.Zones[i]
		if len(zone.Name) == 0 {
			return fmt.Errorf("spec.zones[%d].name is required", i)
		}
		if _, ok := zones[zone.Name]; ok {
			return fmt.Errorf("spec.zones[%d].name=%s is duplicated, zone names must be unique", i, zone.Name)
		}
		zones[zone.Name] = struct{}{}
		clusterName := zone.VMClusterName(cr)
		agentName := zone.VMAgentName(cr)
		if len(clusterName) > 0 {
			if _, ok := clusters[clusterName]; ok {
				return fmt.Errorf("spec.zones[%d].vmcluster.name=%s is already added in a different zone", i, clusterName)
			}
			clusters[clusterName] = struct{}{}
		}
		if len(agentName) > 0 {
			if _, ok := agents[agentName]; ok {
				return fmt.Errorf("spec.zones[%d].vmagent.name=%s is already added in a different zone", i, agentName)
			}
			agents[agentName] = struct{}{}
		}
		if zone.VMCluster.Spec.VMInsert == nil && !hasCommonVMInsert {
			return fmt.Errorf("either zoneCommon.vmcluster.spec.vminsert or spec.zones[%d].vmcluster.spec.vminsert is required", i)
		}
		if zone.VMCluster.Spec.VMSelect == nil && !hasCommonVMSelect {
			return fmt.Errorf("either zoneCommon.vmcluster.spec.vmselect or spec.zones[%d].vmcluster.spec.vmselect is required", i)
		}
	}
	return nil
}
