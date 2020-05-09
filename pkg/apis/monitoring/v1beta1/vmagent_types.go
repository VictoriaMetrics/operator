package v1beta1

import (
	monitoringv1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VmAgentSpec defines the desired state of VmAgent
// +k8s:openapi-gen=true
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="The version of VmAlert"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas",description="The desired replicas number of VmAlerts"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VmAgentSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	// PodMetadata configures Labels and Annotations which are propagated to the vmagent pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	//base image, if not specified - use default from operator config
	Image *string `json:"image,omitempty"`
	// Version/tag.
	//+optional
	Version string `json:"version,omitempty"`
	// An optional list of references to secrets in the same namespace
	// to use for pulling prometheus and vmagent images from registries
	// see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod
	// +listType=set
	//+optional
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the vmagent
	// object, which shall be mounted into the vmagent Pods.
	//+optional
	// +listType=set
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the vmagent
	// object, which shall be mounted into the vmagent Pods.
	//+optional
	// +listType=set
	ConfigMaps []string `json:"configMaps,omitempty"`
	// Log level for vmagent to be configured with.
	//+optional
	//INFO, WARN, ERROR, FATAL, PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// Log format for vmagent to be configured with.
	//+optional
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
	//+optional
	// +listType=set
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output deploy definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the vmagent container,
	// that are generated as a result of StorageSpec objects.
	//+optional
	// +listType=set
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`
	// Define resources requests and limits for single Pods.
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// If specified, the pod's scheduling constraints.
	Affinity *v1.Affinity `json:"affinity,omitempty"`
	// If specified, the pod's tolerations.
	// +listType=set
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
	// SecurityContext holds pod-level security attributes and common container settings.
	// This defaults to the default PodSecurityContext.
	SecurityContext *v1.PodSecurityContext `json:"securityContext,omitempty"`
	// ServiceAccountName is the name of the ServiceAccount to use to run the
	// vmagent Pods.
	// required
	ServiceAccountName string `json:"serviceAccountName"`
	// Containers allows injecting additional containers. This is meant to
	// allow adding an authentication proxy to an vmagent pod.
	// +listType=set
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition. Those can be used to e.g.
	// fetch secrets for injection into the vmagent configuration from external sources. Any
	// errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// Using initContainers for any use case other then secret fetching is entirely outside the scope
	// of what the maintainers will support and by doing so, you accept that this behaviour may break
	// at any time without notice.
	// +listType=set
	InitContainers []v1.Container `json:"initContainers,omitempty"`
	// Priority class assigned to the Pods
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// Port name used for the pods and governing service.
	// This defaults to web
	PortName string `json:"portName,omitempty"`
	// how often scrape by default
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	ScrapeInterval string `json:"scrapeInterval,omitempty"`

	// APIServerConfig allows specifying a host and auth methods to access apiserver.
	// If left empty, vmagent is assumed to run inside of the cluster
	// and will discover API servers automatically and use the pod's CA certificate
	// and bearer token file at /var/run/secrets/kubernetes.io/serviceaccount/.
	APIServerConfig *monitoringv1.APIServerConfig `json:"aPIServerConfig,omitempty"`
	// OverrideHonorLabels if set to true overrides all user configured honor_labels.
	// If HonorLabels is set in ServiceMonitor or PodMonitor to true, this overrides honor_labels to false.
	OverrideHonorLabels bool `json:"overrideHonorLabels,omitempty"`
	// OverrideHonorTimestamps allows to globally enforce honoring timestamps in all scrape configs.
	OverrideHonorTimestamps bool `json:"overrideHonorTimestamps,omitempty"`
	// IgnoreNamespaceSelectors if set to true will ignore NamespaceSelector settings from
	// the podmonitor and servicemonitor configs, and they will only discover endpoints
	// within their current namespace.  Defaults to false.
	IgnoreNamespaceSelectors bool `json:"ignoreNamespaceSelectors,omitempty"`
	// EnforcedNamespaceLabel enforces adding a namespace label of origin for each alert
	// and metric that is user created. The label value will always be the namespace of the object that is
	// being created.
	EnforcedNamespaceLabel string `json:"enforcedNamespaceLabel,omitempty"`
	// Name of Prometheus external label used to denote Prometheus instance
	// name. Defaults to the value of `prometheus`. External label will
	// _not_ be added when value is set to empty string (`""`).
	PrometheusExternalLabelName *string `json:"prometheusExternalLabelName,omitempty"`
	// Name of Prometheus external label used to denote replica name.
	// Defaults to the value of `prometheus_replica`. External label will
	// _not_ be added when value is set to empty string (`""`).
	ReplicaExternalLabelName *string `json:"replicaExternalLabelName,omitempty"`
	//in fact only URL is usefull, atm other params is ignored
	// The labels to add to any time series or alerts when communicating with
	// external systems (federation, remote storage, etc).
	ExternalLabels map[string]string `json:"externalLabels,omitempty"`
	// +listType=set
	//list of victoriametrics /some other remote write system
	//for vm it must looks like: http://victoria-metrics-single:8429/api/v1/write
	//or for cluster different url
	//https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/app/vmagent#splitting-data-streams-among-multiple-systems
	RemoteWrite []monitoringv1.RemoteWriteSpec `json:"remoteWrite"`
	// ServiceMonitors to be selected for target discovery. *Deprecated:* if
	// neither this nor podMonitorSelector are specified, configuration is
	// unmanaged.
	ServiceMonitorSelector *metav1.LabelSelector `json:"serviceMonitorSelector,omitempty"`
	// Namespaces to be selected for ServiceMonitor discovery. If nil, only
	// check own namespace.
	ServiceMonitorNamespaceSelector *metav1.LabelSelector `json:"serviceMonitorNamespaceSelector,omitempty"`
	// *Experimental* PodMonitors to be selected for target discovery.
	// *Deprecated:* if neither this nor serviceMonitorSelector are specified,
	// configuration is unmanaged.
	PodMonitorSelector *metav1.LabelSelector `json:"podMonitorSelector,omitempty"`
	// Namespaces to be selected for PodMonitor discovery. If nil, only
	// check own namespace.
	PodMonitorNamespaceSelector *metav1.LabelSelector `json:"podMonitorNamespaceSelector,omitempty"`
	// As scrape configs are appended, the user is responsible to make sure it
	// is valid. Note that using this feature may expose the possibility to
	// break upgrades of Prometheus/vmagent. It is advised to review Prometheus/vmagent release
	// notes to ensure that no incompatible scrape configs are going to break
	// Prometheus/vmagent after the upgrade.
	AdditionalScrapeConfigs *v1.SecretKeySelector `json:"additionalScrapeConfigs,omitempty"`
	// ArbitraryFSAccessThroughSMs configures whether configuration
	// based on a service monitor can access arbitrary files on the file system
	// of the Prometheus container e.g. bearer token files.
	ArbitraryFSAccessThroughSMs ArbitraryFSAccessThroughSMsConfig `json:"arbitraryFSAccessThroughSMs,omitempty"`
	//listen Port for vmagent
	Port string `json:"port,omitempty"`
	//args for vmagent binary
	// +listType=set
	ExtraArgs []string `json:"extraArgs,omitempty"`
	//env vars that will be added to vm single
	// +listType=set
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
}

// VmAgentStatus defines the observed state of VmAgent
// +k8s:openapi-gen=true
type VmAgentStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
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

// EmbeddedObjectMetadata contains a subset of the fields included in k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta
// Only fields which are relevant to embedded resources are included.
type EmbeddedObjectMetadata struct {
	// Name must be unique within a namespace. Is required when creating resources, although
	// some resources may allow a client to request the generation of an appropriate name
	// automatically. Name is primarily intended for creation idempotence and configuration
	// definition.
	// Cannot be updated.
	// More info: http://kubernetes.io/docs/user-guide/identifiers#names
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: http://kubernetes.io/docs/user-guide/labels
	// +optional
	Labels map[string]string `json:"labels,omitempty" protobuf:"bytes,11,rep,name=labels"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: http://kubernetes.io/docs/user-guide/annotations
	// +optional
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,12,rep,name=annotations"`
}


// EmbeddedPersistentVolumeClaim is an embedded version of k8s.io/api/core/v1.PersistentVolumeClaim.
// It contains TypeMeta and a reduced ObjectMeta.
type EmbeddedPersistentVolumeClaim struct {
	metav1.TypeMeta `json:",inline"`

	// EmbeddedMetadata contains metadata relevant to an EmbeddedResource.
	EmbeddedObjectMetadata `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec defines the desired characteristics of a volume requested by a pod author.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	// +optional
	Spec v1.PersistentVolumeClaimSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current information/status of a persistent volume claim.
	// Read-only.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	// +optional
	Status v1.PersistentVolumeClaimStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// SecretOrConfigMap allows to specify data as a Secret or ConfigMap. Fields are mutually exclusive.
type SecretOrConfigMap struct {
	// Secret containing data to use for the targets.
	Secret *v1.SecretKeySelector `json:"secret,omitempty"`
	// ConfigMap containing data to use for the targets.
	ConfigMap *v1.ConfigMapKeySelector `json:"configMap,omitempty"`
}

// SecretOrConfigMapValidationError is returned by SecretOrConfigMap.Validate()
// on semantically invalid configurations.
// +k8s:openapi-gen=false
type SecretOrConfigMapValidationError struct {
	err string
}

func (e *SecretOrConfigMapValidationError) Error() string {
	return e.err
}

// Validate semantically validates the given TLSConfig.
func (c *SecretOrConfigMap) Validate() error {
	if &c.Secret != nil && &c.ConfigMap != nil {
		return &SecretOrConfigMapValidationError{"SecretOrConfigMap can not specify both Secret and ConfigMap"}
	}

	return nil
}


// ArbitraryFSAccessThroughSMsConfig enables users to configure, whether
// a service monitor selected by the Prometheus instance is allowed to use
// arbitrary files on the file system of the Prometheus container. This is the case
// when e.g. a service monitor specifies a BearerTokenFile in an endpoint. A
// malicious user could create a service monitor selecting arbitrary secret files
// in the Prometheus container. Those secrets would then be sent with a scrape
// request by Prometheus to a malicious target. Denying the above would prevent the
// attack, users can instead use the BearerTokenSecret field.
type ArbitraryFSAccessThroughSMsConfig struct {
	Deny bool `json:"deny,omitempty"`
}

func init() {
	SchemeBuilder.Register(&VmAgent{}, &VmAgentList{})
}
