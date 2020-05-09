package v1beta1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VmSingleSpec defines the desired state of VmSingle
// +k8s:openapi-gen=true
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="The version of VmSingle"
// +kubebuilder:printcolumn:name="RetentionPeriod",type="string",JSONPath=".spec.RetentionPeriod",description="The desired RetentionPeriod for vm single"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VmSingleSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	// PodMetadata configures Labels and Annotations which are propagated to the VmSingle pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// base image for vmSingle
	// configured.
	Image *string `json:"image,omitempty"`
	// Version the cluster should be on.
	Version string `json:"version,omitempty"`
	// An optional list of references to secrets in the same namespace
	// to use for pulling prometheus and VmSingle images from registries
	// see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod
	// +listType=set
	//+optional
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the VmSingle
	// object, which shall be mounted into the VmSingle Pods.
	//+optional
	// +listType=set
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the vmsingle
	// object, which shall be mounted into the vmsingle Pods.
	//+optional
	// +listType=set
	ConfigMaps []string `json:"configMaps,omitempty"`
	// Log level for VmSingle to be configured with.
	//+optional
	LogLevel string `json:"logLevel,omitempty"`
	// Log format for VmSingle to be configured with.
	//+optional
	LogFormat string `json:"logFormat,omitempty"`
	// Size is the expected size of the vmsingle
	//it can be 0 or 1
	//if you need more - use vm cluster
	Replicas *int32 `json:"replicas,omitempty"`

	// Storage is the definition of how storage will be used by the vm single
	//can be empty for emptyDir
	// instances.
	Storage *v1.PersistentVolumeClaimSpec `json:"storage,omitempty"`

	// Volumes allows configuration of additional volumes on the output deploy definition.
	// Volumes specified will be appended to other volumes that are generated as a result of
	// StorageSpec objects.
	//+optional
	// +listType=set
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output Deployment definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the vmsignle container,
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
	// Prometheus Pods.
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// Containers allows injecting additional containers. This is meant to
	// allow adding an authentication proxy to an vmSingle pod.
	// +listType=set
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition. Those can be used to e.g.
	// fetch secrets for injection into the vmSingle configuration from external sources. Any
	// errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// Using initContainers for any use case other then secret fetching is entirely outside the scope
	// of what the maintainers will support and by doing so, you accept that this behaviour may break
	// at any time without notice.
	// +listType=set
	InitContainers []v1.Container `json:"initContainers,omitempty"`
	// Priority class assigned to the Pods
	PriorityClassName string `json:"priorityClassName,omitempty"`

	//listen Port for vmagent
	Port string `json:"port,omitempty"`

	//Retention in months
	// +kubebuilder:validation:Pattern:="[1-9]+"
	RetentionPeriod string `json:"retentionPeriod"`
	//args that will be passed to vmsingle binary
	//-remoteWrite.tmpDataPath=/tmp,-remoteWrite.maxBlockSize=33554432
	//and other
	// +listType=set
	ExtraArgs []string `json:"extraArgs,omitempty"`
	//env vars that will be added to vm single
	// +listType=set
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
}

// VmSingleStatus defines the observed state of VmSingle
// +k8s:openapi-gen=true
type VmSingleStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VmSingle is the Schema for the vmsingles API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmsingles,scope=Namespaced
type VmSingle struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VmSingleSpec   `json:"spec,omitempty"`
	Status VmSingleStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VmSingleList contains a list of VmSingle
type VmSingleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VmSingle `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VmSingle{}, &VmSingleList{})
}
