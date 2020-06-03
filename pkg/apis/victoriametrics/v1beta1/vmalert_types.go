package v1beta1

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"strings"
)

// VmAlertSpec defines the desired state of VmAlert
// +k8s:openapi-gen=true
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="The version of VmAlert"
// +kubebuilder:printcolumn:name="ReplicaCount",type="integer",JSONPath=".spec.replicas",description="The desired replicas number of VmAlerts"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VmAlertSpec struct {
	// PodMetadata configures Labels and Annotations which are propagated to the VmAlert pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// Image victoria metrics alert base image
	// +optional
	Image *string `json:"image,omitempty"`
	// Version the VmAlert should be on.
	Version string `json:"version,omitempty"`
	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling prometheus and VmAlert images from registries
	// see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod
	// +listType=set
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the VmAlert
	// object, which shall be mounted into the VmAlert Pods.
	// The Secrets are mounted into /etc/vmalert/secrets/<secret-name>.
	// +optional
	// +listType=set
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the VmAlert
	// object, which shall be mounted into the VmAlert Pods.
	// The ConfigMaps are mounted into /etc/vmalert/configmaps/<configmap-name>.
	// +optional
	// +listType=set
	ConfigMaps []string `json:"configMaps,omitempty"`
	// LogFormat for VmAlert to be configured with.
	//default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VmAlert to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// ReplicaCount is the expected size of the VmAlert cluster. The controller will
	// eventually make the size of the running cluster equal to the expected
	// size.
	// +optional
	ReplicaCount *int32 `json:"replicaCount,omitempty"`
	// Volumes allows configuration of additional volumes on the output Deployment definition.
	// Volumes specified will be appended to other volumes that are generated as a result of
	// StorageSpec objects.
	// +optional
	// +listType=set
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output Deployment definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the VmAlert container,
	// that are generated as a result of StorageSpec objects.
	// +optional
	// +listType=set
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`
	// Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +optional
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// Affinity If specified, the pod's scheduling constraints.
	// +optional
	Affinity *v1.Affinity `json:"affinity,omitempty"`
	// Tolerations If specified, the pod's tolerations.
	// +optional
	// +listType=set
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
	// SecurityContext holds pod-level security attributes and common container settings.
	// This defaults to the default PodSecurityContext.
	// +optional
	SecurityContext *v1.PodSecurityContext `json:"securityContext,omitempty"`
	// ServiceAccountName is the name of the ServiceAccount to use to run the
	// VmAlert Pods.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// Containers property allows to inject additions sidecars. It can be useful for proxies, backup, etc.
	// +optional
	// +listType=set
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition. Those can be used to e.g.
	// fetch secrets for injection into the VmAlert configuration from external sources. Any
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

	// EvaluationInterval how often evalute rules by default
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	EvaluationInterval string `json:"evaluationInterval,omitempty"`
	// EnforcedNamespaceLabel enforces adding a namespace label of origin for each alert
	// and metric that is user created. The label value will always be the namespace of the object that is
	// being created.
	// +optional
	EnforcedNamespaceLabel string `json:"enforcedNamespaceLabel,omitempty"`
	// RuleSelector selector to select which PrometheusRules to mount for loading alerting
	// rules from.
	// +optional
	RuleSelector *metav1.LabelSelector `json:"ruleSelector,omitempty"`
	// RuleNamespaceSelector to be selected for PrometheusRules discovery. If unspecified, only
	// the same namespace as the Prometheus object is in is used.
	// +optional
	RuleNamespaceSelector *metav1.LabelSelector `json:"ruleNamespaceSelector,omitempty"`

	// Port for listen
	// +optional
	Port string `json:"port,omitempty"`

	// NotifierURL prometheus alertmanager URL. Required parameter. e.g. http://127.0.0.1:9093
	NotifierURL string `json:"notifierURL"`

	// RemoteWrite Optional URL to remote-write compatible storage where to write timeseriesbased on active alerts. E.g. http://127.0.0.1:8428
	// +optional
	RemoteWrite RemoteSpec `json:"remoteWrite,omitempty"`

	// RemoteRead victoria metrics address for loading state
	// This configuration makes sense only if remoteWrite was configured before and has
	// been successfully persisted its state.
	// +optional
	RemoteRead RemoteSpec `json:"remoteRead,omitempty"`

	// RulePath to the file with alert rules.
	// Supports patterns. Flag can be specified multiple times.
	// Examples:
	//         -rule /path/to/file. Path to a single file with alerting rules
	//         -rule dir/*.yaml -rule /*.yaml. Relative path to all .yaml files in "dir" folder,
	//        absolute path to all .yaml files in root.
	// by default operator adds /etc/vmalert/configs/base/vmalert.yaml
	// +listType=set
	// +optional
	RulePath []string `json:"rulePath,omitempty"`
	// Datasource Victoria Metrics or VMSelect url. Required parameter. e.g. http://127.0.0.1:8428
	Datasource RemoteSpec `json:"datasource"`

	// ExtraArgs that will be passed to  VmAlert pod
	// for example -remoteWrite.tmpDataPath=/tmp
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// ExtraEnvs that will be added to VmAlert pod
	// +optional
	// +listType=set
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
}

// VmAlertStatus defines the observed state of VmAlert
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type VmAlertStatus struct {
	// ReplicaCount Total number of non-terminated pods targeted by this VmAlert
	// cluster (their labels match the selector).
	Replicas int32 `json:"replicas"`
	// UpdatedReplicas Total number of non-terminated pods targeted by this VmAlert
	// cluster that have the desired version spec.
	UpdatedReplicas int32 `json:"updatedReplicas"`
	// AvailableReplicas Total number of available pods (ready for at least minReadySeconds)
	// targeted by this VmAlert cluster.
	AvailableReplicas int32 `json:"availableReplicas"`
	// UnavailableReplicas Total number of unavailable pods targeted by this VmAlert cluster.
	UnavailableReplicas int32 `json:"unavailableReplicas"`
}

// VmAlert is the Schema for the vmalerts API
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmalerts,scope=Namespaced
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

func (cr VmAlert) Name() string {
	return cr.ObjectMeta.Name
}

func (cr *VmAlert) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         cr.APIVersion,
			Kind:               cr.Kind,
			Name:               cr.Name(),
			UID:                cr.UID,
			Controller:         pointer.BoolPtr(true),
			BlockOwnerDeletion: pointer.BoolPtr(true),
		},
	}
}
func (cr VmAlert) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr VmAlert) Annotations() map[string]string {
	annotations := make(map[string]string)
	for annotation, value := range cr.ObjectMeta.Annotations {
		if !strings.HasPrefix(annotation, "kubectl.kubernetes.io/") {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr VmAlert) CommonLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmalert",
		"app.kubernetes.io/instance":  cr.Name(),
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VmAlert) PodLabels() map[string]string {
	labels := cr.CommonLabels()
	if cr.Spec.PodMetadata != nil {
		for label, value := range cr.Spec.PodMetadata.Labels {
			labels[label] = value
		}
	}
	return labels
}

func (cr VmAlert) FinalLabels() map[string]string {
	labels := cr.CommonLabels()
	if cr.ObjectMeta.Labels != nil {
		for label, value := range cr.ObjectMeta.Labels {
			labels[label] = value
		}
	}
	return labels
}

func (cr VmAlert) PrefixedName() string {
	return fmt.Sprintf("vmalert-%s", cr.Name())
}

func init() {
	SchemeBuilder.Register(&VmAlert{}, &VmAlertList{})
}
