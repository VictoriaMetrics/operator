package v1beta1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMAuthSpec defines the desired state of VMAuth
type VMAuthSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// PodMetadata configures Labels and Annotations which are propagated to the VMAuth pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// Image - docker image settings for VMAuth
	// if no specified operator uses default config version
	// +optional
	Image Image `json:"image,omitempty"`
	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see https://kubernetes.io/docs/concepts/containers/images/#referring-to-an-imagepullsecrets-on-a-pod
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
	// MinReadySeconds defines a minim number os seconds to wait before starting update next pod
	// if previous in healthy state
	// +optional
	MinReadySeconds int32 `json:"minReadySeconds,omitempty"`
	// ReplicaCount is the expected size of the VMAuth
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Number of pods",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount,urn:alm:descriptor:io.kubernetes:custom"
	ReplicaCount *int32 `json:"replicaCount,omitempty"`
	// The number of old ReplicaSets to retain to allow rollback in deployment or
	// maximum number of revisions that will be maintained in the StatefulSet's revision history.
	// Defaults to 10.
	// +optional
	RevisionHistoryLimitCount *int32 `json:"revisionHistoryLimitCount,omitempty"`

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

	// ConfigReloaderExtraArgs that will be passed to  VMAuths config-reloader container
	// for example resyncInterval: "30s"
	// +optional
	ConfigReloaderExtraArgs map[string]string `json:"configReloaderExtraArgs,omitempty"`

	// ExtraArgs that will be passed to  VMAuth pod
	// for example remoteWrite.tmpDataPath: /tmp
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// ExtraEnvs that will be added to VMAuth pod
	// +optional
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
	// ServiceSpec that will be added to vmsingle service spec
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmauth VMServiceScrape spec
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
	// ReadinessGates defines pod readiness gates
	ReadinessGates []v1.PodReadinessGate `json:"readinessGates,omitempty"`
	// UnauthorizedAccessConfig configures access for un authorized users
	// +optional
	UnauthorizedAccessConfig       []UnauthorizedAccessConfigURLMap `json:"unauthorizedAccessConfig,omitempty"`
	UnauthorizedAccessConfigOption *UserConfigOption                `json:"unauthorizedAccessConfigOption,omitempty"`
	// UseStrictSecurity enables strict security mode for component
	// it restricts disk writes access
	// uses non-root user out of the box
	// drops not needed security permissions
	// +optional
	UseStrictSecurity *bool `json:"useStrictSecurity,omitempty"`
	// License allows to configure license key to be used for enterprise features.
	// Using license key is supported starting from VictoriaMetrics v1.94.0.
	// See: https://docs.victoriametrics.com/enterprise.html
	// +optional
	License *License `json:"license,omitempty"`
	// ConfigSecret is the name of a Kubernetes Secret in the same namespace as the
	// VMAuth object, which contains auth configuration for vmauth,
	// configuration must be inside secret key: config.yaml.
	// It must be created and managed manually.
	// If it's defined, configuration for vmauth becomes unmanaged and operator'll not create any related secrets/config-reloaders
	// +optional
	ConfigSecret string `json:"configSecret,omitempty"`

	// Paused If set to true all actions on the underlaying managed objects are not
	// going to be performed, except for delete actions.
	// +optional
	Paused bool `json:"paused,omitempty"`
}

type UnauthorizedAccessConfigURLMap struct {
	// SrcPaths is an optional list of regular expressions, which must match the request path.
	SrcPaths []string `json:"src_paths,omitempty"`

	// SrcHosts is an optional list of regular expressions, which must match the request hostname.
	SrcHosts []string `json:"src_hosts,omitempty"`

	// UrlPrefix contains backend url prefixes for the proxied request url.
	URLPrefix []string `json:"url_prefix,omitempty"`

	URLMapCommon URLMapCommon `json:",omitempty"`
}

// URLMapCommon contains common fields for unauthorized user and user in vmuser
type URLMapCommon struct {
	// SrcQueryArgs is an optional list of query args, which must match request URL query args.
	SrcQueryArgs []string `json:"src_query_args,omitempty"`

	// SrcHeaders is an optional list of headers, which must match request headers.
	SrcHeaders []string `json:"src_headers,omitempty"`

	// IPFilters defines filter for src ip address
	// enterprise only
	IPFilters VMUserIPFilters `json:"ip_filters,omitempty"`

	// DiscoverBackendIPs instructs discovering URLPrefix backend IPs via DNS.
	DiscoverBackendIPs *bool `json:"discover_backend_ips,omitempty"`

	// RequestHeaders represent additional http headers, that vmauth uses
	// in form of ["header_key: header_value"]
	// multiple values for header key:
	// ["header_key: value1,value2"]
	// it's available since 1.68.0 version of vmauth
	// +optional
	RequestHeaders []string `json:"headers,omitempty"`
	// ResponseHeaders represent additional http headers, that vmauth adds for request response
	// in form of ["header_key: header_value"]
	// multiple values for header key:
	// ["header_key: value1,value2"]
	// it's available since 1.93.0 version of vmauth
	// +optional
	ResponseHeaders []string `json:"response_headers,omitempty"`

	// RetryStatusCodes defines http status codes in numeric format for request retries
	// Can be defined per target or at VMUser.spec level
	// e.g. [429,503]
	// +optional
	RetryStatusCodes []int `json:"retry_status_codes,omitempty"`

	// LoadBalancingPolicy defines load balancing policy to use for backend urls.
	// Supported policies: least_loaded, first_available.
	// See https://docs.victoriametrics.com/vmauth.html#load-balancing for more details (default "least_loaded")
	// +optional
	// +kubebuilder:validation:Enum=least_loaded;first_available
	LoadBalancingPolicy *string `json:"load_balancing_policy,omitempty"`

	// DropSrcPathPrefixParts is the number of `/`-delimited request path prefix parts to drop before proxying the request to backend.
	// See https://docs.victoriametrics.com/vmauth.html#dropping-request-path-prefix for more details.
	// +optional
	DropSrcPathPrefixParts *int `json:"drop_src_path_prefix_parts,omitempty"`
	// MaxConcurrentRequests defines max concurrent requests per user
	// 300 is default value for vmauth
	// +optional
	MaxConcurrentRequests *int `json:"max_concurrent_requests,omitempty"`
}

type UserConfigOption struct {
	// DefaultURLs backend url for non-matching paths filter
	// usually used for default backend with error message
	DefaultURLs []string `json:"default_url,omitempty"`

	TLSCAFile     string `json:"tls_ca_file,omitempty"`
	TLSCertFile   string `json:"tls_cert_file,omitempty"`
	TLSKeyFile    string `json:"tls_key_file,omitempty"`
	TLSServerName string `json:"tls_server_name,omitempty"`

	// TLSInsecureSkipVerify - whether to skip TLS verification when connecting to backend over HTTPS.
	// See https://docs.victoriametrics.com/vmauth.html#backend-tls-setup
	// +optional
	TLSInsecureSkipVerify *bool `json:"tls_insecure_skip_verify,omitempty"`

	// IPFilters defines per target src ip filters
	// supported only with enterprise version of vmauth
	// https://docs.victoriametrics.com/vmauth.html#ip-filters
	// +optional
	IPFilters VMUserIPFilters `json:"ip_filters,omitempty"`

	// DiscoverBackendIPs instructs discovering URLPrefix backend IPs via DNS.
	DiscoverBackendIPs *bool `json:"discover_backend_ips,omitempty"`

	// Headers represent additional http headers, that vmauth uses
	// in form of ["header_key: header_value"]
	// multiple values for header key:
	// ["header_key: value1,value2"]
	// it's available since 1.68.0 version of vmauth
	// +optional
	Headers []string `json:"headers,omitempty"`
	// ResponseHeaders represent additional http headers, that vmauth adds for request response
	// in form of ["header_key: header_value"]
	// multiple values for header key:
	// ["header_key: value1,value2"]
	// it's available since 1.93.0 version of vmauth
	// +optional
	ResponseHeaders []string `json:"response_headers,omitempty"`

	// RetryStatusCodes defines http status codes in numeric format for request retries
	// e.g. [429,503]
	// +optional
	RetryStatusCodes []int `json:"retry_status_codes,omitempty"`

	// MaxConcurrentRequests defines max concurrent requests per user
	// 300 is default value for vmauth
	// +optional
	MaxConcurrentRequests *int `json:"max_concurrent_requests,omitempty"`

	// LoadBalancingPolicy defines load balancing policy to use for backend urls.
	// Supported policies: least_loaded, first_available.
	// See https://docs.victoriametrics.com/vmauth.html#load-balancing for more details (default "least_loaded")
	// +optional
	// +kubebuilder:validation:Enum=least_loaded;first_available
	LoadBalancingPolicy *string `json:"load_balancing_policy,omitempty"`

	// DropSrcPathPrefixParts is the number of `/`-delimited request path prefix parts to drop before proxying the request to backend.
	// See https://docs.victoriametrics.com/vmauth.html#dropping-request-path-prefix for more details.
	// +optional
	DropSrcPathPrefixParts *int `json:"drop_src_path_prefix_parts,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAuthSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAuthSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vmauth spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
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
	// UpdateStatus defines a status for update rollout, effective only for statefuleMode
	UpdateStatus UpdateStatus `json:"updateStatus,omitempty"`
	// Reason defines fail reason for update process, effective only for statefuleMode
	Reason string `json:"reason,omitempty"`
}

// VMAuth is the Schema for the vmauths API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of update rollout"
type VMAuth struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAuthSpec   `json:"spec,omitempty"`
	Status VMAuthStatus `json:"status,omitempty"`
}

func (cr *VMAuth) Probe() *EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

func (cr *VMAuth) ProbePath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

func (cr *VMAuth) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(cr.Spec.ExtraArgs))
}

func (cr *VMAuth) ProbePort() string {
	return cr.Spec.Port
}

func (cr *VMAuth) ProbeNeedLiveness() bool {
	return true
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
			Controller:         pointer.Bool(true),
			BlockOwnerDeletion: pointer.Bool(true),
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
	return filterMapKeysByPrefixes(cr.ObjectMeta.Annotations, annotationFilterPrefixes)
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
	selectorLabels := cr.SelectorLabels()
	// fast path
	if cr.ObjectMeta.Labels == nil {
		return selectorLabels
	}
	crLabels := filterMapKeysByPrefixes(cr.ObjectMeta.Labels, labelFilterPrefixes)
	return labels.Merge(crLabels, selectorLabels)
}

func (cr VMAuth) PrefixedName() string {
	return fmt.Sprintf("vmauth-%s", cr.Name)
}

func (cr VMAuth) ConfigSecretName() string {
	return fmt.Sprintf("vmauth-config-%s", cr.Name)
}

func (cr VMAuth) MetricPath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

func (cr VMAuth) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr VMAuth) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

func (cr VMAuth) GetNSName() string {
	return cr.GetNamespace()
}

// AsCRDOwner implements interface
func (cr *VMAuth) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(Auth)
}

// IsUnmanaged checks if object should managed any  config objects
func (cr *VMAuth) IsUnmanaged() bool {
	return (!cr.Spec.SelectAllByDefault && cr.Spec.UserSelector == nil && cr.Spec.UserNamespaceSelector == nil) || cr.Spec.ConfigSecret != ""
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VMAuth) LastAppliedSpecAsPatch() (client.Patch, error) {
	data, err := json.Marshal(cr.Spec)
	if err != nil {
		return nil, fmt.Errorf("possible bug, cannot serialize specification as json :%w", err)
	}
	patch := fmt.Sprintf(`{"metadata":{"annotations":{"operator.victoriametrics/last-applied-spec": %q}}}`, data)
	return client.RawPatch(types.MergePatchType, []byte(patch)), nil
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (cr *VMAuth) HasSpecChanges() (bool, error) {
	var prevSpec VMAuthSpec
	lastAppliedClusterJSON := cr.Annotations["operator.victoriametrics/last-applied-spec"]
	if len(lastAppliedClusterJSON) == 0 {
		return true, nil
	}
	if err := json.Unmarshal([]byte(lastAppliedClusterJSON), &prevSpec); err != nil {
		return true, fmt.Errorf("cannot parse last applied spec value: %s : %w", lastAppliedClusterJSON, err)
	}
	instanceSpecData, _ := json.Marshal(cr.Spec)
	return !bytes.Equal([]byte(lastAppliedClusterJSON), instanceSpecData), nil
}

func (cr *VMAuth) Paused() bool {
	return cr.Spec.Paused
}

// SetStatusTo changes update status with optional reason of fail
func (cr *VMAuth) SetUpdateStatusTo(ctx context.Context, r client.Client, status UpdateStatus, maybeErr error) error {
	currentStatus := cr.Status.UpdateStatus
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
	if err := r.Status().Update(ctx, cr); err != nil {
		return fmt.Errorf("failed to update object status to=%q: %w", status, err)
	}
	return nil
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMAuth) GetAdditionalService() *AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VMAuth{}, &VMAuthList{})
}
