package v1beta1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
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

	// ServiceAccountName is the name of the ServiceAccount to use to run the
	// VMSelect, VMStorage and VMInsert Pods.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

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
	RequestsLoadBalancer VMAuthLoadBalancer `json:"requestLoadBalancer,omitempty"`
}

// VMAuthLBSelectorLabels defines selector labels for vmauth balancer
func (cr VMCluster) VMAuthLBSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmclusterlb-vmauth-balancer",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// GetVMAuthLBName returns prefixed name for the loadbalanacer components
func (cr VMCluster) GetVMAuthLBName() string {
	return fmt.Sprintf("vmclusterlb-%s", cr.Name)
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMCluster) UnmarshalJSON(src []byte) error {
	type pcr VMCluster
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	prev, err := parseLastAppliedSpec[VMClusterSpec](cr)
	if err != nil {
		return err
	}
	cr.ParsedLastAppliedSpec = prev
	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMClusterSpec) UnmarshalJSON(src []byte) error {
	type pcr VMClusterSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vmcluster spec: %s, err: %s", string(src), err)
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
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.clusterStatus",description="Current status of cluster"
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
	// Deprecated.
	UpdateFailCount int `json:"updateFailCount"`
	// Deprecated.
	LastSync     string       `json:"lastSync,omitempty"`
	UpdateStatus UpdateStatus `json:"clusterStatus,omitempty"`
	Reason       string       `json:"reason,omitempty"`
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

// GetSelectLBName returns headless lb name for select component
func (cr VMCluster) GetSelectLBName() string {
	return PrefixedName(cr.Name, "vmselectinternal")
}

func (s VMSelect) BuildPodName(baseName string, podIndex int32, namespace, portName, domain string) string {
	// The default DNS search path is .svc.<cluster domain>
	if domain == "" {
		return fmt.Sprintf("%s-%d.%s.%s:%s,", baseName, podIndex, baseName, namespace, portName)
	}
	return fmt.Sprintf("%s-%d.%s.%s.svc.%s:%s,", baseName, podIndex, baseName, namespace, domain, portName)
}

func PrefixedName(name, prefix string) string {
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

// GetInsertLBName returns headless lb name for insert component
func (cr VMCluster) GetInsertLBName() string {
	return PrefixedName(cr.Name, "vminsertinternal")
}

func (cr *VMInsert) Probe() *EmbeddedProbes {
	return cr.EmbeddedProbes
}

func (cr *VMInsert) ProbePath() string {
	return buildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

func (cr *VMInsert) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(cr.ExtraArgs))
}

func (cr *VMInsert) ProbePort() string {
	return cr.Port
}

func (cr *VMInsert) ProbeNeedLiveness() bool {
	return true
}

// GetInsertName returns vminsert component name
func (cr VMCluster) GetInsertName() string {
	return PrefixedName(cr.Name, "vminsert")
}

// GetInsertName returns select component name
func (cr VMCluster) GetSelectName() string {
	return PrefixedName(cr.Name, "vmselect")
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

func (cr *VMBackup) sanityCheck(l *License) error {
	if !l.IsProvided() && !cr.AcceptEULA {
		return fmt.Errorf("it is required to provide license key. See [here](https://docs.victoriametrics.com/enterprise)")
	}

	if l.IsProvided() {
		return l.sanityCheck()
	}

	return nil
}

type VMRestore struct {
	// OnStart defines configuration for restore on pod start
	// +optional
	OnStart *VMRestoreOnStartConfig `json:"onStart,omitempty"`
}

type VMRestoreOnStartConfig struct {
	// Enabled defines if restore on start enabled
	// +optional
	Enabled bool `json:"enabled,omitempty"`
}

func (s VMStorage) BuildPodName(baseName string, podIndex int32, namespace string, portName string, domain string) string {
	// The default DNS search path is .svc.<cluster domain>
	if domain == "" {
		return fmt.Sprintf("%s-%d.%s.%s:%s,", baseName, podIndex, baseName, namespace, portName)
	}
	return fmt.Sprintf("%s-%d.%s.%s.svc.%s:%s,", baseName, podIndex, baseName, namespace, domain, portName)
}

func (s VMStorage) GetNameWithPrefix(clusterName string) string {
	return PrefixedName(clusterName, "vmstorage")
}

func (s VMStorage) GetStorageVolumeName() string {
	if s.Storage != nil && s.Storage.VolumeClaimTemplate.Name != "" {
		return s.Storage.VolumeClaimTemplate.Name
	}
	return "vmstorage-db"
}

func (s VMSelect) GetCacheMountVolumeName() string {
	storageSpec := s.StorageSpec
	if storageSpec == nil {
		storageSpec = s.Storage
	}
	if storageSpec != nil && storageSpec.VolumeClaimTemplate.Name != "" {
		return storageSpec.VolumeClaimTemplate.Name
	}
	return PrefixedName("cachedir", "vmselect")
}

// Image defines docker image settings
type Image struct {
	// Repository contains name of docker image + it's repository if needed
	Repository string `json:"repository,omitempty"`
	// Tag contains desired docker image version
	Tag string `json:"tag,omitempty"`
	// PullPolicy describes how to pull docker image
	PullPolicy v1.PullPolicy `json:"pullPolicy,omitempty"`
}

func (cr VMCluster) VMSelectSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmselect",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VMCluster) VMSelectPodLabels() map[string]string {
	selectorLabels := cr.VMSelectSelectorLabels()
	if cr.Spec.VMSelect == nil || cr.Spec.VMSelect.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(cr.Spec.VMSelect.PodMetadata.Labels, selectorLabels)
}

func (cr VMCluster) VMInsertSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vminsert",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VMCluster) VMInsertPodLabels() map[string]string {
	selectorLabels := cr.VMInsertSelectorLabels()
	if cr.Spec.VMInsert == nil || cr.Spec.VMInsert.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(cr.Spec.VMInsert.PodMetadata.Labels, selectorLabels)
}

func (cr VMCluster) VMStorageSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmstorage",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VMCluster) AvailableStorageNodeIDs(requestsType string) []int32 {
	var result []int32
	if cr.Spec.VMStorage == nil || cr.Spec.VMStorage.ReplicaCount == nil {
		return result
	}
	maintenanceNodes := make(map[int32]struct{})
	switch requestsType {
	case "select":
		for _, i := range cr.Spec.VMStorage.MaintenanceSelectNodeIDs {
			maintenanceNodes[i] = struct{}{}
		}
	case "insert":
		for _, i := range cr.Spec.VMStorage.MaintenanceInsertNodeIDs {
			maintenanceNodes[i] = struct{}{}
		}
	default:
		panic("BUG unsupported requestsType: " + requestsType)
	}
	for i := int32(0); i < *cr.Spec.VMStorage.ReplicaCount; i++ {
		if _, ok := maintenanceNodes[i]; ok {
			continue
		}
		result = append(result, i)
	}
	return result
}

func (cr VMCluster) VMStoragePodLabels() map[string]string {
	selectorLabels := cr.VMStorageSelectorLabels()
	if cr.Spec.VMStorage == nil || cr.Spec.VMStorage.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(cr.Spec.VMStorage.PodMetadata.Labels, selectorLabels)
}

// FinalLabels adds cluster labels to the base labels and filters by prefix if needed
func (cr VMCluster) FinalLabels(baseLabels map[string]string) map[string]string {
	if cr.ObjectMeta.Labels == nil {
		return baseLabels
	}
	crLabels := filterMapKeysByPrefixes(cr.ObjectMeta.Labels, labelFilterPrefixes)
	return labels.Merge(crLabels, baseLabels)
}

func (cr VMCluster) VMSelectPodAnnotations() map[string]string {
	if cr.Spec.VMSelect == nil || cr.Spec.VMSelect.PodMetadata == nil {
		return make(map[string]string)
	}
	return cr.Spec.VMSelect.PodMetadata.Annotations
}

func (cr VMCluster) VMInsertPodAnnotations() map[string]string {
	if cr.Spec.VMInsert == nil || cr.Spec.VMInsert.PodMetadata == nil {
		return make(map[string]string)
	}
	return cr.Spec.VMInsert.PodMetadata.Annotations
}

func (cr VMCluster) VMStoragePodAnnotations() map[string]string {
	if cr.Spec.VMStorage == nil || cr.Spec.VMStorage.PodMetadata == nil {
		return make(map[string]string)
	}
	return cr.Spec.VMStorage.PodMetadata.Annotations
}

func (cr VMCluster) AnnotationsFiltered() map[string]string {
	return filterMapKeysByPrefixes(cr.ObjectMeta.Annotations, annotationFilterPrefixes)
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VMCluster) LastAppliedSpecAsPatch() (client.Patch, error) {
	data, err := json.Marshal(cr.Spec)
	if err != nil {
		return nil, fmt.Errorf("possible bug, cannot serialize cluster specification as json :%w", err)
	}
	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q: %q}}}`, lastAppliedSpecAnnotationName, data)
	return client.RawPatch(types.MergePatchType, []byte(patch)), nil
}

// HasSpecChanges compares cluster spec with last applied cluster spec stored in annotation
func (cr *VMCluster) HasSpecChanges() (bool, error) {
	lastAppliedClusterJSON := cr.Annotations[lastAppliedSpecAnnotationName]
	if len(lastAppliedClusterJSON) == 0 {
		return true, nil
	}

	instanceSpecData, err := json.Marshal(cr.Spec)
	if err != nil {
		return true, err
	}
	return !bytes.Equal([]byte(lastAppliedClusterJSON), instanceSpecData), nil
}

func (cr *VMCluster) Paused() bool {
	return cr.Spec.Paused
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VMSelect) GetMetricPath() string {
	if cr == nil {
		return healthPath
	}
	return buildPathWithPrefixFlag(cr.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VMSelect) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VMSelect) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VMInsert) GetMetricPath() string {
	if cr == nil {
		return healthPath
	}
	return buildPathWithPrefixFlag(cr.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VMInsert) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VMInsert) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VMStorage) GetMetricPath() string {
	if cr == nil {
		return healthPath
	}
	return buildPathWithPrefixFlag(cr.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VMStorage) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VMStorage) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

func (cr VMBackup) SnapshotCreatePathWithFlags(port string, extraArgs map[string]string) string {
	return joinBackupAuthKey(fmt.Sprintf("http://localhost:%s%s", port, path.Join(buildPathWithPrefixFlag(extraArgs, snapshotCreate))), extraArgs)
}

func (cr VMBackup) SnapshotDeletePathWithFlags(port string, extraArgs map[string]string) string {
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

func (cr VMCluster) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr VMCluster) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

func (cr VMCluster) PrefixedName() string {
	return fmt.Sprintf("vmcluster-%s", cr.Name)
}

func (cr VMCluster) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmcluster",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// AllLabels defines global CR labels
func (cr VMCluster) AllLabels() map[string]string {
	selectorLabels := cr.SelectorLabels()
	// fast path
	if cr.ObjectMeta.Labels == nil {
		return selectorLabels
	}
	crLabels := filterMapKeysByPrefixes(cr.ObjectMeta.Labels, labelFilterPrefixes)
	return labels.Merge(crLabels, selectorLabels)
}

// AsURL implements stub for interface.
func (cr *VMCluster) AsURL() string {
	return "unknown"
}

func (cr *VMCluster) VMSelectURL() string {
	if cr.Spec.VMSelect == nil {
		return ""
	}
	port := cr.Spec.VMSelect.Port
	if port == "" {
		port = "8481"
	}
	if cr.Spec.VMSelect.ServiceSpec != nil && cr.Spec.VMSelect.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.VMSelect.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", protoFromFlags(cr.Spec.VMSelect.ExtraArgs), cr.GetSelectName(), cr.Namespace, port)
}

func (cr *VMCluster) VMInsertURL() string {
	if cr.Spec.VMInsert == nil {
		return ""
	}
	port := cr.Spec.VMInsert.Port
	if port == "" {
		port = "8480"
	}
	if cr.Spec.VMInsert.ServiceSpec != nil && cr.Spec.VMInsert.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.VMInsert.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", protoFromFlags(cr.Spec.VMInsert.ExtraArgs), cr.GetInsertName(), cr.Namespace, port)
}

func (cr *VMCluster) VMStorageURL() string {
	if cr.Spec.VMStorage == nil {
		return ""
	}
	port := cr.Spec.VMStorage.Port
	if port == "" {
		port = "8482"
	}
	if cr.Spec.VMStorage.ServiceSpec != nil && cr.Spec.VMStorage.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.VMStorage.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", protoFromFlags(cr.Spec.VMStorage.ExtraArgs), cr.Spec.VMStorage.GetNameWithPrefix(cr.Name), cr.Namespace, port)
}

// AsCRDOwner implements interface
func (cr *VMCluster) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(Cluster)
}

func (cr VMCluster) GetNSName() string {
	return cr.GetNamespace()
}

func (cr *VMSelect) Probe() *EmbeddedProbes {
	return cr.EmbeddedProbes
}

func (cr *VMSelect) ProbePath() string {
	return buildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

func (cr *VMSelect) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(cr.ExtraArgs))
}

func (cr *VMSelect) ProbePort() string {
	return cr.Port
}

func (cr *VMSelect) ProbeNeedLiveness() bool {
	return true
}

func (cr *VMStorage) Probe() *EmbeddedProbes {
	return cr.EmbeddedProbes
}

func (cr *VMStorage) ProbePath() string {
	return buildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

func (cr *VMStorage) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(cr.ExtraArgs))
}

func (cr *VMStorage) ProbePort() string {
	return cr.Port
}

// SetStatusTo changes update status with optional reason of fail
func (cr *VMCluster) SetUpdateStatusTo(ctx context.Context, r client.Client, status UpdateStatus, maybeErr error) error {
	currentStatus := cr.Status.UpdateStatus
	prevStatus := cr.Status.DeepCopy()
	switch status {
	case UpdateStatusExpanding:
	case UpdateStatusFailed:
		if maybeErr != nil {
			cr.Status.Reason = maybeErr.Error()
		}
	case UpdateStatusOperational:
		cr.Status.Reason = ""
	case UpdateStatusPaused:
		if currentStatus == status {
			return nil
		}
	default:
		panic(fmt.Sprintf("BUG: not expected status=%q", status))
	}
	if equality.Semantic.DeepEqual(&cr.Status, prevStatus) && currentStatus == status {
		return nil
	}
	cr.Status.UpdateStatus = status
	return statusPatch(ctx, r, cr.DeepCopy(), cr.Status)
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMSelect) GetAdditionalService() *AdditionalServiceSpec {
	return cr.ServiceSpec
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMStorage) GetAdditionalService() *AdditionalServiceSpec {
	return cr.ServiceSpec
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMInsert) GetAdditionalService() *AdditionalServiceSpec {
	return cr.ServiceSpec
}

func (cr *VMStorage) ProbeNeedLiveness() bool {
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
func (cr *VMAuthLoadBalancerSpec) Probe() *EmbeddedProbes {
	return cr.EmbeddedProbes
}

// ProbePort returns port for probe requests
func (cr *VMAuthLoadBalancerSpec) ProbePort() string {
	return cr.Port
}

// ProbeNeedLiveness defines if probe need default liveness
func (cr *VMAuthLoadBalancerSpec) ProbeNeedLiveness() bool {
	return true
}

// ProbePath returns path for probe requests
func (cr *VMAuthLoadBalancerSpec) ProbePath() string {
	return buildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

// ProbeScheme returns scheme for probe requests
func (cr *VMAuthLoadBalancerSpec) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(cr.ExtraArgs))
}

// GetServiceScrape implements build.serviceScrapeBuilder interface
func (cr *VMAuthLoadBalancerSpec) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// GetExtraArgs implements build.serviceScrapeBuilder interface
func (cr *VMAuthLoadBalancerSpec) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// GetMetricPath implements build.serviceScrapeBuilder interface
func (cr *VMAuthLoadBalancerSpec) GetMetricPath() string {
	return buildPathWithPrefixFlag(cr.ExtraArgs, metricPath)
}
