package v1beta1

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// MetaVMAlertDeduplicateRulesKey - controls behavior for vmalert rules deduplication
	// its useful for migration from prometheus.
	MetaVMAlertDeduplicateRulesKey = "operator.victoriametrics.com/vmalert-deduplicate-rules"
)

// VMAlertSpec defines the desired state of VMAlert
// +k8s:openapi-gen=true
type VMAlertSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// PodMetadata configures Labels and Annotations which are propagated to the VMAlert pods.
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *ManagedObjectsMetadata `json:"managedMetadata,omitempty"`

	// LogFormat for VMAlert to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VMAlert to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`

	// EvaluationInterval defines how often to evaluate rules by default
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

	// Notifier prometheus alertmanager endpoint spec. Required at least one of notifier or notifiers when there are alerting rules. e.g. http://127.0.0.1:9093
	// If specified both notifier and notifiers, notifier will be added as last element to notifiers.
	// only one of notifier options could be chosen: notifierConfigRef or notifiers + notifier
	// +optional
	Notifier *VMAlertNotifierSpec `json:"notifier,omitempty"`

	// Notifiers prometheus alertmanager endpoints. Required at least one of notifier or notifiers when there are alerting rules. e.g. http://127.0.0.1:9093
	// If specified both notifier and notifiers, notifier will be added as last element to notifiers.
	// only one of notifier options could be chosen: notifierConfigRef or notifiers + notifier
	// +optional
	Notifiers []VMAlertNotifierSpec `json:"notifiers,omitempty"`

	// NotifierConfigRef reference for secret with notifier configuration for vmalert
	// only one of notifier options could be chosen: notifierConfigRef or notifiers + notifier
	// +optional
	NotifierConfigRef *corev1.SecretKeySelector `json:"notifierConfigRef,omitempty"`

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

	// ExternalLabels in the form 'name: value' to add to all generated recording rules and alerts.
	// +optional
	ExternalLabels map[string]string `json:"externalLabels,omitempty"`

	// ServiceSpec that will be added to vmalert service spec
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmalert VMServiceScrape spec
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
	*EmbeddedProbes     `json:",inline"`
	// License allows to configure license key to be used for enterprise features.
	// Using license key is supported starting from VictoriaMetrics v1.94.0.
	// See [here](https://docs.victoriametrics.com/victoriametrics/enterprise/)
	// +optional
	License *License `json:"license,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	CommonDefaultableParams           `json:",inline,omitempty"`
	CommonConfigReloaderParams        `json:",inline,omitempty"`
	CommonApplicationDeploymentParams `json:",inline,omitempty"`
}

// SetLastSpec implements objectWithLastAppliedState interface
func (cr *VMAlert) SetLastSpec(prevSpec VMAlertSpec) {
	cr.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAlert) UnmarshalJSON(src []byte) error {
	type pcr VMAlert
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	if err := ParseLastAppliedStateTo(cr); err != nil {
		return err
	}

	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAlertSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAlertSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vmalert spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// VMAlertDatasourceSpec defines the remote storage configuration for VmAlert to read alerts from
// +k8s:openapi-gen=true
type VMAlertDatasourceSpec struct {
	// Victoria Metrics or VMSelect url. Required parameter. E.g. http://127.0.0.1:8428
	URL string `json:"url"`
	// HTTPAuth generic auth methods
	HTTPAuth `json:",inline,omitempty"`
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

	HTTPAuth `json:",inline,omitempty"`
}

func (ns *VMAlertNotifierSpec) validate() error {
	if ns.URL == "" && ns.Selector == nil {
		return fmt.Errorf("notifier.url and notifier.selector cannot be empty at the same time, provide at least one setting")
	}
	if len(ns.URL) > 0 {
		if _, err := url.Parse(ns.URL); err != nil {
			return fmt.Errorf("incorrect notifier.url syntax: %q: %s", ns.URL, err)
		}
	}
	ns.OAuth2.validate()

	return nil
}

// VMAlertRemoteReadSpec defines the remote storage configuration for VmAlert to read alerts from
// +k8s:openapi-gen=true
type VMAlertRemoteReadSpec struct {
	// URL of the endpoint to send samples to.
	URL string `json:"url"`
	// Lookback defines how far to look into past for alerts timeseries. For example, if lookback=1h then range from now() to now()-1h will be scanned. (default 1h0m0s)
	// Applied only to RemoteReadSpec
	// +optional
	Lookback *string `json:"lookback,omitempty"`

	HTTPAuth `json:",inline,omitempty"`
}

// VMAlertRemoteWriteSpec defines the remote storage configuration for VmAlert
// +k8s:openapi-gen=true
type VMAlertRemoteWriteSpec struct {
	// URL of the endpoint to send samples to.
	URL string `json:"url"`
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
	// HTTPAuth generic auth methods
	HTTPAuth `json:",inline,omitempty"`
}

// VMAlertStatus defines the observed state of VMAlert
// +k8s:openapi-gen=true
type VMAlertStatus struct {
	StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VMAlertStatus) GetStatusMetadata() *StatusMetadata {
	return &cr.StatusMetadata
}

// VMAlert  executes a list of given alerting or recording rules against configured address.
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMAlert App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +genclient
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmalerts,scope=Namespaced
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of update rollout"
// +kubebuilder:printcolumn:name="ReplicaCount",type="integer",JSONPath=".spec.replicaCount",description="The desired replicas number of VMAlerts"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VMAlert struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VMAlertSpec `json:"spec,omitempty"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VMAlertSpec `json:"-" yaml:"-"`

	Status VMAlertStatus `json:"status,omitempty"`
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMAlert) GetStatus() *VMAlertStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMAlert) DefaultStatusFields(vs *VMAlertStatus) {
}

func (cr *VMAlert) Probe() *EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

func (cr *VMAlert) ProbePath() string {
	return BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

func (cr *VMAlert) ProbeScheme() string {
	return strings.ToUpper(HTTPProtoFromFlags(cr.Spec.ExtraArgs))
}

func (cr *VMAlert) ProbePort() string {
	return cr.Spec.Port
}

func (*VMAlert) ProbeNeedLiveness() bool {
	return true
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VMAlertList contains a list of VMAlert
type VMAlertList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAlert `json:"items"`
}

// AsOwner returns owner references with current object as owner
func (cr *VMAlert) AsOwner() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         cr.APIVersion,
		Kind:               cr.Kind,
		Name:               cr.Name,
		UID:                cr.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
}

func (cr *VMAlert) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

// Validate checks VMAlert spec
func (cr *VMAlert) Validate() error {
	if MustSkipCRValidation(cr) {
		return nil
	}
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.Name == cr.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", cr.PrefixedName())
	}
	if cr.Spec.Datasource.URL == "" {
		return fmt.Errorf("spec.datasource.url cannot be empty")
	}

	validateNotifierConfigs := func() error {
		_, hasNotifierBlackhole := cr.Spec.ExtraArgs["notifier.blackhole"]
		// check configuration based on vmalert validation logic:
		// https://github.com/VictoriaMetrics/VictoriaMetrics/blob/5a80d4c5520045cd1f8202a33a91f0bf77f4ca24/app/vmalert/notifier/init.go#L140
		if hasNotifierBlackhole && (cr.Spec.Notifier != nil || len(cr.Spec.Notifiers) > 0 || cr.Spec.NotifierConfigRef != nil) {
			return fmt.Errorf("notifier.blackhole and spec.notifier or spec.notifiers or spec.notifier.notifierConfigRef cannot be used at the same time, provide at most one setting")
		}
		if (cr.Spec.Notifier != nil || len(cr.Spec.Notifiers) > 0) && cr.Spec.NotifierConfigRef != nil {
			return fmt.Errorf("spec.notifiers and spec.notifier.notifierConfigRef cannot be used at the same time, provide at most one setting")
		}

		if cr.Spec.Notifier != nil {
			if err := cr.Spec.Notifier.validate(); err != nil {
				return fmt.Errorf("incorrect cr.spec.notifier syntax: %w", err)
			}
		}
		for idx, nt := range cr.Spec.Notifiers {
			if err := nt.validate(); err != nil {
				return fmt.Errorf("incorrect cr.spec.notifiers idx=%d syntax: %w", idx, err)
			}
		}
		return nil
	}
	if err := validateNotifierConfigs(); err != nil {
		return err
	}
	return nil
}

func (cr *VMAlert) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmalert",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr *VMAlert) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

// FinalLabels returns combination of selector and managed labels
func (cr *VMAlert) FinalLabels() map[string]string {
	v := cr.SelectorLabels()
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Labels, v)
	}
	return v
}

// FinalAnnotations returns annotations to be applied for created objects
func (cr *VMAlert) FinalAnnotations() map[string]string {
	var v map[string]string
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Annotations, v)
	}
	return v
}

func (cr *VMAlert) PrefixedName() string {
	return fmt.Sprintf("vmalert-%s", cr.Name)
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VMAlert) GetMetricPath() string {
	return BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

// GetExtraArgs returns additionally configured command-line arguments
func (cr *VMAlert) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// GetServiceScrape returns overrides for serviceScrape builder
func (cr *VMAlert) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

func (cr *VMAlert) NeedDedupRules() bool {
	return cr.Annotations[MetaVMAlertDeduplicateRulesKey] != ""
}

func (cr *VMAlert) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

// IsOwnsServiceAccount checks if serviceAccount belongs to the CR
func (cr *VMAlert) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

func (cr *VMAlert) RulesConfigMapSelector() client.ListOption {
	return &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{"vmalert-name": cr.Name}),
		Namespace:     cr.Namespace,
	}
}

func (cr *VMAlert) AsURL() string {
	port := cr.Spec.Port
	if port == "" {
		port = "8080"
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

// IsUnmanaged checks if object should managed any  config objects
func (cr *VMAlert) IsUnmanaged() bool {
	return !cr.Spec.SelectAllByDefault && cr.Spec.RuleSelector == nil && cr.Spec.RuleNamespaceSelector == nil
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VMAlert) LastAppliedSpecAsPatch() (client.Patch, error) {
	return LastAppliedChangesAsPatch(cr.Spec)
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (cr *VMAlert) HasSpecChanges() (bool, error) {
	return HasStateChanges(cr.ObjectMeta, cr.Spec)
}

func (cr *VMAlert) Paused() bool {
	return cr.Spec.Paused
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMAlert) GetAdditionalService() *AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VMAlert{}, &VMAlertList{})
}
