package v1beta1

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMAgentSpec defines the desired state of VMAgent
// +k8s:openapi-gen=true
type VMAgentSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// PodMetadata configures Labels and Annotations which are propagated to the vmagent pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *ManagedObjectsMetadata `json:"managedMetadata,omitempty"`
	// LogLevel for VMAgent to be configured with.
	// INFO, WARN, ERROR, FATAL, PANIC
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VMAgent to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`

	// APIServerConfig allows specifying a host and auth methods to access apiserver.
	// If left empty, VMAgent is assumed to run inside of the cluster
	// and will discover API servers automatically and use the pod's CA certificate
	// and bearer token file at /var/run/secrets/kubernetes.io/serviceaccount/.
	// +optional
	APIServerConfig *APIServerConfig `json:"apiServerConfig,omitempty"`

	// RemoteWrite list of victoria metrics /some other remote write system
	// for vm it must looks like: http://victoria-metrics-single:8428/api/v1/write
	// or for cluster different url
	// https://docs.victoriametrics.com/victoriametrics/vmagent/#splitting-data-streams-among-multiple-systems
	RemoteWrite []VMAgentRemoteWriteSpec `json:"remoteWrite"`
	// RemoteWriteSettings defines global settings for all remoteWrite urls.
	// +optional
	RemoteWriteSettings *VMAgentRemoteWriteSettings `json:"remoteWriteSettings,omitempty"`
	// StreamAggrConfig defines global stream aggregation configuration for VMAgent
	// +optional
	StreamAggrConfig *StreamAggrConfig `json:"streamAggrConfig,omitempty"`
	// InsertPorts - additional listen ports for data ingestion.
	InsertPorts *InsertPorts `json:"insertPorts,omitempty"`

	// ServiceSpec that will be added to vmagent service spec
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmagent VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`

	// ShardCount - numbers of shards of VMAgent
	// in this case operator will use 1 deployment/sts per shard with
	// replicas count according to spec.replicas,
	// see [here](https://docs.victoriametrics.com/victoriametrics/vmagent/#scraping-big-number-of-targets)
	// +optional
	ShardCount *int `json:"shardCount,omitempty"`

	// UpdateStrategy - overrides default update strategy.
	// works only for deployments, statefulset always use OnDelete.
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
	// DaemonSetMode enables DaemonSet deployment mode instead of Deployment.
	// Supports only VMPodScrape
	// (available from v0.55.0).
	// Cannot be used with statefulMode
	// +optional
	DaemonSetMode bool `json:"daemonSetMode,omitempty"`
	// StatefulMode enables StatefulSet for `VMAgent` instead of Deployment
	// it allows using persistent storage for vmagent's persistentQueue
	// +optional
	StatefulMode bool `json:"statefulMode,omitempty"`
	// StatefulStorage configures storage for StatefulSet
	// +optional
	StatefulStorage *StorageSpec `json:"statefulStorage,omitempty"`
	// StatefulRollingUpdateStrategy allows configuration for strategyType
	// set it to RollingUpdate for disabling operator statefulSet rollingUpdate
	// +optional
	StatefulRollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"statefulRollingUpdateStrategy,omitempty"`
	// PersistentVolumeClaimRetentionPolicy allows configuration of PVC retention policy
	// +optional
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`

	// ClaimTemplates allows adding additional VolumeClaimTemplates for VMAgent in StatefulMode
	ClaimTemplates []corev1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`
	// IngestOnlyMode switches vmagent into unmanaged mode
	// it disables any config generation for scraping
	// Currently it prevents vmagent from managing tls and auth options for remote write
	// +optional
	IngestOnlyMode bool `json:"ingestOnlyMode,omitempty"`

	// License allows to configure license key to be used for enterprise features.
	// Using license key is supported starting from VictoriaMetrics v1.94.0.
	// See [here](https://docs.victoriametrics.com/victoriametrics/enterprise/)
	// +optional
	License *License `json:"license,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	CommonRelabelParams               `json:",inline,omitempty"`
	CommonScrapeParams                `json:",inline,omitempty"`
	CommonDefaultableParams           `json:",inline,omitempty"`
	CommonConfigReloaderParams        `json:",inline,omitempty"`
	CommonApplicationDeploymentParams `json:",inline,omitempty"`
}

// SetLastSpec implements objectWithLastAppliedState interface
func (cr *VMAgent) SetLastSpec(prevSpec VMAgentSpec) {
	cr.ParsedLastAppliedSpec = &prevSpec
}

func (cr *VMAgent) Validate() error {
	if MustSkipCRValidation(cr) {
		return nil
	}
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.Name == cr.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", cr.PrefixedName())
	}
	if len(cr.Spec.RemoteWrite) == 0 {
		return fmt.Errorf("spec.remoteWrite cannot be empty array, provide at least one remoteWrite")
	}
	if cr.Spec.InlineScrapeConfig != "" {
		var inlineCfg yaml.MapSlice
		if err := yaml.Unmarshal([]byte(cr.Spec.InlineScrapeConfig), &inlineCfg); err != nil {
			return fmt.Errorf("bad cr.spec.inlineScrapeConfig it must be valid yaml, err :%w", err)
		}
	}
	if len(cr.Spec.InlineRelabelConfig) > 0 {
		if err := checkRelabelConfigs(cr.Spec.InlineRelabelConfig); err != nil {
			return err
		}
	}
	for idx, rw := range cr.Spec.RemoteWrite {
		if rw.URL == "" {
			return fmt.Errorf("remoteWrite.url cannot be empty at idx: %d", idx)
		}
		if len(rw.InlineUrlRelabelConfig) > 0 {
			if err := checkRelabelConfigs(rw.InlineUrlRelabelConfig); err != nil {
				return fmt.Errorf("bad urlRelabelingConfig at idx: %d, err: %w", idx, err)
			}
		}
	}
	if cr.Spec.DaemonSetMode && cr.Spec.StatefulMode {
		return fmt.Errorf("daemonSetMode and statefulMode cannot be used in the same time")
	}
	if cr.Spec.DaemonSetMode {
		if cr.Spec.PodDisruptionBudget != nil {
			return fmt.Errorf("podDisruptionBudget cannot be used with daemonSetMode")
		}
		if cr.Spec.EnableKubernetesAPISelectors {
			return fmt.Errorf("enableKubernetesAPISelectors cannot be used with daemonSetMode")
		}
	}
	scrapeClassNames := make(map[string]struct{})
	defaultScrapeClass := false
	for _, sc := range cr.Spec.ScrapeClasses {
		if _, ok := scrapeClassNames[sc.Name]; ok {
			return fmt.Errorf("duplicate scrape class name %q", sc.Name)
		}
		if ptr.Deref(sc.Default, false) {
			if defaultScrapeClass {
				return fmt.Errorf("multiple default scrape classes defined")
			}
			defaultScrapeClass = true
		}
		if sc.TLSConfig != nil {
			if err := sc.TLSConfig.Validate(); err != nil {
				return fmt.Errorf("incorrect tlsConfig for scrape class %q: %w", sc.Name, err)
			}
		}
		if err := sc.OAuth2.validate(); err != nil {
			return fmt.Errorf("incorrect oauth2 for scrape class %q: %w", sc.Name, err)
		}
		if err := sc.Authorization.validate(); err != nil {
			return fmt.Errorf("incorrect authorization for scrape class %q:: %w", sc.Name, err)
		}
		if err := sc.validate(); err != nil {
			return fmt.Errorf("incorrect relabeling for scrape class %q:: %w", sc.Name, err)
		}
	}
	return nil
}

// IsSharded returns true if sharding is enabled
func (cr *VMAgent) IsSharded() bool {
	return cr != nil && cr.Spec.ShardCount != nil && *cr.Spec.ShardCount > 1 && !cr.Spec.DaemonSetMode
}

// GetShardCount returns shard count for vmagent
func (cr *VMAgent) GetShardCount() int {
	if !cr.IsSharded() {
		return 1
	}
	return *cr.Spec.ShardCount
}

// GetReloadURL implements reloadable interface
func (cr *VMAgent) GetReloadURL(host string) string {
	return BuildLocalURL(reloadAuthKey, host, cr.Spec.Port, reloadPath, cr.Spec.ExtraArgs)
}

// GetReloaderParams implements reloadable interface
func (cr *VMAgent) GetReloaderParams() *CommonConfigReloaderParams {
	return &cr.Spec.CommonConfigReloaderParams
}

// UseProxyProtocol implements reloadable interface
func (cr *VMAgent) UseProxyProtocol() bool {
	if v, ok := cr.Spec.ExtraArgs["httpListenAddr.useProxyProtocol"]; ok && v == "true" {
		return true
	}
	return false
}

// AutomountServiceAccountToken implements reloadable interface
func (cr *VMAgent) AutomountServiceAccountToken() bool {
	return !cr.Spec.DisableAutomountServiceAccountToken
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAgent) UnmarshalJSON(src []byte) error {
	type pcr VMAgent
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	if err := ParseLastAppliedStateTo(cr); err != nil {
		return err
	}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAgentSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAgentSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vmagent spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// VMAgentRemoteWriteSettings - defines global settings for all remoteWrite urls.
type VMAgentRemoteWriteSettings struct {
	// The maximum size in bytes of unpacked request to send to remote storage
	// +optional
	MaxBlockSize *int32 `json:"maxBlockSize,omitempty"`

	// The maximum file-based buffer size in bytes at -remoteWrite.tmpDataPath
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	MaxDiskUsagePerURL *BytesString `json:"maxDiskUsagePerURL,omitempty"`
	// The number of concurrent queues
	// +optional
	Queues *int32 `json:"queues,omitempty"`
	// Whether to show -remoteWrite.url in the exported metrics. It is hidden by default, since it can contain sensitive auth info
	// +optional
	ShowURL *bool `json:"showURL,omitempty"`
	// Path to directory where temporary data for remote write component is stored (default vmagent-remotewrite-data)
	// +optional
	TmpDataPath *string `json:"tmpDataPath,omitempty"`
	// Interval for flushing the data to remote storage. (default 1s)
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	FlushInterval *string `json:"flushInterval,omitempty"`
	// Labels in the form 'name=value' to add to all the metrics before sending them. This overrides the label if it already exists.
	// +optional
	Labels map[string]string `json:"label,omitempty"`
	// Configures vmagent accepting data via the same multitenant endpoints as vminsert at VictoriaMetrics cluster does,
	// see [here](https://docs.victoriametrics.com/victoriametrics/vmagent/#multitenancy).
	// it's global setting and affects all remote storage configurations
	// +optional
	UseMultiTenantMode bool `json:"useMultiTenantMode,omitempty"`
}

type ScrapeClass struct {
	// name of the scrape class.
	//
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// default defines that the scrape applies to all scrape objects that
	// don't configure an explicit scrape class name.
	//
	// Only one scrape class can be set as the default.
	//
	// +optional
	Default *bool `json:"default,omitempty"`

	EndpointAuth        `json:",inline"`
	EndpointRelabelings `json:",inline"`

	// AttachMetadata defines additional metadata to the discovered targets.
	// When the scrape object defines its own configuration, it takes
	// precedence over the scrape class configuration.
	// +optional
	AttachMetadata *AttachMetadata `json:"attachMetadata,omitempty"`
}

// AWS defines AWS cloud auth specific params
type AWS struct {
	// EC2Endpoint is an optional AWS EC2 API endpoint to use for the corresponding -remoteWrite.url if -remoteWrite.aws.useSigv4 is set
	EC2Endpoint string `json:"ec2Endpoint,omitempty"`
	// Region is an optional AWS region to use for the corresponding -remoteWrite.url if -remoteWrite.aws.useSigv4 is set
	Region string `json:"region,omitempty"`
	// RoleARN is an optional AWS region to use for the corresponding -remoteWrite.url if -remoteWrite.aws.useSigv4 is set
	RoleARN string `json:"roleARN,omitempty"`
	// Service is an optional AWS Service to use for the corresponding -remoteWrite.url if -remoteWrite.aws.useSigv4 is set
	Service string `json:"service,omitempty"`
	// STSEndpoint is an optional AWS STS API endpoint to use for the corresponding -remoteWrite.url if -remoteWrite.aws.useSigv4 is set
	STSEndpoint string `json:"stsEndpoint,omitempty"`
	// UseSigv4 enables SigV4 request signing for the corresponding -remoteWrite.url
	UseSigv4 bool `json:"useSigv4,omitempty"`
}

// VMAgentRemoteWriteSpec defines the remote storage configuration for VmAgent
// +k8s:openapi-gen=true
type VMAgentRemoteWriteSpec struct {
	// URL of the endpoint to send samples to.
	URL string `json:"url"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Optional bearer auth token to use for -remoteWrite.url
	// +optional
	BearerTokenSecret *corev1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`

	// ConfigMap with relabeling config which is applied to metrics before sending them to the corresponding -remoteWrite.url
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Key at Configmap with relabelConfig for remoteWrite",xDescriptors="urn:alm:descriptor:io.kubernetes:ConfigMapKeySelector"
	UrlRelabelConfig *corev1.ConfigMapKeySelector `json:"urlRelabelConfig,omitempty"`
	// InlineUrlRelabelConfig defines relabeling config for remoteWriteURL, it can be defined at crd spec.
	// +optional
	InlineUrlRelabelConfig []*RelabelConfig `json:"inlineUrlRelabelConfig,omitempty"`
	// OAuth2 defines auth configuration
	// +optional
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`
	// TLSConfig describes tls configuration for remote write target
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// Timeout for sending a single block of data to -remoteWrite.url (default 1m0s)
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	SendTimeout *string `json:"sendTimeout,omitempty"`
	// Headers allow configuring custom http headers
	// Must be in form of semicolon separated header with value
	// e.g.
	// headerName: headerValue
	// vmagent supports since 1.79.0 version
	// +optional
	Headers []string `json:"headers,omitempty"`
	// StreamAggrConfig defines stream aggregation configuration for VMAgent for -remoteWrite.url
	// +optional
	StreamAggrConfig *StreamAggrConfig `json:"streamAggrConfig,omitempty"`
	// MaxDiskUsage defines the maximum file-based buffer size in bytes for the given remoteWrite
	// It overrides global configuration defined at remoteWriteSettings.maxDiskUsagePerURL
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	MaxDiskUsage *BytesString `json:"maxDiskUsage,omitempty"`
	// ForceVMProto forces using VictoriaMetrics protocol for sending data to -remoteWrite.url
	// +optional
	ForceVMProto bool `json:"forceVMProto,omitempty"`
	// ProxyURL for -remoteWrite.url. Supported proxies: http, https, socks5. Example: socks5://proxy:1234
	// +optional
	ProxyURL *string `json:"proxyURL,omitempty"`
	// AWS describes params specific to AWS cloud
	AWS *AWS `json:"aws,omitempty"`
}

// AsConfigMapKey key for kubernetes configmap
func (*VMAgentRemoteWriteSpec) AsConfigMapKey(idx int, suffix string) string {
	return fmt.Sprintf("RWS_%d-CM-%s", idx, strings.ToUpper(suffix))
}

// VMAgentStatus defines the observed state of VMAgent
// +k8s:openapi-gen=true
type VMAgentStatus struct {
	// Shards represents total number of vmagent deployments with uniq scrape targets
	Shards int32 `json:"shards,omitempty"`
	// Selector string form of label value set for autoscaling
	Selector string `json:"selector,omitempty"`
	// ReplicaCount Total number of pods targeted by this VMAgent
	Replicas       int32 `json:"replicas,omitempty"`
	StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VMAgentStatus) GetStatusMetadata() *StatusMetadata {
	return &cr.StatusMetadata
}

// +genclient

// VMAgent - is a tiny but brave agent, which helps you collect metrics from various sources and stores them in VictoriaMetrics
// or any other Prometheus-compatible storage system that supports the remote_write protocol.
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMAgent App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmagents,scope=Namespaced
// +kubebuilder:subresource:scale:specpath=.spec.shardCount,statuspath=.status.shards,selectorpath=.status.selector
// +kubebuilder:printcolumn:name="Shards Count",type="integer",JSONPath=".status.shards",description="current number of shards"
// +kubebuilder:printcolumn:name="Replica Count",type="integer",JSONPath=".status.replicas",description="current number of replicas"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of update rollout"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VMAgent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VMAgentSpec `json:"spec,omitempty"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VMAgentSpec `json:"-" yaml:"-"`

	Status VMAgentStatus `json:"status,omitempty"`
}

// VMAgentList contains a list of VMAgent
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VMAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAgent `json:"items"`
}

// AsOwner returns owner references with current object as owner
func (cr *VMAgent) AsOwner() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         cr.APIVersion,
		Kind:               cr.Kind,
		Name:               cr.Name,
		UID:                cr.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
}

func (cr *VMAgent) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMAgent) GetStatus() *VMAgentStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMAgent) DefaultStatusFields(vs *VMAgentStatus) {
	replicaCount := int32(0)
	if cr.Spec.ReplicaCount != nil {
		replicaCount = *cr.Spec.ReplicaCount
	}
	var shardCnt int32
	if cr.IsSharded() {
		shardCnt = int32(*cr.Spec.ShardCount)
	}
	vs.Replicas = replicaCount
	vs.Shards = shardCnt
	vs.Selector = labels.SelectorFromSet(cr.SelectorLabels()).String()
}

func (cr *VMAgent) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmagent",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr *VMAgent) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}

	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

// FinalLabels returns combination of selector and managed labels
func (cr *VMAgent) FinalLabels() map[string]string {
	v := cr.SelectorLabels()
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Labels, v)
	}
	return v
}

// FinalAnnotations returns annotations to be applied for created objects
func (cr *VMAgent) FinalAnnotations() map[string]string {
	var v map[string]string
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Annotations, v)
	}
	return v
}

func (cr *VMAgent) PrefixedName() string {
	return fmt.Sprintf("vmagent-%s", cr.Name)
}

func (cr *VMAgent) HealthPath() string {
	return BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VMAgent) GetMetricPath() string {
	return BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VMAgent) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VMAgent) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

func (cr *VMAgent) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

// IsOwnsServiceAccount checks if serviceAccount belongs to the CR
func (cr *VMAgent) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

func (cr *VMAgent) GetClusterRoleName() string {
	return fmt.Sprintf("monitoring:%s:vmagent-%s", cr.Namespace, cr.Name)
}

// AsURL - returns url for http access
func (cr *VMAgent) AsURL() string {
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
	return fmt.Sprintf("%s://%s.%s.svc:%s", HTTPProtoFromFlags(cr.Spec.ExtraArgs), cr.PrefixedName(), cr.Namespace, port)
}

// AsCRDOwner implements interface
func (*VMAgent) AsCRDOwner() *metav1.OwnerReference {
	return GetCRDAsOwner(VMAgentCRD)
}

func (cr *VMAgent) Probe() *EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

func (cr *VMAgent) ProbePath() string {
	return BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

func (cr *VMAgent) ProbeScheme() string {
	return strings.ToUpper(HTTPProtoFromFlags(cr.Spec.ExtraArgs))
}

func (cr *VMAgent) ProbePort() string {
	return cr.Spec.Port
}

func (*VMAgent) ProbeNeedLiveness() bool {
	return true
}

// IsUnmanaged checks if object should managed any config objects
func (cr *VMAgent) IsUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return cr.Spec.isUnmanaged()
}

// IsNodeScrapeUnmanaged checks if vmagent should managed any VMNodeScrape objects
func (cr *VMAgent) IsNodeScrapeUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return cr.Spec.isNodeScrapeUnmanaged()
}

// IsServiceScrapeUnmanaged checks if vmagent should managed any VMServiceScrape objects
func (cr *VMAgent) IsServiceScrapeUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return cr.Spec.isServiceScrapeUnmanaged()
}

// IsUnmanaged checks if vmagent should managed any VMPodScrape objects
func (cr *VMAgent) IsPodScrapeUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return cr.Spec.isPodScrapeUnmanaged()
}

// IsProbeUnmanaged checks if vmagent should managed any VMProbe objects
func (cr *VMAgent) IsProbeUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return cr.Spec.isProbeUnmanaged()
}

// IsStaticScrapeUnmanaged checks if vmagent should managed any VMStaticScrape objects
func (cr *VMAgent) IsStaticScrapeUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return cr.Spec.isStaticScrapeUnmanaged()
}

// IsScrapeConfigUnmanaged checks if vmagent should managed any VMScrapeConfig objects
func (cr *VMAgent) IsScrapeConfigUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return cr.Spec.isScrapeConfigUnmanaged()
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VMAgent) LastAppliedSpecAsPatch() (client.Patch, error) {
	return LastAppliedChangesAsPatch(cr.Spec)
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (cr *VMAgent) HasSpecChanges() (bool, error) {
	return HasStateChanges(cr.ObjectMeta, cr.Spec)
}

func (cr *VMAgent) Paused() bool {
	return cr.Spec.Paused
}

// HasAnyRelabellingConfigs checks if vmagent has any defined relabeling rules
func (cr *VMAgent) HasAnyRelabellingConfigs() bool {
	if cr.Spec.HasAnyRelabellingConfigs() {
		return true
	}
	for _, rw := range cr.Spec.RemoteWrite {
		if rw.UrlRelabelConfig != nil || len(rw.InlineUrlRelabelConfig) > 0 {
			return true
		}
	}

	return false
}

// HasAnyStreamAggrRule checks if vmagent has any defined aggregation rules
func (cr *VMAgent) HasAnyStreamAggrRule() bool {
	if cr.Spec.StreamAggrConfig.HasAnyRule() {
		return true
	}
	for _, rw := range cr.Spec.RemoteWrite {
		if rw.StreamAggrConfig.HasAnyRule() {
			return true
		}
	}

	return false
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMAgent) GetAdditionalService() *AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

func checkRelabelConfigs(src []*RelabelConfig) error {
	// TODO: restore check when issue will be fixed at golang
	// https://github.com/VictoriaMetrics/VictoriaMetrics/issues/6911
	return nil
}

// APIServerConfig defines a host and auth methods to access apiserver.
// +k8s:openapi-gen=true
type APIServerConfig struct {
	// Host of apiserver.
	// A valid string consisting of a hostname or IP followed by an optional port number
	Host string `json:"host"`
	// BasicAuth allow an endpoint to authenticate over basic authentication
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// Bearer token for accessing apiserver.
	// +optional
	BearerToken string `json:"bearerToken,omitempty"`
	// File to read bearer token for accessing apiserver.
	// +optional
	BearerTokenFile string `json:"bearerTokenFile,omitempty"`
	// TLSConfig Config to use for accessing apiserver.
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"`
}

func init() {
	SchemeBuilder.Register(&VMAgent{}, &VMAgentList{})
}
