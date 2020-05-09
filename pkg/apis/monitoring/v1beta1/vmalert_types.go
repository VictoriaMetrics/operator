package v1beta1

import (
	monitoringv1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VmAlertSpec defines the desired state of VmAlert
// +k8s:openapi-gen=true
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="The version of VmAlert"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas",description="The desired replicas number of VmAlerts"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VmAlertSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	// PodMetadata configures Labels and Annotations which are propagated to the VmAlert pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// Image if specified has precedence over baseImage and version
	// combinations.
	Image *string `json:"image,omitempty"`
	// Version the cluster should be on.
	Version string `json:"version,omitempty"`
	// An optional list of references to secrets in the same namespace
	// to use for pulling prometheus and VmAlert images from registries
	// see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod
	// +listType=set
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the VmAlert
	// object, which shall be mounted into the VmAlert Pods.
	// The Secrets are mounted into /etc/vmalert/secrets/<secret-name>.
	// +listType=set
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the VmAlert
	// object, which shall be mounted into the VmAlert Pods.
	// The ConfigMaps are mounted into /etc/vmalert/configmaps/<configmap-name>.
	// +listType=set
	ConfigMaps []string `json:"configMaps,omitempty"`
	// ConfigSecret is the name of a Kubernetes Secret in the same namespace as the
	// VmAlert object, which contains configuration for this VmAlert
	// instance. Defaults to 'VmAlert-<VmAlert-name>'
	// The secret is mounted into /etc/VmAlert/config.
	ConfigSecret string `json:"configSecret,omitempty"`
	// Log format for VmAlert to be configured with.
	LogFormat string `json:"logFormat,omitempty"`
	// Log level for VmAlert to be configured with.
	LogLevel string `json:"logLevel,omitempty"`
	// Size is the expected size of the VmAlert cluster. The controller will
	// eventually make the size of the running cluster equal to the expected
	// size.
	// +kubebuilder:validation:Required
	Replicas *int32 `json:"replicas"`
	// Volumes allows configuration of additional volumes on the output Deployment definition.
	// Volumes specified will be appended to other volumes that are generated as a result of
	// StorageSpec objects.
	// +listType=set
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output Deployment definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the VmAlert container,
	// that are generated as a result of StorageSpec objects.
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
	// vmAlert Pods.
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// Containers allows injecting additional containers. This is meant to
	// allow adding an authentication proxy to an VmAlert pod.
	// +listType=set
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition. Those can be used to e.g.
	// fetch secrets for injection into the VmAlert configuration from external sources. Any
	// errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// Using initContainers for any use case other then secret fetching is entirely outside the scope
	// of what the maintainers will support and by doing so, you accept that this behaviour may break
	// at any time without notice.
	// +listType=set
	InitContainers []v1.Container `json:"initContainers,omitempty"`

	// Priority class assigned to the Pods
	PriorityClassName string `json:"priorityClassName,omitempty"`

	//how often evalute rules
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	EvaluationInterval string `json:"evaluationInterval,omitempty"`
	// https://prometheus.io/docs/prometheus/latest/configuration/configuration/#alert_relabel_configs.
	// As alert relabel configs are appended, the user is responsible to make sure it
	// is valid. Note that using this feature may expose the possibility to
	// break upgrades of Prometheus. It is advised to review Prometheus release
	// notes to ensure that no incompatible alert relabel configs are going to break
	// Prometheus after the upgrade.
	AdditionalAlertRelabelConfigs *v1.SecretKeySelector `json:"additionalAlertRelabelConfigs,omitempty"`
	// AdditionalVmAlertConfigs allows specifying a key of a Secret containing
	// additional Prometheus VmAlert configurations. VmAlert configurations
	// specified are appended to the configurations generated by the Prometheus
	// Operator. Job configurations specified must have the form as specified
	// in the official Prometheus documentation:
	// https://prometheus.io/docs/prometheus/latest/configuration/configuration.
	// As VmAlert configs are appended, the user is responsible to make sure it
	// is valid. Note that using this feature may expose the possibility to
	// break upgrades of Prometheus. It is advised to review Prometheus release
	// notes to ensure that no incompatible VmAlert configs are going to break
	// Prometheus after the upgrade.
	AdditionalVmAlertConfigs *v1.SecretKeySelector `json:"additionalVmAlertConfigs,omitempty"`
	// IgnoreNamespaceSelectors if set to true will ignore NamespaceSelector settings from
	// the podmonitor and servicemonitor configs, and they will only discover endpoints
	// within their current namespace.  Defaults to false.
	IgnoreNamespaceSelectors bool `json:"ignoreNamespaceSelectors,omitempty"`
	// EnforcedNamespaceLabel enforces adding a namespace label of origin for each alert
	// and metric that is user created. The label value will always be the namespace of the object that is
	// being created.
	EnforcedNamespaceLabel string `json:"enforcedNamespaceLabel,omitempty"`
	// A selector to select which PrometheusRules to mount for loading alerting/recording
	// rules from. Until (excluding) Prometheus Operator v0.24.0 Prometheus
	// Operator will migrate any legacy rule ConfigMaps to PrometheusRule custom
	// resources selected by RuleSelector. Make sure it does not match any config
	// maps that you do not want to be migrated.
	RuleSelector *metav1.LabelSelector `json:"ruleSelector,omitempty"`
	// Namespaces to be selected for PrometheusRules discovery. If unspecified, only
	// the same namespace as the Prometheus object is in is used.
	RuleNamespaceSelector *metav1.LabelSelector `json:"ruleNamespaceSelector,omitempty"`

	//SPECIFIC FOR VMALERT SETTINGS

	//port for listen addr
	Port string `json:"port,omitempty"`

	// Address to listen for http connections (default ":8880")
	ListenAddr string `json:"listenAddr,omitempty"`

	// Prometheus alertmanager URL. Required parameter. e.g. http://127.0.0.1:9093
	// +kubebuilder:validation:Required
	NotifierURL string `json:"notifierURL"`
	// Optional URL to remote-write compatible storage where to write timeseriesbased on active alerts. E.g. http://127.0.0.1:8428
	RemoteWrite monitoringv1.RemoteWriteSpec `json:"remoteWrite,omitempty"`
	//Path to the file with alert rules.
	//        Supports patterns. Flag can be specified multiple times.
	//        Examples:
	//         -rule /path/to/file. Path to a single file with alerting rules
	//         -rule dir/*.yaml -rule /*.yaml. Relative path to all .yaml files in "dir" folder,
	//        absolute path to all .yaml files in root.
	//by default operator adds /etc/vmalert/configs/base/vmalert.yaml
	// +listType=set
	RulePath []string `json:"rulePath,omitempty"`
	// Victoria Metrics or VMSelect url. Required parameter. e.g. http://127.0.0.1:8428
	DataSource string `json:"dataSource"`
	// +listType=set
	ExtraArgs []string `json:"extraArgs,omitempty"`
	//env vars that will be added to vm single
	// +listType=set
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
}

// VmAlertStatus defines the observed state of VmAlert
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type VmAlertStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
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

// VmAlert is the Schema for the vmalerts API
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmalerts,scope=Namespaced
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.spec.selector
type VmAlert struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VmAlertSpec   `json:"spec,omitempty"`
	Status VmAlertStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VmAlertList contains a list of VmAlert
type VmAlertList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VmAlert `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VmAlert{}, &VmAlertList{})
}
