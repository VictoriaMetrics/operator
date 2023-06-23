package v1beta1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	"sigs.k8s.io/controller-runtime/pkg/client"
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
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-,omitempty" yaml:"-,omitempty"`
	// RetentionPeriod for the stored metrics
	// Note VictoriaMetrics has data/ and indexdb/ folders
	// metrics from data/ removed eventually as soon as partition leaves retention period
	// reverse index data at indexdb rotates once at the half of configured retention period
	// https://docs.victoriametrics.com/Single-server-VictoriaMetrics.html#retention
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
	// VMSelect, VMStorage and VMInsert Pods.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ClusterVersion defines default images tag for all components.
	// it can be overwritten with component specific image.tag value.
	ClusterVersion string `json:"clusterVersion,omitempty"`

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
	// Deprecated.
	UpdateFailCount int `json:"updateFailCount"`
	// Deprecated.
	LastSync      string `json:"lastSync,omitempty"`
	ClusterStatus string `json:"clusterStatus"`
	Reason        string `json:"reason,omitempty"`
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
	// Name is deprecated and will be removed at 0.22.0 release
	// +deprecated
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
	// default or json
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
	// Specifies the DNS parameters of a pod.
	// Parameters specified here will be merged to the generated DNS
	// configuration based on DNSPolicy.
	// +optional
	DNSConfig *v1.PodDNSConfig `json:"dnsConfig,omitempty"`
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
	// use storage instead of persistentVolume.
	// +deprecated
	// +optional
	Storage *StorageSpec `json:"persistentVolume,omitempty"`
	// StorageSpec - add persistent volume claim for cacheMounthPath
	// its needed for persistent cache
	// +optional
	StorageSpec *StorageSpec `json:"storage,omitempty"`

	// ExtraEnvs that will be added to VMSelect pod
	// +optional
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`

	// Port listen port
	// +optional
	Port string `json:"port,omitempty"`

	// ClusterNativePort for multi-level cluster setup.
	// More details: https://docs.victoriametrics.com/Cluster-VictoriaMetrics.html#multi-level-cluster-setup
	// +optional
	ClusterNativePort string `json:"clusterNativeListenPort,omitempty"`

	// SchedulerName - defines kubernetes scheduler name
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`
	// RuntimeClassName - defines runtime class for kubernetes pod.
	// https://kubernetes.io/docs/concepts/containers/runtime-class/
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`
	// ServiceSpec that will be added to vmselect service spec
	// +optional
	ServiceSpec *ServiceSpec `json:"serviceSpec,omitempty"`
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
	// NodeSelector Define which Nodes the Pods are scheduled on.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// RollingUpdateStrategy defines strategy for application updates
	// Default is OnDelete, in this case operator handles update process
	// Can be changed for RollingUpdate
	// +optional
	RollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"rollingUpdateStrategy,omitempty"`
	// TerminationGracePeriodSeconds period for container graceful termination
	// +optional
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
	// ReadinessGates defines pod readiness gates
	ReadinessGates []v1.PodReadinessGate `json:"readinessGates,omitempty"`
	// ClaimTemplates allows adding additional VolumeClaimTemplates for StatefulSet
	ClaimTemplates []v1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`
}

func (s VMSelect) GetNameWithPrefix(clusterName string) string {
	if s.Name == "" {
		return PrefixedName(clusterName, "vmselect")
	}
	return PrefixedName(s.Name, "vmselect")
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
	// Name is deprecated and will be removed at 0.22.0 release
	// +deprecated
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
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VMSelect to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// ReplicaCount is the expected size of the VMInsert cluster. The controller will
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
	// Specifies the DNS parameters of a pod.
	// Parameters specified here will be merged to the generated DNS
	// configuration based on DNSPolicy.
	// +optional
	DNSConfig *v1.PodDNSConfig `json:"dnsConfig,omitempty"`
	// TopologySpreadConstraints embedded kubernetes pod configuration option,
	// controls how pods are spread across your cluster among failure-domains
	// such as regions, zones, nodes, and other user-defined topology domains
	// https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
	// +optional
	TopologySpreadConstraints []v1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`

	// InsertPorts - additional listen ports for data ingestion.
	InsertPorts *InsertPorts `json:"insertPorts,omitempty"`

	// Port listen port
	// +optional
	Port string `json:"port,omitempty"`

	// ClusterNativePort for multi-level cluster setup.
	// More details: https://docs.victoriametrics.com/Cluster-VictoriaMetrics.html#multi-level-cluster-setup
	// +optional
	ClusterNativePort string `json:"clusterNativeListenPort,omitempty"`

	// SchedulerName - defines kubernetes scheduler name
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`
	// RuntimeClassName - defines runtime class for kubernetes pod.
	// https://kubernetes.io/docs/concepts/containers/runtime-class/
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`

	// ExtraEnvs that will be added to VMSelect pod
	// +optional
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`

	// ServiceSpec that will be added to vminsert service spec
	// +optional
	ServiceSpec *ServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmselect VMServiceScrape spec
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
	// NodeSelector Define which Nodes the Pods are scheduled on.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// TerminationGracePeriodSeconds period for container graceful termination
	// +optional
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
	// ReadinessGates defines pod readiness gates
	ReadinessGates []v1.PodReadinessGate `json:"readinessGates,omitempty"`
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

func (i VMInsert) GetNameWithPrefix(clusterName string) string {
	if i.Name == "" {
		return PrefixedName(clusterName, "vminsert")
	}
	return PrefixedName(i.Name, "vminsert")
}

type VMStorage struct {
	// Name is deprecated and will be removed at 0.22.0 release
	// +deprecated
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
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VMSelect to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// ReplicaCount is the expected size of the VMStorage cluster. The controller will
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
	// Specifies the DNS parameters of a pod.
	// Parameters specified here will be merged to the generated DNS
	// configuration based on DNSPolicy.
	// +optional
	DNSConfig *v1.PodDNSConfig `json:"dnsConfig,omitempty"`
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

	// TerminationGracePeriodSeconds period for container graceful termination
	// +optional
	TerminationGracePeriodSeconds int64 `json:"terminationGracePeriodSeconds,omitempty"`
	// SchedulerName - defines kubernetes scheduler name
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`
	// RuntimeClassName - defines runtime class for kubernetes pod.
	// https://kubernetes.io/docs/concepts/containers/runtime-class/
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`

	// Port for health check connetions
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
	// ServiceSpec that will be create additional service for vmstorage
	// +optional
	ServiceSpec *ServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmselect VMServiceScrape spec
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
	// NodeSelector Define which Nodes the Pods are scheduled on.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// RollingUpdateStrategy defines strategy for application updates
	// Default is OnDelete, in this case operator handles update process
	// Can be changed for RollingUpdate
	// +optional
	RollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"rollingUpdateStrategy,omitempty"`
	// ReadinessGates defines pod readiness gates
	ReadinessGates []v1.PodReadinessGate `json:"readinessGates,omitempty"`

	// ClaimTemplates allows adding additional VolumeClaimTemplates for StatefulSet
	ClaimTemplates []v1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`
}

type VMBackup struct {
	// AcceptEULA accepts enterprise feature usage, must be set to true.
	// otherwise backupmanager cannot be added to single/cluster version.
	// https://victoriametrics.com/legal/eula/
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
	// Read more: https://docs.victoriametrics.com/vmbackupmanager.html#restore-commands
	// +optional
	Restore *VMRestore `json:"restore,omitempty"`
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
	if s.Name == "" {
		return PrefixedName(clusterName, "vmstorage")
	}
	return PrefixedName(s.Name, "vmstorage")
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

func (s VMStorage) UpdateStrategy() appsv1.StatefulSetUpdateStrategyType {
	if s.RollingUpdateStrategy == "" {
		return appsv1.OnDeleteStatefulSetStrategyType
	}
	return s.RollingUpdateStrategy
}

func (s VMSelect) UpdateStrategy() appsv1.StatefulSetUpdateStrategyType {
	if s.RollingUpdateStrategy == "" {
		return appsv1.OnDeleteStatefulSetStrategyType
	}
	return s.RollingUpdateStrategy
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
	patch := fmt.Sprintf(`{"metadata":{"annotations":{"operator.victoriametrics/last-applied-spec": %q}}}`, data)
	return client.RawPatch(types.MergePatchType, []byte(patch)), nil
}

// HasSpecChanges compares cluster spec with last applied cluster spec stored in annotation
func (cr *VMCluster) HasSpecChanges() (bool, error) {
	var prevClusterSpec VMClusterSpec
	lastAppliedClusterJSON := cr.Annotations["operator.victoriametrics/last-applied-spec"]
	if err := json.Unmarshal([]byte(lastAppliedClusterJSON), &prevClusterSpec); err != nil {
		return true, fmt.Errorf("cannot parse last applied cluster spec value: %s : %w", lastAppliedClusterJSON, err)
	}
	instanceSpecData, _ := json.Marshal(cr.Spec)
	return bytes.Equal([]byte(lastAppliedClusterJSON), instanceSpecData), nil
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
	return cr.Spec.ServiceAccountName == "" || cr.Spec.ServiceAccountName == cr.PrefixedName()
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
	return map[string]string{
		"app.kubernetes.io/name":      "vmcluster",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

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
	return fmt.Sprintf("http://%s.%s.svc:%s", cr.Spec.VMSelect.GetNameWithPrefix(cr.Name), cr.Namespace, port)
}

func (cr *VMCluster) VMInsertURL() string {
	if cr.Spec.VMInsert == nil {
		return ""
	}
	port := cr.Spec.VMInsert.Port
	if port == "" {
		port = "8480"
	}
	return fmt.Sprintf("http://%s.%s.svc:%s", cr.Spec.VMInsert.GetNameWithPrefix(cr.Name), cr.Namespace, port)
}

func (cr *VMCluster) VMStorageURL() string {
	if cr.Spec.VMStorage == nil {
		return ""
	}
	port := cr.Spec.VMStorage.Port
	if port == "" {
		port = "8482"
	}
	return fmt.Sprintf("http://%s.%s.svc:%s", cr.Spec.VMStorage.GetNameWithPrefix(cr.Name), cr.Namespace, port)
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

func (cr *VMStorage) ProbeNeedLiveness() bool {
	return false
}
