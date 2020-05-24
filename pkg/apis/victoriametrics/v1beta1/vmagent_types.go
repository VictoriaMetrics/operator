package v1beta1

import (
	monitoringv1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VmAgentSpec defines the desired state of VmAgent
// +k8s:openapi-gen=true
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="The version of VmAlert"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas",description="The desired replicas number of VmAlerts"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VmAgentSpec struct {
	// PodMetadata configures Labels and Annotations which are propagated to the vmagent pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	//base image, if not specified - use default from operator config
	// +optional
	Image *string `json:"image,omitempty"`
	// Version/tag.
	// +optional
	Version string `json:"version,omitempty"`
	// An optional list of references to secrets in the same namespace
	// to use for pulling prometheus and vmagent images from registries
	// see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod
	// +optional
	// +listType=set
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the vmagent
	// object, which shall be mounted into the vmagent Pods.
	// will be mounted at path /etc/vmagent/secrets
	// +optional
	// +listType=set
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the vmagent
	// object, which shall be mounted into the vmagent Pods.
	// will be mounted at path  /etc/vmagent/configs
	// +optional
	// +listType=set
	ConfigMaps []string `json:"configMaps,omitempty"`
	// Log level for vmagent to be configured with.
	//INFO, WARN, ERROR, FATAL, PANIC
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// Log format for vmagent to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// Size is the expected size of the vmagent cluster. The controller will
	// eventually make the size of the running cluster equal to the expected
	// size.
	// NOTE enable vm deduplication for replica usage
	// +kubebuilder:validation:Required
	Replicas *int32 `json:"replicas"`
	// Volumes allows configuration of additional volumes on the output deploy definition.
	// Volumes specified will be appended to other volumes that are generated as a result of
	// StorageSpec objects.
	// +optional
	// +listType=set
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output deploy definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the vmagent container,
	// that are generated as a result of StorageSpec objects.
	// +optional
	// +listType=set
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`
	// Define resources requests and limits for.
	//if not specified - default setting will be used
	// +optional
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// If specified, the pod's scheduling constraints.
	// +optional
	Affinity *v1.Affinity `json:"affinity,omitempty"`
	// If specified, the pod's tolerations.
	// +optional
	// +listType=set
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
	// SecurityContext holds pod-level security attributes and common container settings.
	// This defaults to the default PodSecurityContext.
	// +optional
	SecurityContext *v1.PodSecurityContext `json:"securityContext,omitempty"`
	// ServiceAccountName is the name of the ServiceAccount to use to run the
	// vmagent Pods.
	// required
	ServiceAccountName string `json:"serviceAccountName"`
	// Containers allows injecting additional containers. This is meant to
	// allow adding an authentication proxy to an vmagent pod.
	// +optional
	// +listType=set
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition. Those can be used to e.g.
	// fetch secrets for injection into the vmagent configuration from external sources. Any
	// errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// Using initContainers for any use case other then secret fetching is entirely outside the scope
	// of what the maintainers will support and by doing so, you accept that this behaviour may break
	// at any time without notice.
	// +optional
	// +listType=set
	InitContainers []v1.Container `json:"initContainers,omitempty"`
	// Priority class assigned to the Pods
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// how often scrape by default
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	// +optional
	ScrapeInterval string `json:"scrapeInterval,omitempty"`

	// APIServerConfig allows specifying a host and auth methods to access apiserver.
	// If left empty, vmagent is assumed to run inside of the cluster
	// and will discover API servers automatically and use the pod's CA certificate
	// and bearer token file at /var/run/secrets/kubernetes.io/serviceaccount/.
	// +optional
	APIServerConfig *monitoringv1.APIServerConfig `json:"aPIServerConfig,omitempty"`
	// OverrideHonorLabels if set to true overrides all user configured honor_labels.
	// If HonorLabels is set in ServiceMonitor or PodMonitor to true, this overrides honor_labels to false.
	// +optional
	OverrideHonorLabels bool `json:"overrideHonorLabels,omitempty"`
	// OverrideHonorTimestamps allows to globally enforce honoring timestamps in all scrape configs.
	// +optional
	OverrideHonorTimestamps bool `json:"overrideHonorTimestamps,omitempty"`
	// IgnoreNamespaceSelectors if set to true will ignore NamespaceSelector settings from
	// the podmonitor and servicemonitor configs, and they will only discover endpoints
	// within their current namespace.  Defaults to false.
	// +optional
	IgnoreNamespaceSelectors bool `json:"ignoreNamespaceSelectors,omitempty"`
	// EnforcedNamespaceLabel enforces adding a namespace label of origin for each alert
	// and metric that is user created. The label value will always be the namespace of the object that is
	// being created.
	// +optional
	EnforcedNamespaceLabel string `json:"enforcedNamespaceLabel,omitempty"`
	// Name of vmAgent external label used to denote vmAgent instance
	// name. Defaults to the value of `prometheus`. External label will
	// _not_ be added when value is set to empty string (`""`).
	// +optional
	VmAgentExternalLabelName *string `json:"vmAgentExternalLabelName,omitempty"`
	// Name of vmagent external label used to denote replica name.
	// Defaults to the value of `prometheus_replica`. External label will
	// _not_ be added when value is set to empty string (`""`).
	// +optional
	ReplicaExternalLabelName *string `json:"replicaExternalLabelName,omitempty"`
	//in fact only URL is usefull, atm other params is ignored
	// The labels to add to any time series or alerts when communicating with
	// external systems (federation, remote storage, etc).
	// +optional
	ExternalLabels map[string]string `json:"externalLabels,omitempty"`
	//list of victoriametrics /some other remote write system
	//for vm it must looks like: http://victoria-metrics-single:8429/api/v1/write
	//or for cluster different url
	//https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/app/vmagent#splitting-data-streams-among-multiple-systems
	// +optional
	// +listType=set
	RemoteWrite []RemoteSpec `json:"remoteWrite"`
	// ServiceMonitors to be selected for target discovery. *Deprecated:* if
	// neither this nor podMonitorSelector are specified, configuration is
	// unmanaged.
	// +optional
	ServiceMonitorSelector *metav1.LabelSelector `json:"serviceMonitorSelector,omitempty"`
	// Namespaces to be selected for ServiceMonitor discovery. If nil, only
	// check own namespace.
	// +optional
	ServiceMonitorNamespaceSelector *metav1.LabelSelector `json:"serviceMonitorNamespaceSelector,omitempty"`
	// *Experimental* PodMonitors to be selected for target discovery.
	// *Deprecated:* if neither this nor serviceMonitorSelector are specified,
	// configuration is unmanaged.
	// +optional
	PodMonitorSelector *metav1.LabelSelector `json:"podMonitorSelector,omitempty"`
	// Namespaces to be selected for PodMonitor discovery. If nil, only
	// check own namespace.
	// +optional
	PodMonitorNamespaceSelector *metav1.LabelSelector `json:"podMonitorNamespaceSelector,omitempty"`
	// As scrape configs are appended, the user is responsible to make sure it
	// is valid. Note that using this feature may expose the possibility to
	// break upgrades of Prometheus/vmagent. It is advised to review Prometheus/vmagent release
	// notes to ensure that no incompatible scrape configs are going to break
	// Prometheus/vmagent after the upgrade.
	// +optional
	AdditionalScrapeConfigs *v1.SecretKeySelector `json:"additionalScrapeConfigs,omitempty"`
	// ArbitraryFSAccessThroughSMs configures whether configuration
	// based on a service monitor can access arbitrary files on the file system
	// of the Prometheus container e.g. bearer token files.
	// +optional
	ArbitraryFSAccessThroughSMs monitoringv1.ArbitraryFSAccessThroughSMsConfig `json:"arbitraryFSAccessThroughSMs,omitempty"`
	//listen Port for vmagent
	// +optional
	Port string `json:"port,omitempty"`
	//args for vmagent binary
	//format -flag=value for each arg
	// +optional
	// +listType=set
	ExtraArgs []string `json:"extraArgs,omitempty"`
	//env vars that will be added to vm single
	// +optional
	// +listType=set
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
}

// VmAgentStatus defines the observed state of VmAgent
// +k8s:openapi-gen=true
type VmAgentStatus struct {
	// Total number of non-terminated pods targeted by this VmAlert
	// cluster (their labels match the selector).
	Replicas int32 `json:"replicas"`
	// Total number of non-terminated pods targeted by this VmAlert
	// cluster that have the desired version spec.
	UpdatedReplicas int32 `json:"updatedReplicas"`
	// Total number of available pods (ready for at least minReadySeconds)
	// targeted by this VmAlert cluster.
	AvailableReplicas int32 `json:"availableReplicas"`
	// Total number of unavailable pods targeted by this VmAlert cluster.
	UnavailableReplicas int32 `json:"unavailableReplicas"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// VmAgent is the Schema for the vmagents API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmagents,scope=Namespaced
type VmAgent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VmAgentSpec   `json:"spec,omitempty"`
	Status VmAgentStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// VmAgentList contains a list of VmAgent
type VmAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VmAgent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VmAgent{}, &VmAgentList{})
}
