package v1beta1

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/VictoriaMetrics/metricsql"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMAgentSpec defines the desired state of VMAgent
// +k8s:openapi-gen=true
type VMAgentSpec struct {
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
	// +kubebuilder:validation:Type=object
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`

	// ShardCount - numbers of shards of VMAgent
	// in this case operator will use 1 deployment/sts per shard with
	// replicas count according to spec.replicas,
	// see [here](https://docs.victoriametrics.com/victoriametrics/vmagent/#scraping-big-number-of-targets)
	// +optional
	// +kubebuilder:default=0
	ShardCount *int32 `json:"shardCount,omitempty"`

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
	// NetworkPolicy defines network access rules for pods created by this CR.
	// +optional
	NetworkPolicy *EmbeddedNetworkPolicy `json:"networkPolicy,omitempty"`
	// DaemonSetMode enables DaemonSet deployment mode instead of Deployment.
	// Supports only VMPodScrape
	// Cannot be used with statefulMode
	// +optional
	// +notes={available_from: "v0.55.0"}
	DaemonSetMode bool `json:"daemonSetMode,omitempty"`
	// DaemonSetUpdateStrategy allows configuration for the DaemonSet update strategy type.
	// +optional
	DaemonSetUpdateStrategy *appsv1.DaemonSetUpdateStrategyType `json:"daemonSetUpdateStrategy,omitempty"`
	// DaemonSetRollingUpdateStrategyBehavior defines customized behavior for rolling updates.
	// It applies if the DaemonSetUpdateStrategy is set to RollingUpdate, which is the default.
	// +optional
	DaemonSetRollingUpdateStrategyBehavior *appsv1.RollingUpdateDaemonSet `json:"daemonSetRollingUpdateStrategyBehavior,omitempty"`
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
	// StatefulRollingUpdateStrategyBehavior defines customized behavior for rolling updates.
	// It applies if the RollingUpdateStrategy is set to OnDelete, which is the default.
	// +optional
	StatefulRollingUpdateStrategyBehavior *StatefulSetUpdateStrategyBehavior `json:"statefulRollingUpdateStrategyBehavior,omitempty"`
	// PersistentVolumeClaimRetentionPolicy allows configuration of PVC retention policy
	// +optional
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`

	// ClaimTemplates allows adding additional VolumeClaimTemplates for VMAgent in StatefulMode
	ClaimTemplates []corev1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`

	// License allows to configure license key to be used for enterprise features.
	// Using license key is supported starting from VictoriaMetrics v1.94.0.
	// See [here](https://docs.victoriametrics.com/victoriametrics/enterprise/)
	// +optional
	License *License `json:"license,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// UseLegacyNaming uses standalone Helm chart naming for managed resources:
	// the CR name is used directly instead of the default "<type>-<name>" convention.
	// +optional
	// +notes={available_from: "v0.73.0"}
	UseLegacyNaming bool `json:"useLegacyNaming,omitempty"`

	// ComponentVersion defines default images tag for all components.
	// it can be overwritten with component specific image.tag value.
	// +optional
	ComponentVersion string `json:"componentVersion,omitempty"`

	// Configures horizontal pod autoscaling.
	// +optional
	HPA *EmbeddedHPA `json:"hpa,omitempty"`
	// Configures vertical pod autoscaling.
	// +optional
	VPA *EmbeddedVPA `json:"vpa,omitempty"`

	CommonRelabelParams        `json:",inline,omitempty"`
	CommonScrapeParams         `json:",inline,omitempty"`
	CommonConfigReloaderParams `json:",inline,omitempty"`
	CommonAppsParams           `json:",inline,omitempty"`
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
			return fmt.Errorf("bad cr.spec.inlineScrapeConfig it must be valid yaml: %w", err)
		}
	}
	if len(cr.Spec.InlineRelabelConfig) > 0 {
		if err := checkRelabelConfigs(cr.Spec.InlineRelabelConfig); err != nil {
			return err
		}
	}
	for _, e := range cr.Spec.ExtraEnvs {
		if strings.HasPrefix(e.Name, RemoteWriteEnvPrefix) {
			return fmt.Errorf("spec.extraEnvs[%q]: the %q env variable name prefix is reserved by the operator", e.Name, RemoteWriteEnvPrefix)
		}
	}
	for idx, rw := range cr.Spec.RemoteWrite {
		if rw.URL == "" {
			return fmt.Errorf("remoteWrite[%d].url cannot be empty", idx)
		}
		if len(rw.InlineUrlRelabelConfig) > 0 {
			if err := checkRelabelConfigs(rw.InlineUrlRelabelConfig); err != nil {
				return fmt.Errorf("bad remoteWrite[%d].urlRelabelingConfig: %w", idx, err)
			}
		}
		if err := rw.validate(); err != nil {
			return fmt.Errorf("remoteWrite[%d]: %w", idx, err)
		}
	}
	if cr.Spec.StatefulMode {
		if cr.Spec.DaemonSetMode {
			return fmt.Errorf("daemonSetMode and statefulMode cannot be used in the same time")
		}
		if cr.Spec.StatefulRollingUpdateStrategyBehavior != nil {
			if err := cr.Spec.StatefulRollingUpdateStrategyBehavior.Validate(); err != nil {
				return fmt.Errorf("vmagent.spec.statefulRollingUpdateStrategyBehavior: %w", err)
			}
		}
	}
	if cr.Spec.DaemonSetMode {
		if cr.Spec.HPA != nil {
			return fmt.Errorf("hpa cannot be used with daemonSetMode")
		}
		if cr.Spec.VPA != nil {
			return fmt.Errorf("vpa cannot be used with daemonSetMode")
		}
		if cr.Spec.PodDisruptionBudget != nil {
			return fmt.Errorf("podDisruptionBudget cannot be used with daemonSetMode")
		}
		if cr.Spec.EnableKubernetesAPISelectors {
			return fmt.Errorf("enableKubernetesAPISelectors cannot be used with daemonSetMode")
		}
	}
	if cr.Spec.HPA != nil {
		if err := cr.Spec.HPA.Validate(); err != nil {
			return err
		}
	}
	if cr.Spec.VPA != nil {
		if err := cr.Spec.VPA.Validate(); err != nil {
			return err
		}
	}
	scrapeClassNames := sets.New[string]()
	defaultScrapeClass := false
	for _, sc := range cr.Spec.ScrapeClasses {
		if scrapeClassNames.Has(sc.Name) {
			return fmt.Errorf("duplicated scrapeClass=%q", sc.Name)
		}
		scrapeClassNames.Insert(sc.Name)
		if ptr.Deref(sc.Default, false) {
			if defaultScrapeClass {
				return fmt.Errorf("multiple default scrape classes defined")
			}
			defaultScrapeClass = true
		}
		if sc.TLSConfig != nil {
			if err := sc.TLSConfig.Validate(); err != nil {
				return fmt.Errorf("incorrect tlsConfig for scrapeClass=%q: %w", sc.Name, err)
			}
		}
		if err := sc.OAuth2.validate(); err != nil {
			return fmt.Errorf("incorrect oauth2 for scrapeClass=%q: %w", sc.Name, err)
		}
		if err := sc.Authorization.validate(); err != nil {
			return fmt.Errorf("incorrect authorization for scrapeClass=%q: %w", sc.Name, err)
		}
		if err := sc.validate(); err != nil {
			return fmt.Errorf("incorrect relabeling for scrapeClass=%q: %w", sc.Name, err)
		}
		scrapeClassNames.Insert(sc.Name)
	}
	if err := cr.Spec.Validate(); err != nil {
		return err
	}
	return nil
}

// IsSharded returns true if sharding is enabled
func (cr *VMAgent) IsSharded() bool {
	return cr != nil && cr.Spec.ShardCount != nil && *cr.Spec.ShardCount > 0 && !cr.Spec.DaemonSetMode
}

// WorkloadKind returns the kind of workload deployed for VMAgent based on the current mode.
func (cr *VMAgent) WorkloadKind() WorkloadKind {
	switch {
	case cr.Spec.DaemonSetMode:
		return WorkloadKindDaemonSet
	case cr.Spec.StatefulMode:
		return WorkloadKindStatefulSet
	default:
		return WorkloadKindDeployment
	}
}

// GetShardCount returns shard count for vmagent
func (cr *VMAgent) GetShardCount() int32 {
	if !cr.IsSharded() {
		return 1
	}
	return *cr.Spec.ShardCount
}

// ExternalLabels returns external labels for scraping
func (cr *VMAgent) ExternalLabels() map[string]string {
	return cr.Spec.externalLabels(fmt.Sprintf("%s/%s", cr.Namespace, cr.Name))
}

// GetReloadURL implements reloadable interface
func (cr *VMAgent) GetReloadURL(host string) string {
	return BuildLocalURL(reloadAuthKeyFlag, host, cr.Spec.Port, reloadPath, cr.Spec.ExtraArgs)
}

// GetReloaderParams implements reloadable interface
func (cr *VMAgent) GetReloaderParams() *CommonConfigReloaderParams {
	return &cr.Spec.CommonConfigReloaderParams
}

// UseProxyProtocol implements build.probeCRD interface
func (cr *VMAgent) UseProxyProtocol() bool {
	return UseProxyProtocol(cr.Spec.ExtraArgs)
}

// AutomountServiceAccountToken implements reloadable interface
func (cr *VMAgent) AutomountServiceAccountToken() bool {
	return !cr.Spec.DisableAutomountServiceAccountToken
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAgent) UnmarshalJSON(src []byte) error {
	type pcr VMAgent
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
			cr.Status.ParsingSpecError = fmt.Sprintf("cannot parse VMAgentSpec: %s, err: %s", string(s.Spec), err)
		}
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
	// AccessKeySecret defines a reference to the secret with an AWS AccessKey
	// for the corresponding -remoteWrite.url if useSigv4 is set.
	// The operator projects the secret into the application container as an environment
	// variable and passes only its reference to -remoteWrite.aws.accessKey, so the
	// credential never appears in the workload spec or in the process arguments.
	// Must be set together with secretKeySecret.
	// +optional
	// +notes={available_from: "v0.75.0"}
	AccessKeySecret *corev1.SecretKeySelector `json:"accessKeySecret,omitempty"`
	// SecretKeySecret defines a reference to the secret with an AWS SecretKey
	// for the corresponding -remoteWrite.url if useSigv4 is set.
	// See accessKeySecret for the details of how the value is delivered to the application.
	// Must be set together with accessKeySecret.
	// +optional
	// +notes={available_from: "v0.75.0"}
	SecretKeySecret *corev1.SecretKeySelector `json:"secretKeySecret,omitempty"`
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

	// ConfigMap with relabeling config which is applied to metrics before sending them to the corresponding -remoteWrite.url.
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
	// The number of concurrent queues
	// +optional
	Queues *int32 `json:"queues,omitempty"`
	// DisableMetadata disables sending metric metadata to the corresponding -remoteWrite.url.
	// By default metadata sending is controlled by the global -enableMetadata flag.
	// vmagent supports it since v1.140.0 version
	// +optional
	// +notes={available_from: "v0.75.0"}
	DisableMetadata *bool `json:"disableMetadata,omitempty"`
	// DisableOnDiskQueue disables storing pending data at -remoteWrite.tmpDataPath when the remote
	// storage at the corresponding -remoteWrite.url cannot keep up with the data ingestion rate.
	// See [here](https://docs.victoriametrics.com/victoriametrics/vmagent/#disabling-on-disk-persistence).
	// vmagent supports it per -remoteWrite.url since v1.102.0 version
	// +optional
	// +notes={available_from: "v0.75.0"}
	DisableOnDiskQueue *bool `json:"disableOnDiskQueue,omitempty"`
	// ForcePromProto forces using Prometheus remote write protocol for sending data
	// to the corresponding -remoteWrite.url.
	// It cannot be used together with forceVMProto.
	// +optional
	// +notes={available_from: "v0.75.0"}
	ForcePromProto *bool `json:"forcePromProto,omitempty"`
	// InmemoryQueues defines the number of additional workers for the corresponding -remoteWrite.url,
	// which send only recently ingested data from the in-memory queue, while the file-based queue
	// is drained by the workers configured via queues. (default 0)
	// vmagent supports it since v1.146.0 version
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +notes={available_from: "v0.75.0"}
	InmemoryQueues *int32 `json:"inmemoryQueues,omitempty"`
	// MDXEnable makes vmagent retain only metrics from VictoriaMetrics services before sending
	// them to the corresponding -remoteWrite.url.
	// See [here](https://docs.victoriametrics.com/victoriametrics/vmagent/#monitoring-data-exchange).
	// It cannot be used together with the -remoteWrite.shardByURL flag.
	// vmagent supports it since v1.147.0 version
	// +optional
	// +notes={available_from: "v0.75.0"}
	MDXEnable *bool `json:"mdxEnable,omitempty"`
	// ObfuscateLabels defines a list of label names whose values are obfuscated before sending
	// them to the corresponding -remoteWrite.url.
	// See [here](https://docs.victoriametrics.com/victoriametrics/vmagent/#obfuscating-label-values).
	// vmagent supports it since v1.149.0 version
	// +optional
	// +notes={available_from: "v0.75.0"}
	ObfuscateLabels []string `json:"obfuscateLabels,omitempty"`
	// RateLimit defines an optional rate limit in bytes per second for data sent to the
	// corresponding -remoteWrite.url. By default the rate limit is disabled. (default 0)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +notes={available_from: "v0.75.0"}
	RateLimit *int64 `json:"rateLimit,omitempty"`
	// RetryMinInterval defines the minimum delay between retry attempts to send a block of data
	// to the corresponding -remoteWrite.url. Every next retry attempt doubles the delay. (default 1s)
	// vmagent supports it since v1.103.0 version
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	// +notes={available_from: "v0.75.0"}
	RetryMinInterval *string `json:"retryMinInterval,omitempty"`
	// RetryMaxInterval defines the maximum delay between retry attempts to send a block of data
	// to the corresponding -remoteWrite.url. (default 1m0s)
	// vmagent supports it since v1.121.0 version
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	// +notes={available_from: "v0.75.0"}
	RetryMaxInterval *string `json:"retryMaxInterval,omitempty"`
	// RoundDigits rounds metric values to this number of decimal digits after the point before
	// sending them to the corresponding -remoteWrite.url. Negative values round before the point,
	// e.g. -1 rounds 126.78 to 130. Set it to 100 in order to disable rounding. (default 100)
	// +optional
	// +notes={available_from: "v0.75.0"}
	RoundDigits *int32 `json:"roundDigits,omitempty"`
	// SignificantFigures defines the number of significant figures to leave in metric values
	// before sending them to the corresponding -remoteWrite.url.
	// Zero value saves all the significant figures. (default 0)
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +notes={available_from: "v0.75.0"}
	SignificantFigures *int32 `json:"significantFigures,omitempty"`
	// TLSHandshakeTimeout defines the timeout for establishing tls connections
	// to the corresponding -remoteWrite.url. (default 20s)
	// vmagent supports it since v1.98.0 version
	// +optional
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	// +notes={available_from: "v0.75.0"}
	TLSHandshakeTimeout *string `json:"tlsHandshakeTimeout,omitempty"`
}

// AsConfigMapKey key for kubernetes configmap
func (*VMAgentRemoteWriteSpec) AsConfigMapKey(idx int, suffix string) string {
	return fmt.Sprintf("RWS_%d-CM-%s", idx, strings.ToUpper(suffix))
}

// validate checks consistency of the per-url remote write settings.
// It mostly guards against combinations which make vmagent exit with a fatal error
// or silently ignore the provided configuration.
func (rw *VMAgentRemoteWriteSpec) validate() error {
	if ptr.Deref(rw.ForcePromProto, false) && rw.ForceVMProto {
		return fmt.Errorf("forcePromProto and forceVMProto cannot be set at the same time")
	}
	if rw.BasicAuth != nil {
		if len(rw.BasicAuth.PasswordFile) > 0 && len(rw.BasicAuth.Password.Name) > 0 {
			return fmt.Errorf("basicAuth: password and password_file cannot be set at the same time")
		}
	}
	if rw.OAuth2 != nil {
		if len(rw.OAuth2.ClientSecretFile) > 0 && rw.OAuth2.ClientSecret != nil {
			return fmt.Errorf("oauth2: client_secret and client_secret_file cannot be set at the same time")
		}
		if len(rw.OAuth2.EndpointParams) > 0 {
			if len(rw.OAuth2.ClientSecretFile) == 0 && rw.OAuth2.ClientSecret == nil {
				return fmt.Errorf("oauth2: endpoint_params require client_secret or client_secret_file to be set")
			}
			for k, v := range rw.OAuth2.EndpointParams {
				if strings.Contains(v, "'") {
					return fmt.Errorf("oauth2.endpoint_params[%q] cannot contain a single quote", k)
				}
			}
		}
	}
	for i, l := range rw.ObfuscateLabels {
		if len(l) == 0 {
			return fmt.Errorf("obfuscateLabels[%d] cannot be empty", i)
		}
		if strings.Contains(l, "^^") || strings.Contains(l, ",") {
			return fmt.Errorf("obfuscateLabels[%d]=%q cannot contain ',' or '^^'", i, l)
		}
	}
	if err := rw.validateRetryIntervals(); err != nil {
		return err
	}
	return rw.AWS.validate()
}

// defaultRetryMaxInterval must be kept in sync with the -remoteWrite.retryMaxInterval default.
const defaultRetryMaxInterval = time.Minute

func (rw *VMAgentRemoteWriteSpec) validateRetryIntervals() error {
	if rw.RetryMinInterval == nil && rw.RetryMaxInterval == nil {
		return nil
	}
	minInterval := time.Second
	if rw.RetryMinInterval != nil {
		v, err := metricsql.PositiveDurationValue(*rw.RetryMinInterval, 0)
		if err != nil {
			return fmt.Errorf("cannot parse retryMinInterval=%q: %w", *rw.RetryMinInterval, err)
		}
		minInterval = time.Duration(v) * time.Millisecond
	}
	maxInterval := defaultRetryMaxInterval
	if rw.RetryMaxInterval != nil {
		v, err := metricsql.PositiveDurationValue(*rw.RetryMaxInterval, 0)
		if err != nil {
			return fmt.Errorf("cannot parse retryMaxInterval=%q: %w", *rw.RetryMaxInterval, err)
		}
		maxInterval = time.Duration(v) * time.Millisecond
	}
	if minInterval > maxInterval {
		return fmt.Errorf("retryMinInterval=%s cannot exceed retryMaxInterval=%s", minInterval, maxInterval)
	}
	return nil
}

func (aws *AWS) validate() error {
	if aws == nil {
		return nil
	}
	hasAccessKey := aws.AccessKeySecret != nil
	hasSecretKey := aws.SecretKeySecret != nil
	if hasAccessKey != hasSecretKey {
		return fmt.Errorf("aws: accessKeySecret and secretKeySecret must be set together")
	}
	if !hasAccessKey {
		return nil
	}
	if !aws.UseSigv4 {
		return fmt.Errorf("aws: accessKeySecret and secretKeySecret require useSigv4 to be set, otherwise they are ignored by vmagent")
	}
	for name, sel := range map[string]*corev1.SecretKeySelector{
		"accessKeySecret": aws.AccessKeySecret,
		"secretKeySecret": aws.SecretKeySecret,
	} {
		if len(sel.Name) == 0 {
			return fmt.Errorf("aws.%s.name cannot be empty", name)
		}
		if len(sel.Key) == 0 {
			return fmt.Errorf("aws.%s.key cannot be empty", name)
		}
	}
	return nil
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
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	LastAppliedSpec *VMAgentSpec `json:"lastAppliedSpec,omitempty"`
	// ParsingSpecError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingSpecError string `json:"-" yaml:"-"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VMAgent) GetStatusMetadata() *StatusMetadata {
	return &cr.Status.StatusMetadata
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

	Spec   VMAgentSpec   `json:"spec,omitempty"`
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
	vs.Replicas = replicaCount
	vs.Shards = cr.GetShardCount()
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
	if cr.Spec.UseLegacyNaming {
		return cr.Name
	}
	return fmt.Sprintf("vmagent-%s", cr.Name)
}

func (cr *VMAgent) HealthPath() string {
	return BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

// GetMetricsPath returns prefixed path for metric requests
func (cr *VMAgent) GetMetricsPath() string {
	return BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricsPath)
}

// UseTLS returns true if TLS is enabled
func (cr *VMAgent) UseTLS() bool {
	return UseTLS(cr.Spec.ExtraArgs)
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

func (cr *VMAgent) GetRBACName() string {
	return fmt.Sprintf("monitoring:%s:%s", cr.Namespace, cr.PrefixedName())
}

// AsURL - returns url for http access
func (cr *VMAgent) AsURL(isExtra bool) string {
	specPort := cr.Spec.Port
	if specPort == "" {
		specPort = "8429"
	}
	svcName, port := ResolveServiceURL(cr.PrefixedName(), specPort, "http", cr.Spec.ServiceSpec, isExtra)
	return fmt.Sprintf("%s://%s.%s.svc:%s", HTTPProtoFromFlags(cr.Spec.ExtraArgs), svcName, cr.Namespace, port)
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

// ScrapeSelectors gets object and namespace sepectors
func (cr *VMAgent) ScrapeSelectors(scrape client.Object) (*metav1.LabelSelector, *metav1.LabelSelector) {
	return cr.Spec.ScrapeSelectors(scrape)
}

// IsUnmanaged checks if object should managed any config objects
func (cr *VMAgent) IsUnmanaged(scrape client.Object) bool {
	if !cr.DeletionTimestamp.IsZero() || (cr.Status.ParsingSpecError != "" && !HasUnknownFields(cr.Status.ParsingSpecError)) {
		return true
	}
	if scrape == nil {
		return cr.Spec.isUnmanaged()
	}
	switch s := scrape.(type) {
	case *VMNodeScrape:
		return cr.Spec.DaemonSetMode || cr.Spec.isNodeScrapeUnmanaged()
	case *VMServiceScrape:
		return cr.Spec.DaemonSetMode || cr.Spec.isServiceScrapeUnmanaged()
	case *VMPodScrape:
		return cr.Spec.isPodScrapeUnmanaged()
	case *VMProbe:
		return cr.Spec.DaemonSetMode || cr.Spec.isProbeUnmanaged()
	case *VMStaticScrape:
		return cr.Spec.DaemonSetMode || cr.Spec.isStaticScrapeUnmanaged()
	case *VMScrapeConfig:
		return cr.Spec.DaemonSetMode || cr.Spec.isScrapeConfigUnmanaged()
	default:
		panic(fmt.Sprintf("BUG: scrape kind %T is not supported", s))
	}

}

// LastSpecUpdated compares spec with last applied spec stored, replaces old spec and returns true if it's updated
func (cr *VMAgent) LastSpecUpdated() bool {
	updated := cr.Status.LastAppliedSpec == nil || !equality.Semantic.DeepEqual(&cr.Spec, cr.Status.LastAppliedSpec)
	cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
	return updated
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

// HasRemoteWriteSecrets reports whether any remoteWrite entry references a credential secret.
func (cr *VMAgent) HasRemoteWriteSecrets() bool {
	for _, rw := range cr.Spec.RemoteWrite {
		if rw.BasicAuth != nil && len(rw.BasicAuth.Password.Name) > 0 {
			return true
		}
		if rw.BearerTokenSecret != nil && rw.BearerTokenSecret.Name != "" {
			return true
		}
		if rw.OAuth2 != nil && rw.OAuth2.ClientSecret != nil {
			return true
		}
		if rw.TLSConfig != nil && (rw.TLSConfig.CA.Secret != nil || rw.TLSConfig.Cert.Secret != nil || rw.TLSConfig.KeySecret != nil) {
			return true
		}
	}
	return false
}

// HasConfigReloader reports whether vmagent's pod spec includes a config-reloader sidecar.
func (cr *VMAgent) HasConfigReloader() bool {
	return !ptr.Deref(cr.Spec.IngestOnlyMode, false) || cr.HasAnyRelabellingConfigs() || cr.HasAnyStreamAggrRule() || cr.HasRemoteWriteSecrets()
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

// RBACNamespaces returns the namespaces to watch for RBAC resources.
func (cr *VMAgent) RBACNamespaces(watchedNamespaces []string) []string {
	if len(watchedNamespaces) == 0 {
		return nil
	}

	if cr.Spec.SelectAllByDefault ||
		cr.Spec.ServiceScrapeNamespaceSelector != nil ||
		cr.Spec.PodScrapeNamespaceSelector != nil ||
		cr.Spec.ProbeNamespaceSelector != nil ||
		cr.Spec.NodeScrapeNamespaceSelector != nil ||
		cr.Spec.StaticScrapeNamespaceSelector != nil ||
		cr.Spec.ScrapeConfigNamespaceSelector != nil {
		return watchedNamespaces
	}

	return []string{cr.Namespace}
}
