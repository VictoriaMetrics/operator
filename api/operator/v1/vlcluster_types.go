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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VLClusterSpec defines the desired state of VLCluster
type VLClusterSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the
	// VLSelect, VLInsert and VLStorage Pods.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ClusterVersion defines default images tag for all components.
	// it can be overwritten with component specific image.tag value.
	// +optional
	ClusterVersion string `json:"clusterVersion,omitempty"`
	// ClusterDomainName defines domain name suffix for in-cluster dns addresses
	// aka .cluster.local
	// used by vlinsert and vlselect to build vlstorage address
	// +optional
	ClusterDomainName string `json:"clusterDomainName,omitempty"`

	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see https://kubernetes.io/docs/concepts/containers/images/#referring-to-an-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	VLInsert  *VLInsert  `json:"vlinsert,omitempty"`
	VLSelect  *VLSelect  `json:"vlselect,omitempty"`
	VLStorage *VLStorage `json:"vlstorage,omitempty"`

	// Paused If set to true all actions on the underlying managed objects are not
	// going to be performed, except for delete actions.
	// +optional
	Paused bool `json:"paused,omitempty"`
	// UseStrictSecurity enables strict security mode for component
	// it restricts disk writes access
	// uses non-root user out of the box
	// drops not needed security permissions
	// +optional
	UseStrictSecurity *bool `json:"useStrictSecurity,omitempty"`

	// RequestsLoadBalancer configures load-balancing for vlinsert and vlselect requests.
	// It helps to evenly spread load across pods.
	// Usually it's not possible with Kubernetes TCP-based services.
	RequestsLoadBalancer vmv1beta1.VMAuthLoadBalancer `json:"requestsLoadBalancer,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *vmv1beta1.ManagedObjectsMetadata `json:"managedMetadata,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VLClusterSpec) UnmarshalJSON(src []byte) error {
	type pcr VLClusterSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vlcluster spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// VLClusterStatus defines the observed state of VLCluster
type VLClusterStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VLClusterStatus) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.StatusMetadata
}

// VLInsert defines vlinsert component configuration at victoria-logs cluster
type VLInsert struct {
	// PodMetadata configures Labels and Annotations which are propagated to the VLSelect pods.
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for VLSelect to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VLSelect to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`

	// ServiceSpec that will be added to vlselect service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vlselect VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget       *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*vmv1beta1.EmbeddedProbes `json:",inline"`
	// Configures horizontal pod autoscaling.
	// +optional
	HPA *vmv1beta1.EmbeddedHPA `json:"hpa,omitempty"`
	// SyslogSpec defines syslog listener configuration
	// +optional
	SyslogSpec *SyslogServerSpec `json:"syslogSpec,omitempty"`

	// UpdateStrategy - overrides default update strategy.
	// +kubebuilder:validation:Enum=Recreate;RollingUpdate
	// +optional
	UpdateStrategy *appsv1.DeploymentStrategyType `json:"updateStrategy,omitempty"`
	// RollingUpdate - overrides deployment update params.
	// +optional
	RollingUpdate *appsv1.RollingUpdateDeployment `json:"rollingUpdate,omitempty"`

	vmv1beta1.CommonDefaultableParams           `json:",inline"`
	vmv1beta1.CommonApplicationDeploymentParams `json:",inline"`
}

// Probe implements build.probeCRD interface
func (cr *VLInsert) Probe() *vmv1beta1.EmbeddedProbes {
	return cr.EmbeddedProbes
}

// ProbePath implements build.probeCRD interface
func (cr *VLInsert) ProbePath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

// ProbeScheme implements build.probeCRD interface
func (cr *VLInsert) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.ExtraArgs))
}

// ProbePort implements build.probeCRD interface
func (cr *VLInsert) ProbePort() string {
	return cr.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VLInsert) ProbeNeedLiveness() bool {
	return true
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VLInsert) GetMetricPath() string {
	if cr == nil {
		return healthPath
	}
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VLInsert) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VLInsert) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VLInsert) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return cr.ServiceSpec
}

// SyslogServerSpec defines syslog servers configuration
type SyslogServerSpec struct {
	// TCPListeners defines syslog server TCP listener configuration
	TCPListeners []*SyslogTCPListener `json:"tcpListeners,omitempty"`
	// UDPListeners defines syslog server UDP listener configuration
	UDPListeners []*SyslogUDPListener `json:"udpListeners,omitempty"`
}

// SyslogTCPListener defines configuration for TCP syslog server listen
type SyslogTCPListener struct {
	// ListenPort defines listen port
	ListenPort int32 `json:"listenPort"`
	// StreamFields to use as log stream labels
	// see https://docs.victoriametrics.com/victorialogs/data-ingestion/syslog/#stream-fields
	// +optional
	StreamFields FieldsListString `json:"streamFields,omitempty"`
	// IgnoreFields to ignore at logs
	// see https://docs.victoriametrics.com/victorialogs/data-ingestion/syslog/#dropping-fields
	// +optional
	IgnoreFields FieldsListString `json:"ignoreFields,omitempty"`
	// DecolorizeFields to remove ANSI color codes across logs
	// see https://docs.victoriametrics.com/victorialogs/data-ingestion/syslog/#decolorizing-fields
	// +optional
	DecolorizeFields FieldsListString `json:"decolorizeFields,omitempty"`
	// TenantID for logs ingested in form of accountID:projectID
	// see https://docs.victoriametrics.com/victorialogs/data-ingestion/syslog/#multiple-configs
	// +optional
	TenantID string `json:"tenantID,omitempty"`
	// CompressMethod for syslog messages
	// see https://docs.victoriametrics.com/victorialogs/data-ingestion/syslog/#compression
	// +kubebuilder:validation:Pattern:="^(none|zstd|gzip|deflate)$"
	// +optional
	CompressMethod string `json:"compressMethod,omitempty"`
	// +optional
	TLSConfig *TLSServerConfig `json:"tlsConfig,omitempty"`
}

// SyslogUDPListener defines configuration for UDP syslog server listen
type SyslogUDPListener struct {
	// ListenPort defines listen port
	ListenPort int32 `json:"listenPort"`
	// StreamFields to use as log stream labels
	// see https://docs.victoriametrics.com/victorialogs/data-ingestion/syslog/#stream-fields
	// +optional
	StreamFields FieldsListString `json:"streamFields,omitempty"`
	// IgnoreFields to ignore at logs
	// see https://docs.victoriametrics.com/victorialogs/data-ingestion/syslog/#dropping-fields
	// +optional
	IgnoreFields FieldsListString `json:"ignoreFields,omitempty"`
	// DecolorizeFields to remove ANSI color codes across logs
	// see https://docs.victoriametrics.com/victorialogs/data-ingestion/syslog/#decolorizing-fields
	// +optional
	DecolorizeFields FieldsListString `json:"decolorizeFields,omitempty"`
	// TenantID for logs ingested in form of accountID:projectID
	// see https://docs.victoriametrics.com/victorialogs/data-ingestion/syslog/#multiple-configs
	// +optional
	TenantID string `json:"tenantID,omitempty"`
	// CompressMethod for syslog messages
	// see https://docs.victoriametrics.com/victorialogs/data-ingestion/syslog/#compression
	// +kubebuilder:validation:Pattern:="^(none|zstd|gzip|deflate)$"
	// +optional
	CompressMethod string `json:"compressMethod,omitempty"`
}

// FieldsListString represents list of json encoded strings
// ["field"] or ["field1","field2"]
type FieldsListString string

var _ json.Unmarshaler = (*FieldsListString)(nil)

// UnmarshalJSON implements json.Unmarshaller interface
func (sf *FieldsListString) UnmarshalJSON(src []byte) error {
	var str string
	if err := json.Unmarshal(src, &str); err != nil {
		return fmt.Errorf("cannot parse FieldsListString value as string: %w", err)
	}
	*sf = FieldsListString(str)

	var a []string
	if err := json.Unmarshal([]byte(str), &a); err != nil {
		return fmt.Errorf("cannot unpack FieldsListString as array of strings: %w", err)
	}
	return nil
}

// VLSelect defines vlselect component configuration at victoria-logs cluster
type VLSelect struct {
	// PodMetadata configures Labels and Annotations which are propagated to the VLSelect pods.
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for VLSelect to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VLSelect to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`

	// ServiceSpec that will be added to vlselect service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vlselect VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget       *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*vmv1beta1.EmbeddedProbes `json:",inline"`
	// Configures horizontal pod autoscaling.
	// +optional
	HPA *vmv1beta1.EmbeddedHPA `json:"hpa,omitempty"`

	// UpdateStrategy - overrides default update strategy.
	// +kubebuilder:validation:Enum=Recreate;RollingUpdate
	// +optional
	UpdateStrategy *appsv1.DeploymentStrategyType `json:"updateStrategy,omitempty"`
	// RollingUpdate - overrides deployment update params.
	// +optional
	RollingUpdate *appsv1.RollingUpdateDeployment `json:"rollingUpdate,omitempty"`

	vmv1beta1.CommonDefaultableParams           `json:",inline"`
	vmv1beta1.CommonApplicationDeploymentParams `json:",inline"`
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VLSelect) GetMetricPath() string {
	if cr == nil {
		return healthPath
	}
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VLSelect) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VLSelect) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// Probe implements build.probeCRD interface
func (cr *VLSelect) Probe() *vmv1beta1.EmbeddedProbes {
	return cr.EmbeddedProbes
}

// ProbePath implements build.probeCRD interface
func (cr *VLSelect) ProbePath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

// ProbeScheme implements build.probeCRD interface
func (cr *VLSelect) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.ExtraArgs))
}

// ProbePort implements build.probeCRD interface
func (cr *VLSelect) ProbePort() string {
	return cr.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VLSelect) ProbeNeedLiveness() bool {
	return true
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VLSelect) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return cr.ServiceSpec
}

// VLStorage defines vlstorage component configuration at victoria-logs cluster
type VLStorage struct {
	// RetentionPeriod for the stored logs
	// https://docs.victoriametrics.com/victorialogs/#retention
	// +optional
	// +kubebuilder:validation:Pattern:="^[0-9]+(h|d|w|y)?$"
	RetentionPeriod string `json:"retentionPeriod,omitempty"`
	// RetentionMaxDiskSpaceUsageBytes for the stored logs
	// VictoriaLogs keeps at least two last days of data in order to guarantee that the logs for the last day can be returned in queries.
	// This means that the total disk space usage may exceed the -retention.maxDiskSpaceUsageBytes,
	// if the size of the last two days of data exceeds the -retention.maxDiskSpaceUsageBytes.
	// https://docs.victoriametrics.com/victorialogs/#retention-by-disk-space-usage
	// +optional
	RetentionMaxDiskSpaceUsageBytes vmv1beta1.BytesString `json:"retentionMaxDiskSpaceUsageBytes,omitempty"`
	// FutureRetention for the stored logs
	// Log entries with timestamps bigger than now+futureRetention are rejected during data ingestion; see https://docs.victoriametrics.com/victorialogs/#retention
	// +optional
	// +kubebuilder:validation:Pattern:="^[0-9]+(h|d|w|y)?$"
	FutureRetention string `json:"futureRetention,omitempty"`
	// LogNewStreams Whether to log creation of new streams; this can be useful for debugging of high cardinality issues with log streams; see https://docs.victoriametrics.com/victorialogs/keyconcepts/#stream-fields
	LogNewStreams bool `json:"logNewStreams,omitempty"`
	// Whether to log all the ingested log entries; this can be useful for debugging of data ingestion; see https://docs.victoriametrics.com/victorialogs/data-ingestion/
	LogIngestedRows bool `json:"logIngestedRows,omitempty"`

	// PodMetadata configures Labels and Annotations which are propagated to the VLStorage pods.
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for VLStorage to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VLStorage to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`

	// ServiceSpec that will be added to vlselect service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vlselect VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget       *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*vmv1beta1.EmbeddedProbes `json:",inline"`
	// RollingUpdateStrategy defines strategy for application updates
	// Default is OnDelete, in this case operator handles update process
	// Can be changed for RollingUpdate
	// +optional
	RollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"rollingUpdateStrategy,omitempty"`
	// ClaimTemplates allows adding additional VolumeClaimTemplates for StatefulSet
	ClaimTemplates []corev1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`

	// StorageDataPath - path to storage data
	// +optional
	StorageDataPath string `json:"storageDataPath,omitempty"`
	// Storage configures persistent volume for VLStorage
	// +optional
	Storage *vmv1beta1.StorageSpec `json:"storage,omitempty"`
	// PersistentVolumeClaimRetentionPolicy allows configuration of PVC rentention policy
	// +optional
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`

	// MaintenanceInsertNodeIDs - excludes given node ids from insert requests routing, must contain pod suffixes - for pod-0, id will be 0 and etc.
	// lets say, you have pod-0, pod-1, pod-2, pod-3. to exclude pod-0 and pod-3 from insert routing, define nodeIDs: [0,3].
	// Useful at storage expanding, when you want to rebalance some data at cluster.
	// +optional
	MaintenanceInsertNodeIDs []int32 `json:"maintenanceInsertNodeIDs,omitempty"`
	// MaintenanceInsertNodeIDs - excludes given node ids from select requests routing, must contain pod suffixes - for pod-0, id will be 0 and etc.
	MaintenanceSelectNodeIDs []int32 `json:"maintenanceSelectNodeIDs,omitempty"`

	vmv1beta1.CommonDefaultableParams           `json:",inline"`
	vmv1beta1.CommonApplicationDeploymentParams `json:",inline"`
}

// GetStorageVolumeName returns formatted name for vlstorage volume
func (cr *VLStorage) GetStorageVolumeName() string {
	if cr.Storage != nil && cr.Storage.VolumeClaimTemplate.Name != "" {
		return cr.Storage.VolumeClaimTemplate.Name
	}
	return "vlstorage-db"
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VLStorage) GetMetricPath() string {
	if cr == nil {
		return healthPath
	}
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VLStorage) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VLStorage) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// Probe implements build.probeCRD interface
func (cr *VLStorage) Probe() *vmv1beta1.EmbeddedProbes {
	return cr.EmbeddedProbes
}

// ProbePath implements build.probeCRD interface
func (cr *VLStorage) ProbePath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

// ProbeScheme implements build.probeCRD interface
func (cr *VLStorage) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.ExtraArgs))
}

// ProbePort implements build.probeCRD interface
func (cr *VLStorage) ProbePort() string {
	return cr.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VLStorage) ProbeNeedLiveness() bool {
	return false
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VLStorage) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return cr.ServiceSpec
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VLCluster is fast, cost-effective and scalable logs database.
// +kubebuilder:printcolumn:name="Insert Count",type="string",JSONPath=".spec.vlinsert.replicaCount",description="replicas of VLInsert"
// +kubebuilder:printcolumn:name="Storage Count",type="string",JSONPath=".spec.vlstorage.replicaCount",description="replicas of VLStorage"
// +kubebuilder:printcolumn:name="Select Count",type="string",JSONPath=".spec.vlselect.replicaCount",description="replicas of VLSelect"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of cluster"
// +genclient
type VLCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VLClusterSpec   `json:"spec,omitempty"`
	Status VLClusterStatus `json:"status,omitempty"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VLClusterSpec `json:"-" yaml:"-"`
}

// SetLastSpec implements objectWithLastAppliedState interface
func (cr *VLCluster) SetLastSpec(prevSpec VLClusterSpec) {
	cr.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VLCluster) UnmarshalJSON(src []byte) error {
	type pcr VLCluster
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	if err := vmv1beta1.ParseLastAppliedStateTo(cr); err != nil {
		return err
	}
	return nil
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VLCluster) GetStatus() *VLClusterStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VLCluster) DefaultStatusFields(vs *VLClusterStatus) {
}

// AsOwner returns owner references with current object as owner
func (cr *VLCluster) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         cr.APIVersion,
			Kind:               cr.Kind,
			Name:               cr.Name,
			UID:                cr.UID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	}
}

// VMAuthLBSelectorLabels defines selector labels for vmauth balancer
func (cr *VLCluster) VMAuthLBSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vlclusterlb-vmauth-balancer",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// VMAuthLBPodLabels returns pod labels for vlclusterlb-vmauth-balancer cluster component
func (cr *VLCluster) VMAuthLBPodLabels() map[string]string {
	selectorLabels := cr.VMAuthLBSelectorLabels()
	if cr.Spec.RequestsLoadBalancer.Spec.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(cr.Spec.RequestsLoadBalancer.Spec.PodMetadata.Labels, selectorLabels)
}

// VMAuthLBPodAnnotations returns pod annotations for vmstorage cluster component
func (cr *VLCluster) VMAuthLBPodAnnotations() map[string]string {
	if cr.Spec.RequestsLoadBalancer.Spec.PodMetadata == nil {
		return make(map[string]string)
	}
	return cr.Spec.RequestsLoadBalancer.Spec.PodMetadata.Annotations
}

// GetVMAuthLBName returns prefixed name for the loadbalanacer components
func (cr *VLCluster) GetVMAuthLBName() string {
	return fmt.Sprintf("vlclusterlb-%s", cr.Name)
}

// GetVLSelectLBName returns headless proxy service name for select component
func (cr *VLCluster) GetVLSelectLBName() string {
	return prefixedName(cr.Name, "vlselectint")
}

// GetVLInsertLBName returns headless proxy service name for insert component
func (cr *VLCluster) GetVLInsertLBName() string {
	return prefixedName(cr.Name, "vlinsertint")
}

// GetVLInsertName returns insert component name
func (cr *VLCluster) GetVLInsertName() string {
	return prefixedName(cr.Name, "vlinsert")
}

// GetLVSelectName returns select component name
func (cr *VLCluster) GetVLSelectName() string {
	return prefixedName(cr.Name, "vlselect")
}

// GetVLStorageName returns select component name
func (cr *VLCluster) GetVLStorageName() string {
	return prefixedName(cr.Name, "vlstorage")
}

func (cr *VLCluster) Validate() error {
	if vmv1beta1.MustSkipCRValidation(cr) {
		return nil
	}
	if cr.Spec.VLSelect != nil {
		vms := cr.Spec.VLSelect
		if vms.ServiceSpec != nil && vms.ServiceSpec.Name == cr.GetVLSelectName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", cr.GetVLSelectName())
		}
		if vms.HPA != nil {
			if err := vms.HPA.Validate(); err != nil {
				return err
			}
		}
	}
	if cr.Spec.VLInsert != nil {
		vli := cr.Spec.VLInsert
		if vli.ServiceSpec != nil && vli.ServiceSpec.Name == cr.GetVLInsertName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", cr.GetVLInsertName())
		}
		if vli.HPA != nil {
			if err := vli.HPA.Validate(); err != nil {
				return err
			}
		}
	}
	if cr.Spec.VLStorage != nil {
		vls := cr.Spec.VLStorage
		if vls.ServiceSpec != nil && vls.ServiceSpec.Name == cr.GetVLStorageName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", cr.GetVLStorageName())
		}
	}
	if cr.Spec.RequestsLoadBalancer.Enabled {
		rlb := cr.Spec.RequestsLoadBalancer.Spec
		if rlb.AdditionalServiceSpec != nil && rlb.AdditionalServiceSpec.Name == cr.GetVMAuthLBName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", cr.GetVMAuthLBName())
		}
	}

	return nil
}

// VLSelectSelectorLabels returns selector labels for select cluster component
func (cr *VLCluster) VLSelectSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vlselect",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// VLSelectPodLabels returns pod labels for select cluster component
func (cr *VLCluster) VLSelectPodLabels() map[string]string {
	selectorLabels := cr.VLSelectSelectorLabels()
	if cr.Spec.VLSelect == nil || cr.Spec.VLSelect.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(cr.Spec.VLSelect.PodMetadata.Labels, selectorLabels)
}

// VLInsertSelectorLabels returns selector labels for insert cluster component
func (cr *VLCluster) VLInsertSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vlinsert",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// VLInsertPodLabels returns pod labels for vlinsert cluster component
func (cr *VLCluster) VLInsertPodLabels() map[string]string {
	selectorLabels := cr.VLInsertSelectorLabels()
	if cr.Spec.VLInsert == nil || cr.Spec.VLInsert.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(cr.Spec.VLInsert.PodMetadata.Labels, selectorLabels)
}

// VLStorageSelectorLabels  returns pod labels for vlstorage cluster component
func (cr *VLCluster) VLStorageSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vlstorage",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// VLStoragePodLabels returns pod labels for the vmstorage cluster component
func (cr *VLCluster) VLStoragePodLabels() map[string]string {
	selectorLabels := cr.VLStorageSelectorLabels()
	if cr.Spec.VLStorage == nil || cr.Spec.VLStorage.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(cr.Spec.VLStorage.PodMetadata.Labels, selectorLabels)
}

// AvailableStorageNodeIDs returns ids of the storage nodes for the provided component
func (cr *VLCluster) AvailableStorageNodeIDs(requestsType string) []int32 {
	var result []int32
	if cr.Spec.VLStorage == nil || cr.Spec.VLStorage.ReplicaCount == nil {
		return result
	}
	maintenanceNodes := make(map[int32]struct{})
	switch requestsType {
	case "select":
		for _, i := range cr.Spec.VLStorage.MaintenanceSelectNodeIDs {
			maintenanceNodes[i] = struct{}{}
		}
	case "insert":
		for _, i := range cr.Spec.VLStorage.MaintenanceInsertNodeIDs {
			maintenanceNodes[i] = struct{}{}
		}
	default:
		panic("BUG unsupported requestsType: " + requestsType)
	}
	for i := int32(0); i < *cr.Spec.VLStorage.ReplicaCount; i++ {
		if _, ok := maintenanceNodes[i]; ok {
			continue
		}
		result = append(result, i)
	}
	return result
}

var globalClusterLabels = map[string]string{"app.kubernetes.io/part-of": "vlcluster"}

// FinalLabels adds cluster labels to the base labels and filters by prefix if needed
func (cr *VLCluster) FinalLabels(selectorLabels map[string]string) map[string]string {
	baseLabels := labels.Merge(globalClusterLabels, selectorLabels)
	if cr.Spec.ManagedMetadata == nil {
		// fast path
		return baseLabels
	}
	return labels.Merge(cr.Spec.ManagedMetadata.Labels, baseLabels)
}

// VLSelectPodAnnotations returns pod annotations for select cluster component
func (cr *VLCluster) VLSelectPodAnnotations() map[string]string {
	if cr.Spec.VLSelect == nil || cr.Spec.VLSelect.PodMetadata == nil {
		return make(map[string]string)
	}
	return cr.Spec.VLSelect.PodMetadata.Annotations
}

// VLInsertPodAnnotations returns pod annotations for insert cluster component
func (cr *VLCluster) VLInsertPodAnnotations() map[string]string {
	if cr.Spec.VLInsert == nil || cr.Spec.VLInsert.PodMetadata == nil {
		return make(map[string]string)
	}
	return cr.Spec.VLInsert.PodMetadata.Annotations
}

// VLStoragePodAnnotations returns pod annotations for storage cluster component
func (cr *VLCluster) VLStoragePodAnnotations() map[string]string {
	if cr.Spec.VLStorage == nil || cr.Spec.VLStorage.PodMetadata == nil {
		return make(map[string]string)
	}
	return cr.Spec.VLStorage.PodMetadata.Annotations
}

// FinalAnnotations returns global annotations to be applied by objects generate for vlcluster
func (cr *VLCluster) FinalAnnotations() map[string]string {
	if cr.Spec.ManagedMetadata == nil {
		return map[string]string{}
	}
	return cr.Spec.ManagedMetadata.Annotations
}

// AnnotationsFiltered implements finalize.crdObject interface
func (cr *VLCluster) AnnotationsFiltered() map[string]string {
	return cr.FinalAnnotations()
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VLCluster) LastAppliedSpecAsPatch() (client.Patch, error) {
	return vmv1beta1.LastAppliedChangesAsPatch(cr.ObjectMeta, cr.Spec)
}

// HasSpecChanges compares cluster spec with last applied cluster spec stored in annotation
func (cr *VLCluster) HasSpecChanges() (bool, error) {
	return vmv1beta1.HasStateChanges(cr.ObjectMeta, cr.Spec)
}

func (cr *VLCluster) Paused() bool {
	return cr.Spec.Paused
}

// GetServiceAccountName returns service account name for all vlcluster components
func (cr *VLCluster) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr *VLCluster) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

// PrefixedName format name of the component with hard-coded prefix
func (cr *VLCluster) PrefixedName() string {
	return fmt.Sprintf("vlcluster-%s", cr.Name)
}

// SelectorLabels defines labels for objects generated used by all cluster components
func (cr *VLCluster) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vlcluster",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// AsURL implements stub for interface.
func (cr *VLCluster) AsURL() string {
	return "unknown"
}

// SelectURL returns url to access VLSelect component
func (cr *VLCluster) SelectURL() string {
	if cr.Spec.VLSelect == nil {
		return ""
	}
	port := cr.Spec.VLSelect.Port
	if port == "" {
		port = "8481"
	}
	if cr.Spec.VLSelect.ServiceSpec != nil && cr.Spec.VLSelect.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.VLSelect.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", vmv1beta1.HTTPProtoFromFlags(cr.Spec.VLSelect.ExtraArgs), cr.GetVLSelectName(), cr.Namespace, port)
}

// InsertURL returns url to access VLInsert component
func (cr *VLCluster) InsertURL() string {
	if cr.Spec.VLInsert == nil {
		return ""
	}
	port := cr.Spec.VLInsert.Port
	if port == "" {
		port = "8480"
	}
	if cr.Spec.VLInsert.ServiceSpec != nil && cr.Spec.VLInsert.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.VLInsert.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", vmv1beta1.HTTPProtoFromFlags(cr.Spec.VLInsert.ExtraArgs), cr.GetVLInsertName(), cr.Namespace, port)
}

// StorageURL returns url to access VLStorage component
func (cr *VLCluster) StorageURL() string {
	if cr.Spec.VLStorage == nil {
		return ""
	}
	port := cr.Spec.VLStorage.Port
	if port == "" {
		port = "8482"
	}
	if cr.Spec.VLStorage.ServiceSpec != nil && cr.Spec.VLStorage.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.VLStorage.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", vmv1beta1.HTTPProtoFromFlags(cr.Spec.VLStorage.ExtraArgs), cr.GetVLStorageName(), cr.Namespace, port)
}

// +kubebuilder:object:root=true

// VLClusterList contains a list of VLCluster
type VLClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VLCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VLCluster{}, &VLClusterList{})
}
