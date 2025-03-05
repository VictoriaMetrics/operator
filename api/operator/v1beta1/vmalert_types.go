package v1beta1

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
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
	// only one of notifier options could be chosen: notifierConfigRef or notifiers +  notifier
	// +optional
	Notifier *VMAlertNotifierSpec `json:"notifier,omitempty"`

	// Notifiers prometheus alertmanager endpoints. Required at least one of notifier or notifiers when there are alerting rules. e.g. http://127.0.0.1:9093
	// If specified both notifier and notifiers, notifier will be added as last element to notifiers.
	// only one of notifier options could be chosen: notifierConfigRef or notifiers +  notifier
	// +optional
	Notifiers []VMAlertNotifierSpec `json:"notifiers,omitempty"`

	// NotifierConfigRef reference for secret with notifier configuration for vmalert
	// only one of notifier options could be chosen: notifierConfigRef or notifiers +  notifier
	// +optional
	NotifierConfigRef *v1.SecretKeySelector `json:"notifierConfigRef,omitempty"`

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
	// See [here](https://docs.victoriametrics.com/enterprise)
	// +optional
	License *License `json:"license,omitempty"`

	*ServiceAccount `json:",inline,omitempty"`

	CommonDefaultableParams           `json:",inline,omitempty"`
	CommonConfigReloaderParams        `json:",inline,omitempty"`
	CommonApplicationDeploymentParams `json:",inline,omitempty"`
}

func (r *VMAlert) setLastSpec(prevSpec VMAlertSpec) {
	r.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (r *VMAlert) UnmarshalJSON(src []byte) error {
	type pr VMAlert
	if err := json.Unmarshal(src, (*pr)(r)); err != nil {
		return err
	}
	if err := parseLastAppliedState(r); err != nil {
		return err
	}

	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (r *VMAlertSpec) UnmarshalJSON(src []byte) error {
	type pr VMAlertSpec
	if err := json.Unmarshal(src, (*pr)(r)); err != nil {
		r.ParsingError = fmt.Sprintf("cannot parse vmalert spec: %s, err: %s", string(src), err)
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

// NotifierAsMapKey - returns r name with suffix for notifier token/auth maps.
func (r *VMAlert) NotifierAsMapKey(i int) string {
	return fmt.Sprintf("vmalert/%s/%s/%d", r.Namespace, r.Name, i)
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
func (r *VMAlertStatus) GetStatusMetadata() *StatusMetadata {
	return &r.StatusMetadata
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
// +kubebuilder:printcolumn:name="ReplicaCount",type="integer",JSONPath=".spec.replicaCount",description="The desired replicas number of Alertmanagers"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VMAlert struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VMAlertSpec `json:"spec,omitempty"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VMAlertSpec `json:"-" yaml:"-"`

	Status VMAlertStatus `json:"status,omitempty"`
}

func (r *VMAlert) Probe() *EmbeddedProbes {
	return r.Spec.EmbeddedProbes
}

func (r *VMAlert) ProbePath() string {
	return buildPathWithPrefixFlag(r.Spec.ExtraArgs, healthPath)
}

func (r *VMAlert) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(r.Spec.ExtraArgs))
}

func (r *VMAlert) ProbePort() string {
	return r.Spec.Port
}

func (r *VMAlert) ProbeNeedLiveness() bool {
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
func (r *VMAlert) AsOwner() []metav1.OwnerReference {
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

func (r *VMAlert) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if r.Spec.PodMetadata != nil {
		for annotation, value := range r.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (r *VMAlert) AnnotationsFiltered() map[string]string {
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

// Validate checks VMAlert spec
func (r *VMAlert) Validate() error {
	if mustSkipValidation(r) {
		return nil
	}
	if r.Spec.ServiceSpec != nil && r.Spec.ServiceSpec.Name == r.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", r.PrefixedName())
	}
	if r.Spec.Datasource.URL == "" {
		return fmt.Errorf("spec.datasource.url cannot be empty")
	}
	if r.Spec.Notifier != nil {
		if r.Spec.Notifier.URL == "" && r.Spec.Notifier.Selector == nil {
			return fmt.Errorf("spec.notifier.url and spec.notifier.selector cannot be empty at the same time, provide at least one setting")
		}
	}
	for idx, nt := range r.Spec.Notifiers {
		if nt.URL == "" && nt.Selector == nil {
			return fmt.Errorf("notifier.url is empty and selector is not set, provide at least once for spec.notifiers at idx: %d", idx)
		}
	}
	if _, ok := r.Spec.ExtraArgs["notifier.blackhole"]; !ok {
		if r.Spec.Notifier == nil && len(r.Spec.Notifiers) == 0 && r.Spec.NotifierConfigRef == nil {
			return fmt.Errorf("vmalert should have at least one notifier.url or enable `-notifier.blackhole`")
		}
	}
	return nil
}

func (r *VMAlert) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmalert",
		"app.kubernetes.io/instance":  r.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (r *VMAlert) PodLabels() map[string]string {
	lbls := r.SelectorLabels()
	if r.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(r.Spec.PodMetadata.Labels, lbls)
}

func (r *VMAlert) AllLabels() map[string]string {
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

func (r *VMAlert) PrefixedName() string {
	return fmt.Sprintf("vmalert-%s", r.Name)
}

func (r *VMAlert) TLSAssetName() string {
	return fmt.Sprintf("tls-assets-vmalert-%s", r.Name)
}

// GetMetricPath returns prefixed path for metric requests
func (r *VMAlert) GetMetricPath() string {
	return buildPathWithPrefixFlag(r.Spec.ExtraArgs, metricPath)
}

// GetExtraArgs returns additionally configured command-line arguments
func (r *VMAlert) GetExtraArgs() map[string]string {
	return r.Spec.ExtraArgs
}

// GetServiceScrape returns overrides for serviceScrape builder
func (r *VMAlert) GetServiceScrape() *VMServiceScrapeSpec {
	return r.Spec.ServiceScrapeSpec
}

func (r *VMAlert) NeedDedupRules() bool {
	return r.ObjectMeta.Annotations[MetaVMAlertDeduplicateRulesKey] != ""
}

func (r *VMAlert) GetServiceAccount() *ServiceAccount {
	sa := r.Spec.ServiceAccount
	if sa == nil {
		sa = &ServiceAccount{
			Name:           r.PrefixedName(),
			AutomountToken: true,
		}
	}
	return sa
}

func (r *VMAlert) IsOwnsServiceAccount() bool {
	if r.Spec.ServiceAccount != nil && r.Spec.ServiceAccount.Name != "" {
		return r.Spec.ServiceAccount.Name == ""
	}
	return false
}

// GetNSName implements build.builderOpts interface
func (r *VMAlert) GetNSName() string {
	return r.GetNamespace()
}

func (r *VMAlert) RulesConfigMapSelector() client.ListOption {
	return &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{"vmalert-name": r.Name}),
		Namespace:     r.Namespace,
	}
}

func (r *VMAlert) AsURL() string {
	port := r.Spec.Port
	if port == "" {
		port = "8080"
	}
	if r.Spec.ServiceSpec != nil && r.Spec.ServiceSpec.UseAsDefault {
		for _, svcPort := range r.Spec.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
				break
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", protoFromFlags(r.Spec.ExtraArgs), r.PrefixedName(), r.Namespace, port)
}

// AsCRDOwner implements interface
func (r *VMAlert) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(Alert)
}

func (r *VMAlert) GetNotifierSelectors() []*DiscoverySelector {
	var selectors []*DiscoverySelector
	for _, n := range r.Spec.Notifiers {
		if n.Selector == nil {
			continue
		}
		selectors = append(selectors, n.Selector)
	}
	if r.Spec.Notifier != nil && r.Spec.Notifier.Selector != nil {
		selectors = append(selectors, r.Spec.Notifier.Selector)
	}
	return selectors
}

// IsUnmanaged checks if object should managed any  config objects
func (r *VMAlert) IsUnmanaged() bool {
	return !r.Spec.SelectAllByDefault && r.Spec.RuleSelector == nil && r.Spec.RuleNamespaceSelector == nil
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (r *VMAlert) LastAppliedSpecAsPatch() (client.Patch, error) {
	return lastAppliedChangesAsPatch(r.ObjectMeta, r.Spec)
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (r *VMAlert) HasSpecChanges() (bool, error) {
	return hasStateChanges(r.ObjectMeta, r.Spec)
}

func (r *VMAlert) Paused() bool {
	return r.Spec.Paused
}

// SetStatusTo changes update status with optional reason of fail
func (r *VMAlert) SetUpdateStatusTo(ctx context.Context, c client.Client, status UpdateStatus, maybeErr error) error {
	return updateObjectStatus(ctx, c, &patchStatusOpts[*VMAlert, *VMAlertStatus]{
		actualStatus: status,
		r:            r,
		rStatus:      &r.Status,
		maybeErr:     maybeErr,
	})
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (r *VMAlert) GetAdditionalService() *AdditionalServiceSpec {
	return r.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VMAlert{}, &VMAlertList{})
}
