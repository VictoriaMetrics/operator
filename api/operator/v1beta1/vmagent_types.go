package v1beta1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMAgentSecurityEnforcements defines security configuration for endpoint scrapping
type VMAgentSecurityEnforcements struct {
	// OverrideHonorLabels if set to true overrides all user configured honor_labels.
	// If HonorLabels is set in scrape objects  to true, this overrides honor_labels to false.
	// +optional
	OverrideHonorLabels bool `json:"overrideHonorLabels,omitempty"`
	// OverrideHonorTimestamps allows to globally enforce honoring timestamps in all scrape configs.
	// +optional
	OverrideHonorTimestamps bool `json:"overrideHonorTimestamps,omitempty"`
	// IgnoreNamespaceSelectors if set to true will ignore NamespaceSelector settings from
	// scrape objects, and they will only discover endpoints
	// within their current namespace.  Defaults to false.
	// +optional
	IgnoreNamespaceSelectors bool `json:"ignoreNamespaceSelectors,omitempty"`
	// EnforcedNamespaceLabel enforces adding a namespace label of origin for each alert
	// and metric that is user created. The label value will always be the namespace of the object that is
	// being created.
	// +optional
	EnforcedNamespaceLabel string `json:"enforcedNamespaceLabel,omitempty"`
	// ArbitraryFSAccessThroughSMs configures whether configuration
	// based on EndpointAuth can access arbitrary files on the file system
	// of the VMAgent container e.g. bearer token files, basic auth, tls certs
	// +optional
	ArbitraryFSAccessThroughSMs ArbitraryFSAccessThroughSMsConfig `json:"arbitraryFSAccessThroughSMs,omitempty"`
}

// VMAgentSpec defines the desired state of VMAgent
// +k8s:openapi-gen=true
type VMAgentSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// PodMetadata configures Labels and Annotations which are propagated to the vmagent pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogLevel for VMAgent to be configured with.
	// INFO, WARN, ERROR, FATAL, PANIC
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VMAgent to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`

	// ScrapeInterval defines how often scrape targets by default
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	ScrapeInterval string `json:"scrapeInterval,omitempty"`
	// ScrapeTimeout defines global timeout for targets scrape
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	ScrapeTimeout string `json:"scrapeTimeout,omitempty"`

	// APIServerConfig allows specifying a host and auth methods to access apiserver.
	// If left empty, VMAgent is assumed to run inside of the cluster
	// and will discover API servers automatically and use the pod's CA certificate
	// and bearer token file at /var/run/secrets/kubernetes.io/serviceaccount/.
	// +optional
	APIServerConfig *APIServerConfig `json:"aPIServerConfig,omitempty"`
	// VMAgentExternalLabelName Name of vmAgent external label used to denote vmAgent instance
	// name. Defaults to the value of `prometheus`. External label will
	// _not_ be added when value is set to empty string (`""`).
	// +optional
	VMAgentExternalLabelName *string `json:"vmAgentExternalLabelName,omitempty"`

	// ExternalLabels The labels to add to any time series scraped by vmagent.
	// it doesn't affect metrics ingested directly by push API's
	// +optional
	ExternalLabels map[string]string `json:"externalLabels,omitempty"`
	// RemoteWrite list of victoria metrics /some other remote write system
	// for vm it must looks like: http://victoria-metrics-single:8429/api/v1/write
	// or for cluster different url
	// https://github.com/VictoriaMetrics/VictoriaMetrics/tree/master/app/vmagent#splitting-data-streams-among-multiple-systems
	RemoteWrite []VMAgentRemoteWriteSpec `json:"remoteWrite"`
	// RemoteWriteSettings defines global settings for all remoteWrite urls.
	// +optional
	RemoteWriteSettings *VMAgentRemoteWriteSettings `json:"remoteWriteSettings,omitempty"`
	// RelabelConfig ConfigMap with global relabel config -remoteWrite.relabelConfig
	// This relabeling is applied to all the collected metrics before sending them to remote storage.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Key at Configmap with relabelConfig name",xDescriptors="urn:alm:descriptor:io.kubernetes:ConfigMapKeySelector"
	RelabelConfig *v1.ConfigMapKeySelector `json:"relabelConfig,omitempty"`
	// InlineRelabelConfig - defines GlobalRelabelConfig for vmagent, can be defined directly at CRD.
	// +optional
	InlineRelabelConfig []RelabelConfig `json:"inlineRelabelConfig,omitempty"`
	// StreamAggrConfig defines global stream aggregation configuration for VMAgent
	// +optional
	StreamAggrConfig *StreamAggrConfig `json:"streamAggrConfig,omitempty"`
	// SelectAllByDefault changes default behavior for empty CRD selectors, such ServiceScrapeSelector.
	// with selectAllByDefault: true and empty serviceScrapeSelector and ServiceScrapeNamespaceSelector
	// Operator selects all exist serviceScrapes
	// with selectAllByDefault: false - selects nothing
	// +optional
	SelectAllByDefault bool `json:"selectAllByDefault,omitempty"`
	// ServiceScrapeSelector defines ServiceScrapes to be selected for target discovery.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ServiceScrapeSelector *metav1.LabelSelector `json:"serviceScrapeSelector,omitempty"`
	// ServiceScrapeNamespaceSelector Namespaces to be selected for VMServiceScrape discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ServiceScrapeNamespaceSelector *metav1.LabelSelector `json:"serviceScrapeNamespaceSelector,omitempty"`
	// PodScrapeSelector defines PodScrapes to be selected for target discovery.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	PodScrapeSelector *metav1.LabelSelector `json:"podScrapeSelector,omitempty"`
	// PodScrapeNamespaceSelector defines Namespaces to be selected for VMPodScrape discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	PodScrapeNamespaceSelector *metav1.LabelSelector `json:"podScrapeNamespaceSelector,omitempty"`
	// ProbeSelector defines VMProbe to be selected for target probing.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ProbeSelector *metav1.LabelSelector `json:"probeSelector,omitempty"`
	// ProbeNamespaceSelector defines Namespaces to be selected for VMProbe discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ProbeNamespaceSelector *metav1.LabelSelector `json:"probeNamespaceSelector,omitempty"`
	// NodeScrapeSelector defines VMNodeScrape to be selected for scraping.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	NodeScrapeSelector *metav1.LabelSelector `json:"nodeScrapeSelector,omitempty"`
	// NodeScrapeNamespaceSelector defines Namespaces to be selected for VMNodeScrape discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	NodeScrapeNamespaceSelector *metav1.LabelSelector `json:"nodeScrapeNamespaceSelector,omitempty"`
	// StaticScrapeSelector defines VMStaticScrape to be selected for target discovery.
	// Works in combination with NamespaceSelector.
	// If both nil - match everything.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// +optional
	StaticScrapeSelector *metav1.LabelSelector `json:"staticScrapeSelector,omitempty"`
	// StaticScrapeNamespaceSelector defines Namespaces to be selected for VMStaticScrape discovery.
	// Works in combination with NamespaceSelector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	StaticScrapeNamespaceSelector *metav1.LabelSelector `json:"staticScrapeNamespaceSelector,omitempty"`
	// ScrapeConfigSelector defines VMScrapeConfig to be selected for target discovery.
	// Works in combination with NamespaceSelector.
	// +optional
	ScrapeConfigSelector *metav1.LabelSelector `json:"scrapeConfigSelector,omitempty"`
	// ScrapeConfigNamespaceSelector defines Namespaces to be selected for VMScrapeConfig discovery.
	// Works in combination with Selector.
	// NamespaceSelector nil - only objects at VMAgent namespace.
	// Selector nil - only objects at NamespaceSelector namespaces.
	// If both nil - behaviour controlled by selectAllByDefault
	// +optional
	ScrapeConfigNamespaceSelector *metav1.LabelSelector `json:"scrapeConfigNamespaceSelector,omitempty"`
	// InlineScrapeConfig As scrape configs are appended, the user is responsible to make sure it
	// is valid. Note that using this feature may expose the possibility to
	// break upgrades of VMAgent. It is advised to review VMAgent release
	// notes to ensure that no incompatible scrape configs are going to break
	// VMAgent after the upgrade.
	// it should be defined as single yaml file.
	// inlineScrapeConfig: |
	//     - job_name: "prometheus"
	//       static_configs:
	//       - targets: ["localhost:9090"]
	// +optional
	InlineScrapeConfig string `json:"inlineScrapeConfig,omitempty"`
	// AdditionalScrapeConfigs As scrape configs are appended, the user is responsible to make sure it
	// is valid. Note that using this feature may expose the possibility to
	// break upgrades of VMAgent. It is advised to review VMAgent release
	// notes to ensure that no incompatible scrape configs are going to break
	// VMAgent after the upgrade.
	// +optional
	AdditionalScrapeConfigs *v1.SecretKeySelector `json:"additionalScrapeConfigs,omitempty"`
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
	// see [here](https://docs.victoriametrics.com/vmagent/#scraping-big-number-of-targets)
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
	// ServiceScrapeRelabelTemplate defines relabel config, that will be added to each VMServiceScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	ServiceScrapeRelabelTemplate []*RelabelConfig `json:"serviceScrapeRelabelTemplate,omitempty"`
	// PodScrapeRelabelTemplate defines relabel config, that will be added to each VMPodScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	PodScrapeRelabelTemplate []*RelabelConfig `json:"podScrapeRelabelTemplate,omitempty"`
	// NodeScrapeRelabelTemplate defines relabel config, that will be added to each VMNodeScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	NodeScrapeRelabelTemplate []*RelabelConfig `json:"nodeScrapeRelabelTemplate,omitempty"`
	// StaticScrapeRelabelTemplate defines relabel config, that will be added to each VMStaticScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	StaticScrapeRelabelTemplate []*RelabelConfig `json:"staticScrapeRelabelTemplate,omitempty"`
	// ProbeScrapeRelabelTemplate defines relabel config, that will be added to each VMProbeScrape.
	// it's useful for adding specific labels to all targets
	// +optional
	ProbeScrapeRelabelTemplate []*RelabelConfig `json:"probeScrapeRelabelTemplate,omitempty"`
	// ScrapeConfigRelabelTemplate defines relabel config, that will be added to each VMScrapeConfig.
	// it's useful for adding specific labels to all targets
	// +optional
	ScrapeConfigRelabelTemplate []*RelabelConfig `json:"scrapeConfigRelabelTemplate,omitempty"`
	// MinScrapeInterval allows limiting minimal scrape interval for VMServiceScrape, VMPodScrape and other scrapes
	// If interval is lower than defined limit, `minScrapeInterval` will be used.
	MinScrapeInterval *string `json:"minScrapeInterval,omitempty"`
	// MaxScrapeInterval allows limiting maximum scrape interval for VMServiceScrape, VMPodScrape and other scrapes
	// If interval is higher than defined limit, `maxScrapeInterval` will be used.
	MaxScrapeInterval *string `json:"maxScrapeInterval,omitempty"`
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

	// ClaimTemplates allows adding additional VolumeClaimTemplates for VMAgent in StatefulMode
	ClaimTemplates []v1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`
	// IngestOnlyMode switches vmagent into unmanaged mode
	// it disables any config generation for scraping
	// Currently it prevents vmagent from managing tls and auth options for remote write
	// +optional
	IngestOnlyMode bool `json:"ingestOnlyMode,omitempty"`

	// License allows to configure license key to be used for enterprise features.
	// Using license key is supported starting from VictoriaMetrics v1.94.0.
	// See [here](https://docs.victoriametrics.com/enterprise)
	// +optional
	License *License `json:"license,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	VMAgentSecurityEnforcements       `json:",inline"`
	CommonDefaultableParams           `json:",inline,omitempty"`
	CommonConfigReloaderParams        `json:",inline,omitempty"`
	CommonApplicationDeploymentParams `json:",inline,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAgent) UnmarshalJSON(src []byte) error {
	type pcr VMAgent
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	prev, err := parseLastAppliedSpec[VMAgentSpec](cr)
	if err != nil {
		return err
	}
	cr.ParsedLastAppliedSpec = prev
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
	MaxDiskUsagePerURL *int64 `json:"maxDiskUsagePerURL,omitempty"`
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
	// see [here](https://docs.victoriametrics.com/vmagent/#multitenancy).
	// it's global setting and affects all remote storage configurations
	// +optional
	UseMultiTenantMode bool `json:"useMultiTenantMode,omitempty"`
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
	BearerTokenSecret *v1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`

	// ConfigMap with relabeling config which is applied to metrics before sending them to the corresponding -remoteWrite.url
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Key at Configmap with relabelConfig for remoteWrite",xDescriptors="urn:alm:descriptor:io.kubernetes:ConfigMapKeySelector"
	UrlRelabelConfig *v1.ConfigMapKeySelector `json:"urlRelabelConfig,omitempty"`
	// InlineUrlRelabelConfig defines relabeling config for remoteWriteURL, it can be defined at crd spec.
	// +optional
	InlineUrlRelabelConfig []RelabelConfig `json:"inlineUrlRelabelConfig,omitempty"`
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
	// MaxDiskUsage defines the maximum file-based buffer size in bytes for -remoteWrite.url
	// +optional
	MaxDiskUsage *string `json:"maxDiskUsage,omitempty"`
	// ForceVMProto forces using VictoriaMetrics protocol for sending data to -remoteWrite.url
	// +optional
	ForceVMProto bool `json:"forceVMProto,omitempty"`
}

// AsMapKey key for internal cache map
func (rw *VMAgentRemoteWriteSpec) AsMapKey() string {
	return fmt.Sprintf("remoteWrite-%s", rw.URL)
}

// AsSecretKey key for kubernetes secret data
func (rw *VMAgentRemoteWriteSpec) AsSecretKey(idx int, suffix string) string {
	return fmt.Sprintf("RWS_%d-SECRET-%s", idx, strings.ToUpper(suffix))
}

// AsConfigMapKey key for kubernetes configmap
func (rw *VMAgentRemoteWriteSpec) AsConfigMapKey(idx int, suffix string) string {
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
	Replicas int32 `json:"replicas,omitempty"`
	// Deprecated
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty"`
	// Deprecated
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`
	// Deprecated
	UnavailableReplicas int32 `json:"unavailableReplicas,omitempty"`
	StatusMetadata      `json:",inline"`
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

func (cr *VMAgent) AsOwner() []metav1.OwnerReference {
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

func (cr VMAgent) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr VMAgent) AnnotationsFiltered() map[string]string {
	return filterMapKeysByPrefixes(cr.ObjectMeta.Annotations, annotationFilterPrefixes)
}

func (cr VMAgent) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmagent",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VMAgent) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}

	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

func (cr VMAgent) AllLabels() map[string]string {
	selectorLabels := cr.SelectorLabels()
	// fast path
	if cr.ObjectMeta.Labels == nil {
		return selectorLabels
	}
	crLabels := filterMapKeysByPrefixes(cr.ObjectMeta.Labels, labelFilterPrefixes)
	return labels.Merge(crLabels, selectorLabels)
}

func (cr VMAgent) PrefixedName() string {
	return fmt.Sprintf("vmagent-%s", cr.Name)
}

func (cr VMAgent) TLSAssetName() string {
	return fmt.Sprintf("tls-assets-vmagent-%s", cr.Name)
}

func (cr VMAgent) RelabelingAssetName() string {
	return fmt.Sprintf("relabelings-assets-vmagent-%s", cr.Name)
}

func (cr VMAgent) StreamAggrConfigName() string {
	return fmt.Sprintf("stream-aggr-vmagent-%s", cr.Name)
}

func (cr VMAgent) HealthPath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

// GetMetricPath returns prefixed path for metric requests
func (cr VMAgent) GetMetricPath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr VMAgent) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr VMAgent) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

func (cr VMAgent) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

// IsOwnsServiceAccount checks if service account owned by CR
func (cr VMAgent) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

func (cr VMAgent) GetClusterRoleName() string {
	return fmt.Sprintf("monitoring:%s:vmagent-cluster-access-%s", cr.Namespace, cr.Name)
}

func (cr VMAgent) GetNSName() string {
	return cr.GetNamespace()
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
	return fmt.Sprintf("%s://%s.%s.svc:%s", protoFromFlags(cr.Spec.ExtraArgs), cr.PrefixedName(), cr.Namespace, port)
}

// AsCRDOwner implements interface
func (cr *VMAgent) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(Agent)
}

func (cr *VMAgent) Probe() *EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

func (cr *VMAgent) ProbePath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

func (cr *VMAgent) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(cr.Spec.ExtraArgs))
}

func (cr VMAgent) ProbePort() string {
	return cr.Spec.Port
}

func (cr VMAgent) ProbeNeedLiveness() bool {
	return true
}

// IsUnmanaged checks if object should managed any config objects
func (cr *VMAgent) IsUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return !cr.Spec.SelectAllByDefault &&
		cr.Spec.NodeScrapeSelector == nil && cr.Spec.NodeScrapeNamespaceSelector == nil &&
		cr.Spec.ServiceScrapeSelector == nil && cr.Spec.ServiceScrapeNamespaceSelector == nil &&
		cr.Spec.PodScrapeSelector == nil && cr.Spec.PodScrapeNamespaceSelector == nil &&
		cr.Spec.ProbeSelector == nil && cr.Spec.ProbeNamespaceSelector == nil &&
		cr.Spec.StaticScrapeSelector == nil && cr.Spec.StaticScrapeNamespaceSelector == nil &&
		cr.Spec.ScrapeConfigSelector == nil && cr.Spec.ScrapeConfigNamespaceSelector == nil
}

// IsNodeScrapeUnmanaged checks if vmagent should managed any VMNodeScrape objects
func (cr *VMAgent) IsNodeScrapeUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return !cr.Spec.SelectAllByDefault &&
		cr.Spec.NodeScrapeSelector == nil && cr.Spec.NodeScrapeNamespaceSelector == nil
}

// IsServiceScrapeUnmanaged checks if vmagent should managed any VMServiceScrape objects
func (cr *VMAgent) IsServiceScrapeUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return !cr.Spec.SelectAllByDefault &&
		cr.Spec.ServiceScrapeSelector == nil && cr.Spec.ServiceScrapeNamespaceSelector == nil
}

// IsUnmanaged checks if vmagent should managed any VMPodScrape objects
func (cr *VMAgent) IsPodScrapeUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return !cr.Spec.SelectAllByDefault &&
		cr.Spec.PodScrapeSelector == nil && cr.Spec.PodScrapeNamespaceSelector == nil
}

// IsProbeUnmanaged checks if vmagent should managed any VMProbe objects
func (cr *VMAgent) IsProbeUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return !cr.Spec.SelectAllByDefault &&
		cr.Spec.ProbeSelector == nil && cr.Spec.ProbeNamespaceSelector == nil
}

// IsStaticScrapeUnmanaged checks if vmagent should managed any VMStaticScrape objects
func (cr *VMAgent) IsStaticScrapeUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return !cr.Spec.SelectAllByDefault &&
		cr.Spec.StaticScrapeSelector == nil && cr.Spec.StaticScrapeNamespaceSelector == nil
}

// IsScrapeConfigUnmanaged checks if vmagent should managed any VMScrapeConfig objects
func (cr *VMAgent) IsScrapeConfigUnmanaged() bool {
	// fast path
	if cr.Spec.IngestOnlyMode {
		return true
	}
	return !cr.Spec.SelectAllByDefault &&
		cr.Spec.ScrapeConfigSelector == nil && cr.Spec.ScrapeConfigNamespaceSelector == nil
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VMAgent) LastAppliedSpecAsPatch() (client.Patch, error) {
	data, err := json.Marshal(cr.Spec)
	if err != nil {
		return nil, fmt.Errorf("possible bug, cannot serialize specification as json :%w", err)
	}
	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q: %q}}}`, lastAppliedSpecAnnotationName, data)
	return client.RawPatch(types.MergePatchType, []byte(patch)), nil
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (cr *VMAgent) HasSpecChanges() (bool, error) {
	lastAppliedClusterJSON := cr.Annotations[lastAppliedSpecAnnotationName]
	if len(lastAppliedClusterJSON) == 0 {
		return true, nil
	}

	instanceSpecData, err := json.Marshal(cr.Spec)
	if err != nil {
		return true, err
	}
	return !bytes.Equal([]byte(lastAppliedClusterJSON), instanceSpecData), nil
}

func (cr *VMAgent) Paused() bool {
	return cr.Spec.Paused
}

// HasAnyRelabellingConfigs checks if vmagent has any defined relabeling rules
func (cr *VMAgent) HasAnyRelabellingConfigs() bool {
	if cr.Spec.RelabelConfig != nil || len(cr.Spec.InlineRelabelConfig) > 0 {
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

// SetStatusTo changes update status with optional reason of fail
func (cr *VMAgent) SetUpdateStatusTo(ctx context.Context, r client.Client, status UpdateStatus, maybeErr error) error {
	return updateObjectStatus(ctx, r, &patchStatusOpts[*VMAgent, *VMAgentStatus]{
		actualStatus: status,
		cr:           cr,
		crStatus:     &cr.Status,
		maybeErr:     maybeErr,
		mutateCurrentBeforeCompare: func(vs *VMAgentStatus) {
			replicaCount := int32(0)
			if cr.Spec.ReplicaCount != nil {
				replicaCount = *cr.Spec.ReplicaCount
			}
			var shardCnt int32
			if cr.Spec.ShardCount != nil {
				shardCnt = int32(*cr.Spec.ShardCount)
			}
			vs.Replicas = replicaCount
			vs.Shards = shardCnt
			vs.Selector = labels.SelectorFromSet(cr.SelectorLabels()).String()
		},
	})
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMAgent) GetAdditionalService() *AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
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
