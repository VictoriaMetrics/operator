package v1beta1

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMClusterSpec defines the desired state of VMCluster
// +k8s:openapi-gen=true
type VMClusterSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// RetentionPeriod for the stored metrics
	// Note VictoriaMetrics has data/ and indexdb/ folders
	// metrics from data/ removed eventually as soon as partition leaves retention period
	// reverse index data at indexdb rotates once at the half of configured
	// [retention period](https://docs.victoriametrics.com/Single-server-VictoriaMetrics/#retention)
	RetentionPeriod string `json:"retentionPeriod"`
	// ReplicationFactor defines how many copies of data make among
	// distinct storage nodes
	// +optional
	ReplicationFactor *int32 `json:"replicationFactor,omitempty"`

	*ServiceAccount `json:",inline,omitempty"`

	// ClusterVersion defines default images tag for all components.
	// it can be overwritten with component specific image.tag value.
	// +optional
	ClusterVersion string `json:"clusterVersion,omitempty"`
	// ClusterDomainName defines domain name suffix for in-cluster dns addresses
	// aka .cluster.local
	// used by vminsert and vmselect to build vmstorage address
	// +optional
	ClusterDomainName string `json:"clusterDomainName,omitempty"`

	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see https://kubernetes.io/docs/concepts/containers/images/#referring-to-an-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// License allows to configure license key to be used for enterprise features.
	// Using license key is supported starting from VictoriaMetrics v1.94.0.
	// See [here](https://docs.victoriametrics.com/enterprise)
	// +optional
	License *License `json:"license,omitempty"`

	// +optional
	VMSelect *VMSelect `json:"vmselect,omitempty"`
	// +optional
	VMInsert *VMInsert `json:"vminsert,omitempty"`
	// +optional
	VMStorage *VMStorage `json:"vmstorage,omitempty"`
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

	// RequestsLoadBalancer configures load-balancing for vminsert and vmselect requests
	// it helps to evenly spread load across pods
	// usually it's not possible with kubernetes TCP based service
	RequestsLoadBalancer VMAuthLoadBalancer `json:"requestsLoadBalancer,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *ManagedObjectsMetadata `json:"managedMetadata,omitempty"`
}

// VMAuthLBSelectorLabels defines selector labels for vmauth balancer
func (r *VMCluster) VMAuthLBSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmclusterlb-vmauth-balancer",
		"app.kubernetes.io/instance":  r.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// GetVMAuthLBName returns prefixed name for the loadbalanacer components
func (r *VMCluster) GetVMAuthLBName() string {
	return fmt.Sprintf("vmclusterlb-%s", r.Name)
}

func (r *VMCluster) setLastSpec(prevSpec VMClusterSpec) {
	r.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (r *VMCluster) UnmarshalJSON(src []byte) error {
	type pr VMCluster
	if err := json.Unmarshal(src, (*pr)(r)); err != nil {
		return err
	}
	if err := parseLastAppliedState(r); err != nil {
		return err
	}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (r *VMClusterSpec) UnmarshalJSON(src []byte) error {
	type pr VMClusterSpec
	if err := json.Unmarshal(src, (*pr)(r)); err != nil {
		r.ParsingError = fmt.Sprintf("cannot parse vmcluster spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// VMCluster is fast, cost-effective and scalable time-series database.
// Cluster version with
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMCluster App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Statefulset,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=vmclusters,scope=Namespaced
// +kubebuilder:printcolumn:name="Insert Count",type="string",JSONPath=".spec.vminsert.replicaCount",description="replicas of VMInsert"
// +kubebuilder:printcolumn:name="Storage Count",type="string",JSONPath=".spec.vmstorage.replicaCount",description="replicas of VMStorage"
// +kubebuilder:printcolumn:name="Select Count",type="string",JSONPath=".spec.vmselect.replicaCount",description="replicas of VMSelect"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VMCluster struct {
	// +optional
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              VMClusterSpec `json:"spec"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VMClusterSpec `json:"-" yaml:"-"`
	// +optional
	Status VMClusterStatus `json:"status,omitempty"`
}

// AsOwner returns owner references with current object as owner
func (c *VMCluster) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         c.APIVersion,
			Kind:               c.Kind,
			Name:               c.Name,
			UID:                c.UID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	}
}

// VMClusterStatus defines the observed state of VMCluster
type VMClusterStatus struct {
	StatusMetadata `json:",inline"`
	// LegacyStatus is deprecated and will be removed at v0.52.0 version
	LegacyStatus UpdateStatus `json:"clusterStatus,omitempty"`
}

// GetStatusMetadata returns metadata for object status
func (r *VMClusterStatus) GetStatusMetadata() *StatusMetadata {
	return &r.StatusMetadata
}

// VMClusterList contains a list of VMCluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VMClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VMCluster{}, &VMClusterList{})
}

// VMSelect defines configuration section for vmselect components of the victoria-metrics cluster
type VMSelect struct {
	// PodMetadata configures Labels and Annotations which are propagated to the VMSelect pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for VMSelect to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VMSelect to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// CacheMountPath allows to add cache persistent for VMSelect,
	// will use "/cache" as default if not specified.
	// +optional
	CacheMountPath string `json:"cacheMountPath,omitempty"`

	// Storage - add persistent volume for cacheMountPath
	// its useful for persistent cache
	// use storage instead of persistentVolume.
	// +deprecated
	// +optional
	Storage *StorageSpec `json:"persistentVolume,omitempty"`
	// StorageSpec - add persistent volume claim for cacheMountPath
	// its needed for persistent cache
	// +optional
	StorageSpec *StorageSpec `json:"storage,omitempty"`
	// ClusterNativePort for multi-level cluster setup.
	// More [details](https://docs.victoriametrics.com/Cluster-VictoriaMetrics#multi-level-cluster-setup)
	// +optional
	ClusterNativePort string `json:"clusterNativeListenPort,omitempty"`

	// ServiceSpec that will be added to vmselect service spec
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmselect VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*EmbeddedProbes     `json:",inline"`
	// Configures horizontal pod autoscaling.
	// Note, enabling this option disables vmselect to vmselect communication. In most cases it's not an issue.
	// +optional
	HPA *EmbeddedHPA `json:"hpa,omitempty"`
	// RollingUpdateStrategy defines strategy for application updates
	// Default is OnDelete, in this case operator handles update process
	// Can be changed for RollingUpdate
	// +optional
	RollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"rollingUpdateStrategy,omitempty"`
	// ClaimTemplates allows adding additional VolumeClaimTemplates for StatefulSet
	ClaimTemplates []v1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`

	CommonDefaultableParams           `json:",inline"`
	CommonApplicationDeploymentParams `json:",inline"`
}

// GetVMSelectLBName returns headless proxy service name for select component
func (r *VMCluster) GetVMSelectLBName() string {
	return prefixedName(r.Name, "vmselectinternal")
}

func prefixedName(name, prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, name)
}

type InsertPorts struct {
	// GraphitePort listen port
	// +optional
	GraphitePort string `json:"graphitePort,omitempty"`
	// InfluxPort listen port
	// +optional
	InfluxPort string `json:"influxPort,omitempty"`
	// OpenTSDBHTTPPort for http connections.
	// +optional
	OpenTSDBHTTPPort string `json:"openTSDBHTTPPort,omitempty"`
	// OpenTSDBPort for tcp and udp listen
	// +optional
	OpenTSDBPort string `json:"openTSDBPort,omitempty"`
}

type VMInsert struct {
	// PodMetadata configures Labels and Annotations which are propagated to the VMInsert pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for VMInsert to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VMInsert to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`

	// InsertPorts - additional listen ports for data ingestion.
	InsertPorts *InsertPorts `json:"insertPorts,omitempty"`

	// ClusterNativePort for multi-level cluster setup.
	// More [details](https://docs.victoriametrics.com/Cluster-VictoriaMetrics#multi-level-cluster-setup)
	// +optional
	ClusterNativePort string `json:"clusterNativeListenPort,omitempty"`

	// ServiceSpec that will be added to vminsert service spec
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vminsert VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`

	// UpdateStrategy - overrides default update strategy.
	// +kubebuilder:validation:Enum=Recreate;RollingUpdate
	// +optional
	UpdateStrategy *appsv1.DeploymentStrategyType `json:"updateStrategy,omitempty"`
	// RollingUpdate - overrides deployment update params.
	// +optional
	RollingUpdate *appsv1.RollingUpdateDeployment `json:"rollingUpdate,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*EmbeddedProbes     `json:",inline"`
	// HPA defines kubernetes PodAutoScaling configuration version 2.
	HPA *EmbeddedHPA `json:"hpa,omitempty"`

	CommonDefaultableParams           `json:",inline"`
	CommonApplicationDeploymentParams `json:",inline"`
}

// GetVMInsertLBName returns headless proxy service name for insert component
func (r *VMCluster) GetVMInsertLBName() string {
	return prefixedName(r.Name, "vminsertinternal")
}

func (r *VMInsert) Probe() *EmbeddedProbes {
	return r.EmbeddedProbes
}

func (r *VMInsert) ProbePath() string {
	return buildPathWithPrefixFlag(r.ExtraArgs, healthPath)
}

func (r *VMInsert) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(r.ExtraArgs))
}

func (r *VMInsert) ProbePort() string {
	return r.Port
}

func (r *VMInsert) ProbeNeedLiveness() bool {
	return true
}

// GetVMInsertName returns vminsert component name
func (r *VMCluster) GetVMInsertName() string {
	return prefixedName(r.Name, "vminsert")
}

// GetInsertName returns select component name
func (r *VMCluster) GetVMSelectName() string {
	return prefixedName(r.Name, "vmselect")
}

func (r *VMCluster) GetVMStorageName() string {
	return prefixedName(r.Name, "vmstorage")
}

type VMStorage struct {
	// PodMetadata configures Labels and Annotations which are propagated to the VMStorage pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for VMStorage to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VMStorage to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// StorageDataPath - path to storage data
	// +optional
	StorageDataPath string `json:"storageDataPath,omitempty"`
	// Storage - add persistent volume for StorageDataPath
	// its useful for persistent cache
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`

	// VMInsertPort for VMInsert connections
	// +optional
	VMInsertPort string `json:"vmInsertPort,omitempty"`

	// VMSelectPort for VMSelect connections
	// +optional
	VMSelectPort string `json:"vmSelectPort,omitempty"`

	// VMBackup configuration for backup
	// +optional
	VMBackup *VMBackup `json:"vmBackup,omitempty"`
	// ServiceSpec that will be create additional service for vmstorage
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmstorage VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*EmbeddedProbes     `json:",inline"`
	// MaintenanceInsertNodeIDs - excludes given node ids from insert requests routing, must contain pod suffixes - for pod-0, id will be 0 and etc.
	// lets say, you have pod-0, pod-1, pod-2, pod-3. to exclude pod-0 and pod-3 from insert routing, define nodeIDs: [0,3].
	// Useful at storage expanding, when you want to rebalance some data at cluster.
	// +optional
	MaintenanceInsertNodeIDs []int32 `json:"maintenanceInsertNodeIDs,omitempty"`
	// MaintenanceInsertNodeIDs - excludes given node ids from select requests routing, must contain pod suffixes - for pod-0, id will be 0 and etc.
	MaintenanceSelectNodeIDs []int32 `json:"maintenanceSelectNodeIDs,omitempty"`

	// RollingUpdateStrategy defines strategy for application updates
	// Default is OnDelete, in this case operator handles update process
	// Can be changed for RollingUpdate
	// +optional
	RollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"rollingUpdateStrategy,omitempty"`

	// ClaimTemplates allows adding additional VolumeClaimTemplates for StatefulSet
	ClaimTemplates []v1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`

	CommonDefaultableParams           `json:",inline"`
	CommonApplicationDeploymentParams `json:",inline"`
}

type VMBackup struct {
	// AcceptEULA accepts enterprise feature usage, must be set to true.
	// otherwise backupmanager cannot be added to single/cluster version.
	// https://victoriametrics.com/legal/esa/
	// +optional
	AcceptEULA bool `json:"acceptEULA"`
	// SnapshotCreateURL overwrites url for snapshot create
	// +optional
	SnapshotCreateURL string `json:"snapshotCreateURL,omitempty"`
	// SnapShotDeleteURL overwrites url for snapshot delete
	// +optional
	SnapShotDeleteURL string `json:"snapshotDeleteURL,omitempty"`
	// Defines number of concurrent workers. Higher concurrency may reduce backup duration (default 10)
	// +optional
	Concurrency *int32 `json:"concurrency,omitempty"`
	// Defines destination for backup
	Destination string `json:"destination,omitempty"`
	// DestinationDisableSuffixAdd - disables suffix adding for cluster version backups
	// each vmstorage backup must have unique backup folder
	// so operator adds POD_NAME as suffix for backup destination folder.
	// +optional
	DestinationDisableSuffixAdd bool `json:"destinationDisableSuffixAdd,omitempty"`
	// Custom S3 endpoint for use with S3-compatible storages (e.g. MinIO). S3 is used if not set
	// +optional
	CustomS3Endpoint *string `json:"customS3Endpoint,omitempty"`
	// CredentialsSecret is secret in the same namespace for access to remote storage
	// The secret is mounted into /etc/vm/creds.
	// +optional
	CredentialsSecret *v1.SecretKeySelector `json:"credentialsSecret,omitempty"`

	// Defines if hourly backups disabled (default false)
	// +optional
	DisableHourly *bool `json:"disableHourly,omitempty"`
	// Defines if daily backups disabled (default false)
	// +optional
	DisableDaily *bool `json:"disableDaily,omitempty"`
	// Defines if weekly backups disabled (default false)
	// +optional
	DisableWeekly *bool `json:"disableWeekly,omitempty"`
	// Defines if monthly backups disabled (default false)
	// +optional
	DisableMonthly *bool `json:"disableMonthly,omitempty"`
	// Image - docker image settings for VMBackuper
	// +optional
	Image Image `json:"image,omitempty"`
	// Port for health check connections
	Port string `json:"port,omitempty"`
	// LogFormat for VMBackup to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat *string `json:"logFormat,omitempty"`
	// LogLevel for VMBackup to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel *string `json:"logLevel,omitempty"`
	// Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// if not defined default resources from operator config will be used
	// +optional
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// extra args like maxBytesPerSecond default 0
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// +optional
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`

	// VolumeMounts allows configuration of additional VolumeMounts on the output Deployment definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the vmbackupmanager container,
	// that are generated as a result of StorageSpec objects.
	// +optional
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`

	// Restore Allows to enable restore options for pod
	// Read [more](https://docs.victoriametrics.com/vmbackupmanager#restore-commands)
	// +optional
	Restore *VMRestore `json:"restore,omitempty"`
}

func (r *VMBackup) validate(l *License) error {
	if !l.IsProvided() && !r.AcceptEULA {
		return fmt.Errorf("it is required to provide license key. See [here](https://docs.victoriametrics.com/enterprise)")
	}

	if l.IsProvided() {
		return l.validate()
	}

	return nil
}

// VMRestore defines config options for vmrestore start-up
type VMRestore struct {
	// OnStart defines configuration for restore on pod start
	// +optional
	OnStart *VMRestoreOnStartConfig `json:"onStart,omitempty"`
}

// VMRestoreOnStartConfig controls vmrestore setting
type VMRestoreOnStartConfig struct {
	// Enabled defines if restore on start enabled
	// +optional
	Enabled bool `json:"enabled,omitempty"`
}

// GetStorageVolumeName returns formatted name for vmstorage volume
func (r *VMStorage) GetStorageVolumeName() string {
	if r.Storage != nil && r.Storage.VolumeClaimTemplate.Name != "" {
		return r.Storage.VolumeClaimTemplate.Name
	}
	return "vmstorage-db"
}

// GetCacheMountVolumeName returns formatted name for vmselect volume
func (r *VMSelect) GetCacheMountVolumeName() string {
	storageSpec := r.StorageSpec
	if storageSpec == nil {
		storageSpec = r.Storage
	}
	if storageSpec != nil && storageSpec.VolumeClaimTemplate.Name != "" {
		return storageSpec.VolumeClaimTemplate.Name
	}
	return prefixedName("cachedir", "vmselect")
}

func (r *VMCluster) Validate() error {
	if mustSkipValidation(r) {
		return nil
	}
	if r.Spec.VMSelect != nil {
		vms := r.Spec.VMSelect
		if vms.ServiceSpec != nil && vms.ServiceSpec.Name == r.GetVMSelectName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", r.GetVMSelectName())
		}
		if vms.HPA != nil {
			if err := vms.HPA.validate(); err != nil {
				return err
			}
		}
	}
	if r.Spec.VMInsert != nil {
		vmi := r.Spec.VMInsert
		if vmi.ServiceSpec != nil && vmi.ServiceSpec.Name == r.GetVMInsertName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", r.GetVMInsertName())
		}
		if vmi.HPA != nil {
			if err := vmi.HPA.validate(); err != nil {
				return err
			}
		}
	}
	if r.Spec.VMStorage != nil {
		vms := r.Spec.VMStorage
		if vms.ServiceSpec != nil && vms.ServiceSpec.Name == r.GetVMInsertName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", r.GetVMStorageName())
		}
		if r.Spec.VMStorage.VMBackup != nil {
			if err := r.Spec.VMStorage.VMBackup.validate(r.Spec.License); err != nil {
				return err
			}
		}
	}
	if r.Spec.RequestsLoadBalancer.Enabled {
		rlb := r.Spec.RequestsLoadBalancer.Spec
		if rlb.AdditionalServiceSpec != nil && rlb.AdditionalServiceSpec.Name == r.GetVMAuthLBName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", r.GetVMAuthLBName())
		}
	}

	return nil
}

// VMSelectSelectorLabels returns selector labels for vmselect cluster component
func (r *VMCluster) VMSelectSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmselect",
		"app.kubernetes.io/instance":  r.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// VMSelectPodLabels returns pod labels for vmselect cluster component
func (r *VMCluster) VMSelectPodLabels() map[string]string {
	selectorLabels := r.VMSelectSelectorLabels()
	if r.Spec.VMSelect == nil || r.Spec.VMSelect.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(r.Spec.VMSelect.PodMetadata.Labels, selectorLabels)
}

// VMInsertSelectorLabels returns selector labels for vminsert cluster component
func (r *VMCluster) VMInsertSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vminsert",
		"app.kubernetes.io/instance":  r.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// VMInsertPodLabels returns pod labels for vminsert cluster component
func (r *VMCluster) VMInsertPodLabels() map[string]string {
	selectorLabels := r.VMInsertSelectorLabels()
	if r.Spec.VMInsert == nil || r.Spec.VMInsert.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(r.Spec.VMInsert.PodMetadata.Labels, selectorLabels)
}

// VMStorageSelectorLabels  returns pod labels for vmstorage cluster component
func (r VMCluster) VMStorageSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmstorage",
		"app.kubernetes.io/instance":  r.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// VMStoragePodLabels returns pod labels for the vmstorage cluster component
func (r *VMCluster) VMStoragePodLabels() map[string]string {
	selectorLabels := r.VMStorageSelectorLabels()
	if r.Spec.VMStorage == nil || r.Spec.VMStorage.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(r.Spec.VMStorage.PodMetadata.Labels, selectorLabels)
}

// AvailableStorageNodeIDs returns ids of the storage nodes for the provided component
func (r *VMCluster) AvailableStorageNodeIDs(requestsType string) []int32 {
	var result []int32
	if r.Spec.VMStorage == nil || r.Spec.VMStorage.ReplicaCount == nil {
		return result
	}
	maintenanceNodes := make(map[int32]struct{})
	switch requestsType {
	case "select":
		for _, i := range r.Spec.VMStorage.MaintenanceSelectNodeIDs {
			maintenanceNodes[i] = struct{}{}
		}
	case "insert":
		for _, i := range r.Spec.VMStorage.MaintenanceInsertNodeIDs {
			maintenanceNodes[i] = struct{}{}
		}
	default:
		panic("BUG unsupported requestsType: " + requestsType)
	}
	for i := int32(0); i < *r.Spec.VMStorage.ReplicaCount; i++ {
		if _, ok := maintenanceNodes[i]; ok {
			continue
		}
		result = append(result, i)
	}
	return result
}

var globalClusterLabels = map[string]string{"app.kubernetes.io/part-of": "vmcluster"}

// FinalLabels adds cluster labels to the base labels and filters by prefix if needed
func (r *VMCluster) FinalLabels(selectorLabels map[string]string) map[string]string {
	baseLabels := labels.Merge(globalClusterLabels, selectorLabels)
	if r.ObjectMeta.Labels == nil && r.Spec.ManagedMetadata == nil {
		// fast path
		return baseLabels
	}
	var result map[string]string
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	if r.ObjectMeta.Labels != nil {
		result = filterMapKeysByPrefixes(r.ObjectMeta.Labels, labelFilterPrefixes)
	}
	if r.Spec.ManagedMetadata != nil {
		result = labels.Merge(result, r.Spec.ManagedMetadata.Labels)
	}
	return labels.Merge(result, baseLabels)
}

// VMSelectPodAnnotations returns pod annotations for vmselect cluster component
func (r *VMCluster) VMSelectPodAnnotations() map[string]string {
	if r.Spec.VMSelect == nil || r.Spec.VMSelect.PodMetadata == nil {
		return make(map[string]string)
	}
	return r.Spec.VMSelect.PodMetadata.Annotations
}

// VMInsertPodAnnotations returns pod annotations for vminsert cluster component
func (r *VMCluster) VMInsertPodAnnotations() map[string]string {
	if r.Spec.VMInsert == nil || r.Spec.VMInsert.PodMetadata == nil {
		return make(map[string]string)
	}
	return r.Spec.VMInsert.PodMetadata.Annotations
}

// VMStoragePodAnnotations returns pod annotations for vmstorage cluster component
func (r *VMCluster) VMStoragePodAnnotations() map[string]string {
	if r.Spec.VMStorage == nil || r.Spec.VMStorage.PodMetadata == nil {
		return make(map[string]string)
	}
	return r.Spec.VMStorage.PodMetadata.Annotations
}

// AnnotationsFiltered returns global annotations to be applied by objects generate for vmcluster
func (r *VMCluster) AnnotationsFiltered() map[string]string {
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	dst := filterMapKeysByPrefixes(r.ObjectMeta.Annotations, annotationFilterPrefixes)
	if r.Spec.ManagedMetadata != nil {
		if dst == nil {
			dst = make(map[string]string)
		}
		for k, v := range r.Spec.ManagedMetadata.Annotations {
			dst[k] = v
		}
	}
	return dst

}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (r *VMCluster) LastAppliedSpecAsPatch() (client.Patch, error) {
	return lastAppliedChangesAsPatch(r.ObjectMeta, r.Spec)
}

// HasSpecChanges compares cluster spec with last applied cluster spec stored in annotation
func (r *VMCluster) HasSpecChanges() (bool, error) {
	return hasStateChanges(r.ObjectMeta, r.Spec)
}

func (r *VMCluster) Paused() bool {
	return r.Spec.Paused
}

// GetMetricPath returns prefixed path for metric requests
func (r *VMSelect) GetMetricPath() string {
	if r == nil {
		return healthPath
	}
	return buildPathWithPrefixFlag(r.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (r *VMSelect) GetExtraArgs() map[string]string {
	return r.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (r *VMSelect) GetServiceScrape() *VMServiceScrapeSpec {
	return r.ServiceScrapeSpec
}

// GetMetricPath returns prefixed path for metric requests
func (r *VMInsert) GetMetricPath() string {
	if r == nil {
		return healthPath
	}
	return buildPathWithPrefixFlag(r.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (r *VMInsert) GetExtraArgs() map[string]string {
	return r.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (r *VMInsert) GetServiceScrape() *VMServiceScrapeSpec {
	return r.ServiceScrapeSpec
}

// GetMetricPath returns prefixed path for metric requests
func (r *VMStorage) GetMetricPath() string {
	if r == nil {
		return healthPath
	}
	return buildPathWithPrefixFlag(r.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (r *VMStorage) GetExtraArgs() map[string]string {
	return r.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (r *VMStorage) GetServiceScrape() *VMServiceScrapeSpec {
	return r.ServiceScrapeSpec
}

// SnapshotCreatePathWithFlags returns url for accessing vmbackupmanager component
func (r *VMBackup) SnapshotCreatePathWithFlags(port string, extraArgs map[string]string) string {
	return joinBackupAuthKey(fmt.Sprintf("http://localhost:%s%s", port, path.Join(buildPathWithPrefixFlag(extraArgs, snapshotCreate))), extraArgs)
}

// SnapshotDeletePathWithFlags returns url for accessing vmbackupmanager component
func (r *VMBackup) SnapshotDeletePathWithFlags(port string, extraArgs map[string]string) string {
	return joinBackupAuthKey(fmt.Sprintf("http://localhost:%s%s", port, path.Join(buildPathWithPrefixFlag(extraArgs, snapshotDelete))), extraArgs)
}

func joinBackupAuthKey(urlPath string, extraArgs map[string]string) string {
	if authKey, ok := extraArgs["snapshotAuthKey"]; ok {
		separator := "?"
		idx := strings.IndexByte(urlPath, '?')
		if idx > 0 {
			separator = "&"
		}
		return urlPath + separator + "authKey=" + authKey
	}
	return urlPath
}

func (r *VMCluster) GetServiceAccount() *ServiceAccount {
	sa := r.Spec.ServiceAccount
	if sa == nil {
		sa = &ServiceAccount{
			Name:           r.PrefixedName(),
			AutomountToken: true,
		}
	}
	return sa
}

func (r *VMCluster) IsOwnsServiceAccount() bool {
	if r.Spec.ServiceAccount != nil && r.Spec.ServiceAccount.Name != "" {
		return r.Spec.ServiceAccount.Name == ""
	}
	return false
}

// PrefixedName format name of the component with hard-coded prefix
func (r *VMCluster) PrefixedName() string {
	return fmt.Sprintf("vmcluster-%s", r.Name)
}

// SelectorLabels defines labels for objects generated used by all cluster components
func (r *VMCluster) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmcluster",
		"app.kubernetes.io/instance":  r.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// AsURL implements stub for interface.
func (r *VMCluster) AsURL() string {
	return "unknown"
}

func (r *VMCluster) VMSelectURL() string {
	if r.Spec.VMSelect == nil {
		return ""
	}
	port := r.Spec.VMSelect.Port
	if port == "" {
		port = "8481"
	}
	if r.Spec.VMSelect.ServiceSpec != nil && r.Spec.VMSelect.ServiceSpec.UseAsDefault {
		for _, svcPort := range r.Spec.VMSelect.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", protoFromFlags(r.Spec.VMSelect.ExtraArgs), r.GetVMSelectName(), r.Namespace, port)
}

func (r *VMCluster) VMInsertURL() string {
	if r.Spec.VMInsert == nil {
		return ""
	}
	port := r.Spec.VMInsert.Port
	if port == "" {
		port = "8480"
	}
	if r.Spec.VMInsert.ServiceSpec != nil && r.Spec.VMInsert.ServiceSpec.UseAsDefault {
		for _, svcPort := range r.Spec.VMInsert.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", protoFromFlags(r.Spec.VMInsert.ExtraArgs), r.GetVMInsertName(), r.Namespace, port)
}

func (r *VMCluster) VMStorageURL() string {
	if r.Spec.VMStorage == nil {
		return ""
	}
	port := r.Spec.VMStorage.Port
	if port == "" {
		port = "8482"
	}
	if r.Spec.VMStorage.ServiceSpec != nil && r.Spec.VMStorage.ServiceSpec.UseAsDefault {
		for _, svcPort := range r.Spec.VMStorage.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", protoFromFlags(r.Spec.VMStorage.ExtraArgs), r.GetVMStorageName(), r.Namespace, port)
}

// AsCRDOwner implements interface
func (r *VMCluster) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(Cluster)
}

// GetNSName implements build.builderOpts interface
func (r *VMCluster) GetNSName() string {
	return r.GetNamespace()
}

func (r *VMSelect) Probe() *EmbeddedProbes {
	return r.EmbeddedProbes
}

func (r *VMSelect) ProbePath() string {
	return buildPathWithPrefixFlag(r.ExtraArgs, healthPath)
}

func (r *VMSelect) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(r.ExtraArgs))
}

func (r *VMSelect) ProbePort() string {
	return r.Port
}

func (r *VMSelect) ProbeNeedLiveness() bool {
	return true
}

func (r *VMStorage) Probe() *EmbeddedProbes {
	return r.EmbeddedProbes
}

func (r *VMStorage) ProbePath() string {
	return buildPathWithPrefixFlag(r.ExtraArgs, healthPath)
}

func (r *VMStorage) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(r.ExtraArgs))
}

func (r *VMStorage) ProbePort() string {
	return r.Port
}

// SetStatusTo changes update status with optional reason of fail
func (r *VMCluster) SetUpdateStatusTo(ctx context.Context, c client.Client, status UpdateStatus, maybeErr error) error {
	return updateObjectStatus(ctx, c, &patchStatusOpts[*VMCluster, *VMClusterStatus]{
		actualStatus: status,
		r:            r,
		rStatus:      &r.Status,
		maybeErr:     maybeErr,
		mutateCurrentBeforeCompare: func(vs *VMClusterStatus) {
			vs.LegacyStatus = vs.UpdateStatus
		},
	})
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (r *VMSelect) GetAdditionalService() *AdditionalServiceSpec {
	return r.ServiceSpec
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (r *VMStorage) GetAdditionalService() *AdditionalServiceSpec {
	return r.ServiceSpec
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (r *VMInsert) GetAdditionalService() *AdditionalServiceSpec {
	return r.ServiceSpec
}

// ProbeNeedLiveness implements build.probeCRD interface
func (r *VMStorage) ProbeNeedLiveness() bool {
	return false
}

// VMAuthLoadBalancer configures vmauth as a load balancer
// for the requests
type VMAuthLoadBalancer struct {
	Enabled                bool                   `json:"enabled,omitempty"`
	DisableInsertBalancing bool                   `json:"disableInsertBalancing,omitempty"`
	DisableSelectBalancing bool                   `json:"disableSelectBalancing,omitempty"`
	Spec                   VMAuthLoadBalancerSpec `json:"spec,omitempty"`
}

// VMAuthLoadBalancerSpec defines configuration spec for VMAuth used as load-balancer
// for VMCluster component
type VMAuthLoadBalancerSpec struct {
	// Common params for scheduling
	// PodMetadata configures Labels and Annotations which are propagated to the vmauth lb pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// AdditionalServiceSpec defines service override configuration for vmauth lb deployment
	// it'll be only applied to vmclusterlb- service
	AdditionalServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmauthlb VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`

	// LogFormat for vmauth
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for vmauth container.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel                          string `json:"logLevel,omitempty"`
	CommonApplicationDeploymentParams `json:",inline"`
	CommonDefaultableParams           `json:",inline"`
	*EmbeddedProbes                   `json:",inline"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
}

// ProbePath returns path for probe requests
func (r *VMAuthLoadBalancerSpec) Probe() *EmbeddedProbes {
	return r.EmbeddedProbes
}

// ProbePort returns port for probe requests
func (r *VMAuthLoadBalancerSpec) ProbePort() string {
	return r.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (r *VMAuthLoadBalancerSpec) ProbeNeedLiveness() bool {
	return false
}

// ProbePath returns path for probe requests
func (r *VMAuthLoadBalancerSpec) ProbePath() string {
	return buildPathWithPrefixFlag(r.ExtraArgs, healthPath)
}

// ProbeScheme returns scheme for probe requests
func (r *VMAuthLoadBalancerSpec) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(r.ExtraArgs))
}

// GetServiceScrape implements build.serviceScrapeBuilder interface
func (r *VMAuthLoadBalancerSpec) GetServiceScrape() *VMServiceScrapeSpec {
	return r.ServiceScrapeSpec
}

// GetExtraArgs implements build.serviceScrapeBuilder interface
func (r *VMAuthLoadBalancerSpec) GetExtraArgs() map[string]string {
	return r.ExtraArgs
}

// GetMetricPath implements build.serviceScrapeBuilder interface
func (r *VMAuthLoadBalancerSpec) GetMetricPath() string {
	return buildPathWithPrefixFlag(r.ExtraArgs, metricPath)
}
