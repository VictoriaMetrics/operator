package v1beta1

import (
	"context"
	"encoding/json"
	"fmt"
	"path"

	amlabels "github.com/prometheus/alertmanager/pkg/labels"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMAlertmanager represents Victoria-Metrics deployment for Alertmanager.
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMAlertmanager App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="StatefulSet,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
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
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VMAlertmanagerSpec `json:"-" yaml:"-"`

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
	ParsingError string `json:"-" yaml:"-"`

	// PodMetadata configures Labels and Annotations which are propagated to the alertmanager pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *ManagedObjectsMetadata `json:"managedMetadata,omitempty"`

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
	// +kubebuilder:validation:Enum=debug;info;warn;error;DEBUG;INFO;WARN;ERROR
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VMAlertmanager to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=logfmt;json
	LogFormat string `json:"logFormat,omitempty"`

	// Retention Time duration VMAlertmanager shall retain data for. Default is '120h',
	// and must match the regular expression `[0-9]+(ms|s|m|h)` (milliseconds seconds minutes hours).
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	// +optional
	Retention string `json:"retention,omitempty"`
	// Storage is the definition of how storage will be used by the VMAlertmanager
	// instances.
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`

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

	// ClusterDomainName defines domain name suffix for in-cluster dns addresses
	// aka .cluster.local
	// used to build pod peer addresses for in-cluster communication
	// +optional
	ClusterDomainName string `json:"clusterDomainName,omitempty"`
	// ListenLocal makes the VMAlertmanager server listen on loopback, so that it
	// does not bind against the Pod IP. Note this is only for the VMAlertmanager
	// UI, not the gossip communication.
	// +optional
	ListenLocal bool `json:"listenLocal,omitempty"`
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

	// DisableNamespaceMatcher disables top route namespace label matcher for VMAlertmanagerConfig
	// It may be useful if alert doesn't have namespace label for some reason
	// +optional
	DisableNamespaceMatcher bool `json:"disableNamespaceMatcher,omitempty"`

	// DisableRouteContinueEnforce cancel the behavior for VMAlertmanagerConfig that always enforce first-level route continue to true
	// +optional
	DisableRouteContinueEnforce bool `json:"disableRouteContinueEnforce,omitempty"`

	// EnforcedTopRouteMatchers defines label matchers to be added for the top route
	// of VMAlertmanagerConfig
	// It allows to make some set of labels required for alerts.
	// https://prometheus.io/docs/alerting/latest/configuration/#matcher
	EnforcedTopRouteMatchers []string `json:"enforcedTopRouteMatchers,omitempty"`

	// RollingUpdateStrategy defines strategy for application updates
	// Default is OnDelete, in this case operator handles update process
	// Can be changed for RollingUpdate
	// +optional
	RollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"rollingUpdateStrategy,omitempty"`
	// ClaimTemplates allows adding additional VolumeClaimTemplates for StatefulSet
	ClaimTemplates []v1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`

	// WebConfig defines configuration for webserver
	// https://github.com/prometheus/alertmanager/blob/main/docs/https.md
	// +optional
	WebConfig *AlertmanagerWebConfig `json:"webConfig,omitempty"`

	// GossipConfig defines gossip TLS configuration for Alertmanager cluster
	// +optional
	GossipConfig *AlertmanagerGossipConfig `json:"gossipConfig,omitempty"`

	*ServiceAccount `json:",inline,omitempty"`

	CommonDefaultableParams           `json:",inline,omitempty"`
	CommonConfigReloaderParams        `json:",inline,omitempty"`
	CommonApplicationDeploymentParams `json:",inline,omitempty"`
}

func (r *VMAlertmanager) setLastSpec(prevSpec VMAlertmanagerSpec) {
	r.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (r *VMAlertmanager) UnmarshalJSON(src []byte) error {
	type pr VMAlertmanager
	if err := json.Unmarshal(src, (*pr)(r)); err != nil {
		return err
	}
	if err := parseLastAppliedState(r); err != nil {
		return err
	}

	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (r *VMAlertmanagerSpec) UnmarshalJSON(src []byte) error {
	type pr VMAlertmanagerSpec
	if err := json.Unmarshal(src, (*pr)(r)); err != nil {
		r.ParsingError = fmt.Sprintf("cannot parse vmalertmanager spec: %s, err: %s", string(src), err)
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
	StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (r *VMAlertmanagerStatus) GetStatusMetadata() *StatusMetadata {
	return &r.StatusMetadata
}

// AsOwner returns owner references with current object as owner
func (r *VMAlertmanager) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         r.APIVersion,
			Kind:               r.Kind,
			Name:               r.Name,
			UID:                r.UID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	}
}

func (r *VMAlertmanager) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if r.Spec.PodMetadata != nil {
		for annotation, value := range r.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (r *VMAlertmanager) AnnotationsFiltered() map[string]string {
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	dst := filterMapKeysByPrefixes(r.ObjectMeta.Annotations, annotationFilterPrefixes)
	if r.Spec.ManagedMetadata != nil {
		if dst == nil {
			dst = make(map[string]string)
		}
		for k, v := range r.Spec.ManagedMetadata.Annotations {
			dst[k] = v
		}
	}
	return dst
}

func (r *VMAlertmanager) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmalertmanager",
		"app.kubernetes.io/instance":  r.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (r *VMAlertmanager) PodLabels() map[string]string {
	lbls := r.SelectorLabels()
	if r.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(r.Spec.PodMetadata.Labels, lbls)
}

func (r *VMAlertmanager) AllLabels() map[string]string {
	selectorLabels := r.SelectorLabels()
	// fast path
	if r.ObjectMeta.Labels == nil && r.Spec.ManagedMetadata == nil {
		return selectorLabels
	}
	var result map[string]string
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	if r.ObjectMeta.Labels != nil {
		result = filterMapKeysByPrefixes(r.ObjectMeta.Labels, labelFilterPrefixes)
	}
	if r.Spec.ManagedMetadata != nil {
		result = labels.Merge(result, r.Spec.ManagedMetadata.Labels)
	}
	return labels.Merge(result, selectorLabels)
}

// ConfigSecretName returns configuration secret name for alertmanager
func (r *VMAlertmanager) ConfigSecretName() string {
	return fmt.Sprintf("%s-config", r.PrefixedName())
}

func (r *VMAlertmanager) PrefixedName() string {
	return fmt.Sprintf("vmalertmanager-%s", r.Name)
}

func (r *VMAlertmanager) GetServiceAccount() *ServiceAccount {
	sa := r.Spec.ServiceAccount
	if sa == nil {
		sa = &ServiceAccount{
			Name:           r.PrefixedName(),
			AutomountToken: true,
		}
	}
	return sa
}

func (r *VMAlertmanager) IsOwnsServiceAccount() bool {
	if r.Spec.ServiceAccount != nil && r.Spec.ServiceAccount.Name != "" {
		return r.Spec.ServiceAccount.Name == ""
	}
	return false
}

// GetNSName implements build.builderOpts interface
func (r *VMAlertmanager) GetNSName() string {
	return r.GetNamespace()
}

// Port returns port for accessing alertmanager
func (r *VMAlertmanager) Port() string {
	port := r.Spec.Port
	if port == "" {
		port = "9093"
	}

	return port
}

// AsURL returns url for accessing alertmanager
// via corresponding service
func (r *VMAlertmanager) AsURL() string {
	port := r.Port()
	portName := r.Spec.PortName
	if portName == "" {
		portName = "web"
	}
	if r.Spec.ServiceSpec != nil && r.Spec.ServiceSpec.UseAsDefault {
		for _, svcPort := range r.Spec.ServiceSpec.Spec.Ports {
			if svcPort.Name == portName {
				port = fmt.Sprintf("%d", svcPort.Port)
				break
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", r.accessScheme(), r.PrefixedName(), r.Namespace, port)
}

// returns fqdn for direct pod access
func (r *VMAlertmanager) asPodFQDN(idx int) string {
	return fmt.Sprintf("%s://%s-%d.%s.%s.svc:%s", r.accessScheme(), r.PrefixedName(), idx, r.PrefixedName(), r.Namespace, r.Port())
}

// GetMetricPath returns prefixed path for metric requests
func (r *VMAlertmanager) GetMetricPath() string {
	if prefix := r.Spec.RoutePrefix; prefix != "" {
		return path.Join(prefix, metricPath)
	}
	return metricPath
}

// GetExtraArgs returns additionally configured command-line arguments
func (r *VMAlertmanager) GetExtraArgs() map[string]string {
	return r.Spec.ExtraArgs
}

// GetServiceScrape returns overrides for serviceScrape builder
func (r *VMAlertmanager) GetServiceScrape() *VMServiceScrapeSpec {
	return r.Spec.ServiceScrapeSpec
}

// AsCRDOwner implements interface
func (r *VMAlertmanager) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(AlertManager)
}

// AsNotifiers converts VMAlertmanager into VMAlertNotifierSpec
func (r *VMAlertmanager) AsNotifiers() []VMAlertNotifierSpec {
	var notifiers []VMAlertNotifierSpec
	replicaCount := 1
	if r.Spec.ReplicaCount != nil {
		replicaCount = int(*r.Spec.ReplicaCount)
	}
	for i := 0; i < replicaCount; i++ {
		ns := VMAlertNotifierSpec{
			URL: r.asPodFQDN(i),
		}
		notifiers = append(notifiers, ns)
	}
	return notifiers
}

func (r *VMAlertmanager) GetVolumeName() string {
	if r.Spec.Storage != nil && r.Spec.Storage.VolumeClaimTemplate.Name != "" {
		return r.Spec.Storage.VolumeClaimTemplate.Name
	}
	return fmt.Sprintf("vmalertmanager-%s-db", r.Name)
}

func (r *VMAlertmanager) Probe() *EmbeddedProbes {
	return r.Spec.EmbeddedProbes
}

func (r *VMAlertmanager) ProbePath() string {
	webRoutePrefix := "/"
	if r.Spec.RoutePrefix != "" {
		webRoutePrefix = r.Spec.RoutePrefix
	}
	return path.Clean(webRoutePrefix + "/-/healthy")
}

func (r *VMAlertmanager) ProbePort() string {
	return r.Spec.PortName
}

func (r *VMAlertmanager) accessScheme() string {
	if r.Spec.WebConfig != nil && r.Spec.WebConfig.TLSServerConfig != nil {
		// special case for mTLS
		return "https"
	}
	return "http"
}

// ProbeScheme returns scheme for probe
func (r *VMAlertmanager) ProbeScheme() string {
	if r.Spec.WebConfig != nil && r.Spec.WebConfig.TLSServerConfig != nil {
		return "HTTPS"
	}
	return "HTTP"
}

func (r *VMAlertmanager) ProbeNeedLiveness() bool {
	return true
}

// IsUnmanaged checks if alertmanager should managed any alertmanager config objects
func (r *VMAlertmanager) IsUnmanaged() bool {
	return !r.Spec.SelectAllByDefault && r.Spec.ConfigSelector == nil && r.Spec.ConfigNamespaceSelector == nil
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (r *VMAlertmanager) LastAppliedSpecAsPatch() (client.Patch, error) {
	return lastAppliedChangesAsPatch(r.ObjectMeta, r.Spec)
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (r *VMAlertmanager) HasSpecChanges() (bool, error) {
	return hasStateChanges(r.ObjectMeta, r.Spec)
}

func (r *VMAlertmanager) Paused() bool {
	return r.Spec.Paused
}

// SetStatusTo changes update status with optional reason of fail
func (r *VMAlertmanager) SetUpdateStatusTo(ctx context.Context, c client.Client, status UpdateStatus, maybeErr error) error {
	return updateObjectStatus(ctx, c, &patchStatusOpts[*VMAlertmanager, *VMAlertmanagerStatus]{
		actualStatus: status,
		r:            r,
		rStatus:      &r.Status,
		maybeErr:     maybeErr,
	})
}

func (r *VMAlertmanager) Validate() error {
	if r.Spec.ServiceSpec != nil && r.Spec.ServiceSpec.Name == r.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", r.PrefixedName())
	}
	for idx, matchers := range r.Spec.EnforcedTopRouteMatchers {
		_, err := amlabels.ParseMatchers(matchers)
		if err != nil {
			fmt.Errorf("incorrect EnforcedTopRouteMatchers=%q at idx=%d: %w", matchers, idx, err)
		}
	}

	if len(r.Spec.ConfigRawYaml) > 0 {
		if err := validateAlertmanagerConfigSpec([]byte(r.Spec.ConfigRawYaml)); err != nil {
			return fmt.Errorf("bad config syntax at spec.configRawYaml: %w", err)
		}
	}
	if r.Spec.ConfigSecret == r.ConfigSecretName() {
		return fmt.Errorf("spec.configSecret uses the same name as built-in config secret used by operator. Please change it's name")
	}
	if r.Spec.WebConfig != nil {
		if r.Spec.WebConfig.HTTPServerConfig != nil {
			if r.Spec.WebConfig.HTTPServerConfig.HTTP2 && r.Spec.WebConfig.TLSServerConfig == nil {
				return fmt.Errorf("with enabled http2 for webserver, tls_server_config must be defined")
			}
		}
		if r.Spec.WebConfig.TLSServerConfig != nil {
			tc := r.Spec.WebConfig.TLSServerConfig
			if tc.Certs.CertFile == "" && tc.Certs.CertSecretRef == nil {
				return fmt.Errorf("either cert_secret_ref or cert_file must be set for tls_server_config")
			}
			if tc.Certs.KeyFile == "" && tc.Certs.KeySecretRef == nil {
				return fmt.Errorf("either key_secret_ref or key_file must be set for tls_server_config")
			}
			if tc.ClientAuthType == "RequireAndVerifyClientCert" {
				if tc.ClientCAFile == "" && tc.ClientCASecretRef == nil {
					return fmt.Errorf("either client_ca_secret_ref or client_ca_file must be set for tls_server_config with enabled RequireAndVerifyClientCert")
				}
			}
		}
	}

	if r.Spec.GossipConfig != nil {
		if r.Spec.GossipConfig.TLSServerConfig != nil {
			tc := r.Spec.GossipConfig.TLSServerConfig
			if tc.Certs.CertFile == "" && tc.Certs.CertSecretRef == nil {
				return fmt.Errorf("either cert_secret_ref or cert_file must be set for tls_server_config")
			}
			if tc.Certs.KeyFile == "" && tc.Certs.KeySecretRef == nil {
				return fmt.Errorf("either key_secret_ref or key_file must be set for tls_server_config")
			}
			if tc.ClientAuthType == "RequireAndVerifyClientCert" {
				if tc.ClientCAFile == "" && tc.ClientCASecretRef == nil {
					return fmt.Errorf("either client_ca_secret_ref or client_ca_file must be set for tls_server_config with enabled RequireAndVerifyClientCert")
				}
			}
		}
		if r.Spec.GossipConfig.TLSClientConfig != nil {
			tc := r.Spec.GossipConfig.TLSClientConfig
			if tc.Certs.CertFile == "" && tc.Certs.CertSecretRef == nil {
				return fmt.Errorf("either cert_secret_ref or cert_file must be set for tls_client_config")
			}
			if tc.Certs.KeyFile == "" && tc.Certs.KeySecretRef == nil {
				return fmt.Errorf("either key_secret_ref or key_file must be set for tls_client_config")
			}
		}
	}
	return nil
}

// AlertmanagerGossipConfig defines Gossip TLS configuration for alertmanager
type AlertmanagerGossipConfig struct {
	// TLSServerConfig defines server TLS configuration for alertmanager
	TLSServerConfig *TLSServerConfig `json:"tls_server_config,omitempty"`
	// TLSClientConfig defines client TLS configuration for alertmanager
	TLSClientConfig *TLSClientConfig `json:"tls_client_config,omitempty"`
}

// AlertmanagerWebConfig defines web server configuration for alertmanager
type AlertmanagerWebConfig struct {
	// TLSServerConfig defines server TLS configuration for alertmanager
	// +optional
	TLSServerConfig *TLSServerConfig `json:"tls_server_config,omitempty"`
	// HTTPServerConfig defines http server configuration for alertmanager web server
	// +optional
	HTTPServerConfig *AlertmanagerHTTPConfig `json:"http_server_config,omitempty"`
	// BasicAuthUsers Usernames and hashed passwords that have full access to the web server
	// Passwords must be hashed with bcrypt
	// +optional
	BasicAuthUsers map[string]string `json:"basic_auth_users,omitempty"`
}

// AlertmanagerHTTPConfig defines http server configuration for alertmanager
type AlertmanagerHTTPConfig struct {
	// HTTP2 enables HTTP/2 support. Note that HTTP/2 is only supported with TLS.
	// This can not be changed on the fly.
	// +optional
	HTTP2 bool `json:"http2,omitempty"`
	// Headers defines list of headers that can be added to HTTP responses.
	// +optional
	Headers map[string]string `json:"headers,omitempty"`
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (r *VMAlertmanager) GetAdditionalService() *AdditionalServiceSpec {
	return r.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VMAlertmanager{}, &VMAlertmanagerList{})
}
