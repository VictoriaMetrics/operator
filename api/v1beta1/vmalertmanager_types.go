package v1beta1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
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
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.image.tag",description="The version of VMAlertmanager"
// +kubebuilder:printcolumn:name="ReplicaCount",type="integer",JSONPath=".spec.replicaCount",description="The desired replicas number of Alertmanagers"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:path=vmalertmanagers,scope=Namespaced,shortName=vma,singular=vmalertmanager
// +kubebuilder:printcolumn:name="Update Status",type="string",JSONPath=".status.updateStatus",description="Current update status"
type VMAlertmanager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Specification of the desired behavior of the VMAlertmanager cluster. More info:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec VMAlertmanagerSpec `json:"spec"`
	// Most recent observed status of the VMAlertmanager cluster.
	// Operator API itself. More info:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Status VMAlertmanagerStatus `json:"status,omitempty"`
}

// VMAlertmanagerSpec is a specification of the desired behavior of the VMAlertmanager cluster. More info:
// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
// +k8s:openapi-gen=true
type VMAlertmanagerSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-,omitempty" yaml:"-,omitempty"`
	// PodMetadata configures Labels and Annotations which are propagated to the alertmanager pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`

	// Image - docker image settings for VMAlertmanager
	// if no specified operator uses default config version
	// +optional
	Image Image `json:"image,omitempty"`

	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see https://kubernetes.io/docs/concepts/containers/images/#referring-to-an-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the VMAlertmanager
	// object, which shall be mounted into the VMAlertmanager Pods.
	// The Secrets are mounted into /etc/vm/secrets/<secret-name>
	// +optional
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the VMAlertmanager
	// object, which shall be mounted into the VMAlertmanager Pods.
	// The ConfigMaps are mounted into /etc/vm/configs/<configmap-name>.
	// +optional
	ConfigMaps []string `json:"configMaps,omitempty"`
	// Templates is a list of ConfigMap key references for ConfigMaps in the same namespace as the VMAlertmanager
	// object, which shall be mounted into the VMAlertmanager Pods.
	// The Templates are mounted into /etc/vm/templates/<configmap-name>/<configmap-key>.
	// +optional
	Templates []ConfigMapKeyReference `json:"templates,omitempty"`

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
	// MinReadySeconds defines a minim number os seconds to wait before starting update next pod
	// if previous in healthy state
	// +optional
	MinReadySeconds int32 `json:"minReadySeconds,omitempty"`
	// ReplicaCount Size is the expected size of the alertmanager cluster. The controller will
	// eventually make the size of the running cluster equal to the expected
	// +kubebuilder:validation:Minimum:=0
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Number of pods",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount,urn:alm:descriptor:io.kubernetes:custom"
	ReplicaCount *int32 `json:"replicaCount,omitempty"`
	// The number of old ReplicaSets to retain to allow rollback in deployment or
	// maximum number of revisions that will be maintained in the StatefulSet's revision history.
	// Defaults to 10.
	// +optional
	RevisionHistoryLimitCount *int32 `json:"revisionHistoryLimitCount,omitempty"`
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
	// https://kubernetes.io/docs/concepts/containers/runtime-class/
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
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmalertmanager VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*EmbeddedProbes     `json:",inline"`
	// SelectAllByDefault changes default behavior for empty CRD selectors, such ConfigSelector.
	// with selectAllByDefault: true and undefined ConfigSelector and ConfigNamespaceSelector
	// Operator selects all exist alertManagerConfigs
	// with selectAllByDefault: false - selects nothing
	// +optional
	SelectAllByDefault bool `json:"selectAllByDefault,omitempty"`
	// ConfigSelector defines selector for VMAlertmanagerConfig, result config will be merged with with Raw or Secret config.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAlertmanager namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ConfigSelector *metav1.LabelSelector `json:"configSelector,omitempty"`
	//  ConfigNamespaceSelector defines namespace selector for VMAlertmanagerConfig.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAlertmanager namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ConfigNamespaceSelector *metav1.LabelSelector `json:"configNamespaceSelector,omitempty"`
	// ExtraArgs that will be passed to  VMAlertmanager pod
	// for example log.level: debug
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// ExtraEnvs that will be added to VMAlertmanager pod
	// +optional
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`

	// DisableNamespaceMatcher disables namespace label matcher for VMAlertmanagerConfig
	// It may be useful if alert doesn't have namespace label for some reason
	// +optional
	DisableNamespaceMatcher bool `json:"disableNamespaceMatcher,omitempty"`

	// DisableRouteContinueEnforce cancel the behavior for VMAlertmanagerConfig that always enforce first-level route continue to true
	// +optional
	DisableRouteContinueEnforce bool `json:"disableRouteContinueEnforce,omitempty"`

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
	// UseStrictSecurity enables strict security mode for component
	// it restricts disk writes access
	// uses non-root user out of the box
	// drops not needed security permissions
	// +optional
	UseStrictSecurity *bool `json:"useStrictSecurity,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAlertmanagerSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAlertmanagerSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vmalertmanager spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
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
type VMAlertmanagerStatus struct {
	// Status defines a status of object update
	UpdateStatus UpdateStatus `json:"updateStatus,omitempty"`
	// Reason has non empty reason for update failure
	Reason string `json:"reason,omitempty,omitempty"`
}

func (cr *VMAlertmanager) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         cr.APIVersion,
			Kind:               cr.Kind,
			Name:               cr.Name,
			UID:                cr.UID,
			Controller:         pointer.Bool(true),
			BlockOwnerDeletion: pointer.Bool(true),
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

func (cr VMAlertmanager) AnnotationsFiltered() map[string]string {
	return filterMapKeysByPrefixes(cr.ObjectMeta.Annotations, annotationFilterPrefixes)
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

func (cr VMAlertmanager) AllLabels() map[string]string {
	selectorLabels := cr.SelectorLabels()
	// fast path
	if cr.ObjectMeta.Labels == nil {
		return selectorLabels
	}
	crLabels := filterMapKeysByPrefixes(cr.ObjectMeta.Labels, labelFilterPrefixes)
	return labels.Merge(crLabels, selectorLabels)
}

// ConfigSecretName returns configuration secret name for alertmanager
func (cr VMAlertmanager) ConfigSecretName() string {
	return fmt.Sprintf("%s-config", cr.PrefixedName())
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

func (cr VMAlertmanager) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == "" || cr.Spec.ServiceAccountName == cr.PrefixedName()
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

func (cr *VMAlertmanager) MetricPath() string {
	if prefix := cr.Spec.RoutePrefix; prefix != "" {
		return path.Join(prefix, metricPath)
	}
	return metricPath
}

// AsCRDOwner implements interface
func (cr *VMAlertmanager) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(AlertManager)
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

func (cr VMAlertmanager) UpdateStrategy() appsv1.StatefulSetUpdateStrategyType {
	if cr.Spec.RollingUpdateStrategy == "" {
		return appsv1.OnDeleteStatefulSetStrategyType
	}
	return cr.Spec.RollingUpdateStrategy
}

func (cr *VMAlertmanager) GetVolumeName() string {
	if cr.Spec.Storage != nil && cr.Spec.Storage.VolumeClaimTemplate.Name != "" {
		return cr.Spec.Storage.VolumeClaimTemplate.Name
	}
	return fmt.Sprintf("vmalertmanager-%s-db", cr.Name)
}

func (cr *VMAlertmanager) Probe() *EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

func (cr *VMAlertmanager) ProbePath() string {
	webRoutePrefix := "/"
	if cr.Spec.RoutePrefix != "" {
		webRoutePrefix = cr.Spec.RoutePrefix
	}
	return path.Clean(webRoutePrefix + "/-/healthy")
}

func (cr *VMAlertmanager) ProbePort() string {
	return cr.Spec.PortName
}

func (cr *VMAlertmanager) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(cr.Spec.ExtraArgs))
}

func (cr *VMAlertmanager) ProbeNeedLiveness() bool {
	return true
}

// IsUnmanaged checks if alertmanager should managed any alertmanager config objects
func (cr *VMAlertmanager) IsUnmanaged() bool {
	return !cr.Spec.SelectAllByDefault && cr.Spec.ConfigSelector == nil && cr.Spec.ConfigNamespaceSelector == nil
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VMAlertmanager) LastAppliedSpecAsPatch() (client.Patch, error) {
	data, err := json.Marshal(cr.Spec)
	if err != nil {
		return nil, fmt.Errorf("possible bug, cannot serialize specification as json :%w", err)
	}
	patch := fmt.Sprintf(`{"metadata":{"annotations":{"operator.victoriametrics/last-applied-spec": %q}}}`, data)
	return client.RawPatch(types.MergePatchType, []byte(patch)), nil
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (cr *VMAlertmanager) HasSpecChanges() (bool, error) {
	var prevSpec VMAlertmanagerSpec
	lastAppliedClusterJSON := cr.Annotations["operator.victoriametrics/last-applied-spec"]
	if len(lastAppliedClusterJSON) == 0 {
		return true, nil
	}
	if err := json.Unmarshal([]byte(lastAppliedClusterJSON), &prevSpec); err != nil {
		return true, fmt.Errorf("cannot parse last applied cluster spec value: %s : %w", lastAppliedClusterJSON, err)
	}
	instanceSpecData, _ := json.Marshal(cr.Spec)
	return !bytes.Equal([]byte(lastAppliedClusterJSON), instanceSpecData), nil
}

// SetStatusTo changes update status with optional reason of fail
func (cr *VMAlertmanager) SetUpdateStatusTo(ctx context.Context, r client.Client, status UpdateStatus, maybeErr error) error {
	cr.Status.UpdateStatus = status
	switch status {
	case UpdateStatusExpanding:
	case UpdateStatusFailed:
		if maybeErr != nil {
			cr.Status.Reason = maybeErr.Error()
		}
	case UpdateStatusOperational:
		cr.Status.Reason = ""
	default:
		panic(fmt.Sprintf("BUG: not expected status=%q", status))
	}
	if err := r.Status().Update(ctx, cr); err != nil {
		return fmt.Errorf("failed to update object status to=%q: %w", status, err)
	}
	return nil
}

func init() {
	SchemeBuilder.Register(&VMAlertmanager{}, &VMAlertmanagerList{})
}
