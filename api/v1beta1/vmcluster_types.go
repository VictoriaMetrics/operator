package v1beta1

import (
	"fmt"
	"path"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

const (
	ClusterStatusExpanding   = "expanding"
	ClusterStatusOperational = "operational"
	ClusterStatusFailed      = "failed"

	InternalOperatorError = "failed to perform vmcluster preparing jobs"

	StorageRollingUpdateFailed = "failed to perform rolling update on vmStorage"
	StorageCreationFailed      = "failed to create vmStorage statefulset"

	SelectRollingUpdateFailed = "failed to perform rolling update on vmSelect"
	SelectCreationFailed      = "failed to create vmSelect statefulset"
	InsertCreationFailed      = "failed to create vmInsert deployment"
)

// VMClusterSpec defines the desired state of VMCluster
// +k8s:openapi-gen=true
type VMClusterSpec struct {
	// RetentionPeriod in months
	// +kubebuilder:validation:Pattern:="[1-9]+"
	RetentionPeriod string `json:"retentionPeriod"`
	// ReplicationFactor defines how many copies of data make among
	// distinct storage nodes
	// +optional
	ReplicationFactor *int32 `json:"replicationFactor,omitempty"`
	// PodSecurityPolicyName - defines name for podSecurityPolicy
	// in case of empty value, prefixedName will be used.
	// +optional
	PodSecurityPolicyName string `json:"podSecurityPolicyName,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the
	// VMSelect Pods.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// +optional
	VMSelect *VMSelect `json:"vmselect,omitempty"`
	// +optional
	VMInsert *VMInsert `json:"vminsert,omitempty"`
	// +optional
	VMStorage *VMStorage `json:"vmstorage,omitempty"`
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
// +kubebuilder:resource:path=vmclusters,scope=Namespaced
// +kubebuilder:printcolumn:name="Insert Count",type="string",JSONPath=".spec.vminsert.replicaCount",description="replicas of VMInsert"
// +kubebuilder:printcolumn:name="Storage Count",type="string",JSONPath=".spec.vmstorage.replicaCount",description="replicas of VMStorage"
// +kubebuilder:printcolumn:name="Select Count",type="string",JSONPath=".spec.vmselect.replicaCount",description="replicas of VMSelect"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.clusterStatus",description="Current status of cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VMCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              VMClusterSpec   `json:"spec"`
	Status            VMClusterStatus `json:"status,omitempty"`
}

func (c *VMCluster) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         c.APIVersion,
			Kind:               c.Kind,
			Name:               c.Name,
			UID:                c.UID,
			Controller:         pointer.BoolPtr(true),
			BlockOwnerDeletion: pointer.BoolPtr(true),
		},
	}
}

// VMClusterStatus defines the observed state of VMCluster
type VMClusterStatus struct {
	UpdateFailCount int    `json:"updateFailCount"`
	LastSync        string `json:"lastSync,omitempty"`
	ClusterStatus   string `json:"clusterStatus"`
	Reason          string `json:"reason,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// VMClusterList contains a list of VMCluster
type VMClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VMCluster{}, &VMClusterList{})
}

type VMSelect struct {
	Name string `json:"name,omitempty"`
	// PodMetadata configures Labels and Annotations which are propagated to the VMSelect pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// Image - docker image settings for VMSelect
	// +optional
	Image Image `json:"image,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the VMSelect
	// object, which shall be mounted into the VMSelect Pods.
	// The Secrets are mounted into /etc/vm/secrets/<secret-name>.
	// +optional
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the VMSelect
	// object, which shall be mounted into the VMSelect Pods.
	// The ConfigMaps are mounted into /etc/vm/configs/<configmap-name>.
	// +optional
	ConfigMaps []string `json:"configMaps,omitempty"`
	// LogFormat for VMSelect to be configured with.
	//default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VMSelect to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// ReplicaCount is the expected size of the VMSelect cluster. The controller will
	// eventually make the size of the running cluster equal to the expected
	// size.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Number of pods",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount,urn:alm:descriptor:io.kubernetes:custom"
	ReplicaCount *int32 `json:"replicaCount"`
	// Volumes allows configuration of additional volumes on the output Deployment definition.
	// Volumes specified will be appended to other volumes that are generated as a result of
	// StorageSpec objects.
	// +optional
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output Deployment definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the VMSelect container,
	// that are generated as a result of StorageSpec objects.
	// +optional
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`
	// Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resources",xDescriptors="urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	// +optional
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// Affinity If specified, the pod's scheduling constraints.
	// +optional
	Affinity *v1.Affinity `json:"affinity,omitempty"`
	// Tolerations If specified, the pod's tolerations.
	// +optional
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
	// SecurityContext holds pod-level security attributes and common container settings.
	// This defaults to the default PodSecurityContext.
	// +optional
	SecurityContext *v1.PodSecurityContext `json:"securityContext,omitempty"`
	// Containers property allows to inject additions sidecars or to patch existing containers.
	// It can be useful for proxies, backup, etc.
	// +optional
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition. Those can be used to e.g.
	// fetch secrets for injection into the VMSelect configuration from external sources. Any
	// errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// Using initContainers for any use case other then secret fetching is entirely outside the scope
	// of what the maintainers will support and by doing so, you accept that this behaviour may break
	// at any time without notice.
	// +optional
	InitContainers []v1.Container `json:"initContainers,omitempty"`

	// Priority class assigned to the Pods
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// HostNetwork controls whether the pod may use the node network namespace
	// +optional
	HostNetwork bool `json:"hostNetwork,omitempty"`
	// DNSPolicy sets DNS policy for the pod
	// +optional
	DNSPolicy v1.DNSPolicy `json:"dnsPolicy,omitempty"`
	// TopologySpreadConstraints embedded kubernetes pod configuration option,
	// controls how pods are spread across your cluster among failure-domains
	// such as regions, zones, nodes, and other user-defined topology domains
	// https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
	// +optional
	TopologySpreadConstraints []v1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// CacheMountPath allows to add cache persistent for VMSelect
	// +optional
	CacheMountPath string `json:"cacheMountPath,omitempty"`

	// Storage - add persistent volume for cacheMounthPath
	// its useful for persistent cache
	// +optional
	Storage *StorageSpec `json:"persistentVolume,omitempty"`
	// ExtraEnvs that will be added to VMSelect pod
	// +optional
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`

	//Port listen port
	// +optional
	Port string `json:"port,omitempty"`

	// SchedulerName - defines kubernetes scheduler name
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`
	// RuntimeClassName - defines runtime class for kubernetes pod.
	//https://kubernetes.io/docs/concepts/containers/runtime-class/
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`
}

func (s VMSelect) GetNameWithPrefix(clusterName string) string {
	if s.Name == "" {
		return PrefixedName(clusterName, "vmselect")
	}
	return PrefixedName(s.Name, "vmselect")
}
func (s VMSelect) BuildPodFQDNName(baseName string, podIndex int32, namespace, portName, domain string) string {
	return fmt.Sprintf("%s-%d.%s.%s.svc.%s:%s,", baseName, podIndex, baseName, namespace, domain, portName)
}

func PrefixedName(name, prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, name)
}

type VMInsert struct {
	// +optional
	Name string `json:"name,omitempty"`

	// PodMetadata configures Labels and Annotations which are propagated to the VMSelect pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`

	// Image - docker image settings for VMInsert
	// +optional
	Image Image `json:"image,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the VMSelect
	// object, which shall be mounted into the VMSelect Pods.
	// The Secrets are mounted into /etc/vm/secrets/<secret-name>.
	// +optional
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the VMSelect
	// object, which shall be mounted into the VMSelect Pods.
	// The ConfigMaps are mounted into /etc/vm/configs/<configmap-name>.
	// +optional
	ConfigMaps []string `json:"configMaps,omitempty"`
	// LogFormat for VMSelect to be configured with.
	//default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VMSelect to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// ReplicaCount is the expected size of the VMSelect cluster. The controller will
	// eventually make the size of the running cluster equal to the expected
	// size.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Number of pods",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount,urn:alm:descriptor:io.kubernetes:custom"
	ReplicaCount *int32 `json:"replicaCount"`
	// Volumes allows configuration of additional volumes on the output Deployment definition.
	// Volumes specified will be appended to other volumes that are generated as a result of
	// StorageSpec objects.
	// +optional
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output Deployment definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the VMSelect container,
	// that are generated as a result of StorageSpec objects.
	// +optional
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`
	// Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resources",xDescriptors="urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	// +optional
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// Affinity If specified, the pod's scheduling constraints.
	// +optional
	Affinity *v1.Affinity `json:"affinity,omitempty"`
	// Tolerations If specified, the pod's tolerations.
	// +optional
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
	// SecurityContext holds pod-level security attributes and common container settings.
	// This defaults to the default PodSecurityContext.
	// +optional
	SecurityContext *v1.PodSecurityContext `json:"securityContext,omitempty"`
	// Containers property allows to inject additions sidecars or to patch existing containers.
	// It can be useful for proxies, backup, etc.
	// +optional
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition. Those can be used to e.g.
	// fetch secrets for injection into the VMSelect configuration from external sources. Any
	// errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// Using initContainers for any use case other then secret fetching is entirely outside the scope
	// of what the maintainers will support and by doing so, you accept that this behaviour may break
	// at any time without notice.
	// +optional
	InitContainers []v1.Container `json:"initContainers,omitempty"`

	// Priority class assigned to the Pods
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// HostNetwork controls whether the pod may use the node network namespace
	// +optional
	HostNetwork bool `json:"hostNetwork,omitempty"`
	// DNSPolicy sets DNS policy for the pod
	// +optional
	DNSPolicy v1.DNSPolicy `json:"dnsPolicy,omitempty"`
	// TopologySpreadConstraints embedded kubernetes pod configuration option,
	// controls how pods are spread across your cluster among failure-domains
	// such as regions, zones, nodes, and other user-defined topology domains
	// https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
	// +optional
	TopologySpreadConstraints []v1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`

	//Port listen port
	// +optional
	Port string `json:"port,omitempty"`

	// SchedulerName - defines kubernetes scheduler name
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`
	// RuntimeClassName - defines runtime class for kubernetes pod.
	//https://kubernetes.io/docs/concepts/containers/runtime-class/
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`

	// ExtraEnvs that will be added to VMSelect pod
	// +optional
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
}

func (i VMInsert) GetNameWithPrefix(clusterName string) string {
	if i.Name == "" {
		return PrefixedName(clusterName, "vminsert")
	}
	return PrefixedName(i.Name, "vminsert")
}

type VMStorage struct {
	// +optional
	Name string `json:"name,omitempty"`

	// PodMetadata configures Labels and Annotations which are propagated to the VMSelect pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`

	// Image - docker image settings for VMStorage
	// +optional
	Image Image `json:"image,omitempty"`

	// Secrets is a list of Secrets in the same namespace as the VMSelect
	// object, which shall be mounted into the VMSelect Pods.
	// The Secrets are mounted into /etc/vm/secrets/<secret-name>.
	// +optional
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the VMSelect
	// object, which shall be mounted into the VMSelect Pods.
	// The ConfigMaps are mounted into /etc/vm/configs/<configmap-name>.
	// +optional
	ConfigMaps []string `json:"configMaps,omitempty"`
	// LogFormat for VMSelect to be configured with.
	//default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VMSelect to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// ReplicaCount is the expected size of the VMSelect cluster. The controller will
	// eventually make the size of the running cluster equal to the expected
	// size.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Number of pods",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount,urn:alm:descriptor:io.kubernetes:custom"
	ReplicaCount *int32 `json:"replicaCount"`
	// Volumes allows configuration of additional volumes on the output Deployment definition.
	// Volumes specified will be appended to other volumes that are generated as a result of
	// StorageSpec objects.
	// +optional
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output Deployment definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the VMSelect container,
	// that are generated as a result of StorageSpec objects.
	// +optional
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`
	// Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resources",xDescriptors="urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	// +optional
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// Affinity If specified, the pod's scheduling constraints.
	// +optional
	Affinity *v1.Affinity `json:"affinity,omitempty"`
	// Tolerations If specified, the pod's tolerations.
	// +optional
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
	// SecurityContext holds pod-level security attributes and common container settings.
	// This defaults to the default PodSecurityContext.
	// +optional
	SecurityContext *v1.PodSecurityContext `json:"securityContext,omitempty"`
	// Containers property allows to inject additions sidecars or to patch existing containers.
	// It can be useful for proxies, backup, etc.
	// +optional
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition. Those can be used to e.g.
	// fetch secrets for injection into the VMSelect configuration from external sources. Any
	// errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// Using initContainers for any use case other then secret fetching is entirely outside the scope
	// of what the maintainers will support and by doing so, you accept that this behaviour may break
	// at any time without notice.
	// +optional
	InitContainers []v1.Container `json:"initContainers,omitempty"`

	// Priority class assigned to the Pods
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// HostNetwork controls whether the pod may use the node network namespace
	// +optional
	HostNetwork bool `json:"hostNetwork,omitempty"`
	// DNSPolicy sets DNS policy for the pod
	// +optional
	DNSPolicy v1.DNSPolicy `json:"dnsPolicy,omitempty"`
	// TopologySpreadConstraints embedded kubernetes pod configuration option,
	// controls how pods are spread across your cluster among failure-domains
	// such as regions, zones, nodes, and other user-defined topology domains
	// https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
	// +optional
	TopologySpreadConstraints []v1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// StorageDataPath - path to storage data
	// +optional
	StorageDataPath string `json:"storageDataPath,omitempty"`
	// Storage - add persistent volume for StorageDataPath
	// its useful for persistent cache
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`

	// +optional
	TerminationGracePeriodSeconds int64 `json:"terminationGracePeriodSeconds,omitempty"`
	// SchedulerName - defines kubernetes scheduler name
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`
	// RuntimeClassName - defines runtime class for kubernetes pod.
	//https://kubernetes.io/docs/concepts/containers/runtime-class/
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`

	//Port for health check connetions
	Port string `json:"port,omitempty"`

	// VMInsertPort for VMInsert connections
	// +optional
	VMInsertPort string `json:"vmInsertPort,omitempty"`

	// VMSelectPort for VMSelect connections
	// +optional
	VMSelectPort string `json:"vmSelectPort,omitempty"`

	// VMBackup configuration for backup
	// +optional
	VMBackup *VMBackup `json:"vmBackup,omitempty"`

	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// ExtraEnvs that will be added to VMSelect pod
	// +optional
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
}

type VMBackup struct {
	// Defines number of concurrent workers. Higher concurrency may reduce backup duration (default 10)
	// +optional
	Concurrency *int32 `json:"concurrency,omitempty"`
	// Defines destination for backup
	Destination string `json:"destination,omitempty"`
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
	//Port for health check connections
	Port string `json:"port,omitempty"`
	// LogFormat for VMSelect to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat *string `json:"logFormat,omitempty"`
	// LogLevel for VMSelect to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel *string `json:"logLevel,omitempty"`
	// Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// if not defined default resources from operator config will be used
	// +optional
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	//extra args like maxBytesPerSecond default 0
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
}

func (s VMStorage) BuildPodFQDNName(baseName string, podIndex int32, namespace, portName, domain string) string {
	return fmt.Sprintf("%s-%d.%s.%s.svc.%s:%s,", baseName, podIndex, baseName, namespace, domain, portName)
}

func (s VMStorage) GetNameWithPrefix(clusterName string) string {
	if s.Name == "" {
		return PrefixedName(clusterName, "vmstorage")
	}
	return PrefixedName(s.Name, "vmstorage")
}

func (s VMStorage) GetStorageVolumeName() string {

	return "vmstorage-db"
}

func (s VMSelect) GetCacheMountVolmeName() string {
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
	//we dont allow to override selector labels
	labels := cr.Spec.VMSelect.PodMetadata.Labels
	for label, value := range selectorLabels {
		labels[label] = value
	}
	return labels
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
	labels := cr.Spec.VMInsert.PodMetadata.Labels
	for label, value := range selectorLabels {
		labels[label] = value
	}
	return labels
}
func (cr VMCluster) VMStorageSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmstorage",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VMCluster) VMStoragePodLabels() map[string]string {
	selectorLabels := cr.VMStorageSelectorLabels()
	if cr.Spec.VMStorage == nil || cr.Spec.VMStorage.PodMetadata == nil {
		return selectorLabels
	}
	labels := cr.Spec.VMStorage.PodMetadata.Labels
	for label, value := range selectorLabels {
		labels[label] = value
	}
	return labels
}

func (cr VMCluster) FinalLabels(baseLabels map[string]string) map[string]string {
	labels := map[string]string{}
	if cr.ObjectMeta.Labels != nil {
		for label, value := range cr.ObjectMeta.Labels {
			labels[label] = value
		}
	}
	for label, value := range baseLabels {
		labels[label] = value
	}
	return labels
}

func (cr VMCluster) VMSelectPodAnnotations() map[string]string {
	if cr.Spec.VMSelect == nil || cr.Spec.VMSelect.PodMetadata == nil {
		return nil
	}
	return cr.Spec.VMSelect.PodMetadata.Annotations
}

func (cr VMCluster) VMInsertPodAnnotations() map[string]string {
	if cr.Spec.VMInsert == nil || cr.Spec.VMInsert.PodMetadata == nil {
		return nil
	}
	return cr.Spec.VMInsert.PodMetadata.Annotations
}

func (cr VMCluster) VMStoragePodAnnotations() map[string]string {
	if cr.Spec.VMStorage == nil || cr.Spec.VMStorage.PodMetadata == nil {
		return nil
	}
	return cr.Spec.VMStorage.PodMetadata.Annotations
}

func (cr VMCluster) Annotations() map[string]string {
	annotations := make(map[string]string, len(cr.ObjectMeta.Annotations))
	for annotation, value := range cr.ObjectMeta.Annotations {
		if !strings.HasPrefix(annotation, "kubectl.kubernetes.io/") {
			annotations[annotation] = value
		}
	}
	return annotations
}
func (cr VMCluster) HealthPathSelect() string {
	if cr.Spec.VMSelect == nil {
		return healthPath
	}
	return buildPathWithPrefixFlag(cr.Spec.VMSelect.ExtraArgs, healthPath)
}

func (cr VMCluster) HealthPathInsert() string {
	if cr.Spec.VMInsert == nil {
		return healthPath
	}
	return buildPathWithPrefixFlag(cr.Spec.VMInsert.ExtraArgs, healthPath)
}

func (cr VMCluster) HealthPathStorage() string {
	if cr.Spec.VMStorage == nil {
		return healthPath
	}
	return buildPathWithPrefixFlag(cr.Spec.VMStorage.ExtraArgs, healthPath)
}

func (cr VMCluster) MetricPathSelect() string {
	if cr.Spec.VMSelect == nil {
		return healthPath
	}
	return buildPathWithPrefixFlag(cr.Spec.VMSelect.ExtraArgs, metricPath)
}

func (cr VMCluster) MetricPathInsert() string {
	if cr.Spec.VMInsert == nil {
		return healthPath
	}
	return buildPathWithPrefixFlag(cr.Spec.VMInsert.ExtraArgs, metricPath)
}

func (cr VMCluster) MetricPathStorage() string {
	if cr.Spec.VMStorage == nil {
		return healthPath
	}
	return buildPathWithPrefixFlag(cr.Spec.VMStorage.ExtraArgs, metricPath)
}

func (cr VMBackup) SnapshotCreatePathWithFlags(port string, extraArgs map[string]string) string {
	return fmt.Sprintf("http://localhost:%s%s", port, path.Join(buildPathWithPrefixFlag(extraArgs, snapshotCreate)))
}

func (cr VMBackup) SnapshotDeletePathWithFlags(port string, extraArgs map[string]string) string {
	return fmt.Sprintf("http://localhost:%s%s", port, path.Join(buildPathWithPrefixFlag(extraArgs, snapshotDelete)))
}

func (cr VMCluster) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr VMCluster) PrefixedName() string {
	return fmt.Sprintf("vmcluster-%s", cr.Name)
}

func (cr VMCluster) GetPSPName() string {
	if cr.Spec.PodSecurityPolicyName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.PodSecurityPolicyName
}

func (cr VMCluster) SelectorLabels() map[string]string {
	return map[string]string{"app.kubernetes.io/name": "vmcluster",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VMCluster) Labels() map[string]string {
	labels := cr.SelectorLabels()
	if cr.ObjectMeta.Labels != nil {
		for label, value := range cr.ObjectMeta.Labels {
			if _, ok := labels[label]; ok {
				// forbid changes for selector labels
				continue
			}
			labels[label] = value
		}
	}
	return labels
}
