package v1beta1

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/pointer"
)

// VMAuthSpec defines the desired state of VMAuth
type VMAuthSpec struct {

	// PodMetadata configures Labels and Annotations which are propagated to the VMAuth pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// Image - docker image settings for VMAuth
	// if no specified operator uses default config version
	// +optional
	Image Image `json:"image,omitempty"`
	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the VMAuth
	// object, which shall be mounted into the VMAuth Pods.
	// +optional
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the VMAuth
	// object, which shall be mounted into the VMAuth Pods.
	// +optional
	ConfigMaps []string `json:"configMaps,omitempty"`
	// LogLevel for victoria metrics single to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VMAuth to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// ReplicaCount is the expected size of the VMAuth
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Number of pods",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount,urn:alm:descriptor:io.kubernetes:custom"
	ReplicaCount *int32 `json:"replicaCount,omitempty"`

	// Volumes allows configuration of additional volumes on the output deploy definition.
	// Volumes specified will be appended to other volumes that are generated as a result of
	// StorageSpec objects.
	// +optional
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output Deployment definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the VMAuth container,
	// that are generated as a result of StorageSpec objects.
	// +optional
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`
	// Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// if not defined default resources from operator config will be used
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
	// ServiceAccountName is the name of the ServiceAccount to use to run the
	// VMAuth Pods.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// SchedulerName - defines kubernetes scheduler name
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`
	// RuntimeClassName - defines runtime class for kubernetes pod.
	// https://kubernetes.io/docs/concepts/containers/runtime-class/
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`
	// PodSecurityPolicyName - defines name for podSecurityPolicy
	// in case of empty value, prefixedName will be used.
	// +optional
	PodSecurityPolicyName string `json:"podSecurityPolicyName,omitempty"`
	// HostAliases provides mapping for ip and hostname,
	// that would be propagated to pod,
	// cannot be used with HostNetwork.
	// +optional
	HostAliases []v1.HostAlias `json:"hostAliases,omitempty"`
	// Containers property allows to inject additions sidecars or to patch existing containers.
	// It can be useful for proxies, backup, etc.
	// +optional
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition. Those can be used to e.g.
	// fetch secrets for injection into the vmSingle configuration from external sources. Any
	// errors during the execution of an initContainer will lead to a restart of the Pod. More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// Using initContainers for any use case other then secret fetching is entirely outside the scope
	// of what the maintainers will support and by doing so, you accept that this behaviour may break
	// at any time without notice.
	// +optional
	InitContainers []v1.Container `json:"initContainers,omitempty"`
	// PriorityClassName assigned to the Pods
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

	// Port listen port
	// +optional
	Port string `json:"port,omitempty"`

	// SelectAllByDefault changes default behavior for empty CRD selectors, such userSelector.
	// with selectAllByDefault: true and empty userSelector and userNamespaceSelector
	// Operator selects all exist users
	// with selectAllByDefault: false - selects nothing
	// +optional
	SelectAllByDefault bool `json:"selectAllByDefault,omitempty"`
	// UserSelector defines VMUser to be selected for config file generation.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAuth namespace.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	UserSelector *metav1.LabelSelector `json:"userSelector,omitempty"`
	// UserNamespaceSelector Namespaces to be selected for  VMAuth discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAuth namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	UserNamespaceSelector *metav1.LabelSelector `json:"userNamespaceSelector,omitempty"`

	// ExtraArgs that will be passed to  VMAuth pod
	// for example remoteWrite.tmpDataPath: /tmp
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// ExtraEnvs that will be added to VMAuth pod
	// +optional
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
	// ServiceSpec that will be added to vmsingle service spec
	// +optional
	ServiceSpec *ServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmselect VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// Ingress enables ingress configuration for VMAuth.
	Ingress *EmbeddedIngress `json:"ingress,omitempty"`
	// LivenessProbe that will be added to VMAuth pod
	*EmbeddedProbes `json:",inline"`
	// NodeSelector Define which Nodes the Pods are scheduled on.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// TerminationGracePeriodSeconds period for container graceful termination
	// +optional
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
}

// EmbeddedIngress describes ingress configuration options.
type EmbeddedIngress struct {
	// ClassName defines ingress class name for VMAuth
	// +optional
	ClassName *string `json:"class_name,omitempty"`
	//  EmbeddedObjectMetadata adds labels and annotations for object.
	EmbeddedObjectMetadata `json:",inline"`
	// TlsHosts configures TLS access for ingress, tlsSecretName must be defined for it.
	TlsHosts []string `json:"tlsHosts,omitempty"`
	// TlsSecretName defines secretname at the VMAuth namespace with cert and key
	// https://kubernetes.io/docs/concepts/services-networking/ingress/#tls
	// +optional
	TlsSecretName string `json:"tlsSecretName,omitempty"`
	// ExtraRules - additional rules for ingress,
	// must be checked for correctness by user.
	// +optional
	ExtraRules []v12.IngressRule `json:"extraRules,omitempty"`
	// ExtraTLS - additional TLS configuration for ingress
	// must be checked for correctness by user.
	// +optional
	ExtraTLS []v12.IngressTLS `json:"extraTls,omitempty"`
	// Host defines ingress host parameter for default rule
	// It will be used, only if TlsHosts is empty
	// +optional
	Host string `json:"host,omitempty"`
}

// VMAuthStatus defines the observed state of VMAuth
type VMAuthStatus struct {
	// todo add status.
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAuth is the Schema for the vmauths API
type VMAuth struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAuthSpec   `json:"spec,omitempty"`
	Status VMAuthStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VMAuthList contains a list of VMAuth
type VMAuthList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAuth `json:"items"`
}

func (cr *VMAuth) AsOwner() []metav1.OwnerReference {
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

func (cr VMAuth) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr VMAuth) AnnotationsFiltered() map[string]string {
	annotations := make(map[string]string)
	for annotation, value := range cr.ObjectMeta.Annotations {
		if !strings.HasPrefix(annotation, "kubectl.kubernetes.io/") {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr VMAuth) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmauth",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VMAuth) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

func (cr VMAuth) AllLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.ObjectMeta.Labels == nil {
		return lbls
	}
	return labels.Merge(cr.ObjectMeta.Labels, lbls)
}

func (cr VMAuth) PrefixedName() string {
	return fmt.Sprintf("vmauth-%s", cr.Name)
}

func (cr VMAuth) ConfigSecretName() string {
	return fmt.Sprintf("vmauth-config-%s", cr.Name)
}

func (cr VMAuth) HealthPath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

func (cr VMAuth) MetricPath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

func (cr VMAuth) ReloadPathWithPort(port string) string {
	return fmt.Sprintf("%s://localhost:%s%s", protoFromFlags(cr.Spec.ExtraArgs), port, buildPathWithPrefixFlag(cr.Spec.ExtraArgs, reloadPath))
}

func (cr VMAuth) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr VMAuth) GetPSPName() string {
	if cr.Spec.PodSecurityPolicyName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.PodSecurityPolicyName
}

func (cr VMAuth) GetNSName() string {
	return cr.GetNamespace()
}

// AsCRDOwner implements interface
func (cr *VMAuth) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(Auth)
}

func init() {
	SchemeBuilder.Register(&VMAuth{}, &VMAuthList{})
}
