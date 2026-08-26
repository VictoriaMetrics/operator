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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VTAgentSpec defines the desired state of VTAgent
// +k8s:openapi-gen=true
type VTAgentSpec struct {

	// ComponentVersion defines default images tag for all components.
	// it can be overwritten with component specific image.tag value.
	// +optional
	ComponentVersion string `json:"componentVersion,omitempty"`
	// PodMetadata configures Labels and Annotations which are propagated to the vtagent pods.
	// +optional
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *vmv1beta1.ManagedObjectsMetadata `json:"managedMetadata,omitempty"`
	// LogLevel for VTAgent to be configured with.
	// INFO, WARN, ERROR, FATAL, PANIC
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VTAgent to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`

	// RemoteWrite list of VictoriaTraces endpoints to replicate ingested trace spans to.
	// The url must point to the `/insert/native` API of a VictoriaTraces instance, e.g.
	// http://vtsingle-example:10428/insert/native
	// See https://docs.victoriametrics.com/victoriatraces/vtagent/
	RemoteWrite []VTAgentRemoteWriteSpec `json:"remoteWrite"`
	// RemoteWriteSettings defines global settings for all remoteWrite urls.
	// +optional
	RemoteWriteSettings *VTAgentRemoteWriteSettings `json:"remoteWriteSettings,omitempty"`

	// Path to directory where temporary data for vtagent stored
	// Defaults to /vtagent-data
	// If defined, operator ignores spec.storage field and skips adding volume and volumeMount for tmp data
	// +optional
	TmpDataPath *string `json:"tmpDataPath,omitempty"`

	// GRPCSpec defines OTLP gRPC ingestion listener configuration
	// +optional
	GRPCSpec *OTLPGRPCSpec `json:"grpcSpec,omitempty"`

	// ServiceSpec that will be added to vtagent service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vtagent VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`

	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// NetworkPolicy defines network access rules for pods created by this CR.
	// +optional
	NetworkPolicy *vmv1beta1.EmbeddedNetworkPolicy `json:"networkPolicy,omitempty"`
	// Storage configures storage for StatefulSet
	// +optional
	Storage *vmv1beta1.StorageSpec `json:"storage,omitempty"`
	// RollingUpdateStrategy allows configuration for strategyType
	// set it to RollingUpdate for disabling operator statefulSet rollingUpdate
	// +optional
	RollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"rollingUpdateStrategy,omitempty"`
	// PersistentVolumeClaimRetentionPolicy allows configuration of PVC retention policy
	// +optional
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`

	// ClaimTemplates allows adding additional VolumeClaimTemplates for VTAgent
	ClaimTemplates []corev1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// Configures vertical pod autoscaling.
	// +optional
	VPA                        *vmv1beta1.EmbeddedVPA `json:"vpa,omitempty"`
	vmv1beta1.CommonAppsParams `json:",inline,omitempty"`
}

// Validate performs syntax validation
func (cr *VTAgent) Validate() error {
	if vmv1beta1.MustSkipCRValidation(cr) {
		return nil
	}
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.Name == cr.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", cr.PrefixedName())
	}
	if len(cr.Spec.RemoteWrite) == 0 {
		return fmt.Errorf("spec.remoteWrite cannot be empty array, provide at least one remoteWrite")
	}
	for idx, rw := range cr.Spec.RemoteWrite {
		if rw.URL == "" {
			return fmt.Errorf("remoteWrite.url cannot be empty at idx: %d", idx)
		}
		if err := rw.OAuth2.Validate(); err != nil {
			return fmt.Errorf("remoteWrite.oauth2 has incorrect syntax at idx: %d: %w", idx, err)
		}
		if err := rw.TLSConfig.Validate(); err != nil {
			return fmt.Errorf("remoteWrite.tlsConfig has incorrect syntax at idx: %d: %w", idx, err)
		}
	}
	if cr.Spec.VPA != nil {
		if err := cr.Spec.VPA.Validate(); err != nil {
			return err
		}
	}
	specPort := cr.Spec.Port
	if specPort == "" {
		specPort = "10429"
	}
	if err := cr.Spec.GRPCSpec.Validate(specPort); err != nil {
		return err
	}
	if err := cr.Spec.Validate(); err != nil {
		return err
	}
	return nil
}

// UseProxyProtocol implements build.probeCRD interface
func (cr *VTAgent) UseProxyProtocol() bool {
	return vmv1beta1.UseProxyProtocol(cr.Spec.ExtraArgs)
}

// VTAgentRemoteWriteSettings - defines global settings for all remoteWrite urls.
type VTAgentRemoteWriteSettings struct {
	// The maximum size of unpacked request to send to remote storage
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	MaxBlockSize *vmv1beta1.BytesString `json:"maxBlockSize,omitempty"`

	// The maximum file-based buffer size in bytes at -remoteWrite.tmpDataPath
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	MaxDiskUsagePerURL *vmv1beta1.BytesString `json:"maxDiskUsagePerURL,omitempty"`
	// The number of concurrent queues
	// +optional
	Queues *int32 `json:"queues,omitempty"`
	// Whether to show -remoteWrite.url in the exported metrics. It is hidden by default, since it can contain sensitive auth info
	// +optional
	ShowURL *bool `json:"showURL,omitempty"`
	// Path to directory where temporary data for remote write component is stored.
	// Defaults to /vtagent-data/vtagent-remotewrite-data
	// +optional
	TmpDataPath *string `json:"tmpDataPath,omitempty"`
	// Interval for flushing the data to remote storage. (default 1s)
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	FlushInterval *string `json:"flushInterval,omitempty"`
}

// VTAgentRemoteWriteSpec defines the remote storage configuration for VTAgent
// +k8s:openapi-gen=true
type VTAgentRemoteWriteSpec struct {
	// URL of the VictoriaTraces `/insert/native` endpoint to send trace spans to.
	URL string `json:"url"`
	// Format defines the wire format used to send data to the given url.
	// +optional
	// +kubebuilder:validation:Enum=native;jsonline
	Format *string `json:"format,omitempty"`
	// BasicAuth allow an endpoint to authenticate over basic authentication.
	// +optional
	BasicAuth *vmv1beta1.BasicAuth `json:"basicAuth,omitempty"`
	// Optional bearer auth token to use for -remoteWrite.url
	// +optional
	BearerTokenSecret *corev1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
	// Optional bearer auth token to use for -remoteWrite.url
	// +optional
	BearerTokenPath string `json:"bearerTokenPath,omitempty"`
	// OAuth2 defines auth configuration
	// +optional
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`
	// TLSConfig describes tls configuration for remote write target
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
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

// VTAgentStatus defines the observed state of VTAgent
// +k8s:openapi-gen=true
type VTAgentStatus struct {
	// Selector string form of label value set for autoscaling
	Selector string `json:"selector,omitempty"`
	// ReplicaCount Total number of pods targeted by this VTAgent
	Replicas                 int32 `json:"replicas,omitempty"`
	vmv1beta1.StatusMetadata `json:",inline"`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	LastAppliedSpec *VTAgentSpec `json:"lastAppliedSpec,omitempty"`
	// ParsingSpecError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingSpecError string `json:"-" yaml:"-"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VTAgent) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +genclient

// VTAgent - is a lightweight agent which replicates ingested OTLP trace spans to one or more VictoriaTraces instances.
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VTAgent App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="StatefulSet,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vtagents,scope=Namespaced
// +kubebuilder:printcolumn:name="Replica Count",type="integer",JSONPath=".status.replicas",description="current number of replicas"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of update rollout"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VTAgent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VTAgentSpec   `json:"spec,omitempty"`
	Status VTAgentStatus `json:"status,omitempty"`
}

// VTAgentList contains a list of VTAgent
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VTAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VTAgent `json:"items"`
}

// AsOwner returns owner references with current object as owner
func (cr *VTAgent) AsOwner() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         cr.APIVersion,
		Kind:               cr.Kind,
		Name:               cr.Name,
		UID:                cr.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
}

// PodAnnotations returns pod metadata annotations
func (cr *VTAgent) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VTAgent) GetStatus() *VTAgentStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VTAgent) DefaultStatusFields(vs *VTAgentStatus) {
	replicaCount := int32(0)
	if cr.Spec.ReplicaCount != nil {
		replicaCount = *cr.Spec.ReplicaCount
	}
	vs.Replicas = replicaCount
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VTAgent) UnmarshalJSON(src []byte) error {
	type pcr VTAgent
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
			cr.Status.ParsingSpecError = fmt.Sprintf("cannot parse VTAgentSpec: %s, err: %s", string(s.Spec), err)
		}
	}
	return nil
}

// FinalAnnotations implements build.builderOpts interface
func (cr *VTAgent) FinalAnnotations() map[string]string {
	var v map[string]string
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Annotations, v)
	}
	return v
}

// SelectorLabels returns selector labels for querying any vtagent related resources
func (cr *VTAgent) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vtagent",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// PodLabels returns labels for pod metadata
func (cr *VTAgent) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}

	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

// FinalLabels returns global labels for all vtagent related resources
func (cr *VTAgent) FinalLabels() map[string]string {
	v := cr.SelectorLabels()
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Labels, v)
	}
	return v
}

// PrefixedName returns name of resource with fixed prefix
func (cr *VTAgent) PrefixedName() string {
	return fmt.Sprintf("vtagent-%s", cr.Name)
}

// HealthPath returns path for health requests
func (cr *VTAgent) HealthPath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

// GetMetricsPath returns prefixed path for metric requests
func (cr *VTAgent) GetMetricsPath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricsPath)
}

// UseTLS returns true if TLS is enabled
func (cr *VTAgent) UseTLS() bool {
	return vmv1beta1.UseTLS(cr.Spec.ExtraArgs)
}

// GetExtraArgs returns additionally configured command-line arguments
func (cr *VTAgent) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// GetServiceScrape returns overrides for serviceScrape builder
func (cr *VTAgent) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

// GetServiceAccountName returns ServiceAccount for resource
func (cr *VTAgent) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

// IsOwnsServiceAccount checks if serviceAccount belongs to the CR
func (cr *VTAgent) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

// AsURL - returns url for http access
func (cr *VTAgent) AsURL(isExtra bool) string {
	specPort := cr.Spec.Port
	if specPort == "" {
		specPort = "10429"
	}
	svcName, port := vmv1beta1.ResolveServiceURL(cr.PrefixedName(), specPort, "http", cr.Spec.ServiceSpec, isExtra)
	return fmt.Sprintf("%s://%s.%s.svc:%s", vmv1beta1.HTTPProtoFromFlags(cr.Spec.ExtraArgs), svcName, cr.Namespace, port)
}

// ProbePath implements build.probeCRD interface
func (cr *VTAgent) ProbePath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

// ProbeScheme implements build.probeCRD interface
func (cr *VTAgent) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.Spec.ExtraArgs))
}

// ProbePort implements build.probeCRD interface
func (cr *VTAgent) ProbePort() string {
	return cr.Spec.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VTAgent) ProbeNeedLiveness() bool {
	return true
}

// LastSpecUpdated compares spec with last applied spec stored, replaces old spec and returns true if it's updated
func (cr *VTAgent) LastSpecUpdated() bool {
	updated := cr.Status.LastAppliedSpec == nil || !equality.Semantic.DeepEqual(&cr.Spec, cr.Status.LastAppliedSpec)
	cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
	return updated
}

// Paused checks if resource reconcile should be paused
func (cr *VTAgent) Paused() bool {
	return cr.Spec.Paused
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VTAgent) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}
