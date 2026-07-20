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
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VLDistributedSpec defines configurable parameters for VLDistributed CR
// +k8s:openapi-gen=true
type VLDistributedSpec struct {
	// BackendType defines the storage backend type used across all zones.
	// VLCluster uses a distributed VLCluster per zone; VLSingle uses a single-node VLSingle per zone.
	// All zones must use the same backend type; mixed configurations are not supported.
	// +optional
	// +kubebuilder:validation:Enum=VLCluster;VLSingle
	BackendType VLDistributedBackendType `json:"backendType,omitempty"`
	// VMAuth is a VMAuth definition (name + optional spec) that acts as a proxy for the VMUsers created by the operator.
	// Use an inline spec to define a VMAuth object in-place or provide a name to reference an existing VMAuth.
	// +optional
	VMAuth VLDistributedAuth `json:"vmauth,omitempty"`
	// Zones is a list of zones to update. Each item in the list represents a "zone" within the distributed setup.
	Zones []VLDistributedZone `json:"zones,omitempty"`
	// ZoneCommon defines common properties for all zones
	// +optional
	ZoneCommon VLDistributedZoneCommon `json:"zoneCommon,omitempty"`
	// License configures license key for enterprise features. If not nil, it will be passed to VLAgent and VLClusters.
	// +optional
	License *vmv1beta1.License `json:"license,omitempty"`
	// Paused If set to true all actions on the underlying managed objects are not
	// going to be performed, except for delete actions.
	// +optional
	Paused bool `json:"paused,omitempty"`
	// Retain keeps resources in case of VLDistributed removal
	// +optional
	Retain bool `json:"retain,omitempty"`
}

// +k8s:openapi-gen=true
// VLDistributedZoneCommon defines items, that are common for all zones
type VLDistributedZoneCommon struct {
	// VLCluster defines VictoriaLogs cluster database
	// +optional
	VLCluster VLDistributedZoneCluster `json:"vlcluster,omitempty"`
	// VLSingle defines VictoriaLogs single-node database.
	// +optional
	VLSingle *VLDistributedZoneSingle `json:"vlsingle,omitempty"`
	// VLAgent defines VLAgent to balance incoming traffic between VLClusters.
	// +optional
	VLAgent VLDistributedZoneAgent `json:"vlagent,omitempty"`
	// RemoteWrite defines VLAgent remote write settings for given zone
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	RemoteWrite VLDistributedZoneRemoteWriteSpec `json:"remoteWrite,omitempty"`
	// ReadyTimeout is the readiness timeout for each zone update.
	// +optional
	ReadyTimeout *metav1.Duration `json:"readyTimeout,omitempty"`
	// UpdatePause is the time the operator should wait between zone updates to ensure a smooth transition.
	// +optional
	UpdatePause *metav1.Duration `json:"updatePause,omitempty"`
}

type VLDistributedTrafficMode string

const (
	VLDistributedTrafficModeReadWrite   VLDistributedTrafficMode = "read-write"
	VLDistributedTrafficModeReadOnly    VLDistributedTrafficMode = "read-only"
	VLDistributedTrafficModeWriteOnly   VLDistributedTrafficMode = "write-only"
	VLDistributedTrafficModeMaintenance VLDistributedTrafficMode = "maintenance"
)

// VLDistributedBackendType defines the storage backend type for all zones.
type VLDistributedBackendType string

const (
	// VLDistributedBackendTypeVLCluster uses VLCluster as the storage backend for all zones.
	VLDistributedBackendTypeVLCluster VLDistributedBackendType = "VLCluster"
	// VLDistributedBackendTypeVLSingle uses VLSingle as the storage backend for all zones.
	VLDistributedBackendTypeVLSingle VLDistributedBackendType = "VLSingle"
)

// +k8s:openapi-gen=true
// VLDistributedZone defines items within a single zone to update.
type VLDistributedZone struct {
	// Name defines a name of zone, which can be used in zoneCommon spec as %ZONE%
	Name string `json:"name"`
	// TrafficMode defines allowed traffic mode for a zone: read-only, write-only, read-write, maintenance
	// +kubebuilder:validation:Enum=read-only;write-only;read-write;maintenance
	TrafficMode VLDistributedTrafficMode `json:"trafficMode,omitempty"`
	// VLCluster defines a new inline or referencing existing one VLCluster.
	// Mutually exclusive with VLSingle in the same zone.
	// +optional
	VLCluster VLDistributedZoneCluster `json:"vlcluster,omitempty"`
	// VLSingle defines a new inline or referencing existing one VLSingle.
	// Mutually exclusive with VLCluster in the same zone.
	// +optional
	VLSingle *VLDistributedZoneSingle `json:"vlsingle,omitempty"`
	// VLAgent defines VLAgent to balance incoming traffic between VLClusters.
	// +optional
	VLAgent VLDistributedZoneAgent `json:"vlagent,omitempty"`
	// RemoteWrite defines VLAgent remote write settings for given zone
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	RemoteWrite VLDistributedZoneRemoteWriteSpec `json:"remoteWrite,omitempty"`
}

// VLAgentName returns the name of the VLAgent for zone.
func (z *VLDistributedZone) VLAgentName(cr *VLDistributed) string {
	switch {
	case len(z.VLAgent.Name) > 0:
		return z.VLAgent.Name
	case len(cr.Spec.ZoneCommon.VLAgent.Name) > 0:
		return strings.ReplaceAll(cr.Spec.ZoneCommon.VLAgent.Name, ZonePlaceholder, z.Name)
	default:
		return fmt.Sprintf("%s-%s", cr.Name, z.Name)
	}
}

// VLClusterName return cluster name for zone
func (z *VLDistributedZone) VLClusterName(cr *VLDistributed) string {
	switch {
	case len(z.VLCluster.Name) > 0:
		return z.VLCluster.Name
	case len(cr.Spec.ZoneCommon.VLCluster.Name) > 0:
		return strings.ReplaceAll(cr.Spec.ZoneCommon.VLCluster.Name, ZonePlaceholder, z.Name)
	default:
		return fmt.Sprintf("%s-%s", cr.Name, z.Name)
	}
}

// VLSingleName returns single name for zone
func (z *VLDistributedZone) VLSingleName(cr *VLDistributed) string {
	switch {
	case z.VLSingle != nil && len(z.VLSingle.Name) > 0:
		return z.VLSingle.Name
	case cr.Spec.ZoneCommon.VLSingle != nil && len(cr.Spec.ZoneCommon.VLSingle.Name) > 0:
		return strings.ReplaceAll(cr.Spec.ZoneCommon.VLSingle.Name, ZonePlaceholder, z.Name)
	default:
		return fmt.Sprintf("%s-%s", cr.Name, z.Name)
	}
}

// +k8s:openapi-gen=true
// VLDistributedZoneCluster defines the name and specification of a VLCluster to be created or updated.
type VLDistributedZoneCluster struct {
	// Name specifies the static name to be used for the new VLCluster.
	// +optional
	Name string `json:"name,omitempty"`

	// Spec defines the desired state of a new or update spec for existing VLCluster.
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	Spec vmv1.VLClusterSpec `json:"spec"`
}

// +k8s:openapi-gen=true
// VLDistributedZoneSingle defines the name and specification of a VLSingle to be created or updated.
type VLDistributedZoneSingle struct {
	// Name specifies the static name to be used for the new VLSingle.
	// +optional
	Name string `json:"name,omitempty"`

	// Spec defines the desired state of a new or update spec for existing VLSingle.
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	Spec *vmv1.VLSingleSpec `json:"spec,omitempty"`
}

// +k8s:openapi-gen=true
// VLDistributedZoneAgent is a name and a specification of a new VLAgent.
type VLDistributedZoneAgent struct {
	// Name specifies the static name to be used for the VLAgent when Spec is provided.
	// +optional
	Name string `json:"name,omitempty"`

	// Spec defines the desired state of a new VLAgent.
	// +optional
	Spec VLDistributedZoneAgentSpec `json:"spec,omitempty"`
}

// +k8s:openapi-gen=true
// VLDistributedZoneAgentSpec is a customized specification of a new VLAgent.
// It includes selected options from the original VLAgentSpec.
type VLDistributedZoneAgentSpec struct {
	// PodMetadata configures Labels and Annotations which are propagated to the vlagent pods.
	// +optional
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *vmv1beta1.ManagedObjectsMetadata `json:"managedMetadata,omitempty"`
	// LogLevel for VLAgent to be configured with.
	// INFO, WARN, ERROR, FATAL, PANIC
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VLAgent to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`

	// RemoteWriteSettings defines global settings for all remoteWrite urls.
	// +optional
	RemoteWriteSettings *vmv1.VLAgentRemoteWriteSettings `json:"remoteWriteSettings,omitempty"`
	// RollingUpdateStrategy allows configuration for strategyType
	// set it to RollingUpdate for disabling operator statefulSet rollingUpdate
	// +optional
	RollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"rollingUpdateStrategy,omitempty"`
	// PersistentVolumeClaimRetentionPolicy allows configuration of PVC retention policy
	// +optional
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`
	// ClaimTemplates allows adding additional VolumeClaimTemplates for VLAgent in StatefulSet mode
	ClaimTemplates []corev1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// Storage configures storage for StatefulSet
	// +optional
	Storage *vmv1beta1.StorageSpec `json:"storage,omitempty"`

	// License allows to configure license key to be used for enterprise features.
	// See [here](https://docs.victoriametrics.com/victoriametrics/enterprise/#victorialogs-enterprise-features)
	// +optional
	License *vmv1beta1.License `json:"license,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// Configures vertical pod autoscaling.
	// +optional
	VPA *vmv1beta1.EmbeddedVPA `json:"vpa,omitempty"`

	vmv1beta1.CommonAppsParams `json:",inline,omitempty"`
}

// ToVLAgentSpec converts VLDistributedZoneAgentSpec to vmv1.VLAgentSpec via JSON round-trip.
func (s *VLDistributedZoneAgentSpec) ToVLAgentSpec() (*vmv1.VLAgentSpec, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal: %w", err)
	}
	var spec vmv1.VLAgentSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal: %w", err)
	}
	return &spec, nil
}

// VLDistributedZoneRemoteWriteSpec is a copy of VLAgentRemoteWriteSpec without the URL field.
// The URL will be overwritten by the controller.
// +k8s:openapi-gen=true
type VLDistributedZoneRemoteWriteSpec struct {
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// +optional
	BasicAuth *vmv1beta1.BasicAuth `json:"basicAuth,omitempty"`
	// Optional bearer auth token to use for -remoteWrite.url
	// +optional
	BearerTokenSecret *corev1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
	// Optional bearer auth token path
	// +optional
	BearerTokenPath string `json:"bearerTokenPath,omitempty"`

	// OAuth2 defines auth configuration
	// +optional
	OAuth2 *vmv1.OAuth2 `json:"oauth2,omitempty"`
	// TLSConfig describes tls configuration for remote write target
	// +optional
	TLSConfig *vmv1.TLSConfig `json:"tlsConfig,omitempty"`
	// Timeout for sending a single block of data to -remoteWrite.url (default 1m0s)
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	SendTimeout *string `json:"sendTimeout,omitempty"`
	// Headers allow configuring custom http headers
	// Must be in form of semicolon separated header with value
	// e.g.
	// headerName: headerValue
	// +optional
	Headers []string `json:"headers,omitempty"`

	// MaxDiskUsage defines the maximum file-based buffer size in bytes for the given remoteWrite
	// It overrides global configuration defined at remoteWriteSettings.maxDiskUsagePerURL
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	MaxDiskUsage *vmv1beta1.BytesString `json:"maxDiskUsage,omitempty"`
	// ProxyURL for -remoteWrite.url. Supported proxies: http, https, socks5. Example: socks5://proxy:1234
	// +optional
	ProxyURL *string `json:"proxyURL,omitempty"`
}

// ToVLAgentRemoteWriteSpec converts to vmv1.VLAgentRemoteWriteSpec, setting the given URL.
func (s *VLDistributedZoneRemoteWriteSpec) ToVLAgentRemoteWriteSpec(url string) (*vmv1.VLAgentRemoteWriteSpec, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal remote write: %w", err)
	}
	var rw vmv1.VLAgentRemoteWriteSpec
	if err := json.Unmarshal(data, &rw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal remote write: %w", err)
	}
	rw.URL = url
	return &rw, nil
}

// +k8s:openapi-gen=true
// VLDistributedAuth defines a VMAuth by name or inline spec
type VLDistributedAuth struct {
	// Enabled defines if vmauth should be created.
	// +optional
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`
	// Name specifies the static name to be used for the VLDistributedAuth when Spec is provided.
	// +optional
	Name string `json:"name,omitempty"`
	// Spec defines the desired state of a new VMAuth.
	// +optional
	Spec vmv1beta1.VMAuthSpec `json:"spec,omitempty"`
}

// +k8s:openapi-gen=true
// VLDistributedStatus defines the observed state of VLDistributed
type VLDistributedStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	LastAppliedSpec *VLDistributedSpec `json:"lastAppliedSpec,omitempty"`
	// ParsingSpecError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingSpecError string `json:"-" yaml:"-"`
}

// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VLDistributed App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +kubebuilder:object:root=true
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vldistributed,scope=Namespaced
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="current status of update rollout"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// VLDistributed is progressively rolling out updates to multiple zone components for VictoriaLogs.
type VLDistributed struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of VLDistributed
	// +required
	Spec VLDistributedSpec `json:"spec"`

	// status defines the observed state of VLDistributed
	// +optional
	Status VLDistributedStatus `json:"status,omitempty,omitzero"`
}

// Owns returns error if owned by other CR
func (cr *VLDistributed) Owns(r client.Object) error {
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

// VLAuthName returns the name of the VMAuth resource to be created/managed.
func (cr *VLDistributed) VLAuthName() string {
	switch {
	case len(cr.Spec.VMAuth.Name) > 0:
		return cr.Spec.VMAuth.Name
	default:
		return cr.Name
	}
}

// AsOwner returns owner references with current object as owner
func (cr *VLDistributed) AsOwner() metav1.OwnerReference {
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
// VLDistributedList contains a list of VLDistributed
type VLDistributedList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VLDistributed `json:"items"`
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VLDistributed) GetStatus() *VLDistributedStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VLDistributed) DefaultStatusFields(vs *VLDistributedStatus) {
}

// GetStatusMetadata returns metadata for object status
func (cr *VLDistributed) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// LastSpecUpdated compares spec with last applied spec stored, replaces old spec and returns true if it's updated
func (cr *VLDistributed) LastSpecUpdated() bool {
	updated := cr.Status.LastAppliedSpec == nil || !equality.Semantic.DeepEqual(&cr.Spec, cr.Status.LastAppliedSpec)
	cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
	return updated
}

// Paused checks if resource reconcile should be paused
func (cr *VLDistributed) Paused() bool {
	return cr.Spec.Paused
}

// SelectorLabels returns selector labels for distributed resources
func (cr *VLDistributed) SelectorLabels() map[string]string {
	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VLDistributed) UnmarshalJSON(src []byte) error {
	type pcr VLDistributed
	type shadow struct {
		*pcr
		Spec json.RawMessage `json:"spec"`
	}
	s := shadow{pcr: (*pcr)(cr)}
	if err := json.Unmarshal(src, &s); err != nil {
		return err
	}
	if len(s.Spec) > 0 {
		if err := vmv1beta1.UnmarshalSpecStrict(s.Spec, &cr.Spec); err != nil {
			cr.Status.ParsingSpecError = fmt.Sprintf("cannot parse VLDistributedSpec: %s, err: %s", string(s.Spec), err)
		}
	}
	return nil
}

// Validate validates the VLDistributed resource
//
//nolint:dupl
func (cr *VLDistributed) Validate() error {
	zones := sets.New[string]()
	clusters := sets.New[string]()
	singles := sets.New[string]()
	agents := sets.New[string]()
	spec := cr.Spec
	isVLSingle := cr.Spec.BackendType == VLDistributedBackendTypeVLSingle
	hasCommonVLInsert := cr.Spec.ZoneCommon.VLCluster.Spec.VLInsert != nil
	hasCommonVLSelect := cr.Spec.ZoneCommon.VLCluster.Spec.VLSelect != nil
	hasCommonVLSingle := cr.Spec.ZoneCommon.VLSingle != nil && (cr.Spec.ZoneCommon.VLSingle.Name != "" || cr.Spec.ZoneCommon.VLSingle.Spec != nil)
	hasCommonVLCluster := cr.Spec.ZoneCommon.VLCluster.Name != "" || !equality.Semantic.DeepEqual(cr.Spec.ZoneCommon.VLCluster.Spec, vmv1.VLClusterSpec{})
	if isVLSingle && hasCommonVLCluster {
		return fmt.Errorf("backendType=VLSingle is incompatible with zoneCommon.vlcluster configuration")
	}
	if !isVLSingle && hasCommonVLSingle {
		return fmt.Errorf("backendType=VLCluster is incompatible with zoneCommon.vlsingle configuration")
	}
	for i := range spec.Zones {
		zone := &spec.Zones[i]
		if len(zone.Name) == 0 {
			return fmt.Errorf("spec.zones[%d].name is required", i)
		}
		if zones.Has(zone.Name) {
			return fmt.Errorf("spec.zones[%d].name=%s is duplicated, zone names must be unique", i, zone.Name)
		}
		zones.Insert(zone.Name)
		if isVLSingle {
			if zone.VLCluster.Name != "" || !equality.Semantic.DeepEqual(zone.VLCluster.Spec, vmv1.VLClusterSpec{}) {
				return fmt.Errorf("spec.zones[%d]: backendType=VLSingle is incompatible with vlcluster configuration", i)
			}
			singleName := zone.VLSingleName(cr)
			if singles.Has(singleName) {
				return fmt.Errorf("spec.zones[%d].vlsingle.name=%s is already added in a different zone", i, singleName)
			}
			singles.Insert(singleName)
		} else {
			if zone.VLSingle != nil && (zone.VLSingle.Name != "" || zone.VLSingle.Spec != nil) {
				return fmt.Errorf("spec.zones[%d]: backendType=VLCluster is incompatible with vlsingle configuration", i)
			}
			clusterName := zone.VLClusterName(cr)
			if len(clusterName) > 0 {
				if clusters.Has(clusterName) {
					return fmt.Errorf("spec.zones[%d].vlcluster.name=%s is already added in a different zone", i, clusterName)
				}
				clusters.Insert(clusterName)
			}
			if zone.VLCluster.Spec.VLInsert == nil && !hasCommonVLInsert {
				return fmt.Errorf("either zoneCommon.vlcluster.spec.vlinsert or spec.zones[%d].vlcluster.spec.vlinsert is required", i)
			}
			if zone.VLCluster.Spec.VLSelect == nil && !hasCommonVLSelect {
				return fmt.Errorf("either zoneCommon.vlcluster.spec.vlselect or spec.zones[%d].vlcluster.spec.vlselect is required", i)
			}
		}
		agentName := zone.VLAgentName(cr)
		if len(agentName) > 0 {
			if agents.Has(agentName) {
				return fmt.Errorf("spec.zones[%d].vlagent.name=%s is already added in a different zone", i, agentName)
			}
			agents.Insert(agentName)
		}
	}
	return nil
}
