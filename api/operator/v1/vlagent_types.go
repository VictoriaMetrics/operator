package v1

import (
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VLAgentSpec defines the desired state of VLAgent
// +k8s:openapi-gen=true
type VLAgentSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
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

	// RemoteWrite list of victoria logs endpoints
	// for victorialogs it must looks like: http://victoria-logs-single:9428/
	// or for cluster different url
	// https://docs.victoriametrics.com/victorialogs/vlagent/#replication-and-high-availability
	RemoteWrite []VLAgentRemoteWriteSpec `json:"remoteWrite"`
	// RemoteWriteSettings defines global settings for all remoteWrite urls.
	// +optional
	RemoteWriteSettings *VLAgentRemoteWriteSettings `json:"remoteWriteSettings,omitempty"`

	// Path to directory where temporary data for vlagent stored
	// Defaults to /vlagent-data or /var/lib/vlagent-data in collector mode
	// If defined, operator ignores spec.storage field and skips adding volume and volumeMount for tmp data
	// +optional
	TmpDataPath *string `json:"tmpDataPath,omitempty"`

	// ServiceSpec that will be added to vlagent service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vlagent VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`

	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
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

	// ClaimTemplates allows adding additional VolumeClaimTemplates for VLAgent in Mode: StatefulSet
	ClaimTemplates []corev1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`

	// SyslogSpec defines syslog listener configuration
	// +optional
	SyslogSpec *SyslogServerSpec `json:"syslogSpec,omitempty"`

	// K8sCollector configures VLAgent logs collection from K8s pods
	K8sCollector VLAgentK8sCollector `json:"k8sCollector,omitempty"`

	// License allows to configure license key to be used for enterprise features.
	// See [here](https://docs.victoriametrics.com/victoriametrics/enterprise/#victorialogs-enterprise-features)
	// +optional
	License *vmv1beta1.License `json:"license,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	*vmv1beta1.EmbeddedProbes                   `json:",inline"`
	vmv1beta1.CommonDefaultableParams           `json:",inline,omitempty"`
	vmv1beta1.CommonApplicationDeploymentParams `json:",inline,omitempty"`
}

type VLAgentK8sCollector struct {
	// Enabled switches VLAgent to log collection mode.
	// Note, for this purpose operator uses DaemonSet, while by default VLAgent uses StatefulSet.
	// Switching this option will drop all persisted data.
	Enabled bool `json:"enabled,omitempty"`

	// LogsPath configures root for logs path
	// By default VLAgent collects logs from /var/log/containers
	LogsPath string `json:"logsPath,omitempty"`

	// CheckpointsPath configures path to file where logs checkpoints are stored.
	// By default it's stored at host's /var/lib/vlagent-data/checkpoints.json.
	CheckpointsPath *string `json:"checkpointsPath,omitempty"`

	// TenantID defines default tenant ID to use for logs collected from pods in format: <accountID>:<projectID>
	TenantID string `json:"tenantID,omitempty"`

	// IgnoreFields defines fields to ignore across logs ingested from Kubernetes
	IgnoreFields []string `json:"ignoreFields,omitempty"`

	// ExcludeFilter defines logsql expression that allows to filter out containers to collect logs from
	ExcludeFilter string `json:"excludeFilter,omitempty"`

	// IncludePodLabels enables attachment of pod labels to log entries
	IncludePodLabels *bool `json:"includePodLabels,omitempty"`

	// IncludePodAnnotations enables attachment of pod annotations to log entries
	IncludePodAnnotations *bool `json:"includePodAnnotations,omitempty"`

	// IncludeNodeLabels enables attachment of node labels to log entries
	IncludeNodeLabels *bool `json:"includeNodeLabels,omitempty"`

	// IncludeNodeAnnotations enables attachment of node annotations to log entries
	IncludeNodeAnnotations *bool `json:"includeNodeAnnotations,omitempty"`

	// DecolorizeFields defines fields to remove ANSI color codes across logs ingested from Kubernetes
	DecolorizeFields []string `json:"decolorizeFields,omitempty"`

	// StreamFields defines list of fields to use as log stream fields for logs ingested from Kubernetes Pods
	StreamFields []string `json:"streamFields,omitempty"`

	// MsgField defines fields that may contain the _msg field
	MsgFields []string `json:"msgFields,omitempty"`

	// TimeFields defines fields that may contain the _time field
	TimeFields []string `json:"timeFields,omitempty"`

	// ExtraFields defines extra fields as JSON string which should be added to each collected log line
	// Example: '{"env":"dev","cluster":"staging"}'
	ExtraFields string `json:"extraFields,omitempty"`
}

// SetLastSpec implements objectWithLastAppliedState interface
func (cr *VLAgent) SetLastSpec(prevSpec VLAgentSpec) {
	cr.ParsedLastAppliedSpec = &prevSpec
}

// Validate performs syntax validation
func (cr *VLAgent) Validate() error {
	if vmv1beta1.MustSkipCRValidation(cr) {
		return nil
	}
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.Name == cr.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", cr.PrefixedName())
	}
	if len(cr.Spec.RemoteWrite) == 0 {
		return fmt.Errorf("spec.remoteWrite cannot be empty array, provide at least one remoteWrite")
	}
	if cr.Spec.K8sCollector.Enabled {
		if len(cr.Spec.K8sCollector.ExtraFields) > 0 {
			var raw map[string]any
			if err := json.Unmarshal([]byte(cr.Spec.K8sCollector.ExtraFields), &raw); err != nil {
				return fmt.Errorf("spec.k8sCollector.extraFields is not a valid JSON: %w", err)
			}
		}
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
	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VLAgent) UnmarshalJSON(src []byte) error {
	type pcr VLAgent
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	if err := vmv1beta1.ParseLastAppliedStateTo(cr); err != nil {
		return err
	}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VLAgentSpec) UnmarshalJSON(src []byte) error {
	type pcr VLAgentSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vlagent spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// VLAgentRemoteWriteSettings - defines global settings for all remoteWrite urls.
type VLAgentRemoteWriteSettings struct {
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
	// Defaults to /vlagent-data/vlagent-remotewrite-data or /var/lib/vlagent-data/vlagent-remotewrite-data in collector mode
	// +optional
	TmpDataPath *string `json:"tmpDataPath,omitempty"`
	// Interval for flushing the data to remote storage. (default 1s)
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	FlushInterval *string `json:"flushInterval,omitempty"`
}

// VLAgentRemoteWriteSpec defines the remote storage configuration for VmAgent
// +k8s:openapi-gen=true
type VLAgentRemoteWriteSpec struct {
	// URL of the endpoint to send samples to.
	URL string `json:"url"`
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

// VLAgentStatus defines the observed state of VLAgent
// +k8s:openapi-gen=true
type VLAgentStatus struct {
	// Selector string form of label value set for autoscaling
	Selector string `json:"selector,omitempty"`
	// ReplicaCount Total number of pods targeted by this VLAgent
	Replicas                 int32 `json:"replicas,omitempty"`
	vmv1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VLAgentStatus) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.StatusMetadata
}

// +genclient

// VLAgent - is a tiny but brave agent, which helps you collect logs from various sources and stores them in VictoriaLogs.
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VLAgent App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vlagents,scope=Namespaced
// +kubebuilder:subresource:scale:specpath=.spec.shardCount,statuspath=.status.shards,selectorpath=.status.selector
// +kubebuilder:printcolumn:name="Replica Count",type="integer",JSONPath=".status.replicas",description="current number of replicas"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of update rollout"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VLAgent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VLAgentSpec `json:"spec,omitempty"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VLAgentSpec `json:"-" yaml:"-"`

	Status VLAgentStatus `json:"status,omitempty"`
}

// VLAgentList contains a list of VLAgent
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VLAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VLAgent `json:"items"`
}

// AsOwner returns owner references with current object as owner
func (cr *VLAgent) AsOwner() metav1.OwnerReference {
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
func (cr *VLAgent) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VLAgent) GetStatus() *VLAgentStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VLAgent) DefaultStatusFields(vs *VLAgentStatus) {
	replicaCount := int32(0)
	if cr.Spec.ReplicaCount != nil {
		replicaCount = *cr.Spec.ReplicaCount
	}
	vs.Replicas = replicaCount
}

// FinalAnnotations implements build.builderOpts interface
func (cr *VLAgent) FinalAnnotations() map[string]string {
	var v map[string]string
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Annotations, v)
	}
	return v
}

// AsCRDOwner implements interface
func (*VLAgent) AsCRDOwner() *metav1.OwnerReference {
	return vmv1beta1.GetCRDAsOwner(vmv1beta1.VLAgentCRD)
}

// SelectorLabels returns selector labels for querying any vlagent related resources
func (cr *VLAgent) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vlagent",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// PodLabels returns labels for pod metadata
func (cr *VLAgent) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}

	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

// FinalLabels returns global labels for all vlagent related resources
func (cr *VLAgent) FinalLabels() map[string]string {
	v := cr.SelectorLabels()
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Labels, v)
	}
	return v
}

// PrefixedName returns name of resource with fixed prefix
func (cr *VLAgent) PrefixedName() string {
	return fmt.Sprintf("vlagent-%s", cr.Name)
}

// HealthPath returns path for health requests
func (cr *VLAgent) HealthPath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

// GetMetricsPath returns prefixed path for metric requests
func (cr *VLAgent) GetMetricsPath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricsPath)
}

// UseTLS returns true if TLS is enabled
func (cr *VLAgent) UseTLS() bool {
	return vmv1beta1.UseTLS(cr.Spec.ExtraArgs)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VLAgent) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VLAgent) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

// GetServiceAccountName returns ServiceAccount for resource
func (cr *VLAgent) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

// IsOwnsServiceAccount checks if serviceAccount belongs to the CR
func (cr *VLAgent) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

// AsURL - returns url for http access
func (cr *VLAgent) AsURL() string {
	port := cr.Spec.Port
	if port == "" {
		port = "9429"
	}
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
				break
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", vmv1beta1.HTTPProtoFromFlags(cr.Spec.ExtraArgs), cr.PrefixedName(), cr.Namespace, port)
}

// Probe implements build.probeCRD interface
func (cr *VLAgent) Probe() *vmv1beta1.EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

// ProbePath implements build.probeCRD interface
func (cr *VLAgent) ProbePath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

// ProbeScheme implements build.probeCRD interface
func (cr *VLAgent) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.Spec.ExtraArgs))
}

// ProbePort implements build.probeCRD interface
func (cr *VLAgent) ProbePort() string {
	return cr.Spec.Port
}

func (cr *VLAgent) GetClusterRoleName() string {
	return fmt.Sprintf("monitoring:%s:vlagent-%s", cr.Namespace, cr.Name)
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VLAgent) ProbeNeedLiveness() bool {
	return true
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VLAgent) LastAppliedSpecAsPatch() (client.Patch, error) {
	return vmv1beta1.LastAppliedChangesAsPatch(cr.Spec)
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (cr *VLAgent) HasSpecChanges() (bool, error) {
	return vmv1beta1.HasStateChanges(cr.ObjectMeta, cr.Spec)
}

// Paused checks if resource reconcile should be paused
func (cr *VLAgent) Paused() bool {
	return cr.Spec.Paused
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VLAgent) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VLAgent{}, &VLAgentList{})
}
