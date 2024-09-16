package v1beta1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMSingleSpec defines the desired state of VMSingle
// +k8s:openapi-gen=true
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="The version of VMSingle"
// +kubebuilder:printcolumn:name="RetentionPeriod",type="string",JSONPath=".spec.RetentionPeriod",description="The desired RetentionPeriod for vm single"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VMSingleSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// PodMetadata configures Labels and Annotations which are propagated to the VMSingle pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// Image - docker image settings for VMSingle
	// if no specified operator uses default config version
	// +optional
	Image Image `json:"image,omitempty"`
	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see https://kubernetes.io/docs/concepts/containers/images/#referring-to-an-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the VMSingle
	// object, which shall be mounted into the VMSingle Pods.
	// +optional
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the VMSingle
	// object, which shall be mounted into the VMSingle Pods.
	// +optional
	ConfigMaps []string `json:"configMaps,omitempty"`
	// LogLevel for victoria metrics single to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VMSingle to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// ReplicaCount is the expected size of the VMSingle
	// it can be 0 or 1
	// if you need more - use vm cluster
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Number of pods",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount,urn:alm:descriptor:io.kubernetes:custom"
	ReplicaCount *int32 `json:"replicaCount,omitempty"`
	// The number of old ReplicaSets to retain to allow rollback in deployment or
	// maximum number of revisions that will be maintained in the StatefulSet's revision history.
	// Defaults to 10.
	// +optional
	RevisionHistoryLimitCount *int32 `json:"revisionHistoryLimitCount,omitempty"`

	// StorageDataPath disables spec.storage option and overrides arg for victoria-metrics binary --storageDataPath,
	// its users responsibility to mount proper device into given path.
	// +optional
	StorageDataPath string `json:"storageDataPath,omitempty"`
	// Storage is the definition of how storage will be used by the VMSingle
	// by default it`s empty dir
	// +optional
	Storage *v1.PersistentVolumeClaimSpec `json:"storage,omitempty"`

	// StorageMeta defines annotations and labels attached to PVC for given vmsingle CR
	// +optional
	StorageMetadata EmbeddedObjectMetadata `json:"storageMetadata,omitempty"`
	// Volumes allows configuration of additional volumes on the output deploy definition.
	// Volumes specified will be appended to other volumes that are generated as a result of
	// StorageSpec objects.
	// +optional
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output Deployment definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the VMSingle container,
	// that are generated as a result of StorageSpec objects.
	// +optional
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`
	// Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// if not defined default resources from operator config will be used
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resources",xDescriptors="urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
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
	// VMSingle Pods.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// SchedulerName - defines kubernetes scheduler name
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`
	// RuntimeClassName - defines runtime class for kubernetes pod.
	// https://kubernetes.io/docs/concepts/containers/runtime-class/
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`
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

	// InsertPorts - additional listen ports for data ingestion.
	InsertPorts *InsertPorts `json:"insertPorts,omitempty"`
	// Port listen port
	// +optional
	Port string `json:"port,omitempty"`

	// RemovePvcAfterDelete - if true, controller adds ownership to pvc
	// and after VMSingle object deletion - pvc will be garbage collected
	// by controller manager
	// +optional
	RemovePvcAfterDelete bool `json:"removePvcAfterDelete,omitempty"`

	// RetentionPeriod for the stored metrics
	// Note VictoriaMetrics has data/ and indexdb/ folders
	// metrics from data/ removed eventually as soon as partition leaves retention period
	// reverse index data at indexdb rotates once at the half of configured [retention period](https://docs.victoriametrics.com/Single-server-VictoriaMetrics/#retention)
	RetentionPeriod string `json:"retentionPeriod"`
	// VMBackup configuration for backup
	// +optional
	VMBackup *VMBackup `json:"vmBackup,omitempty"`
	// License allows to configure license key to be used for enterprise features.
	// Using license key is supported starting from VictoriaMetrics v1.94.0.
	// See [here](https://docs.victoriametrics.com/enterprise)
	// +optional
	License *License `json:"license,omitempty"`
	// ExtraArgs that will be passed to  VMSingle pod
	// for example remoteWrite.tmpDataPath: /tmp
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// ExtraEnvs that will be added to VMSingle pod
	// +optional
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
	// ServiceSpec that will be added to vmsingle service spec
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmsingle VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// LivenessProbe that will be added to VMSingle pod
	*EmbeddedProbes `json:",inline"`
	// NodeSelector Define which Nodes the Pods are scheduled on.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// TerminationGracePeriodSeconds period for container graceful termination
	// +optional
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
	// ReadinessGates defines pod readiness gates
	ReadinessGates []v1.PodReadinessGate `json:"readinessGates,omitempty"`
	// StreamAggrConfig defines stream aggregation configuration for VMSingle
	StreamAggrConfig *StreamAggrConfig `json:"streamAggrConfig,omitempty"`
	// UseStrictSecurity enables strict security mode for component
	// it restricts disk writes access
	// uses non-root user out of the box
	// drops not needed security permissions
	// +optional
	UseStrictSecurity *bool `json:"useStrictSecurity,omitempty"`

	// Paused If set to true all actions on the underlying managed objects are not
	// going to be performed, except for delete actions.
	// +optional
	Paused bool `json:"paused,omitempty"`
}

// HasAnyStreamAggrRule checks if vmsingle has any defined aggregation rules
func (cr *VMSingle) HasAnyStreamAggrRule() bool {
	return cr.Spec.StreamAggrConfig.HasAnyRule()
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMSingleSpec) UnmarshalJSON(src []byte) error {
	type pcr VMSingleSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vmsingle spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// VMSingleStatus defines the observed state of VMSingle
// +k8s:openapi-gen=true
type VMSingleStatus struct {
	// ReplicaCount Total number of non-terminated pods targeted by this VMSingle.
	Replicas int32 `json:"replicas"`
	// UpdatedReplicas Total number of non-terminated pods targeted by this VMSingle.
	UpdatedReplicas int32 `json:"updatedReplicas"`
	// AvailableReplicas Total number of available pods (ready for at least minReadySeconds) targeted by this VMSingle.
	AvailableReplicas int32 `json:"availableReplicas"`
	// UnavailableReplicas Total number of unavailable pods targeted by this VMSingle.
	UnavailableReplicas int32 `json:"unavailableReplicas"`

	// UpdateStatus defines a status of single node rollout
	UpdateStatus UpdateStatus `json:"singleStatus,omitempty"`
	// Reason defines a reason in case of update failure
	Reason string `json:"reason,omitempty"`
}

// VMSingle  is fast, cost-effective and scalable time-series database.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMSingle App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmsingles,scope=Namespaced
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.singleStatus",description="Current status of single node update process"
type VMSingle struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMSingleSpec   `json:"spec,omitempty"`
	Status VMSingleStatus `json:"status,omitempty"`
}

func (cr *VMSingle) Probe() *EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

func (cr *VMSingle) ProbePath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

func (cr *VMSingle) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(cr.Spec.ExtraArgs))
}

func (cr *VMSingle) ProbePort() string {
	return cr.Spec.Port
}

func (cr *VMSingle) ProbeNeedLiveness() bool {
	return false
}

// VMSingleList contains a list of VMSingle
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VMSingleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMSingle `json:"items"`
}

func (cr *VMSingle) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         cr.APIVersion,
			Kind:               cr.Kind,
			Name:               cr.Name,
			UID:                cr.UID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	}
}

func (cr VMSingle) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr VMSingle) AnnotationsFiltered() map[string]string {
	return filterMapKeysByPrefixes(cr.ObjectMeta.Annotations, annotationFilterPrefixes)
}

func (cr VMSingle) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmsingle",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VMSingle) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

func (cr VMSingle) AllLabels() map[string]string {
	selectorLabels := cr.SelectorLabels()
	// fast path
	if cr.ObjectMeta.Labels == nil {
		return selectorLabels
	}
	crLabels := filterMapKeysByPrefixes(cr.ObjectMeta.Labels, labelFilterPrefixes)
	return labels.Merge(crLabels, selectorLabels)
}

func (cr VMSingle) PrefixedName() string {
	return fmt.Sprintf("vmsingle-%s", cr.Name)
}

func (cr VMSingle) StreamAggrConfigName() string {
	return fmt.Sprintf("stream-aggr-vmsingle-%s", cr.Name)
}

// GetMetricPath returns prefixed path for metric requests
func (cr VMSingle) GetMetricPath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr VMSingle) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr VMSingle) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

func (cr VMSingle) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr VMSingle) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

func (cr VMSingle) GetNSName() string {
	return cr.GetNamespace()
}

func (cr *VMSingle) AsURL() string {
	port := cr.Spec.Port
	if port == "" {
		port = "8429"
	}
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
				break
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", protoFromFlags(cr.Spec.ExtraArgs), cr.PrefixedName(), cr.Namespace, port)
}

// AsCRDOwner implements interface
func (cr *VMSingle) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(Single)
}

// LastAppliedSpecAsPatch return last applied single spec as patch annotation
func (cr *VMSingle) LastAppliedSpecAsPatch() (client.Patch, error) {
	data, err := json.Marshal(cr.Spec)
	if err != nil {
		return nil, fmt.Errorf("possible bug, cannot serialize single specification as json :%w", err)
	}
	patch := fmt.Sprintf(`{"metadata":{"annotations":{"operator.victoriametrics/last-applied-spec": %q}}}`, data)
	return client.RawPatch(types.MergePatchType, []byte(patch)), nil
}

// HasSpecChanges compares single spec with last applied single spec stored in annotation
func (cr *VMSingle) HasSpecChanges() (bool, error) {
	var prevSingleSpec VMSingleSpec
	lastAppliedSingleJSON := cr.Annotations["operator.victoriametrics/last-applied-spec"]
	if len(lastAppliedSingleJSON) == 0 {
		return true, nil
	}
	if err := json.Unmarshal([]byte(lastAppliedSingleJSON), &prevSingleSpec); err != nil {
		return true, fmt.Errorf("cannot parse last applied single spec value: %s : %w", lastAppliedSingleJSON, err)
	}
	instanceSpecData, _ := json.Marshal(cr.Spec)
	return !bytes.Equal([]byte(lastAppliedSingleJSON), instanceSpecData), nil
}

func (cr *VMSingle) Paused() bool {
	return cr.Spec.Paused
}

// SetStatusTo changes update status with optional reason of fail
func (cr *VMSingle) SetUpdateStatusTo(ctx context.Context, r client.Client, status UpdateStatus, maybeErr error) error {
	currentStatus := cr.Status.UpdateStatus
	prevStatus := cr.Status.DeepCopy()
	cr.Status.UpdateStatus = status
	switch status {
	case UpdateStatusExpanding:
	case UpdateStatusFailed:
		if maybeErr != nil {
			cr.Status.Reason = maybeErr.Error()
		}
	case UpdateStatusOperational:
		cr.Status.Reason = ""
	case UpdateStatusPaused:
		if currentStatus == status {
			return nil
		}
	default:
		panic(fmt.Sprintf("BUG: not expected status=%q", status))
	}
	if equality.Semantic.DeepEqual(&cr.Status, prevStatus) {
		return nil
	}
	if err := r.Status().Update(ctx, cr); err != nil {
		return fmt.Errorf("failed to update object status to=%q: %w", status, err)
	}
	return nil
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMSingle) GetAdditionalService() *AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VMSingle{}, &VMSingleList{})
}
