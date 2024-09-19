package v1beta1

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"strings"

	"gopkg.in/yaml.v2"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/autoscaling/v2beta2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateStatus defines status for application
type UpdateStatus string

const (
	UpdateStatusExpanding   UpdateStatus = "expanding"
	UpdateStatusOperational UpdateStatus = "operational"
	UpdateStatusFailed      UpdateStatus = "failed"
	UpdateStatusPaused      UpdateStatus = "paused"
)

const (
	vmPathPrefixFlagName = "http.pathPrefix"
	healthPath           = "/health"
	metricPath           = "/metrics"
	reloadPath           = "/-/reload"
	reloadAuthKey        = "reloadAuthKey"
	snapshotCreate       = "/snapshot/create"
	snapshotDelete       = "/snapshot/delete"
)

const (
	// FinalizerName name of vm-operator finalizer.
	FinalizerName            = "apps.victoriametrics.com/finalizer"
	SkipValidationAnnotation = "operator.victoriametrics.com/skip-validation"
	APIGroup                 = "operator.victoriametrics.com"
	SkipValidationValue      = "true"
	AdditionalServiceLabel   = "operator.victoriametrics.com/additional-service"
	// PVCExpandableLabel controls checks for storageClass
	PVCExpandableLabel            = "operator.victoriametrics.com/pvc-allow-volume-expansion"
	lastAppliedSpecAnnotationName = "operator.victoriametrics/last-applied-spec"
)

const (
	SecretsDir          = "/etc/vm/secrets"
	ConfigMapsDir       = "/etc/vm/configs"
	TemplatesDir        = "/etc/vm/templates"
	StreamAggrConfigDir = "/etc/vm/stream-aggr"
	RelabelingConfigDir = "/etc/vm/relabeling"
)

// SchemeGroupVersion is group version used to register these objects
var SchemeGroupVersion = schema.GroupVersion{Group: "operator.victoriametrics.com", Version: "v1beta1"}

var (
	labelFilterPrefixes []string
	// default ignored annotations
	annotationFilterPrefixes = []string{"kubectl.kubernetes.io/", "operator.victoriametrics.com/", "operator.victoriametrics/last-applied-spec"}
)

// SetLabelAndAnnotationPrefixes configures global filtering for child labels and annotations
// cannot be used concurrently and should be called only once at lib init
func SetLabelAndAnnotationPrefixes(labelPrefixes, annotationPrefixes []string) {
	labelFilterPrefixes = labelPrefixes
	annotationFilterPrefixes = append(annotationFilterPrefixes, annotationPrefixes...)
}

func filterMapKeysByPrefixes(src map[string]string, prefixes []string) map[string]string {
	dst := make(map[string]string, len(src))
OUTER:
	for key, value := range src {
		for _, matchPrefix := range prefixes {
			if strings.HasPrefix(key, matchPrefix) {
				continue OUTER
			}
		}
		dst[key] = value
	}
	return dst
}

// skip validation, if object has annotation.
func mustSkipValidation(cr client.Object) bool {
	return cr.GetAnnotations()[SkipValidationAnnotation] == SkipValidationValue
}

// AddFinalizer conditionally adds vm-operator finalizer to the dst object
// respectfully merges exist finalizers from src to dst
func AddFinalizer(dst, src client.Object) {
	srcFinalizers := src.GetFinalizers()
	if !isContainsFinalizer(srcFinalizers) {
		srcFinalizers = append(srcFinalizers, FinalizerName)
		dst.SetFinalizers(srcFinalizers)
	}
	dst.SetFinalizers(srcFinalizers)
}

// AddFinalizerAndThen conditionally adds vm-operator finalizer to the dst object
// respectfully merges exist finalizers from src to dst
// if finalizer was added, peforms callback
func AddFinalizerAndThen(src client.Object, andThen func(client.Object) error) error {
	srcFinalizers := src.GetFinalizers()
	var wasNotFinalizerFound bool
	if !isContainsFinalizer(srcFinalizers) {
		srcFinalizers = append(srcFinalizers, FinalizerName)
		wasNotFinalizerFound = true
		src.SetFinalizers(srcFinalizers)
	}
	if wasNotFinalizerFound {
		return andThen(src)
	}
	return nil
}

func isContainsFinalizer(src []string) bool {
	for _, s := range src {
		if s == FinalizerName {
			return true
		}
	}
	return false
}

// RemoveFinalizer - removes vm-operator finalizer from finalizers list.
// executes provided callback if finalizer found
func RemoveFinalizer(src client.Object, andThen func(client.Object) error) error {
	return RemoveFinalizerWithOwnerReference(src, true, andThen)
}

func RemoveFinalizerWithOwnerReference(src client.Object, keepOwnerReference bool, andThen func(client.Object) error) error {
	existFinalizers := src.GetFinalizers()
	var wasFound bool
	dstFinalizers := existFinalizers[:0]
	// filter in-place
	for _, s := range existFinalizers {
		if s == FinalizerName {
			wasFound = true
			continue
		}
		dstFinalizers = append(dstFinalizers, s)
	}
	src.SetFinalizers(dstFinalizers)
	if !keepOwnerReference {
		existOwnerReferences := src.GetOwnerReferences()
		dstOwnerReferences := existOwnerReferences[:0]
		// filter in-place
		for _, s := range existOwnerReferences {
			if strings.HasPrefix(s.APIVersion, APIGroup) {
				wasFound = true
				continue
			}
			dstOwnerReferences = append(dstOwnerReferences, s)
		}
		src.SetOwnerReferences(dstOwnerReferences)
	}

	if wasFound && andThen != nil {
		return andThen(src)
	}
	return nil
}

// EmbeddedObjectMetadata contains a subset of the fields included in k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta
// Only fields which are relevant to embedded resources are included.
type EmbeddedObjectMetadata struct {
	// Name must be unique within a namespace. Is required when creating resources, although
	// some resources may allow a client to request the generation of an appropriate name
	// automatically. Name is primarily intended for creation idempotence and configuration
	// definition.
	// Cannot be updated.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names#names
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

	// Labels Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="PodLabels"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:label"
	// +optional
	Labels map[string]string `json:"labels,omitempty" protobuf:"bytes,11,rep,name=labels"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations
	// +optional
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,12,rep,name=annotations"`
}

// StorageSpec defines the configured storage for a group Prometheus servers.
// If neither `emptyDir` nor `volumeClaimTemplate` is specified, then by default an [EmptyDir](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir) will be used.
// +k8s:openapi-gen=true
type StorageSpec struct {
	// Deprecated: subPath usage will be disabled by default in a future release, this option will become unnecessary.
	// DisableMountSubPath allows to remove any subPath usage in volume mounts.
	// +optional
	DisableMountSubPath bool `json:"disableMountSubPath,omitempty"`
	// EmptyDirVolumeSource to be used by the Prometheus StatefulSets. If specified, used in place of any volumeClaimTemplate. More
	// info: https://kubernetes.io/docs/concepts/storage/volumes/#emptydir
	// +optional
	EmptyDir *v1.EmptyDirVolumeSource `json:"emptyDir,omitempty"`
	// A PVC spec to be used by the VMAlertManager StatefulSets.
	// +optional
	VolumeClaimTemplate EmbeddedPersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
}

// IntoSTSVolume converts storageSpec into proper volume for statefulsetSpec
// by default, it adds emptyDir volume.
func (ss *StorageSpec) IntoSTSVolume(name string, sts *appsv1.StatefulSetSpec) {
	switch {
	case ss == nil:
		sts.Template.Spec.Volumes = append(sts.Template.Spec.Volumes, v1.Volume{
			Name: name,
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		})
	case ss.EmptyDir != nil:
		sts.Template.Spec.Volumes = append(sts.Template.Spec.Volumes, v1.Volume{
			Name: name,
			VolumeSource: v1.VolumeSource{
				EmptyDir: ss.EmptyDir,
			},
		})
	default:
		claimTemplate := ss.VolumeClaimTemplate
		stsClaim := v1.PersistentVolumeClaim{
			TypeMeta: metav1.TypeMeta{
				APIVersion: claimTemplate.APIVersion,
				Kind:       claimTemplate.Kind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Labels:      claimTemplate.Labels,
				Annotations: claimTemplate.Annotations,
			},
			Spec:   claimTemplate.Spec,
			Status: claimTemplate.Status,
		}
		if stsClaim.Spec.AccessModes == nil {
			stsClaim.Spec.AccessModes = []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}
		}
		sts.VolumeClaimTemplates = append(sts.VolumeClaimTemplates, stsClaim)
	}
}

// EmbeddedPersistentVolumeClaim is an embedded version of k8s.io/api/core/v1.PersistentVolumeClaim.
// It contains TypeMeta and a reduced ObjectMeta.
type EmbeddedPersistentVolumeClaim struct {
	metav1.TypeMeta `json:",inline"`

	// EmbeddedMetadata contains metadata relevant to an EmbeddedResource.
	// +optional
	EmbeddedObjectMetadata `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec defines the desired characteristics of a volume requested by a pod author.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	// +optional
	Spec v1.PersistentVolumeClaimSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current information/status of a persistent volume claim.
	// Read-only.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	// +optional
	Status v1.PersistentVolumeClaimStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// HTTPAuth generic auth used with http protocols
type HTTPAuth struct {
	// +optional
	BasicAuth *BasicAuth `json:"basicAuth,omitempty"`
	// +optional
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`
	// +optional
	TLSConfig *TLSConfig `json:"tlsConfig,omitempty"`
	// +optional
	*BearerAuth `json:",inline,omitempty"`
	// Headers allow configuring custom http headers
	// Must be in form of semicolon separated header with value
	// e.g.
	// headerName:headerValue
	// vmalert supports it since 1.79.0 version
	// +optional
	Headers []string `json:"headers,omitempty"`
}

// BearerAuth defines auth with bearer token
type BearerAuth struct {
	// Path to bearer token file
	// +optional
	TokenFilePath string `json:"bearerTokenFile,omitempty"`
	// Optional bearer auth token to use for -remoteWrite.url
	// +optional
	TokenSecret *v1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
}

// BasicAuth allow an endpoint to authenticate over basic authentication
// +k8s:openapi-gen=true
type BasicAuth struct {
	// Username defines reference for secret with username value
	// The secret needs to be in the same namespace as scrape object
	// +optional
	Username v1.SecretKeySelector `json:"username,omitempty"`
	// Password defines reference for secret with password value
	// The secret needs to be in the same namespace as scrape object
	// +optional
	Password v1.SecretKeySelector `json:"password,omitempty"`
	// PasswordFile defines path to password file at disk
	// must be pre-mounted
	// +optional
	PasswordFile string `json:"password_file,omitempty" yaml:"password_file,omitempty"`
}

// ServiceSpec defines additional service for CRD with user-defined params.
// by default, some of fields can be inherited from default service definition for the CRD:
// labels,selector, ports.
// if metadata.name is not defined, service will have format {{CRD_TYPE}}-{{CRD_NAME}}-additional-service.
// if UseAsDefault is set to true, changes applied to the main service without additional service creation
// +k8s:openapi-gen=true
type AdditionalServiceSpec struct {
	// UseAsDefault applies changes from given service definition to the main object Service
	// Changing from headless service to clusterIP or loadbalancer may break cross-component communication
	// +optional
	UseAsDefault bool `json:"useAsDefault,omitempty"`
	// EmbeddedObjectMetadata defines objectMeta for additional service.
	// +optional
	EmbeddedObjectMetadata `json:"metadata,omitempty"`
	// ServiceSpec describes the attributes that a user creates on a service.
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/
	Spec v1.ServiceSpec `json:"spec"`
}

// IsSomeAndThen applies callback to the addtionalServiceSpec if it's not nil or do not used as default service
func (asc *AdditionalServiceSpec) IsSomeAndThen(cb func(s *AdditionalServiceSpec) error) error {
	if asc == nil || asc.UseAsDefault {
		return nil
	}
	return cb(asc)
}

// NameOrDefault returns name or default value with suffix
func (ss *AdditionalServiceSpec) NameOrDefault(defaultName string) string {
	if ss.Name != "" {
		return ss.Name
	}
	return defaultName + "-additional-service"
}

// MaybeEnableProxyProtocol conditionally adds proxy protocol for custom config-reloader image
// useful for vmagent and vmauth
func MaybeEnableProxyProtocol(args []string, extaArgs map[string]string) []string {
	if v, ok := extaArgs["httpListenAddr.useProxyProtocol"]; ok && v == "true" {
		args = append(args, "--reload-use-proxy-protocol")
	}
	return args
}

// BuildReloadPathWithPort builds reload api path for given args
func BuildReloadPathWithPort(extraArgs map[string]string, port string) string {
	proto := protoFromFlags(extraArgs)
	urlPath := joinPathAuthKey(buildPathWithPrefixFlag(extraArgs, reloadPath), reloadAuthKey, extraArgs)
	return fmt.Sprintf("%s://localhost:%s%s", proto, port, urlPath)
}

func buildPathWithPrefixFlag(flags map[string]string, defaultPath string) string {
	if prefix, ok := flags[vmPathPrefixFlagName]; ok {
		return path.Join(prefix, defaultPath)
	}
	return defaultPath
}

func protoFromFlags(flags map[string]string) string {
	proto := "http"
	if flags["tls"] == "true" {
		proto = "https"
	}
	return proto
}

func joinPathAuthKey(urlPath string, keyName string, extraArgs map[string]string) string {
	if authKey, ok := extraArgs[keyName]; ok {
		separator := "?"
		idx := strings.IndexByte(urlPath, '?')
		if idx > 0 {
			separator = "&"
		}
		return urlPath + separator + "authKey=" + authKey
	}
	return urlPath
}

type EmbeddedPodDisruptionBudgetSpec struct {
	// An eviction is allowed if at least "minAvailable" pods selected by
	// "selector" will still be available after the eviction, i.e. even in the
	// absence of the evicted pod.  So for example you can prevent all voluntary
	// evictions by specifying "100%".
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty"`

	// An eviction is allowed if at most "maxUnavailable" pods selected by
	// "selector" are unavailable after the eviction, i.e. even in absence of
	// the evicted pod. For example, one can prevent all voluntary evictions
	// by specifying 0. This is a mutually exclusive setting with "minAvailable".
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
	// replaces default labels selector generated by operator
	// it's useful when you need to create custom budget
	// +optional
	SelectorLabels map[string]string `json:"selectorLabels,omitempty"`
}

// SelectorLabelsWithDefaults return defaultSelector or replaced selector defined by user
func (epdbs *EmbeddedPodDisruptionBudgetSpec) SelectorLabelsWithDefaults(defaultSelector map[string]string) map[string]string {
	if epdbs == nil || epdbs.SelectorLabels == nil {
		return defaultSelector
	}
	return epdbs.SelectorLabels
}

// EmbeddedProbes - it allows to override some probe params.
// its not necessary to specify all options,
// operator will replace missing spec with default values.
type EmbeddedProbes struct {
	// LivenessProbe that will be added CRD pod
	// +optional
	LivenessProbe *v1.Probe `json:"livenessProbe,omitempty"`
	// ReadinessProbe that will be added CRD pod
	// +optional
	ReadinessProbe *v1.Probe `json:"readinessProbe,omitempty"`
	// StartupProbe that will be added to CRD pod
	// +optional
	StartupProbe *v1.Probe `json:"startupProbe,omitempty"`
}

// EmbeddedHPA embeds HorizontalPodAutoScaler spec v2.
// https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/horizontal-pod-autoscaler-v2/
type EmbeddedHPA struct {
	MinReplicas *int32                                   `json:"minReplicas,omitempty"`
	MaxReplicas int32                                    `json:"maxReplicas,omitempty"`
	Metrics     []v2beta2.MetricSpec                     `json:"metrics,omitempty"`
	Behaviour   *v2beta2.HorizontalPodAutoscalerBehavior `json:"behaviour,omitempty"`
}

func (cr *EmbeddedHPA) sanityCheck() error {
	if cr.MinReplicas != nil && *cr.MinReplicas > cr.MaxReplicas {
		return fmt.Errorf("minReplicas cannot be greater then maxReplicas")
	}
	if cr.Behaviour == nil && len(cr.Metrics) == 0 {
		return fmt.Errorf("at least behaviour or metrics property must be configuread")
	}
	return nil
}

// DiscoverySelector can be used at CRD components discovery
type DiscoverySelector struct {
	Namespace *NamespaceSelector    `json:"namespaceSelector,omitempty"`
	Labels    *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

func (ds *DiscoverySelector) AsListOptions() (*client.ListOptions, error) {
	if ds.Labels == nil {
		return &client.ListOptions{}, nil
	}
	s, err := metav1.LabelSelectorAsSelector(ds.Labels)
	if err != nil {
		return nil, err
	}
	return &client.ListOptions{
		LabelSelector: s,
	}, nil
}

// ConfigMapKeyReference refers to a key in a ConfigMap.
type ConfigMapKeyReference struct {
	// The ConfigMap to refer to.
	v1.LocalObjectReference `json:",inline"`
	// The ConfigMap key to refer to.
	Key string `json:"key"`
}

// StreamAggrConfig defines the stream aggregation config
// +k8s:openapi-gen=true
type StreamAggrConfig struct {
	// Stream aggregation rules
	// +optional
	Rules []StreamAggrRule `json:"rules"`
	// ConfigMap with stream aggregation rules
	// +optional
	RuleConfigMap *v1.ConfigMapKeySelector `json:"configmap,omitempty"`
	// Allows writing both raw and aggregate data
	// +optional
	KeepInput bool `json:"keepInput,omitempty"`
	// Allow drop all the input samples after the aggregation
	// +optional
	DropInput bool `json:"dropInput,omitempty"`
	// Allows setting different de-duplication intervals per each configured remote storage
	// +optional
	DedupInterval string `json:"dedupInterval,omitempty"`
	// labels to drop from samples for aggregator before stream de-duplication and aggregation
	// +optional
	DropInputLabels      []string `json:"dropInputLabels,omitempty"`
	IgnoreFirstIntervals int      `json:"ignoreFirstIntervals,omitempty"`
	// IgnoreOldSamples instructs to ignore samples with old timestamps outside the current aggregation interval.
	// +optional
	IgnoreOldSamples bool `json:"ignoreOldSamples,omitempty"`
}

// StreamAggrRule defines the rule in stream aggregation config
// +k8s:openapi-gen=true
type StreamAggrRule struct {
	// Match is a label selector (or list of label selectors) for filtering time series for the given selector.
	//
	// If the match isn't set, then all the input time series are processed.
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	Match StringOrArray `json:"match,omitempty" yaml:"match,omitempty"`

	// Interval is the interval between aggregations.
	Interval string `json:"interval" yaml:"interval"`

	// NoAlignFlushToInterval disables aligning of flushes to multiples of Interval.
	// By default flushes are aligned to Interval.
	// +optional
	NoAlignFlushToInterval *bool `json:"no_align_flush_to_interval,omitempty" yaml:"no_align_flush_to_interval,omitempty"`

	// FlushOnShutdown defines whether to flush the aggregation state on process termination
	// or config reload. Is `false` by default.
	// It is not recommended changing this setting, unless unfinished aggregations states
	// are preferred to missing data points.
	// +optional
	FlushOnShutdown bool `json:"flush_on_shutdown,omitempty" yaml:"flush_on_shutdown,omitempty"`

	// DedupInterval is an optional interval for deduplication.
	// +optional
	DedupInterval string `json:"dedup_interval,omitempty" yaml:"dedup_interval,omitempty"`
	// Staleness interval is interval after which the series state will be reset if no samples have been sent during it.
	// The parameter is only relevant for outputs: total, total_prometheus, increase, increase_prometheus and histogram_bucket.
	// +optional
	StalenessInterval string `json:"staleness_interval,omitempty" yaml:"staleness_interval,omitempty"`

	// Outputs is a list of output aggregate functions to produce.
	//
	// The following names are allowed:
	//
	// - total - aggregates input counters
	// - increase - counts the increase over input counters
	// - count_series - counts the input series
	// - count_samples - counts the input samples
	// - sum_samples - sums the input samples
	// - last - the last biggest sample value
	// - min - the minimum sample value
	// - max - the maximum sample value
	// - avg - the average value across all the samples
	// - stddev - standard deviation across all the samples
	// - stdvar - standard variance across all the samples
	// - histogram_bucket - creates VictoriaMetrics histogram for input samples
	// - quantiles(phi1, ..., phiN) - quantiles' estimation for phi in the range [0..1]
	//
	// The output time series will have the following names:
	//
	//   input_name:aggr_<interval>_<output>
	//
	Outputs []string `json:"outputs"`

	// KeepMetricNames instructs to leave metric names as is for the output time series without adding any suffix.
	// +optional
	KeepMetricNames *bool `json:"keep_metric_names,omitempty" yaml:"keep_metric_names,omitempty"`

	// IgnoreOldSamples instructs to ignore samples with old timestamps outside the current aggregation interval.
	// +optional
	IgnoreOldSamples *bool `json:"ignore_old_samples,omitempty" yaml:"ignore_old_samples,omitempty"`

	// By is an optional list of labels for grouping input series.
	//
	// See also Without.
	//
	// If neither By nor Without are set, then the Outputs are calculated
	// individually per each input time series.
	// +optional
	By []string `json:"by,omitempty" yaml:"by,omitempty"`

	// Without is an optional list of labels, which must be excluded when grouping input series.
	//
	// See also By.
	//
	// If neither By nor Without are set, then the Outputs are calculated
	// individually per each input time series.
	// +optional
	Without []string `json:"without,omitempty" yaml:"without,omitempty"`

	IgnoreFirstIntervals *int `json:"ignore_first_intervals,omitempty" yaml:"ignore_first_intervals,omitempty"`

	// DropInputLabels is an optional list with labels, which must be dropped before further processing of input samples.
	//
	// Labels are dropped before de-duplication and aggregation.
	// +optional
	DropInputLabels *[]string `json:"drop_input_labels,omitempty" yaml:"drop_input_labels,omitempty"`

	// InputRelabelConfigs is an optional relabeling rules, which are applied on the input
	// before aggregation.
	// +optional
	InputRelabelConfigs []RelabelConfig `json:"input_relabel_configs,omitempty" yaml:"input_relabel_configs,omitempty"`

	// OutputRelabelConfigs is an optional relabeling rules, which are applied
	// on the aggregated output before being sent to remote storage.
	// +optional
	OutputRelabelConfigs []RelabelConfig `json:"output_relabel_configs,omitempty" yaml:"output_relabel_configs,omitempty"`
}

// HasAnyRule returns true if there is at least one aggregation rule
func (config *StreamAggrConfig) HasAnyRule() bool {
	if config != nil && (len(config.Rules) > 0 || config.RuleConfigMap != nil) {
		return true
	}
	return false
}

// KeyValue defines a (key, value) tuple.
// +kubebuilder:object:generate=false
// +k8s:openapi-gen=false
type KeyValue struct {
	// Key of the tuple.
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
	// Value of the tuple.
	Value string `json:"value"`
}

// StringOrArray is a helper type for storing string or array of string.
type StringOrArray []string

func (m StringOrArray) MarshalYAML() (interface{}, error) {
	switch len(m) {
	case 0:
		return "", nil
	case 1:
		return m[0], nil
	default:
		return []string(m), nil
	}
}

func (m *StringOrArray) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var raw any
	if err := unmarshal(&raw); err != nil {
		return fmt.Errorf("cannot unmarshal StringOrArray: %w", err)
	}
	rawType := reflect.TypeOf(raw)
	switch rawType.Kind() {
	case reflect.String:
		var match string
		if err := unmarshal(&match); err != nil {
			return err
		}
		*m = []string{match}
		return nil
	case reflect.Slice, reflect.Array:
		var match []string
		if err := unmarshal(&match); err != nil {
			return err
		}
		*m = match
		return nil
	default:
		return &yaml.TypeError{Errors: []string{
			fmt.Sprintf("cannot unmarshal %#v into StringOrArray of type %v", raw, rawType),
		}}
	}
}

func (m *StringOrArray) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("cannot unmarshal match: %w", err)
	}
	rawType := reflect.TypeOf(raw)
	switch rawType.Kind() {
	case reflect.String:
		var match string
		if err := json.Unmarshal(data, &match); err != nil {
			return err
		}
		*m = []string{match}
		return nil
	case reflect.Slice, reflect.Array:
		var match []string
		if err := json.Unmarshal(data, &match); err != nil {
			return err
		}
		*m = match
		return nil
	default:
		return &json.UnmarshalTypeError{Value: string(data), Type: rawType}
	}
}

// License holds license key for enterprise features.
// Using license key is supported starting from VictoriaMetrics v1.94.0.
// See [here](https://docs.victoriametrics.com/enterprise)
type License struct {
	// Enterprise license key. This flag is available only in [VictoriaMetrics enterprise](https://docs.victoriametrics.com/enterprise).
	// To request a trial license, [go to](https://victoriametrics.com/products/enterprise/trial)
	Key *string `json:"key,omitempty"`
	// KeyRef is reference to secret with license key for enterprise features.
	KeyRef *v1.SecretKeySelector `json:"keyRef,omitempty"`
}

// IsProvided returns true if license is provided.
func (l *License) IsProvided() bool {
	if l == nil {
		return false
	}

	return l.Key != nil || l.KeyRef != nil
}

// MaybeAddToArgs conditionally adds license commandline args into given args
func (l *License) MaybeAddToArgs(args []string, secretMountDir string) []string {
	if l == nil || !l.IsProvided() {
		return args
	}
	if l.Key != nil {
		args = append(args, fmt.Sprintf("-license=%s", *l.Key))
	}
	if l.KeyRef != nil {
		args = append(args, fmt.Sprintf("-licenseFile=%s", path.Join(secretMountDir, l.KeyRef.Name, l.KeyRef.Key)))
	}
	return args
}

// MaybeAddToVolumes conditionally mounts secret with license key into given volumes and mounts
func (l *License) MaybeAddToVolumes(volumes []v1.Volume, mounts []v1.VolumeMount, secretMountDir string) ([]v1.Volume, []v1.VolumeMount) {
	if l == nil || l.KeyRef == nil {
		return volumes, mounts
	}
	volumes = append(volumes, v1.Volume{
		Name: "license",
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: l.KeyRef.Name,
			},
		},
	})
	mounts = append(mounts, v1.VolumeMount{
		Name:      "license",
		ReadOnly:  true,
		MountPath: path.Join(secretMountDir, l.KeyRef.Name),
	})
	return volumes, mounts
}

func (l *License) sanityCheck() error {
	if !l.IsProvided() {
		return nil
	}

	if l.Key != nil && l.KeyRef != nil {
		return fmt.Errorf("only one of key or keyRef can be specified")
	}

	return nil
}

// SecretOrConfigMap allows to specify data as a Secret or ConfigMap. Fields are mutually exclusive.
type SecretOrConfigMap struct {
	// Secret containing data to use for the targets.
	// +optional
	Secret *v1.SecretKeySelector `json:"secret,omitempty"`
	// ConfigMap containing data to use for the targets.
	// +optional
	ConfigMap *v1.ConfigMapKeySelector `json:"configMap,omitempty"`
}

// TLSConfig specifies TLSConfig configuration parameters.
// +k8s:openapi-gen=true
type TLSConfig struct {
	// Path to the CA cert in the container to use for the targets.
	// +optional
	CAFile string `json:"caFile,omitempty" yaml:"ca_file,omitempty"`
	// Stuct containing the CA cert to use for the targets.
	// +optional
	CA SecretOrConfigMap `json:"ca,omitempty"`

	// Path to the client cert file in the container for the targets.
	// +optional
	CertFile string `json:"certFile,omitempty" yaml:"cert_file,omitempty"`
	// Struct containing the client cert file for the targets.
	// +optional
	Cert SecretOrConfigMap `json:"cert,omitempty"`

	// Path to the client key file in the container for the targets.
	// +optional
	KeyFile string `json:"keyFile,omitempty" yaml:"key_file,omitempty"`
	// Secret containing the client key file for the targets.
	// +optional
	KeySecret *v1.SecretKeySelector `json:"keySecret,omitempty" yaml:"key_secret,omitempty"`

	// Used to verify the hostname for the targets.
	// +optional
	ServerName string `json:"serverName,omitempty" yaml:"server_name,omitempty"`
	// Disable target certificate validation.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty" yaml:"insecure_skip_verify,omitempty"`
}

func (c *TLSConfig) AsArgs(args []string, prefix, pathPrefix string) []string {
	if c.CAFile != "" {
		args = append(args, fmt.Sprintf("-%s.tlsCAFile=%s", prefix, c.CAFile))
	} else if c.CA.PrefixedName() != "" {
		args = append(args, fmt.Sprintf("-%s.tlsCAFile=%s", prefix, c.BuildAssetPath(pathPrefix, c.CA.PrefixedName(), c.CA.Key())))
	}
	if c.CertFile != "" {
		args = append(args, fmt.Sprintf("-%s.tlsCertFile=%s", prefix, c.CertFile))
	} else if c.Cert.PrefixedName() != "" {
		args = append(args, fmt.Sprintf("-%s.tlsCertFile=%s", prefix, c.BuildAssetPath(pathPrefix, c.Cert.PrefixedName(), c.Cert.Key())))
	}
	if c.KeyFile != "" {
		args = append(args, fmt.Sprintf("-%s.tlsKeyFile=%s", prefix, c.KeyFile))
	} else if c.KeySecret != nil {
		args = append(args, fmt.Sprintf("-%s.tlsKeyFile=%s", prefix, c.BuildAssetPath(pathPrefix, c.KeySecret.Name, c.KeySecret.Key)))
	}
	if c.ServerName != "" {
		args = append(args, fmt.Sprintf("-%s.tlsServerName=%s", prefix, c.ServerName))
	}
	if c.InsecureSkipVerify {
		args = append(args, fmt.Sprintf("-%s.tlsInsecureSkipVerify=%v", prefix, c.InsecureSkipVerify))
	}
	return args
}

// TLSConfigValidationError is returned by TLSConfig.Validate() on semantically
// invalid tls configurations.
// +k8s:openapi-gen=false
type TLSConfigValidationError struct {
	err string
}

func (e *TLSConfigValidationError) Error() string {
	return e.err
}

// Validate semantically validates the given TLSConfig.
func (c *TLSConfig) Validate() error {
	if c.CA != (SecretOrConfigMap{}) {
		if c.CAFile != "" {
			return &TLSConfigValidationError{"tls config can not both specify CAFile and CA"}
		}
		if err := c.CA.Validate(); err != nil {
			return err
		}
	}

	if c.Cert != (SecretOrConfigMap{}) {
		if c.CertFile != "" {
			return &TLSConfigValidationError{"tls config can not both specify CertFile and Cert"}
		}
		if err := c.Cert.Validate(); err != nil {
			return err
		}
	}

	if c.KeyFile != "" && c.KeySecret != nil {
		return &TLSConfigValidationError{"tls config can not both specify KeyFile and KeySecret"}
	}

	return nil
}

// SecretOrConfigMapValidationError is returned by SecretOrConfigMap.Validate()
// on semantically invalid configurations.
// +k8s:openapi-gen=false
type SecretOrConfigMapValidationError struct {
	err string
}

func (e *SecretOrConfigMapValidationError) Error() string {
	return e.err
}

// Validate semantically validates the given TLSConfig.
func (c *SecretOrConfigMap) Validate() error {
	if c.Secret != nil && c.ConfigMap != nil {
		return &SecretOrConfigMapValidationError{"SecretOrConfigMap can not specify both Secret and ConfigMap"}
	}

	return nil
}

// PrefixedName returns name with possible prefix
// prefix added only to configmap to avoid clash with secret name
func (c *SecretOrConfigMap) PrefixedName() string {
	if c.Secret != nil {
		return c.Secret.Name
	}
	if c.ConfigMap != nil {
		return fmt.Sprintf("configmap_%s", c.ConfigMap.Name)
	}
	return ""
}

// Key returns actual key name
func (c *SecretOrConfigMap) Key() string {
	if c.Secret != nil {
		return c.Secret.Key
	}
	if c.ConfigMap != nil {
		return c.ConfigMap.Key
	}
	return ""
}

// BuildAssetPath buildds path for usage with assets
func (c *TLSConfig) BuildAssetPath(prefix, name, key string) string {
	if name == "" || key == "" {
		return ""
	}
	return fmt.Sprintf("%s_%s_%s", prefix, name, key)
}

// Certs defines TLS certs configuration
type Certs struct {
	CertSecretRef *v1.SecretKeySelector `json:"cert_secret_ref,omitempty"`
	// CertFile defines path to the pre-mounted file with certificate
	// mutually exclusive with CertSecretRef
	CertFile string `json:"cert_file,omitempty"`
	// Key defines reference for secret with certificate key content under given key
	// mutually exclusive with KeyFile
	KeySecretRef *v1.SecretKeySelector `json:"key_secret_ref,omitempty"`
	// KeyFile defines path to the pre-mounted file with certificate key
	// mutually exclusive with KeySecretRef
	KeyFile string `json:"key_file,omitempty"`
}

// TLSServerConfig defines TLS configuration for the application's server
type TLSServerConfig struct {
	// ClientCASecretRef defines reference for secret with CA content under given key
	// mutually exclusive with ClientCAFile
	ClientCASecretRef *v1.SecretKeySelector `json:"client_ca_secret_ref,omitempty"`
	// ClientCAFile defines path to the pre-mounted file with CA
	// mutually exclusive with ClientCASecretRef
	ClientCAFile string `json:"client_ca_file,omitempty"`
	// Cert defines reference for secret with CA content under given key
	// mutually exclusive with CertFile
	// ClientAuthType defines server policy for client authentication
	// If you want to enable client authentication (aka mTLS), you need to use RequireAndVerifyClientCert
	// Note, mTLS is supported only at enterprise version of VictoriaMetrics components
	// +kubebuilder:validation:Enum=NoClientCert;RequireAndVerifyClientCert
	ClientAuthType string `json:"client_auth_type,omitempty"`
	// MinVersion minimum TLS version that is acceptable.
	// +kubebuilder:validation:Enum=TLS10;TLS11;TLS12;TLS13
	MinVersion string `json:"min_version,omitempty"`
	// MaxVersion maximum TLS version that is acceptable.
	// +kubebuilder:validation:Enum=TLS10;TLS11;TLS12;TLS13
	MaxVersion string `json:"max_version,omitempty"`
	// CipherSuites defines list of supported cipher suites for TLS versions up to TLS 1.2
	// https://golang.org/pkg/crypto/tls/#pkg-constants
	CipherSuites []string `json:"cipher_suites,omitempty"`
	// CurvePreferences defines elliptic curves that will be used in an ECDHE handshake, in preference order.
	// https://golang.org/pkg/crypto/tls/#CurveID
	CurvePreferences []string `json:"curve_preferences,omitempty"`
	// PreferServerCipherSuites controls whether the server selects the
	// client's most preferred ciphersuite
	PreferServerCipherSuites bool `json:"prefer_server_cipher_suites,omitempty"`
	// Certs defines cert, CA and key for TLS auth
	Certs `json:",inline"`
}

// TLSClientConfig defines TLS configuration for the application's client
type TLSClientConfig struct {
	// CA defines reference for secret with CA content under given key
	// mutually exclusive with CAFile
	CASecretRef *v1.SecretKeySelector `json:"ca_secret_ref,omitempty"`
	// CAFile defines path to the pre-mounted file with CA
	// mutually exclusive with CASecretRef
	CAFile string `json:"ca_file,omitempty"`
	// Cert defines reference for secret with CA content under given key
	// mutually exclusive with CertFile
	InsecureSkipVerify bool `json:"insecure_skip_verify,omitempty"`
	// ServerName indicates a name of a server
	ServerName string `json:"server_name,omitempty"`
	// Certs defines cert, CA and key for TLS auth
	Certs `json:",inline"`
}

// ScrapeObjectStatus defines the observed state of ScrapeObjects
type ScrapeObjectStatus struct {
	// Status defines update status of resource
	Status UpdateStatus `json:"status,omitempty"`
	// LastSyncError contains error message for unsuccessful config generation
	LastSyncError string `json:"lastSyncError,omitempty"`
	// CurrentSyncError holds an error occured during reconcile loop
	CurrentSyncError string `json:"-"`
}

func parseLastAppliedSpec[T any](cr client.Object) (*T, error) {
	var prevSpec T
	lastAppliedClusterJSON := cr.GetAnnotations()[lastAppliedSpecAnnotationName]
	if len(lastAppliedClusterJSON) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(lastAppliedClusterJSON), &prevSpec); err != nil {
		return nil, fmt.Errorf("cannot parse last applied spec annotation=%q, remove this annotation manually from object : %w", lastAppliedSpecAnnotationName, err)
	}
	return &prevSpec, nil
}

// CommonDefaultableParams contains Application settings
// with known values populated from operator configuration
type CommonDefaultableParams struct {
	// Image - docker image settings
	// if no specified operator uses default version from operator config
	// +optional
	Image Image `json:"image,omitempty"`
	// Resources container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// if not defined default resources from operator config will be used
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resources",xDescriptors="urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	// +optional
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// UseDefaultResources controls resource settings
	// By default, operator sets built-in resource requirements
	// +optional
	UseDefaultResources *bool `json:"useDefaultResources,omitempty"`
	// Port listen address
	// +optional
	Port string `json:"port,omitempty"`
	// UseStrictSecurity enables strict security mode for component
	// it restricts disk writes access
	// uses non-root user out of the box
	// drops not needed security permissions
	// +optional
	UseStrictSecurity *bool `json:"useStrictSecurity,omitempty"`
	// DisableSelfServiceScrape controls creation of VMServiceScrape by operator
	// for the application.
	// Has priority over `VM_DISABLESELFSERVICESCRAPECREATION` operator env variable
	DisableSelfServiceScrape *bool `json:"disableSelfServiceScrape,omitempty"`
}

type CommonConfigReloaderParams struct {
	// UseVMConfigReloader replaces prometheus-like config-reloader
	// with vm one. It uses secrets watch instead of file watch
	// which greatly increases speed of config updates
	UseVMConfigReloader *bool `json:"useVMConfigReloader,omitempty"`
	// ConfigReloaderImageTag defines image:tag for config-reloader container
	ConfigReloaderImageTag string `json:"configReloaderImageTag,omitempty"`
	// ConfigReloaderResources config-reloader container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// if not defined default resources from operator config will be used
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resources",xDescriptors="urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	ConfigReloaderResources v1.ResourceRequirements `json:"configReloaderResources,omitempty"`
	// ConfigReloaderExtraArgs that will be passed to  VMAuths config-reloader container
	// for example resyncInterval: "30s"
	// +optional
	ConfigReloaderExtraArgs map[string]string `json:"configReloaderExtraArgs,omitempty"`
}

// CommonApplicationDeploymentParams defines common params
// for deployment and statefulset specifications
type CommonApplicationDeploymentParams struct {
	// Affinity If specified, the pod's scheduling constraints.
	// +optional
	Affinity *v1.Affinity `json:"affinity,omitempty"`
	// Tolerations If specified, the pod's tolerations.
	// +optional
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
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
	// HostAliasesUnderScore provides mapping for ip and hostname,
	// that would be propagated to pod,
	// cannot be used with HostNetwork.
	// Has Priority over hostAliases field
	// +optional
	HostAliasesUnderScore []v1.HostAlias `json:"host_aliases,omitempty"`
	// PriorityClassName class assigned to the Pods
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
	// NodeSelector Define which Nodes the Pods are scheduled on.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// SecurityContext holds pod-level security attributes and common container settings.
	// This defaults to the default PodSecurityContext.
	// +optional
	SecurityContext *SecurityContext `json:"securityContext,omitempty"`
	// TopologySpreadConstraints embedded kubernetes pod configuration option,
	// controls how pods are spread across your cluster among failure-domains
	// such as regions, zones, nodes, and other user-defined topology domains
	// https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
	// +optional
	TopologySpreadConstraints []v1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see https://kubernetes.io/docs/concepts/containers/images/#referring-to-an-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []v1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// TerminationGracePeriodSeconds period for container graceful termination
	// +optional
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
	// ReadinessGates defines pod readiness gates
	ReadinessGates []v1.PodReadinessGate `json:"readinessGates,omitempty"`
	// MinReadySeconds defines a minim number os seconds to wait before starting update next pod
	// if previous in healthy state
	// Has no effect for VLogs and VMSingle
	// +optional
	MinReadySeconds int32 `json:"minReadySeconds,omitempty"`
	// ReplicaCount is the expected size of the Application.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Number of pods",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount,urn:alm:descriptor:io.kubernetes:custom"
	// +optional
	ReplicaCount *int32 `json:"replicaCount,omitempty"`
	// The number of old ReplicaSets to retain to allow rollback in deployment or
	// maximum number of revisions that will be maintained in the Deployment revision history.
	// Has no effect at StatefulSets
	// Defaults to 10.
	// +optional
	RevisionHistoryLimitCount *int32 `json:"revisionHistoryLimitCount,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// Containers property allows to inject additions sidecars or to patch existing containers.
	// It can be useful for proxies, backup, etc.
	// +optional
	Containers []v1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition.
	// Any errors during the execution of an initContainer will lead to a restart of the Pod.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// +optional
	InitContainers []v1.Container `json:"initContainers,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the Application
	// object, which shall be mounted into the Application container
	// at /etc/vm/secrets/SECRET_NAME folder
	// +optional
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the Application
	// object, which shall be mounted into the Application container
	// at /etc/vm/configs/CONFIGMAP_NAME folder
	// +optional
	ConfigMaps []string `json:"configMaps,omitempty"`
	// Volumes allows configuration of additional volumes on the output Deployment/StatefulSet definition.
	// Volumes specified will be appended to other volumes that are generated.
	/// +optional
	Volumes []v1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output Deployment/StatefulSet definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the Application container
	// +optional
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`
	// ExtraArgs that will be passed to the application container
	// for example remoteWrite.tmpDataPath: /tmp
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// ExtraEnvs that will be passed to the application container
	// +optional
	ExtraEnvs []v1.EnvVar `json:"extraEnvs,omitempty"`
	// Paused If set to true all actions on the underlying managed objects are not
	// going to be performed, except for delete actions.
	// +optional
	Paused bool `json:"paused,omitempty"`
}

// SecurityContext extends PodSecurityContext with ContainerSecurityContext
// It allows to globally configure security params for pod and all containers
type SecurityContext struct {
	*v1.PodSecurityContext    `json:",inline"`
	*ContainerSecurityContext `json:",inline"`
}

// ContainerSecurityContext defines security context for each application container
type ContainerSecurityContext struct {
	// Run containers in privileged mode.
	// Processes in privileged containers are essentially equivalent to root on the host.
	// Note that this field cannot be set when spec.os.name is windows.
	// +optional
	Privileged *bool `json:"privileged,omitempty"`
	// The capabilities to add/drop when running containers.
	// Defaults to the default set of capabilities granted by the container runtime.
	// Note that this field cannot be set when spec.os.name is windows.
	// +optional
	Capabilities *v1.Capabilities `json:"capabilities,omitempty"`
	// Whether this containers has a read-only root filesystem.
	// Default is false.
	// Note that this field cannot be set when spec.os.name is windows.
	// +optional
	ReadOnlyRootFilesystem *bool `json:"readOnlyRootFilesystem,omitempty"`
	// AllowPrivilegeEscalation controls whether a process can gain more
	// privileges than its parent process. This bool directly controls if
	// the no_new_privs flag will be set on the container process.
	// AllowPrivilegeEscalation is true always when the container is:
	// 1) run as Privileged
	// 2) has CAP_SYS_ADMIN
	// Note that this field cannot be set when spec.os.name is windows.
	// +optional
	AllowPrivilegeEscalation *bool `json:"allowPrivilegeEscalation,omitempty"`
	// procMount denotes the type of proc mount to use for the containers.
	// The default is DefaultProcMount which uses the container runtime defaults for
	// readonly paths and masked paths.
	// This requires the ProcMountType feature flag to be enabled.
	// Note that this field cannot be set when spec.os.name is windows.
	// +optional
	ProcMount *v1.ProcMountType `json:"procMount,omitempty"`
}

func statusPatch(ctx context.Context, rclient client.Client, object client.Object, st interface{}) error {
	type patch struct {
		OP    string      `json:"op"`
		Path  string      `json:"path"`
		Value interface{} `json:"value"`
	}
	ops := []patch{
		{
			OP:    "replace",
			Path:  "/status",
			Value: st,
		},
	}
	data, err := json.Marshal(ops)
	if err != nil {
		return fmt.Errorf("possible bug, cannot serialize specification as json :%w", err)
	}
	pr := client.RawPatch(types.JSONPatchType, data)
	return rclient.Status().Patch(ctx, object, pr)
}
