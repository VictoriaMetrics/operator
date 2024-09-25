package v1beta1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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
	// UseStrictSecurity enables strict security mode for component
	// it restricts disk writes access
	// uses non-root user out of the box
	// drops not needed security permissions
	// +optional
	UseStrictSecurity *bool `json:"useStrictSecurity,omitempty"`

	// WebConfig defines configuration for webserver
	// https://github.com/prometheus/alertmanager/blob/main/docs/https.md
	// +optional
	WebConfig *AlertmanagerWebConfig `json:"webConfig,omitempty"`

	// GossipConfig defines gossip TLS configuration for Alertmanager cluster
	// +optional
	GossipConfig *AlertmanagerGossipConfig `json:"gossipConfig,omitempty"`

	CommonDefaultableParams           `json:",inline,omitempty"`
	CommonConfigReloaderParams        `json:",inline,omitempty"`
	CommonApplicationDeploymentParams `json:",inline,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAlertmanager) UnmarshalJSON(src []byte) error {
	type pcr VMAlertmanager
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	prev, err := parseLastAppliedSpec[VMAlertmanagerSpec](cr)
	if err != nil {
		return err
	}
	cr.ParsedLastAppliedSpec = prev
	return nil
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
	Reason string `json:"reason,omitempty"`
}

func (cr *VMAlertmanager) AsOwner() []metav1.OwnerReference {
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
	return cr.Spec.ServiceAccountName == ""
}

func (cr VMAlertmanager) GetNSName() string {
	return cr.GetNamespace()
}

// Port returns port for accessing alertmanager
func (cr *VMAlertmanager) Port() string {
	port := cr.Spec.Port
	if port == "" {
		port = "9093"
	}

	return port
}

// AsURL returns url for accessing alertmanager
// via corresponding service
func (cr *VMAlertmanager) AsURL() string {
	port := cr.Port()
	portName := cr.Spec.PortName
	if portName == "" {
		portName = "web"
	}
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.ServiceSpec.Spec.Ports {
			if svcPort.Name == portName {
				port = fmt.Sprintf("%d", svcPort.Port)
				break
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", cr.accessScheme(), cr.PrefixedName(), cr.Namespace, port)
}

// returns fqdn for direct pod access
func (cr *VMAlertmanager) asPodFQDN(idx int) string {
	return fmt.Sprintf("%s://%s-%d.%s.%s.svc:%s", cr.accessScheme(), cr.PrefixedName(), idx, cr.PrefixedName(), cr.Namespace, cr.Port())
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VMAlertmanager) GetMetricPath() string {
	if prefix := cr.Spec.RoutePrefix; prefix != "" {
		return path.Join(prefix, metricPath)
	}
	return metricPath
}

// GetExtraArgs returns additionally configured command-line arguments
func (cr VMAlertmanager) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// GetServiceScrape returns overrides for serviceScrape builder
func (cr VMAlertmanager) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
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
			URL: cr.asPodFQDN(i),
		}
		r = append(r, ns)
	}
	return r
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

func (cr *VMAlertmanager) accessScheme() string {
	if cr.Spec.WebConfig != nil && cr.Spec.WebConfig.TLSServerConfig != nil {
		// special case for mTLS
		return "https"
	}
	return "http"
}

// ProbeScheme returns scheme for probe
func (cr *VMAlertmanager) ProbeScheme() string {
	if cr.Spec.WebConfig != nil && cr.Spec.WebConfig.TLSServerConfig != nil {
		return "HTTPS"
	}
	return "HTTP"
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
	lastAppliedClusterJSON := cr.Annotations[lastAppliedSpecAnnotationName]
	if len(lastAppliedClusterJSON) == 0 {
		return true, nil
	}

	instanceSpecData, err := json.Marshal(cr.Spec)
	if err != nil {
		return false, err
	}
	return !bytes.Equal([]byte(lastAppliedClusterJSON), instanceSpecData), nil
}

func (cr *VMAlertmanager) Paused() bool {
	return cr.Spec.Paused
}

// SetStatusTo changes update status with optional reason of fail
func (cr *VMAlertmanager) SetUpdateStatusTo(ctx context.Context, r client.Client, status UpdateStatus, maybeErr error) error {
	currentStatus := cr.Status.UpdateStatus
	prevStatus := cr.Status.DeepCopy()

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
	if equality.Semantic.DeepEqual(&cr.Status, prevStatus) && currentStatus == status {
		return nil
	}
	cr.Status.UpdateStatus = status
	if err := statusPatch(ctx, r, cr.DeepCopy(), cr.Status); err != nil {
		return fmt.Errorf("cannot patch status: %w", err)
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
func (cr *VMAlertmanager) GetAdditionalService() *AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VMAlertmanager{}, &VMAlertmanagerList{})
}
