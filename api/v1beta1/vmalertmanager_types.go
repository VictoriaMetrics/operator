package v1beta1

import (
	"fmt"
	"strings"

	"github.com/VictoriaMetrics/operator/controllers/factory/crd"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/pointer"
)

// VMAlertmanager represents Victoria-Metrics deployment for Alertmanager.
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMAlertmanager App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="StatefulSet,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="The version of VMAlertmanager"
// +kubebuilder:printcolumn:name="ReplicaCount",type="integer",JSONPath=".spec.ReplicaCount",description="The desired replicas number of Alertmanagers"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:path=vmalertmanagers,scope=Namespaced,shortName=vma,singular=vmalertmanager
type VMAlertmanager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Specification of the desired behavior of the VMAlertmanager cluster. More info:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec VMAlertmanagerSpec `json:"spec"`
	// Most recent observed status of the VMAlertmanager cluster.
	// Operator API itself. More info:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Status *VMAlertmanagerStatus `json:"status,omitempty"`
}

// VMAlertmanagerSpec is a specification of the desired behavior of the VMAlertmanager cluster. More info:
// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
// +k8s:openapi-gen=true
type VMAlertmanagerSpec struct {
	// PodMetadata configures Labels and Annotations which are propagated to the alertmanager pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`

	// Image - docker image settings for VMAlertmanager
	// if no specified operator uses default config version
	// +optional
	Image Image `json:"image,omitempty"`

	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the VMAlertmanager
	// object, which shall be mounted into the VMAlertmanager Pods.
	// The Secrets are mounted into /etc/alertmanager/secrets/<secret-name>
	// +optional
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the VMAlertmanager
	// object, which shall be mounted into the VMAlertmanager Pods.
	// The ConfigMaps are mounted into /etc/alertmanager/configmaps/<configmap-name>.
	// +optional
	ConfigMaps []string `json:"configMaps,omitempty"`

	// ConfigRawYaml - raw configuration for alertmanager,
	// it helps it to start without secret.
	// priority -> hardcoded ConfigRaw -> ConfigRaw, provided by user -> ConfigSecret.
	// +optional
	ConfigRawYaml string `json:"configRawYaml,omitempty"`
	// ConfigSecret is the name of a Kubernetes Secret in the same namespace as the
	// VMAlertmanager object, which contains configuration for this VMAlertmanager,
	// configuration must be inside secret key: alertmanager.yaml.
	// It must be created by user.
	// instance. Defaults to 'vmalertmanager-<alertmanager-name>'
	// The secret is mounted into /etc/alertmanager/config.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Secret with alertmanager config",xDescriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ConfigSecret string `json:"configSecret,omitempty"`
	// Log level for VMAlertmanager to be configured with.
	// +optional
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VMAlertmanager to be configured with.
	// +optional
	LogFormat string `json:"logFormat,omitempty"`
	// ReplicaCount Size is the expected size of the alertmanager cluster. The controller will
	// eventually make the size of the running cluster equal to the expected
	// +kubebuilder:validation:Minimum:=1
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Number of pods",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount,urn:alm:descriptor:io.kubernetes:custom"
	ReplicaCount *int32 `json:"replicaCount,omitempty"`
	// Retention Time duration VMAlertmanager shall retain data for. Default is '120h',
	// and must match the regular expression `[0-9]+(ms|s|m|h)` (milliseconds seconds minutes hours).
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	// +optional
	Retention string `json:"retention,omitempty"`
	// Storage is the definition of how storage will be used by the VMAlertmanager
	// instances.
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`
	// Volumes allows configuration of additional volumes on the output StatefulSet definition.
	// Volumes specified will be appended to other volumes that are generated as a result of
	// StorageSpec objects.
	// +optional
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output StatefulSet definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the alertmanager container,
	// that are generated as a result of StorageSpec objects.
	// +optional
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`
	// ExternalURL the VMAlertmanager instances will be available under. This is
	// necessary to generate correct URLs. This is necessary if VMAlertmanager is not
	// served from root of a DNS name.
	// +optional
	ExternalURL string `json:"externalURL,omitempty"`
	// RoutePrefix VMAlertmanager registers HTTP handlers for. This is useful,
	// if using ExternalURL and a proxy is rewriting HTTP routes of a request,
	// and the actual ExternalURL is still true, but the server serves requests
	// under a different route prefix. For example for use with `kubectl proxy`.
	// +optional
	RoutePrefix string `json:"routePrefix,omitempty"`
	// Paused If set to true all actions on the underlaying managed objects are not
	// goint to be performed, except for delete actions.
	// +optional
	Paused bool `json:"paused,omitempty"`
	// NodeSelector Define which Nodes the Pods are scheduled on.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Resources container resource request and limits,
	// https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
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
	// ServiceAccountName is the name of the ServiceAccount to use
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ServiceAccount name",xDescriptors="urn:alm:descriptor:io.kubernetes:ServiceAccount"
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// SchedulerName - defines kubernetes scheduler name
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`
	// RuntimeClassName - defines runtime class for kubernetes pod.
	//https://kubernetes.io/docs/concepts/containers/runtime-class/
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`
	// PodSecurityPolicyName - defines name for podSecurityPolicy
	// in case of empty value, prefixedName will be used.
	// +optional
	PodSecurityPolicyName string `json:"podSecurityPolicyName,omitempty"`
	// ListenLocal makes the VMAlertmanager server listen on loopback, so that it
	// does not bind against the Pod IP. Note this is only for the VMAlertmanager
	// UI, not the gossip communication.
	// +optional
	ListenLocal bool `json:"listenLocal,omitempty"`
	// Containers allows injecting additional containers or patching existing containers.
	// This is meant to allow adding an authentication proxy to an VMAlertmanager pod.
	// +optional
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition. Those can be used to e.g.
	// fetch secrets for injection into the VMAlertmanager configuration from external sources. Any
	// errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// Using initContainers for any use case other then secret fetching is entirely outside the scope
	// of what the maintainers will support and by doing so, you accept that this behaviour may break
	// at any time without notice.
	// +optional
	InitContainers []v1.Container `json:"initContainers,omitempty"`
	// PriorityClassName class assigned to the Pods
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

	// AdditionalPeers allows injecting a set of additional Alertmanagers to peer with to form a highly available cluster.
	AdditionalPeers []string `json:"additionalPeers,omitempty"`
	// ClusterAdvertiseAddress is the explicit address to advertise in cluster.
	// Needs to be provided for non RFC1918 [1] (public) addresses.
	// [1] RFC1918: https://tools.ietf.org/html/rfc1918
	// +optional
	ClusterAdvertiseAddress string `json:"clusterAdvertiseAddress,omitempty"`
	// PortName used for the pods and governing service.
	// This defaults to web
	// +optional
	PortName string `json:"portName,omitempty"`
	// ServiceSpec that will be added to vmalertmanager service spec
	// +optional
	ServiceSpec *ServiceSpec `json:"serviceSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*EmbeddedProbes     `json:",inline"`
	// ConfigSelector defines selector for VMAlertmanagerConfig, result config will be merged with with Raw or Secret config.
	// Works in combination with NamespaceSelector.
	// If both nil - match everything.
	// NamespaceSelector nil - only objects at VMAlertmanager namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// +optional
	ConfigSelector *metav1.LabelSelector `json:"configSelector,omitempty"`
	//  ConfigNamespaceSelector defines namespace selector for VMAlertmanagerConfig.
	// Works in combination with Selector.
	// If both nil - match everything.
	// NamespaceSelector nil - only objects at VMAlertmanager namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// +optional
	ConfigNamespaceSelector *metav1.LabelSelector `json:"configNamespaceSelector,omitempty"`
	// ExtraArgs that will be passed to  VMAuth pod
	// for example remoteWrite.tmpDataPath: /tmp
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// ExtraEnvs that will be added to VMAuth pod
	// +optional
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
}

// VMAlertmanagerList is a list of Alertmanagers.
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VMAlertmanagerList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of Alertmanagers
	Items []VMAlertmanager `json:"items"`
}

// VMAlertmanagerStatus is the most recent observed status of the VMAlertmanager cluster
// Operator API itself. More info:
// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
// +k8s:openapi-gen=true
type VMAlertmanagerStatus struct {
	// Paused Represents whether any actions on the underlaying managed objects are
	// being performed. Only delete actions will be performed.
	Paused bool `json:"paused"`
	// ReplicaCount Total number of non-terminated pods targeted by this VMAlertmanager
	// cluster (their labels match the selector).
	Replicas int32 `json:"replicas"`
	// UpdatedReplicas Total number of non-terminated pods targeted by this VMAlertmanager
	// cluster that have the desired version spec.
	UpdatedReplicas int32 `json:"updatedReplicas"`
	// AvailableReplicas Total number of available pods (ready for at least minReadySeconds)
	// targeted by this VMAlertmanager cluster.
	AvailableReplicas int32 `json:"availableReplicas"`
	// UnavailableReplicas Total number of unavailable pods targeted by this VMAlertmanager cluster.
	UnavailableReplicas int32 `json:"unavailableReplicas"`
}

func (cr *VMAlertmanager) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         cr.APIVersion,
			Kind:               cr.Kind,
			Name:               cr.Name,
			UID:                cr.UID,
			Controller:         pointer.BoolPtr(true),
			BlockOwnerDeletion: pointer.BoolPtr(true),
		},
	}
}

func (cr VMAlertmanager) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr VMAlertmanager) Annotations() map[string]string {
	annotations := make(map[string]string)
	for annotation, value := range cr.ObjectMeta.Annotations {
		if !strings.HasPrefix(annotation, "kubectl.kubernetes.io/") {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr VMAlertmanager) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmalertmanager",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VMAlertmanager) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)

}

func (cr VMAlertmanager) Labels() map[string]string {

	lbls := cr.SelectorLabels()
	if cr.ObjectMeta.Labels == nil {
		return lbls
	}
	return labels.Merge(cr.ObjectMeta.Labels, lbls)
}

func (cr VMAlertmanager) PrefixedName() string {
	return fmt.Sprintf("vmalertmanager-%s", cr.Name)
}

func (cr VMAlertmanager) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr VMAlertmanager) GetPSPName() string {
	if cr.Spec.PodSecurityPolicyName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.PodSecurityPolicyName
}

func (cr VMAlertmanager) GetNSName() string {
	return cr.GetNamespace()
}

func (cr *VMAlertmanager) AsURL() string {
	return fmt.Sprintf("http://%s.%s.svc:9093", cr.PrefixedName(), cr.Namespace)
}

func (cr *VMAlertmanager) AsPodFQDN(idx int) string {
	return fmt.Sprintf("http://%s-%d.%s.%s.svc:9093", cr.PrefixedName(), idx, cr.PrefixedName(), cr.Namespace)
}

// AsCRDOwner implements interface
func (cr *VMAlertmanager) AsCRDOwner() []metav1.OwnerReference {
	return crd.GetCRDAsOwner(crd.VMAlertManager)
}

// AsNotifiers converts VMAlertmanager into VMAlertNotifierSpec
func (cr *VMAlertmanager) AsNotifiers() []VMAlertNotifierSpec {
	var r []VMAlertNotifierSpec
	replicaCount := 1
	if cr.Spec.ReplicaCount != nil {
		replicaCount = int(*cr.Spec.ReplicaCount)
	}
	for i := 0; i < replicaCount; i++ {
		ns := VMAlertNotifierSpec{
			URL: cr.AsPodFQDN(i),
		}
		r = append(r, ns)
	}
	return r
}

func (cr *VMAlertmanager) GetVolumeName() string {
	if cr.Spec.Storage != nil && cr.Spec.Storage.VolumeClaimTemplate.Name != "" {
		return cr.Spec.Storage.VolumeClaimTemplate.Name
	}
	return fmt.Sprintf("vmalertmanager-%s-db", cr.Name)
}

func init() {
	SchemeBuilder.Register(&VMAlertmanager{}, &VMAlertmanagerList{})
}
