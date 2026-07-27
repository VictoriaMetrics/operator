package v1beta1

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
)

// VMClusterSpec defines the desired state of VMCluster
// +k8s:openapi-gen=true
type VMClusterSpec struct {
	// RetentionPeriod defines how long to retain stored metrics, specified as a duration (e.g., "1d", "1w", "1m").
	// Data with timestamps outside the RetentionPeriod is automatically deleted. The minimum allowed value is 1d, or 24h.
	// The default value is 1 (one month).
	// See [retention](https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/#retention) docs for details.
	// +optional
	// +kubebuilder:validation:Pattern:="^[0-9]+(h|d|w|y)?$"
	RetentionPeriod string `json:"retentionPeriod,omitempty"`
	// ReplicationFactor defines how many copies of data make among
	// distinct storage nodes
	// +optional
	ReplicationFactor *int32 `json:"replicationFactor,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the
	// VMSelect, VMStorage and VMInsert Pods.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ClusterVersion defines default images tag for all components.
	// it can be overwritten with component specific image.tag value.
	// +optional
	ClusterVersion string `json:"clusterVersion,omitempty"`
	// ClusterDomainName defines domain name suffix for in-cluster dns addresses
	// aka .cluster.local
	// used by vminsert and vmselect to build vmstorage address
	// +optional
	ClusterDomainName string `json:"clusterDomainName,omitempty"`

	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see https://kubernetes.io/docs/concepts/containers/images/#specifying-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// License allows to configure license key to be used for enterprise features.
	// Using license key is supported starting from VictoriaMetrics v1.94.0.
	// See [here](https://docs.victoriametrics.com/victoriametrics/enterprise/)
	// +optional
	License *License `json:"license,omitempty"`

	// Downsampling defines downsampling rules applied to vmselect and vmstorage components.
	// Requires enterprise license. See https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#downsampling
	// +optional
	Downsampling *DownsamplingConfig `json:"downsampling,omitempty"`

	// +optional
	VMSelect *VMSelect `json:"vmselect,omitempty"`
	// +optional
	VMInsert *VMInsert `json:"vminsert,omitempty"`
	// +optional
	VMStorage *VMStorage `json:"vmstorage,omitempty"`
	// Paused If set to true all actions on the underlying managed objects are not
	// going to be performed, except for delete actions.
	// +optional
	Paused bool `json:"paused,omitempty"`
	// UseLegacyNaming switches component resource naming from the default prefix convention
	// (<component>-<name>, e.g. "vmselect-myapp") to a suffix convention (<name>-<component>,
	// e.g. "myapp-vmselect"). Useful when migrating from standalone Helm charts.
	// +optional
	// +notes={available_from: "v0.73.0"}
	UseLegacyNaming bool `json:"useLegacyNaming,omitempty"`
	// UseStrictSecurity enables strict security mode for component
	// it restricts disk writes access
	// uses non-root user out of the box
	// drops not needed security permissions
	// +optional
	UseStrictSecurity *bool `json:"useStrictSecurity,omitempty"`

	// RequestsLoadBalancer configures load-balancing for vminsert and vmselect requests.
	// It helps to evenly spread load across pods.
	// Usually it's not possible with Kubernetes TCP-based services.
	// See more [here](https://docs.victoriametrics.com/operator/resources/vmcluster/#requests-load-balancing)
	RequestsLoadBalancer VMAuthLoadBalancer `json:"requestsLoadBalancer,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *ManagedObjectsMetadata `json:"managedMetadata,omitempty"`

	// Discovery configures automatic vmstorage node discovery for vminsert and vmselect.
	// This is an enterprise feature and requires a valid license key.
	// See https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#automatic-vmstorage-discovery
	// +optional
	Discovery *VMClusterDiscovery `json:"discovery,omitempty"`

	// Pools defines named groups of vmstorage (and optionally vminsert) components.
	// Each pool gets its own StatefulSet and headless Service named <component>-<cluster>-<pool>.
	// Top-level vmstorage and vminsert specs act as defaults; pool specs override them field-by-field.
	// vmselect queries all pools using the pool name as a storage group name (-storageNode=<pool>/<addr>).
	// When pools are defined the top-level vmstorage is not deployed; pools replace it entirely.
	// The top-level vminsert is deployed as a shared insert group across all pools only when no pool
	// defines its own vminsert; as soon as any pool has a dedicated vminsert the top-level one is skipped.
	// +optional
	// +listType=map
	// +listMapKey=name
	Pools []VMClusterPool `json:"pools,omitempty"`
}

// VMClusterDiscovery configures automatic vmstorage node discovery for vminsert and vmselect.
// It maps to the -storageNode.discoveryInterval and -storageNode.filter flags.
// +k8s:openapi-gen=true
type VMClusterDiscovery struct {
	// Enabled turns on automatic vmstorage node discovery via DNS SRV records.
	// This is an enterprise feature and requires a valid license key.
	// See https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#automatic-vmstorage-discovery
	Enabled bool `json:"enabled"`
	// Interval is the interval for refreshing the vmstorage node list resolved from DNS SRV records.
	// The minimum supported value is 1s.
	// Defaults to 2s if not set.
	// +optional
	// +kubebuilder:validation:Pattern:="^[0-9]+(s|m|h)$"
	Interval string `json:"interval,omitempty"`
	// Filter is an optional regexp filter applied to discovered vmstorage addresses.
	// Only addresses matching the filter are used; non-matching addresses are ignored.
	// +optional
	Filter string `json:"filter,omitempty"`
}

// OrDefault returns d if non-nil, otherwise returns fallback.
func (d *VMClusterDiscovery) OrDefault(fallback *VMClusterDiscovery) *VMClusterDiscovery {
	if d != nil {
		return d
	}
	return fallback
}

func (d *VMClusterDiscovery) enabled() bool {
	return d != nil && d.Enabled
}

func (d *VMClusterDiscovery) validate(license *License) error {
	if !d.enabled() {
		return nil
	}
	if !license.IsProvided() {
		return fmt.Errorf("discovery requires a valid license key, see https://docs.victoriametrics.com/victoriametrics/enterprise/")
	}
	if err := license.validate(); err != nil {
		return err
	}
	if len(d.Filter) > 0 {
		if _, err := regexp.Compile(d.Filter); err != nil {
			return fmt.Errorf("discovery.filter is not a valid regexp: %w", err)
		}
	}
	if len(d.Interval) > 0 {
		if _, err := time.ParseDuration(d.Interval); err != nil {
			return fmt.Errorf("discovery.interval=%s is invalid", d.Interval)
		}
	}
	return nil
}

// SelectorLabels defines selector labels for given component kind
func (cr *VMCluster) SelectorLabels(kind ClusterComponent) map[string]string {
	return ClusterSelectorLabels(kind, cr.Name, "vm")
}

// PodMetadata return pod metadata for given component kind
func (cr *VMCluster) PodMetadata(kind ClusterComponent) *EmbeddedObjectMetadata {
	if cr == nil {
		return nil
	}
	switch kind {
	case ClusterComponentInsert:
		if cr.Spec.VMInsert == nil {
			return nil
		}
		return cr.Spec.VMInsert.PodMetadata
	case ClusterComponentSelect:
		if cr.Spec.VMSelect == nil {
			return nil
		}
		return cr.Spec.VMSelect.PodMetadata
	case ClusterComponentStorage:
		if cr.Spec.VMStorage == nil {
			return nil
		}
		return cr.Spec.VMStorage.PodMetadata
	case ClusterComponentBalancer:
		return cr.Spec.RequestsLoadBalancer.Spec.PodMetadata
	default:
		panic("BUG unsupported cluster kind=" + string(kind))
	}
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMCluster) GetAdditionalService(kind ClusterComponent) *AdditionalServiceSpec {
	if cr == nil {
		return nil
	}
	switch kind {
	case ClusterComponentInsert:
		if cr.Spec.VMInsert == nil {
			return nil
		}
		return cr.Spec.VMInsert.ServiceSpec
	case ClusterComponentSelect:
		if cr.Spec.VMSelect == nil {
			return nil
		}
		return cr.Spec.VMSelect.ServiceSpec
	case ClusterComponentStorage:
		if cr.Spec.VMStorage == nil {
			return nil
		}
		return cr.Spec.VMStorage.ServiceSpec
	case ClusterComponentBalancer:
		return cr.Spec.RequestsLoadBalancer.Spec.AdditionalServiceSpec
	default:
		panic("BUG unsupported cluster kind=" + string(kind))
	}
}

// PodLabels returns pod labels for given component kind
func (cr *VMCluster) PodLabels(kind ClusterComponent) map[string]string {
	selectorLabels := cr.SelectorLabels(kind)
	podMetadata := cr.PodMetadata(kind)
	if podMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(podMetadata.Labels, selectorLabels)
}

// PodAnnotations returns pod annotations for given component kind
func (cr *VMCluster) PodAnnotations(kind ClusterComponent) map[string]string {
	podMetadata := cr.PodMetadata(kind)
	if podMetadata == nil {
		return nil
	}
	return podMetadata.Annotations
}

// PrefixedName returns prefixed name for the given component kind
func (cr *VMCluster) PrefixedName(kind ClusterComponent) string {
	if cr.Spec.UseLegacyNaming {
		return ClusterSuffixedName(kind, cr.Name, "vm", false)
	}
	return ClusterPrefixedName(kind, cr.Name, "vm", false)
}

// PrefixedInternalName returns prefixed name for the given component kind
func (cr *VMCluster) PrefixedInternalName(kind ClusterComponent) string {
	if cr.Spec.UseLegacyNaming {
		return ClusterSuffixedName(kind, cr.Name, "vm", true)
	}
	return ClusterPrefixedName(kind, cr.Name, "vm", true)
}

// VMCluster is fast, cost-effective and scalable time-series database.
// Cluster version with
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMCluster App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Statefulset,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=vmclusters,scope=Namespaced
// +kubebuilder:printcolumn:name="Insert Count",type="string",JSONPath=".spec.vminsert.replicaCount",description="replicas of VMInsert"
// +kubebuilder:printcolumn:name="Storage Count",type="string",JSONPath=".spec.vmstorage.replicaCount",description="replicas of VMStorage"
// +kubebuilder:printcolumn:name="Select Count",type="string",JSONPath=".spec.vmselect.replicaCount",description="replicas of VMSelect"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of cluster"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VMCluster struct {
	// +optional
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              VMClusterSpec `json:"spec"`
	// +optional
	Status VMClusterStatus `json:"status,omitempty"`
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMCluster) GetStatus() *VMClusterStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMCluster) DefaultStatusFields(vs *VMClusterStatus) {}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMCluster) UnmarshalJSON(src []byte) error {
	type pcr VMCluster
	type shadow struct {
		*pcr
		Spec json.RawMessage `json:"spec"`
	}
	s := shadow{pcr: (*pcr)(cr)}
	if err := json.Unmarshal(src, &s); err != nil {
		return err
	}
	if len(s.Spec) > 0 {
		if err := UnmarshalSpecStrict(s.Spec, &cr.Spec); err != nil {
			cr.Status.ParsingSpecError = fmt.Sprintf("cannot parse VMClusterSpec: %s, err: %s", string(s.Spec), err)
		}
	}
	return nil
}

// AsOwner returns owner references with current object as owner
func (cr *VMCluster) AsOwner() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         cr.APIVersion,
		Kind:               cr.Kind,
		Name:               cr.Name,
		UID:                cr.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
}

// VMClusterStatus defines the observed state of VMCluster
type VMClusterStatus struct {
	StatusMetadata `json:",inline"`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	LastAppliedSpec *VMClusterSpec `json:"lastAppliedSpec,omitempty"`
	// ParsingSpecError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingSpecError string `json:"-" yaml:"-"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VMCluster) GetStatusMetadata() *StatusMetadata {
	return &cr.Status.StatusMetadata
}

// VMClusterList contains a list of VMCluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VMClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMCluster `json:"items"`
}

// VMSelect defines configuration section for vmselect components of the victoria-metrics cluster
type VMSelect struct {
	// ComponentVersion defines default images tag for this component.
	// it can be overwritten with component specific image.tag value.
	// +optional
	ComponentVersion string `json:"componentVersion,omitempty"`
	// PodMetadata configures Labels and Annotations which are propagated to the VMSelect pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for VMSelect to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VMSelect to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// CacheMountPath allows to add cache persistent for VMSelect,
	// will use "/cache" as default if not specified.
	// +optional
	CacheMountPath string `json:"cacheMountPath,omitempty"`

	// PersistentVolumeClaimRetentionPolicy allows configuration of PVC retention policy
	// +optional
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`
	// StorageSpec - add persistent volume claim for cacheMountPath
	// its needed for persistent cache
	// +optional
	StorageSpec *StorageSpec `json:"storage,omitempty"`
	// ClusterNativePort for multi-level cluster setup.
	// More [details](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#multi-level-cluster-setup)
	// +optional
	ClusterNativePort string `json:"clusterNativeListenPort,omitempty"`

	// ServiceSpec that will be added to vmselect service spec
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmselect VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// Configures horizontal pod autoscaling.
	// Note, enabling this option disables vmselect to vmselect communication. In most cases it's not an issue.
	// +optional
	HPA *EmbeddedHPA `json:"hpa,omitempty"`
	// Configures vertical pod autoscaling.
	// +optional
	VPA *EmbeddedVPA `json:"vpa,omitempty"`
	// NetworkPolicy defines network access rules for pods created by this CR.
	// +optional
	NetworkPolicy *EmbeddedNetworkPolicy `json:"networkPolicy,omitempty"`

	// RollingUpdateStrategy defines strategy for application updates
	// Default is OnDelete, in this case operator handles update process
	// Can be changed for RollingUpdate
	// +optional
	RollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"rollingUpdateStrategy,omitempty"`
	// RollingUpdateStrategyBehavior defines customized behavior for rolling updates.
	// It applies if the RollingUpdateStrategy is set to OnDelete, which is the default.
	// +optional
	RollingUpdateStrategyBehavior *StatefulSetUpdateStrategyBehavior `json:"rollingUpdateStrategyBehavior,omitempty"`

	// ClaimTemplates allows adding additional VolumeClaimTemplates for StatefulSet
	ClaimTemplates []corev1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`

	// Discovery overrides the cluster-level discovery config for vmselect.
	// +optional
	Discovery *VMClusterDiscovery `json:"discovery,omitempty"`

	// ExtraStorageNodes - defines additional storage nodes to VMSelect,
	// available for select only. Useful for adding an existing vmsingle nodes
	// to the cluster in a read-only mode.
	// +optional
	// +notes={available_from: "v0.74.0"}
	ExtraStorageNodes []VMStorageNode `json:"extraStorageNodes,omitempty"`

	CommonAppsParams `json:",inline"`
}

// VMStorageNode defines an additional, non-operator-managed vmstorage node
type VMStorageNode struct {
	// Addr defines storage node address
	// +kubebuilder:validation:MinLength=1
	Addr string `json:"addr"`
}

type InsertPorts struct {
	// GraphitePort listen port
	// +optional
	GraphitePort string `json:"graphitePort,omitempty"`
	// InfluxPort listen port
	// +optional
	InfluxPort string `json:"influxPort,omitempty"`
	// OpenTSDBHTTPPort for http connections.
	// +optional
	OpenTSDBHTTPPort string `json:"openTSDBHTTPPort,omitempty"`
	// OpenTSDBPort for tcp and udp listen
	// +optional
	OpenTSDBPort string `json:"openTSDBPort,omitempty"`
}

type VMInsert struct {
	// ComponentVersion defines default images tag for this component.
	// it can be overwritten with component specific image.tag value.
	// +optional
	ComponentVersion string `json:"componentVersion,omitempty"`
	// PodMetadata configures Labels and Annotations which are propagated to the VMInsert pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for VMInsert to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VMInsert to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`

	// InsertPorts - additional listen ports for data ingestion.
	InsertPorts *InsertPorts `json:"insertPorts,omitempty"`

	// ClusterNativePort for multi-level cluster setup.
	// More [details](https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#multi-level-cluster-setup)
	// +optional
	ClusterNativePort string `json:"clusterNativeListenPort,omitempty"`

	// ServiceSpec that will be added to vminsert service spec
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vminsert VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`

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
	// HPA defines kubernetes PodAutoScaling configuration version 2.
	HPA *EmbeddedHPA `json:"hpa,omitempty"`
	// Configures vertical pod autoscaling.
	// +optional
	VPA *EmbeddedVPA `json:"vpa,omitempty"`
	// NetworkPolicy defines network access rules for pods created by this CR.
	// +optional
	NetworkPolicy *EmbeddedNetworkPolicy `json:"networkPolicy,omitempty"`

	// Discovery overrides the cluster-level discovery config for vminsert.
	// +optional
	Discovery *VMClusterDiscovery `json:"discovery,omitempty"`

	CommonAppsParams `json:",inline"`
}

func (cr *VMInsert) ProbePath() string {
	return BuildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

// UseProxyProtocol implements build.probeCRD interface
func (cr *VMInsert) UseProxyProtocol() bool {
	return UseProxyProtocol(cr.ExtraArgs)
}

func (cr *VMInsert) ProbeScheme() string {
	return strings.ToUpper(HTTPProtoFromFlags(cr.ExtraArgs))
}

func (cr *VMInsert) ProbePort() string {
	return cr.Port
}

func (*VMInsert) ProbeNeedLiveness() bool {
	return true
}

type VMStorage struct {
	// ComponentVersion defines default images tag for this component.
	// it can be overwritten with component specific image.tag value.
	// +optional
	ComponentVersion string `json:"componentVersion,omitempty"`
	// RetentionPeriod overrides the cluster-level retentionPeriod for this storage instance.
	// Useful when using Pools to implement multi-retention setups.
	// +optional
	// +kubebuilder:validation:Pattern:="^[0-9]+(h|d|w|y)?$"
	RetentionPeriod string `json:"retentionPeriod,omitempty"`
	// PodMetadata configures Labels and Annotations which are propagated to the VMStorage pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for VMStorage to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VMStorage to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// StorageDataPath - path to storage data
	// +optional
	StorageDataPath string `json:"storageDataPath,omitempty"`
	// Storage - add persistent volume for StorageDataPath
	// its useful for persistent cache
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`
	// PersistentVolumeClaimRetentionPolicy allows configuration of PVC retention policy
	// +optional
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`

	// Configures horizontal pod autoscaling.
	// Note, downscaling is not supported.
	// +optional
	HPA *EmbeddedHPA `json:"hpa,omitempty"`
	// Configures vertical pod autoscaling.
	// +optional
	VPA *EmbeddedVPA `json:"vpa,omitempty"`

	// VMInsertPort for VMInsert connections
	// +optional
	VMInsertPort string `json:"vmInsertPort,omitempty"`

	// VMSelectPort for VMSelect connections
	// +optional
	VMSelectPort string `json:"vmSelectPort,omitempty"`

	// VMBackup configuration for backup
	// +optional
	VMBackup *VMBackup `json:"vmBackup,omitempty"`
	// RetentionFilters defines per-series retention filters for vmstorage.
	// Requires enterprise license. See https://docs.victoriametrics.com/victoriametrics/cluster-victoriametrics/#retention-filters
	// +optional
	RetentionFilters *RetentionFiltersConfig `json:"retentionFilters,omitempty"`
	// ServiceSpec that will be create additional service for vmstorage
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmstorage VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// NetworkPolicy defines network access rules for pods created by this CR.
	// +optional
	NetworkPolicy *EmbeddedNetworkPolicy `json:"networkPolicy,omitempty"`
	// MaintenanceInsertNodeIDs - excludes given node ids from insert requests routing, must contain pod suffixes - for pod-0, id will be 0 and etc.
	// lets say, you have pod-0, pod-1, pod-2, pod-3. to exclude pod-0 and pod-3 from insert routing, define nodeIDs: [0,3].
	// Useful at storage expanding, when you want to rebalance some data at cluster.
	// +optional
	MaintenanceInsertNodeIDs []int32 `json:"maintenanceInsertNodeIDs,omitempty"`
	// MaintenanceSelectNodeIDs - excludes given node ids from select requests routing, must contain pod suffixes - for pod-0, id will be 0 and etc.
	// +optional
	MaintenanceSelectNodeIDs []int32 `json:"maintenanceSelectNodeIDs,omitempty"`

	// RollingUpdateStrategy defines strategy for application updates
	// Default is OnDelete, in this case operator handles update process
	// Can be changed for RollingUpdate
	// +optional
	RollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"rollingUpdateStrategy,omitempty"`
	// RollingUpdateStrategyBehavior defines customized behavior for rolling updates.
	// It applies if the RollingUpdateStrategy is set to OnDelete, which is the default.
	// +optional
	RollingUpdateStrategyBehavior *StatefulSetUpdateStrategyBehavior `json:"rollingUpdateStrategyBehavior,omitempty"`

	// ClaimTemplates allows adding additional VolumeClaimTemplates for StatefulSet
	ClaimTemplates []corev1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`

	CommonAppsParams `json:",inline"`
}

type VMBackup struct {
	// AcceptEULA accepts enterprise feature usage, must be set to true.
	// otherwise backupmanager cannot be added to single/cluster version.
	// https://victoriametrics.com/legal/esa/
	// +notes={deprecated_in: "v0.61.0", removed_in: "v0.69.0", replacements: {VMClusterSpec.license}}
	// +optional
	AcceptEULA bool `json:"acceptEULA"`
	// SnapshotCreateURL overwrites url for snapshot create
	// +optional
	SnapshotCreateURL string `json:"snapshotCreateURL,omitempty"`
	// SnapshotDeleteURL overwrites url for snapshot delete
	// +optional
	SnapshotDeleteURL string `json:"snapshotDeleteURL,omitempty"`
	// Defines number of concurrent workers. Higher concurrency may reduce backup duration (default 10)
	// +optional
	Concurrency *int32 `json:"concurrency,omitempty"`
	// Defines destination for backup
	Destination string `json:"destination,omitempty"`
	// DestinationDisableSuffixAdd - disables suffix adding for cluster version backups
	// each vmstorage backup must have unique backup folder
	// so operator adds POD_NAME as suffix for backup destination folder.
	// +optional
	DestinationDisableSuffixAdd bool `json:"destinationDisableSuffixAdd,omitempty"`
	// Custom S3 endpoint for use with S3-compatible storages (e.g. MinIO). S3 is used if not set
	// +optional
	CustomS3Endpoint *string `json:"customS3Endpoint,omitempty"`
	// CredentialsSecret is secret in the same namespace for access to remote storage
	// The secret is mounted into /etc/vm/creds.
	// +optional
	CredentialsSecret *corev1.SecretKeySelector `json:"credentialsSecret,omitempty"`

	// Defines if hourly backups disabled (default false)
	// +optional
	DisableHourly *bool `json:"disableHourly,omitempty"`
	// Defines if daily backups disabled (default false)
	// +optional
	DisableDaily *bool `json:"disableDaily,omitempty"`
	// Defines if weekly backups disabled (default false)
	// +optional
	DisableWeekly *bool `json:"disableWeekly,omitempty"`
	// Defines if monthly backups disabled (default false)
	// +optional
	DisableMonthly *bool `json:"disableMonthly,omitempty"`
	// Image - docker image settings for VMBackuper
	// +optional
	Image Image `json:"image,omitempty"`
	// Port for health check connections
	Port string `json:"port,omitempty"`
	// LogFormat for VMBackup to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat *string `json:"logFormat,omitempty"`
	// LogLevel for VMBackup to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel *string `json:"logLevel,omitempty"`
	// Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// if not defined default resources from operator config will be used
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// extra args like maxBytesPerSecond default 0
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// +optional
	ExtraEnvs []corev1.EnvVar `json:"extraEnvs,omitempty"`
	// ExtraEnvsFrom defines source of env variables for the application container
	// could either be secret or configmap
	// +optional
	ExtraEnvsFrom []corev1.EnvFromSource `json:"extraEnvsFrom,omitempty"`

	// VolumeMounts allows configuration of additional VolumeMounts on the output Deployment definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the vmbackupmanager container,
	// that are generated as a result of StorageSpec objects.
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// Restore Allows to enable restore options for pod
	// Read [more](https://docs.victoriametrics.com/victoriametrics/vmbackupmanager/#restore-commands)
	// +optional
	Restore *VMRestore `json:"restore,omitempty"`
}

func (cr *VMBackup) validate(l *License) error {
	if !l.IsProvided() && !cr.AcceptEULA {
		return fmt.Errorf("it is required to provide license key. See [here](https://docs.victoriametrics.com/victoriametrics/enterprise/)")
	}

	if l.IsProvided() {
		return l.validate()
	}

	return nil
}

// VMRestore defines config options for vmrestore start-up
type VMRestore struct {
	// OnStart defines configuration for restore on pod start
	// +optional
	OnStart *VMRestoreOnStartConfig `json:"onStart,omitempty"`
}

// VMRestoreOnStartConfig controls vmrestore setting
type VMRestoreOnStartConfig struct {
	// Enabled defines if restore on start enabled
	// +optional
	Enabled bool `json:"enabled,omitempty"`
}

// GetStorageVolumeName returns formatted name for vmstorage volume
func (cr *VMStorage) GetStorageVolumeName() string {
	if cr.Storage != nil && cr.Storage.VolumeClaimTemplate.Name != "" {
		return cr.Storage.VolumeClaimTemplate.Name
	}
	return "vmstorage-db"
}

// GetCacheMountVolumeName returns formatted name for vmselect volume
func (cr *VMSelect) GetCacheMountVolumeName() string {
	storageSpec := cr.StorageSpec
	if storageSpec != nil && storageSpec.VolumeClaimTemplate.Name != "" {
		return storageSpec.VolumeClaimTemplate.Name
	}
	return "vmselect-cachedir"
}

// UseProxyProtocol implements build.probeCRD interface
func (cr *VMSelect) UseProxyProtocol() bool {
	return UseProxyProtocol(cr.ExtraArgs)
}

// GetRemoteWriteURL returns remote write url for VMCluster
func (cr *VMCluster) GetRemoteWriteURL() string {
	if cr == nil || cr.Spec.VMInsert == nil {
		return ""
	}
	insertURL, err := cr.AsURL(ClusterComponentInsert, "", false)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s%s", insertURL, BuildPathWithPrefixFlag(cr.Spec.VMInsert.ExtraArgs, "/insert/multitenant/prometheus/api/v1/write"))
}

func (vms *VMStorage) validate(license *License, clusterRetentionPeriod string) error {
	if vms.VMBackup != nil {
		if err := vms.VMBackup.validate(license); err != nil {
			return err
		}
	}
	retention := clusterRetentionPeriod
	if vms.RetentionPeriod != "" {
		retention = vms.RetentionPeriod
	}
	if err := vms.RetentionFilters.validate(license, retention); err != nil {
		return err
	}
	if vms.HPA != nil {
		if vms.HPA.Behaviour != nil && vms.HPA.Behaviour.ScaleDown != nil {
			return fmt.Errorf("scaledown HPA behavior is not supported")
		}
		if err := vms.HPA.Validate(); err != nil {
			return err
		}
	}
	if vms.VPA != nil {
		if err := vms.VPA.Validate(); err != nil {
			return err
		}
	}
	if vms.RollingUpdateStrategyBehavior != nil {
		if err := vms.RollingUpdateStrategyBehavior.Validate(); err != nil {
			return err
		}
	}
	return vms.Validate()
}

func (vmi *VMInsert) validate() error {
	if vmi.HPA != nil {
		if err := vmi.HPA.Validate(); err != nil {
			return err
		}
	}
	if vmi.VPA != nil {
		if err := vmi.VPA.Validate(); err != nil {
			return err
		}
	}
	return vmi.Validate()
}

func (cr *VMCluster) Validate() error {
	if MustSkipCRValidation(cr) {
		return nil
	}
	if cr.Spec.VMSelect != nil {
		vms := cr.Spec.VMSelect
		name := cr.PrefixedName(ClusterComponentSelect)
		if vms.ServiceSpec != nil && vms.ServiceSpec.Name == name {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", name)
		}
		if vms.HPA != nil {
			if err := vms.HPA.Validate(); err != nil {
				return err
			}
		}
		if vms.VPA != nil {
			if err := vms.VPA.Validate(); err != nil {
				return err
			}
		}
		if vms.RollingUpdateStrategyBehavior != nil {
			if err := vms.RollingUpdateStrategyBehavior.Validate(); err != nil {
				return fmt.Errorf("vmselect: %w", err)
			}
		}
		if err := vms.Validate(); err != nil {
			return fmt.Errorf("vmselect: %w", err)
		}
		storageNodes := sets.New[string]()
		if cr.Spec.VMStorage != nil && !vms.Discovery.OrDefault(cr.Spec.Discovery).enabled() {
			storageName := cr.PrefixedName(ClusterComponentStorage)
			for _, idx := range cr.AvailableStorageNodeIDs(ClusterComponentSelect) {
				storageNodes.Insert(PodDNSAddress(storageName, idx, cr.Namespace, cr.Spec.VMStorage.VMSelectPort, cr.Spec.ClusterDomainName))
			}
		}
		if nodes, ok := vms.ExtraArgs["storageNode"]; ok {
			for _, node := range strings.Split(nodes, ",") {
				node = strings.TrimSpace(node)
				if storageNodes.Has(node) {
					return fmt.Errorf("encountered storageNode=%s multiple times, please make all storage node addresses are unique", node)
				}
				storageNodes.Insert(node)
			}
		}
		for _, node := range vms.ExtraStorageNodes {
			if len(node.Addr) == 0 {
				return fmt.Errorf("extraStorageNodes[].addr cannot be empty")
			}
			if storageNodes.Has(node.Addr) {
				return fmt.Errorf("encountered storageNode=%s multiple times, please make all storage node addresses are unique", node.Addr)
			}
			storageNodes.Insert(node.Addr)
		}
	}
	if cr.Spec.VMInsert != nil {
		vmi := cr.Spec.VMInsert
		name := cr.PrefixedName(ClusterComponentSelect)
		if vmi.ServiceSpec != nil && vmi.ServiceSpec.Name == name {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", name)
		}
		if err := vmi.validate(); err != nil {
			return fmt.Errorf("vminsert: %w", err)
		}
	}
	if err := cr.Spec.Downsampling.validate(cr.Spec.License); err != nil {
		return err
	}
	if cr.Spec.VMStorage != nil {
		vms := cr.Spec.VMStorage
		name := cr.PrefixedName(ClusterComponentSelect)
		if vms.ServiceSpec != nil && vms.ServiceSpec.Name == name {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", name)
		}
		if err := vms.validate(cr.Spec.License, cr.Spec.RetentionPeriod); err != nil {
			return fmt.Errorf("vmstorage: %w", err)
		}
	}
	if cr.Spec.RequestsLoadBalancer.Enabled {
		rlb := cr.Spec.RequestsLoadBalancer.Spec
		lbName := cr.PrefixedName(ClusterComponentBalancer)
		if rlb.AdditionalServiceSpec != nil && rlb.AdditionalServiceSpec.Name == lbName {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", lbName)
		}
		if err := rlb.Validate(); err != nil {
			return fmt.Errorf("requestsLoadBalancer: %w", err)
		}
	}
	var vminsertDiscovery, vmselectDiscovery *VMClusterDiscovery
	if cr.Spec.VMInsert != nil {
		vminsertDiscovery = cr.Spec.VMInsert.Discovery.OrDefault(cr.Spec.Discovery)
	}
	if cr.Spec.VMSelect != nil {
		vmselectDiscovery = cr.Spec.VMSelect.Discovery.OrDefault(cr.Spec.Discovery)
	}
	if vminsertDiscovery.enabled() {
		if cr.Spec.VMStorage != nil && len(cr.Spec.VMStorage.MaintenanceInsertNodeIDs) > 0 {
			return fmt.Errorf("maintenanceInsertNodeIDs cannot be used when vminsert discovery is enabled")
		}
	}
	if err := vminsertDiscovery.validate(cr.Spec.License); err != nil {
		return fmt.Errorf("vminsert: %w", err)
	}
	if vmselectDiscovery.enabled() {
		if cr.Spec.VMStorage != nil && len(cr.Spec.VMStorage.MaintenanceSelectNodeIDs) > 0 {
			return fmt.Errorf("maintenanceSelectNodeIDs cannot be used when vmselect discovery is enabled")
		}
	}
	if err := vmselectDiscovery.validate(cr.Spec.License); err != nil {
		return fmt.Errorf("vmselect: %w", err)
	}

	poolNames := make(map[string]struct{}, len(cr.Spec.Pools))
	var hasPoolInsert bool
	for i, pool := range cr.Spec.Pools {
		if errs := validation.IsDNS1123Subdomain(pool.Name); len(errs) > 0 {
			return fmt.Errorf("pools[%d].name %q is invalid: %s", i, pool.Name, strings.Join(errs, "; "))
		}
		if len(pool.Name) > maxVMClusterPoolNameLength {
			return fmt.Errorf("pools[%d].name %q is too long: max %d characters", i, pool.Name, maxVMClusterPoolNameLength)
		}
		if _, dup := poolNames[pool.Name]; dup {
			return fmt.Errorf("pools[%d].name %q is duplicated", i, pool.Name)
		}
		poolNames[pool.Name] = struct{}{}
		if pool.VMStorage != nil {
			vms := pool.VMStorage.DeepCopy()
			if cr.Spec.VMStorage != nil {
				if err := MergeDeep(vms, cr.Spec.VMStorage, true); err != nil {
					return fmt.Errorf("pools[%d] vmstorage merge: %w", i, err)
				}
			}
			if err := vms.validate(cr.Spec.License, cr.Spec.RetentionPeriod); err != nil {
				return fmt.Errorf("pools[%d] vmstorage: %w", i, err)
			}
		}
		if pool.VMInsert != nil {
			hasPoolInsert = true
			vmi := pool.VMInsert.DeepCopy()
			if cr.Spec.VMInsert != nil {
				if err := MergeDeep(vmi, cr.Spec.VMInsert, true); err != nil {
					return fmt.Errorf("pools[%d] vminsert merge: %w", i, err)
				}
			}
			if err := vmi.validate(); err != nil {
				return fmt.Errorf("pools[%d] vminsert: %w", i, err)
			}
			poolDiscovery := vmi.Discovery.OrDefault(cr.Spec.Discovery)
			if err := poolDiscovery.validate(cr.Spec.License); err != nil {
				return fmt.Errorf("pools[%d] vminsert: %w", i, err)
			}
		}
	}
	if hasPoolInsert {
		for i, pool := range cr.Spec.Pools {
			if pool.VMInsert == nil {
				return fmt.Errorf("pools[%d] %q: vminsert must be defined once any pool defines its own vminsert, otherwise this pool has no ingestion path", i, pool.Name)
			}
		}
	}

	return nil
}

// AvailableStorageNodeIDs returns ids of the storage nodes for the provided component
func (cr *VMCluster) AvailableStorageNodeIDs(kind ClusterComponent) []int32 {
	var result []int32
	if cr.Spec.VMStorage == nil || (cr.Spec.VMStorage.ReplicaCount == nil && cr.Spec.VMStorage.HPA == nil) {
		return result
	}
	maintenanceNodes := sets.New[int32]()
	switch kind {
	case ClusterComponentSelect:
		maintenanceNodes.Insert(cr.Spec.VMStorage.MaintenanceSelectNodeIDs...)
	case ClusterComponentInsert:
		maintenanceNodes.Insert(cr.Spec.VMStorage.MaintenanceInsertNodeIDs...)
	default:
		panic("BUG unsupported kind: " + string(kind))
	}
	var replicaCount int32
	if cr.Spec.VMStorage.ReplicaCount != nil {
		replicaCount = *cr.Spec.VMStorage.ReplicaCount
	} else if cr.Spec.VMStorage.HPA != nil {
		replicaCount = cr.Spec.VMStorage.HPA.GetMinReplicas()
	}
	for i := int32(0); i < replicaCount; i++ {
		if maintenanceNodes.Has(i) {
			continue
		}
		result = append(result, i)
	}
	return result
}

// FinalLabels adds cluster labels to the base labels and filters by prefix if needed
func (cr *VMCluster) FinalLabels(kind ClusterComponent) map[string]string {
	v := AddClusterLabels(cr.SelectorLabels(kind), "vm")
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Labels, v)
	}
	return v
}

// FinalAnnotations returns global annotations to be applied by objects generate for vmcluster
func (cr *VMCluster) FinalAnnotations() map[string]string {
	var v map[string]string
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Annotations, v)
	}
	return v
}

// LastSpecUpdated compares spec with last applied spec stored, replaces old spec and returns true if it's updated
func (cr *VMCluster) LastSpecUpdated() bool {
	updated := cr.Status.LastAppliedSpec == nil || !equality.Semantic.DeepEqual(&cr.Spec, cr.Status.LastAppliedSpec)
	cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
	return updated
}

func (cr *VMCluster) Paused() bool {
	return cr.Spec.Paused
}

// GetMetricsPath returns prefixed path for metric requests
func (cr *VMSelect) GetMetricsPath() string {
	if cr == nil {
		return healthPath
	}
	return BuildPathWithPrefixFlag(cr.ExtraArgs, metricsPath)
}

// UseTLS returns true if TLS is enabled
func (cr *VMSelect) UseTLS() bool {
	return UseTLS(cr.ExtraArgs)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VMSelect) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VMSelect) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// GetMetricsPath returns prefixed path for metric requests
func (cr *VMInsert) GetMetricsPath() string {
	if cr == nil {
		return healthPath
	}
	return BuildPathWithPrefixFlag(cr.ExtraArgs, metricsPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VMInsert) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// UseTLS returns true if TLS is enabled
func (cr *VMInsert) UseTLS() bool {
	return UseTLS(cr.ExtraArgs)
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VMInsert) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// GetMetricsPath returns prefixed path for metric requests
func (cr *VMStorage) GetMetricsPath() string {
	if cr == nil {
		return healthPath
	}
	return BuildPathWithPrefixFlag(cr.ExtraArgs, metricsPath)
}

// UseProxyProtocol implements build.probeCRD interface
func (cr *VMStorage) UseProxyProtocol() bool {
	return UseProxyProtocol(cr.ExtraArgs)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VMStorage) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// UseTLS returns true if TLS is enabled
func (cr *VMStorage) UseTLS() bool {
	return UseTLS(cr.ExtraArgs)
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VMStorage) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// SnapshotCreatePathWithFlags returns url for accessing vmbackupmanager component
func (*VMBackup) SnapshotCreatePathWithFlags(host, port string, extraArgs map[string]string) string {
	return BuildLocalURL(snapshotAuthKeyFlag, host, port, snapshotCreate, extraArgs)
}

// SnapshotDeletePathWithFlags returns url for accessing vmbackupmanager component
func (*VMBackup) SnapshotDeletePathWithFlags(host, port string, extraArgs map[string]string) string {
	return BuildLocalURL(snapshotAuthKeyFlag, host, port, snapshotDelete, extraArgs)
}

// GetServiceAccountName returns service account name for all vmcluster components
func (cr *VMCluster) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName(ClusterComponentRoot)
	}
	return cr.Spec.ServiceAccountName
}

// IsOwnsServiceAccount checks if serviceAccount belongs to the CR
func (cr *VMCluster) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

// findPool returns the pool with the given name, if any.
func (cr *VMCluster) findPool(name string) (*VMClusterPool, bool) {
	for i := range cr.Spec.Pools {
		if cr.Spec.Pools[i].Name == name {
			return &cr.Spec.Pools[i], true
		}
	}
	return nil, false
}

// hasAnyPoolInsert reports whether any pool defines its own dedicated vminsert, which is
// exactly the condition under which the operator skips creating the shared top-level vminsert.
func hasAnyPoolInsert(pools []VMClusterPool) bool {
	for _, p := range pools {
		if p.VMInsert != nil {
			return true
		}
	}
	return false
}

// mergedPoolInsert merges pool.VMInsert over the top-level base, mirroring how the operator
// resolves a pool's own vminsert when reconciling it.
func (cr *VMCluster) mergedPoolInsert(pool *VMClusterPool) (*VMInsert, error) {
	merged := pool.VMInsert.DeepCopy()
	if cr.Spec.VMInsert != nil {
		if err := MergeDeep(merged, cr.Spec.VMInsert, true); err != nil {
			return nil, fmt.Errorf("pool %q vminsert merge: %w", pool.Name, err)
		}
	}
	return merged, nil
}

// mergedPoolStorage merges pool.VMStorage over the top-level base, mirroring how the operator
// resolves a pool's own vmstorage when reconciling it. A pool without its own override falls
// back to the base as-is.
func (cr *VMCluster) mergedPoolStorage(pool *VMClusterPool) (*VMStorage, error) {
	if pool.VMStorage == nil {
		return cr.Spec.VMStorage, nil
	}
	merged := pool.VMStorage.DeepCopy()
	if cr.Spec.VMStorage != nil {
		if err := MergeDeep(merged, cr.Spec.VMStorage, true); err != nil {
			return nil, fmt.Errorf("pool %q vmstorage merge: %w", pool.Name, err)
		}
	}
	return merged, nil
}

// AsURL builds the Service URL for the given cluster component. poolName selects a specific
// entry from spec.pools instead of the shared (non-pool) component; it's only meaningful for
// ClusterComponentInsert and ClusterComponentStorage, since vmselect is never pool-scoped.
// It errors instead of silently returning a URL for a Service the operator wouldn't actually
// create - e.g. the shared vminsert once any pool defines its own, or the top-level vmstorage
// once any pool is defined at all.
func (cr *VMCluster) AsURL(kind ClusterComponent, poolName string, isExtra bool) (string, error) {
	var defaultPort string
	var svcSpec *AdditionalServiceSpec
	var extraArgs map[string]string
	svcName := cr.PrefixedName(kind)
	switch kind {
	case ClusterComponentSelect:
		if poolName != "" {
			return "", fmt.Errorf("vmcluster %q: pool %q is not applicable to vmselect, since vmselect is never pool-scoped", cr.Name, poolName)
		}
		if cr.Spec.VMSelect == nil {
			return "", fmt.Errorf("vmcluster %q has no spec.vmSelect configured", cr.Name)
		}
		defaultPort = "8481"
		if cr.Spec.VMSelect.Port != "" {
			defaultPort = cr.Spec.VMSelect.Port
		}
		svcSpec = cr.Spec.VMSelect.ServiceSpec
		extraArgs = cr.Spec.VMSelect.ExtraArgs
	case ClusterComponentInsert:
		defaultPort = "8480"
		vmi := cr.Spec.VMInsert
		if poolName != "" {
			pool, ok := cr.findPool(poolName)
			if !ok {
				return "", fmt.Errorf("vmcluster %q has no pool named %q", cr.Name, poolName)
			}
			if pool.VMInsert != nil {
				merged, err := cr.mergedPoolInsert(pool)
				if err != nil {
					return "", err
				}
				vmi = merged
				svcName = cr.PoolPrefixedName(kind, poolName)
			}
			// pool has no dedicated vminsert: falls through to the shared top-level one below.
		} else if hasAnyPoolInsert(cr.Spec.Pools) {
			return "", fmt.Errorf("vmcluster %q has per-pool vminsert(s); target a specific pool instead of the shared vminsert", cr.Name)
		}
		if vmi == nil {
			return "", fmt.Errorf("vmcluster %q has no shared spec.vmInsert configured", cr.Name)
		}
		if vmi.Port != "" {
			defaultPort = vmi.Port
		}
		svcSpec = vmi.ServiceSpec
		extraArgs = vmi.ExtraArgs
	case ClusterComponentStorage:
		defaultPort = "8482"
		vms := cr.Spec.VMStorage
		if poolName != "" {
			pool, ok := cr.findPool(poolName)
			if !ok {
				return "", fmt.Errorf("vmcluster %q has no pool named %q", cr.Name, poolName)
			}
			merged, err := cr.mergedPoolStorage(pool)
			if err != nil {
				return "", err
			}
			vms = merged
			svcName = cr.PoolPrefixedName(kind, poolName)
		} else if len(cr.Spec.Pools) > 0 {
			return "", fmt.Errorf("vmcluster %q defines pools; target a specific pool instead of the top-level vmstorage", cr.Name)
		}
		if vms == nil {
			return "", fmt.Errorf("vmcluster %q has no shared spec.vmStorage configured", cr.Name)
		}
		if vms.Port != "" {
			defaultPort = vms.Port
		}
		svcSpec = vms.ServiceSpec
		extraArgs = vms.ExtraArgs
	default:
		panic("BUG unsupported cluster kind=" + string(kind))
	}
	resolvedName, port := ResolveServiceURL(svcName, defaultPort, "http", svcSpec, isExtra)
	return fmt.Sprintf("%s://%s.%s.svc:%s", HTTPProtoFromFlags(extraArgs), resolvedName, cr.Namespace, port), nil
}

func (cr *VMSelect) ProbePath() string {
	return BuildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

func (cr *VMSelect) ProbeScheme() string {
	return strings.ToUpper(HTTPProtoFromFlags(cr.ExtraArgs))
}

func (cr *VMSelect) ProbePort() string {
	return cr.Port
}

func (*VMSelect) ProbeNeedLiveness() bool {
	return true
}

func (cr *VMStorage) ProbePath() string {
	return BuildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

func (cr *VMStorage) ProbeScheme() string {
	return strings.ToUpper(HTTPProtoFromFlags(cr.ExtraArgs))
}

func (cr *VMStorage) ProbePort() string {
	return cr.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VMStorage) ProbeNeedLiveness() bool {
	return false
}

// VMAuthLoadBalancer configures vmauth as a load balancer
// for the requests
type VMAuthLoadBalancer struct {
	Enabled                bool                   `json:"enabled,omitempty"`
	DisableInsertBalancing bool                   `json:"disableInsertBalancing,omitempty"`
	DisableSelectBalancing bool                   `json:"disableSelectBalancing,omitempty"`
	Spec                   VMAuthLoadBalancerSpec `json:"spec,omitempty"`
}

// VMAuthLoadBalancerSpec defines configuration spec for VMAuth used as load-balancer
// for VMCluster component
type VMAuthLoadBalancerSpec struct {
	// ComponentVersion defines default images tag for this component.
	// it can be overwritten with component specific image.tag value.
	// +optional
	ComponentVersion string `json:"componentVersion,omitempty"`
	// Common params for scheduling
	// PodMetadata configures Labels and Annotations which are propagated to the vmauth lb pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// AdditionalServiceSpec defines service override configuration for vmauth lb deployment
	// it'll be only applied to vmclusterlb- service
	AdditionalServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmauthlb VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`

	// LogFormat for vmauth
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for vmauth container.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// NetworkPolicy defines network access rules for pods created by this CR.
	// +optional
	NetworkPolicy *EmbeddedNetworkPolicy `json:"networkPolicy,omitempty"`

	// UpdateStrategy - overrides default update strategy.
	// +kubebuilder:validation:Enum=Recreate;RollingUpdate
	// +optional
	// +notes={available_from: "v0.64.0"}
	UpdateStrategy *appsv1.DeploymentStrategyType `json:"updateStrategy,omitempty"`
	// RollingUpdate - overrides deployment update params.
	// +optional
	// +notes={available_from: "v0.64.0"}
	RollingUpdate *appsv1.RollingUpdateDeployment `json:"rollingUpdate,omitempty"`
	// License configures enterprise features license key
	// +optional
	License *License `json:"license,omitempty"`
	// HPA defines HorizontalPodAutoscaler configuration for vmauth lb deployment
	// +optional
	HPA              *EmbeddedHPA `json:"hpa,omitempty"`
	CommonAppsParams `json:",inline"`
}

// ProbePort returns port for probe requests
func (cr *VMAuthLoadBalancerSpec) ProbePort() string {
	return cr.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VMAuthLoadBalancerSpec) ProbeNeedLiveness() bool {
	return false
}

// ProbePath returns path for probe requests
func (cr *VMAuthLoadBalancerSpec) ProbePath() string {
	return BuildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

// ProbeScheme returns scheme for probe requests
func (cr *VMAuthLoadBalancerSpec) ProbeScheme() string {
	return strings.ToUpper(HTTPProtoFromFlags(cr.ExtraArgs))
}

// UseProxyProtocol implements build.probeCRD interface
func (cr *VMAuthLoadBalancerSpec) UseProxyProtocol() bool {
	return UseProxyProtocol(cr.ExtraArgs)
}

// GetServiceScrape implements build.serviceScrapeBuilder interface
func (cr *VMAuthLoadBalancerSpec) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// GetExtraArgs implements build.serviceScrapeBuilder interface
func (cr *VMAuthLoadBalancerSpec) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// UseTLS returns true if TLS is enabled
func (cr *VMAuthLoadBalancerSpec) UseTLS() bool {
	return UseTLS(cr.ExtraArgs)
}

// GetMetricsPath implements build.serviceScrapeBuilder interface
func (cr *VMAuthLoadBalancerSpec) GetMetricsPath() string {
	return BuildPathWithPrefixFlag(cr.ExtraArgs, metricsPath)
}

// maxVMClusterPoolNameLength must match the kubebuilder MaxLength on VMClusterPool.Name below.
const maxVMClusterPoolNameLength = 16

// VMClusterPool defines a named group of vmstorage (and optionally vminsert) components
// within a VMCluster. Each pool gets its own StatefulSet and headless Service.
// +k8s:openapi-gen=true
type VMClusterPool struct {
	// Name is the unique identifier for this pool within the cluster.
	// Used as a suffix for generated resource names (e.g. vmstorage-<cluster>-<pool>) and as a
	// storage group name in vmselect. Kept short since the cluster name itself isn't length-limited,
	// and generated StatefulSet/Deployment names must still fit Kubernetes' 63-character limit.
	// Must be a lowercase alphanumeric DNS label; hyphens allowed in the interior.
	// +kubebuilder:validation:Pattern:="^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"
	// +kubebuilder:validation:MaxLength=16
	Name string `json:"name"`
	// VMStorage defines pool-specific vmstorage configuration.
	// Each field overrides the corresponding field in the top-level vmstorage spec.
	// Fields absent here inherit from the top-level vmstorage.
	// RetentionPeriod on VMStorage overrides the cluster-level retentionPeriod for this pool.
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	VMStorage *VMStorage `json:"vmstorage,omitempty"`
	// VMInsert defines a dedicated vminsert for this pool.
	// Each field overrides the corresponding field in the top-level vminsert spec.
	// When nil, the top-level shared vminsert writes to this pool's storage nodes as well.
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	VMInsert *VMInsert `json:"vminsert,omitempty"`
}

// PoolPrefixedName returns the Kubernetes resource name for the given component in a pool.
func (cr *VMCluster) PoolPrefixedName(kind ClusterComponent, poolName string) string {
	return ClusterPrefixedName(kind, cr.Name, "vm", false) + "-" + poolName
}
