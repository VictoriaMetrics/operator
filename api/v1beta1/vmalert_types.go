package v1beta1

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// MetaVMAlertDeduplicateRulesKey - controls behavior for vmalert rules deduplication
	// its useful for migration from prometheus.
	MetaVMAlertDeduplicateRulesKey = "operator.victoriametrics.com/vmalert-deduplicate-rules"
)

// VMAlertSpec defines the desired state of VMAlert
// +k8s:openapi-gen=true
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="The version of VMAlert"
// +kubebuilder:printcolumn:name="ReplicaCount",type="integer",JSONPath=".spec.replicas",description="The desired replicas number of VmAlerts"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VMAlertSpec struct {
	// PodMetadata configures Labels and Annotations which are propagated to the VMAlert pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// Image - docker image settings for VMAlert
	// if no specified operator uses default config version
	// +optional
	Image Image `json:"image,omitempty"`
	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see http://kubernetes.io/docs/user-guide/images#specifying-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Secrets is a list of Secrets in the same namespace as the VMAlert
	// object, which shall be mounted into the VMAlert Pods.
	// The Secrets are mounted into /etc/vm/secrets/<secret-name>.
	// +optional
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the VMAlert
	// object, which shall be mounted into the VMAlert Pods.
	// The ConfigMaps are mounted into /etc/vm/configs/<configmap-name>.
	// +optional
	ConfigMaps []string `json:"configMaps,omitempty"`
	// LogFormat for VMAlert to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VMAlert to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// ReplicaCount is the expected size of the VMAlert cluster. The controller will
	// eventually make the size of the running cluster equal to the expected
	// size.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Number of pods",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount,urn:alm:descriptor:io.kubernetes:custom"
	ReplicaCount *int32 `json:"replicaCount,omitempty"`
	// Volumes allows configuration of additional volumes on the output Deployment definition.
	// Volumes specified will be appended to other volumes that are generated as a result of
	// StorageSpec objects.
	// +optional
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output Deployment definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the VMAlert container,
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
	// ServiceAccountName is the name of the ServiceAccount to use to run the
	// VMAlert Pods.
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
	// Containers property allows to inject additions sidecars or to patch existing containers.
	// It can be useful for proxies, backup, etc.
	// +optional
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition. Those can be used to e.g.
	// fetch secrets for injection into the VMAlert configuration from external sources. Any
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
	// TopologySpreadConstraints embedded kubernetes pod configuration option,
	// controls how pods are spread across your cluster among failure-domains
	// such as regions, zones, nodes, and other user-defined topology domains
	// https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
	// +optional
	TopologySpreadConstraints []v1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// EvaluationInterval how often evalute rules by default
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	EvaluationInterval string `json:"evaluationInterval,omitempty"`
	// EnforcedNamespaceLabel enforces adding a namespace label of origin for each alert
	// and metric that is user created. The label value will always be the namespace of the object that is
	// being created.
	// +optional
	EnforcedNamespaceLabel string `json:"enforcedNamespaceLabel,omitempty"`
	// SelectAllByDefault changes default behavior for empty CRD selectors, such RuleSelector.
	// with selectAllByDefault: true and empty serviceScrapeSelector and RuleNamespaceSelector
	// Operator selects all exist serviceScrapes
	// with selectAllByDefault: false - selects nothing
	// +optional
	SelectAllByDefault bool `json:"selectAllByDefault,omitempty"`
	// RuleSelector selector to select which VMRules to mount for loading alerting
	// rules from.
	// Works in combination with NamespaceSelector.
	// If both nil - behaviour controlled by selectAllByDefault
	// NamespaceSelector nil - only objects at VMAlert namespace.
	// +optional
	RuleSelector *metav1.LabelSelector `json:"ruleSelector,omitempty"`
	// RuleNamespaceSelector to be selected for VMRules discovery.
	// Works in combination with Selector.
	// If both nil - behaviour controlled by selectAllByDefault
	// NamespaceSelector nil - only objects at VMAlert namespace.
	// +optional
	RuleNamespaceSelector *metav1.LabelSelector `json:"ruleNamespaceSelector,omitempty"`

	// Port for listen
	// +optional
	Port string `json:"port,omitempty"`

	// Notifier prometheus alertmanager endpoint spec. Required at least one of  notifier or notifiers. e.g. http://127.0.0.1:9093
	// If specified both notifier and notifiers, notifier will be added as last element to notifiers.
	Notifier *VMAlertNotifierSpec `json:"notifier,omitempty"`

	// Notifiers prometheus alertmanager endpoints. Required at least one of  notifier or notifiers. e.g. http://127.0.0.1:9093
	// If specified both notifier and notifiers, notifier will be added as last element to notifiers.
	Notifiers []VMAlertNotifierSpec `json:"notifiers,omitempty"`

	// RemoteWrite Optional URL to remote-write compatible storage to persist
	// vmalert state and rule results to.
	// Rule results will be persisted according to each rule.
	// Alerts state will be persisted in the form of time series named ALERTS and ALERTS_FOR_STATE
	// see -remoteWrite.url docs in vmalerts for details.
	// E.g. http://127.0.0.1:8428
	// +optional
	RemoteWrite *VMAlertRemoteWriteSpec `json:"remoteWrite,omitempty"`

	// RemoteRead Optional URL to read vmalert state (persisted via RemoteWrite)
	// This configuration only makes sense if alerts state has been successfully
	// persisted (via RemoteWrite) before.
	// see -remoteRead.url docs in vmalerts for details.
	// E.g. http://127.0.0.1:8428
	// +optional
	RemoteRead *VMAlertRemoteReadSpec `json:"remoteRead,omitempty"`

	// RulePath to the file with alert rules.
	// Supports patterns. Flag can be specified multiple times.
	// Examples:
	// -rule /path/to/file. Path to a single file with alerting rules
	// -rule dir/*.yaml -rule /*.yaml. Relative path to all .yaml files in folder,
	// absolute path to all .yaml files in root.
	// by default operator adds /etc/vmalert/configs/base/vmalert.yaml
	// +optional
	RulePath []string `json:"rulePath,omitempty"`
	// Datasource Victoria Metrics or VMSelect url. Required parameter. e.g. http://127.0.0.1:8428
	Datasource VMAlertDatasourceSpec `json:"datasource"`

	// ExtraArgs that will be passed to  VMAlert pod
	// for example -remoteWrite.tmpDataPath=/tmp
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// ExtraEnvs that will be added to VMAlert pod
	// +optional
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
	// ExternalLabels in the form 'name: value' to add to all generated recording rules and alerts.
	// +optional
	ExternalLabels map[string]string `json:"externalLabels,omitempty"`

	// ServiceSpec that will be added to vmalert service spec
	// +optional
	ServiceSpec *ServiceSpec `json:"serviceSpec,omitempty"`

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
	// NodeSelector Define which Nodes the Pods are scheduled on.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// VMAgentRemoteReadSpec defines the remote storage configuration for VmAlert to read alerts from
// +k8s:openapi-gen=true
type VMAlertDatasourceSpec struct {
	// Victoria Metrics or VMSelect url. Required parameter. E.g. http://127.0.0.1:8428
	URL string `json:"url"`
	// BasicAuth allow datasource to authenticate over basic authentication
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// TLSConfig describes tls configuration for datasource target
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
}

// VMAlertNotifierSpec defines the notifier url for sending information about alerts
// +k8s:openapi-gen=true
type VMAlertNotifierSpec struct {
	// AlertManager url.  E.g. http://127.0.0.1:9093
	// +optional
	URL string `json:"url,omitempty"`
	// Selector allows service discovery for alertmanager
	// in this case all matched vmalertmanager replicas will be added into vmalert notifier.url
	// as statefulset pod.fqdn
	// +optional
	Selector *DiscoverySelector `json:"selector,omitempty"`
	// BasicAuth allow notifier to authenticate over basic authentication
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// TLSConfig describes tls configuration for notifier
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
}

// NotifierAsMapKey - returns cr name with suffix for notifier token/auth maps.
func (cr VMAlert) NotifierAsMapKey(i int) string {
	return fmt.Sprintf("vmalert/%s/%s/%d", cr.Namespace, cr.Name, i)
}

// VMAgentRemoteReadSpec defines the remote storage configuration for VmAlert to read alerts from
// +k8s:openapi-gen=true
type VMAlertRemoteReadSpec struct {
	// URL of the endpoint to send samples to.
	URL string `json:"url"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Lookback defines how far to look into past for alerts timeseries. For example, if lookback=1h then range from now() to now()-1h will be scanned. (default 1h0m0s)
	// Applied only to RemoteReadSpec
	// +optional
	Lookback *string `json:"lookback,omitempty"`
	// TLSConfig describes tls configuration for remote read target
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
}

// VMAgentRemoteWriteSpec defines the remote storage configuration for VmAlert
// +k8s:openapi-gen=true
type VMAlertRemoteWriteSpec struct {
	// URL of the endpoint to send samples to.
	URL string `json:"url"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Defines number of readers that concurrently write into remote storage (default 1)
	// +optional
	Concurrency *int32 `json:"concurrency,omitempty"`
	// Defines interval of flushes to remote write endpoint (default 5s)
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	FlushInterval *string `json:"flushInterval,omitempty"`
	// Defines defines max number of timeseries to be flushed at once (default 1000)
	// +optional
	MaxBatchSize *int32 `json:"maxBatchSize,omitempty"`
	// Defines the max number of pending datapoints to remote write endpoint (default 100000)
	// +optional
	MaxQueueSize *int32 `json:"maxQueueSize,omitempty"`
	// TLSConfig describes tls configuration for remote write target
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
}

// VmAlertStatus defines the observed state of VmAlert
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type VMAlertStatus struct {
	// ReplicaCount Total number of non-terminated pods targeted by this VMAlert
	// cluster (their labels match the selector).
	Replicas int32 `json:"replicas"`
	// UpdatedReplicas Total number of non-terminated pods targeted by this VMAlert
	// cluster that have the desired version spec.
	UpdatedReplicas int32 `json:"updatedReplicas"`
	// AvailableReplicas Total number of available pods (ready for at least minReadySeconds)
	// targeted by this VMAlert cluster.
	AvailableReplicas int32 `json:"availableReplicas"`
	// UnavailableReplicas Total number of unavailable pods targeted by this VMAlert cluster.
	UnavailableReplicas int32 `json:"unavailableReplicas"`
}

// VMAlert  executes a list of given alerting or recording rules against configured address.
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMAlert App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +genclient
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmalerts,scope=Namespaced
type VMAlert struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAlertSpec   `json:"spec,omitempty"`
	Status VMAlertStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VMAlertList contains a list of VMAlert
type VMAlertList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAlert `json:"items"`
}

func (cr *VMAlert) AsOwner() []metav1.OwnerReference {
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
func (cr VMAlert) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr VMAlert) Annotations() map[string]string {
	annotations := make(map[string]string)
	for annotation, value := range cr.ObjectMeta.Annotations {
		if !strings.HasPrefix(annotation, "kubectl.kubernetes.io/") {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr VMAlert) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmalert",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VMAlert) PodLabels() map[string]string {

	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

func (cr VMAlert) Labels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.ObjectMeta.Labels == nil {
		return lbls
	}
	return labels.Merge(cr.ObjectMeta.Labels, lbls)
}

func (cr VMAlert) PrefixedName() string {
	return fmt.Sprintf("vmalert-%s", cr.Name)
}
func (cr VMAlert) TLSAssetName() string {
	return fmt.Sprintf("tls-assets-vmalert-%s", cr.Name)
}
func (cr VMAlert) HealthPath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}
func (cr VMAlert) MetricPath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}
func (cr VMAlert) ReloadPathWithPort(port string) string {
	return fmt.Sprintf("http://localhost:%s%s", port, buildPathWithPrefixFlag(cr.Spec.ExtraArgs, reloadPath))
}

func (cr VMAlert) NeedDedupRules() bool {
	return cr.ObjectMeta.Annotations[MetaVMAlertDeduplicateRulesKey] != ""
}

func (cr VMAlert) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr VMAlert) GetPSPName() string {
	if cr.Spec.PodSecurityPolicyName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.PodSecurityPolicyName
}

func (cr VMAlert) GetNSName() string {
	return cr.GetNamespace()
}

func (cr VMAlert) RulesConfigMapSelector() client.ListOption {
	return &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{"vmalert-name": cr.Name}),
		Namespace:     cr.Namespace,
	}
}

func (cr *VMAlert) AsURL() string {
	port := cr.Spec.Port
	if port == "" {
		port = "8880"
	}
	return fmt.Sprintf("http://%s.%s.svc:%s", cr.PrefixedName(), cr.Namespace, port)
}

// AsCRDOwner implements interface
func (cr *VMAlert) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(Alert)
}

func (cr *VMAlert) GetNotifierSelectors() []*DiscoverySelector {
	var r []*DiscoverySelector
	for _, n := range cr.Spec.Notifiers {
		if n.Selector == nil {
			continue
		}
		r = append(r, n.Selector)
	}
	if cr.Spec.Notifier != nil && cr.Spec.Notifier.Selector != nil {
		r = append(r, cr.Spec.Notifier.Selector)
	}
	return r
}
func init() {
	SchemeBuilder.Register(&VMAlert{}, &VMAlertList{})
}
