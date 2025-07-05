package v1beta1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
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

const (
	// ConditionParsingReason defines reason for child objects
	ConditionParsingReason = "ConfigParsedAndApplied"
	// ConditionDomainTypeAppliedSuffix defines type suffix for ConditionParsingReason reason
	ConditionDomainTypeAppliedSuffix = ".victoriametrics.com/Applied"
)

// SchemeGroupVersion is group version used to register these objects
var SchemeGroupVersion = schema.GroupVersion{Group: "operator.victoriametrics.com", Version: "v1beta1"}

var (
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	labelFilterPrefixes []string
	// default ignored annotations
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	annotationFilterPrefixes = []string{"kubectl.kubernetes.io/", "operator.victoriametrics.com/", "operator.victoriametrics/"}
)

// SetLabelAndAnnotationPrefixes configures global filtering for child labels and annotations
// cannot be used concurrently and should be called only once at lib init
// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
func SetLabelAndAnnotationPrefixes(labelPrefixes, annotationPrefixes []string) {
	labelFilterPrefixes = labelPrefixes
	annotationFilterPrefixes = append(annotationFilterPrefixes, annotationPrefixes...)
}

// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
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
func MustSkipCRValidation(cr client.Object) bool {
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
// if finalizer was added, performs callback
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
	EmptyDir *corev1.EmptyDirVolumeSource `json:"emptyDir,omitempty"`
	// A PVC spec to be used by the VMAlertManager StatefulSets.
	// +optional
	VolumeClaimTemplate EmbeddedPersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
}

// IntoSTSVolume converts storageSpec into proper volume for statefulsetSpec
// by default, it adds emptyDir volume.
func (ss *StorageSpec) IntoSTSVolume(name string, sts *appsv1.StatefulSetSpec) {
	switch {
	case ss == nil:
		sts.Template.Spec.Volumes = append(sts.Template.Spec.Volumes, corev1.Volume{
			Name: name,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	case ss.EmptyDir != nil:
		sts.Template.Spec.Volumes = append(sts.Template.Spec.Volumes, corev1.Volume{
			Name: name,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: ss.EmptyDir,
			},
		})
	default:
		claimTemplate := ss.VolumeClaimTemplate
		stsClaim := corev1.PersistentVolumeClaim{
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
			stsClaim.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
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
	Spec corev1.PersistentVolumeClaimSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current information/status of a persistent volume claim.
	// Read-only.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	// +optional
	Status corev1.PersistentVolumeClaimStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
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
	TokenSecret *corev1.SecretKeySelector `json:"bearerTokenSecret,omitempty"`
}

// BasicAuth allow an endpoint to authenticate over basic authentication
// +k8s:openapi-gen=true
type BasicAuth struct {
	// Username defines reference for secret with username value
	// The secret needs to be in the same namespace as scrape object
	// +optional
	Username corev1.SecretKeySelector `json:"username,omitempty"`
	// Password defines reference for secret with password value
	// The secret needs to be in the same namespace as scrape object
	// +optional
	Password corev1.SecretKeySelector `json:"password,omitempty"`
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
	Spec corev1.ServiceSpec `json:"spec"`
}

// IsSomeAndThen applies callback to the additionalServiceSpec if it's not nil or do not used as default service
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

// BuildReloadPathWithPort builds reload api path for given args
func BuildReloadPathWithPort(extraArgs map[string]string, port string) string {
	proto := HTTPProtoFromFlags(extraArgs)
	urlPath := joinPathAuthKey(BuildPathWithPrefixFlag(extraArgs, reloadPath), reloadAuthKey, extraArgs)
	return fmt.Sprintf("%s://localhost:%s%s", proto, port, urlPath)
}

// BuildPathWithPrefixFlag returns provided path with possible prefix from flags
func BuildPathWithPrefixFlag(flags map[string]string, defaultPath string) string {
	if prefix, ok := flags[vmPathPrefixFlagName]; ok {
		return path.Join(prefix, defaultPath)
	}
	return defaultPath
}

// HTTPProtoFromFlags returns HTTP protocol prefix from provided flags
func HTTPProtoFromFlags(flags map[string]string) string {
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
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`
	// ReadinessProbe that will be added CRD pod
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`
	// StartupProbe that will be added to CRD pod
	// +optional
	StartupProbe *corev1.Probe `json:"startupProbe,omitempty"`
}

// EmbeddedHPA embeds HorizontalPodAutoScaler spec v2.
// https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/horizontal-pod-autoscaler-v2/
type EmbeddedHPA struct {
	MinReplicas *int32                                         `json:"minReplicas,omitempty"`
	MaxReplicas int32                                          `json:"maxReplicas,omitempty"`
	Metrics     []autoscalingv2.MetricSpec                     `json:"metrics,omitempty"`
	Behaviour   *autoscalingv2.HorizontalPodAutoscalerBehavior `json:"behaviour,omitempty"`
}

// Validate validates resource configuration
func (cr *EmbeddedHPA) Validate() error {
	if cr.MinReplicas != nil && *cr.MinReplicas > cr.MaxReplicas {
		return fmt.Errorf("minReplicas cannot be greater then maxReplicas")
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
	corev1.LocalObjectReference `json:",inline"`
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
	RuleConfigMap *corev1.ConfigMapKeySelector `json:"configmap,omitempty"`
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
	DropInputLabels []string `json:"dropInputLabels,omitempty"`
	// IgnoreFirstIntervals instructs to ignore first interval
	// +optional
	IgnoreFirstIntervals int `json:"ignoreFirstIntervals,omitempty"`
	// IgnoreOldSamples instructs to ignore samples with old timestamps outside the current aggregation interval.
	// +optional
	IgnoreOldSamples bool `json:"ignoreOldSamples,omitempty"`
	// EnableWindows enables aggregating data in separate windows ( available from v0.54.0).
	// +optional
	EnableWindows bool `json:"enableWindows,omitempty"`
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

	// EnableWindows enables aggregating data in separate windows
	// +optional
	EnableWindows bool `json:"enable_windows,omitempty" yaml:"enable_windows,omitempty"`
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

// MarshalYAML implements yaml.Marshaller interface
func (m StringOrArray) MarshalYAML() (any, error) {
	switch len(m) {
	case 0:
		return "", nil
	case 1:
		return m[0], nil
	default:
		return []string(m), nil
	}
}

// UnmarshalYAML implements   yaml.Unmarshaler interface
func (m *StringOrArray) UnmarshalYAML(unmarshal func(any) error) error {
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

// UnmarshalJSON implements json.Unmarshaller interface
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

// MarshalJSON implements json.Marshaller interface
func (m *StringOrArray) MarshalJSON() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	ms := *m
	switch {
	case len(ms) == 0:
		return json.Marshal("")
	case len(ms) == 1:
		return json.Marshal(ms[0])
	default:
		return json.Marshal([]string(ms))
	}
}

// License holds license key for enterprise features.
// Using license key is supported starting from VictoriaMetrics corev1.94.0.
// See [here](https://docs.victoriametrics.com/victoriametrics/enterprise/)
type License struct {
	// Enterprise license key. This flag is available only in [VictoriaMetrics enterprise](https://docs.victoriametrics.com/victoriametrics/enterprise/).
	// To request a trial license, [go to](https://victoriametrics.com/products/enterprise/trial)
	Key *string `json:"key,omitempty"`
	// KeyRef is reference to secret with license key for enterprise features.
	KeyRef *corev1.SecretKeySelector `json:"keyRef,omitempty"`
	// Enforce offline verification of the license key.
	ForceOffline *bool `json:"forceOffline,omitempty"`
	// Interval to be used for checking for license key changes. Note that this is only applicable when using KeyRef.
	ReloadInterval *string `json:"reloadInterval,omitempty"`
}

// IsProvided returns true if license is provided.
func (l *License) IsProvided() bool {
	if l == nil {
		return false
	}

	return l.Key != nil || l.KeyRef != nil
}

func (l *License) validate() error {
	if !l.IsProvided() {
		return nil
	}

	if l.Key != nil && l.KeyRef != nil {
		return fmt.Errorf("only one of key or keyRef can be specified")
	}

	if l.Key != nil && l.ReloadInterval != nil {
		return fmt.Errorf("reloadInterval can be specified only with keyRef")
	}

	return nil
}

// SecretOrConfigMap allows to specify data as a Secret or ConfigMap. Fields are mutually exclusive.
type SecretOrConfigMap struct {
	// Secret containing data to use for the targets.
	// +optional
	Secret *corev1.SecretKeySelector `json:"secret,omitempty"`
	// ConfigMap containing data to use for the targets.
	// +optional
	ConfigMap *corev1.ConfigMapKeySelector `json:"configMap,omitempty"`
}

// TLSConfig specifies TLSConfig configuration parameters.
// +k8s:openapi-gen=true
type TLSConfig struct {
	// Path to the CA cert in the container to use for the targets.
	// +optional
	CAFile string `json:"caFile,omitempty" yaml:"-"`
	// Struct containing the CA cert to use for the targets.
	// +optional
	CA SecretOrConfigMap `json:"ca,omitempty" yaml:"-"`

	// Path to the client cert file in the container for the targets.
	// +optional
	CertFile string `json:"certFile,omitempty" yaml:"cert_file,omitempty"`
	// Struct containing the client cert file for the targets.
	// +optional
	Cert SecretOrConfigMap `json:"cert,omitempty" yaml:"-"`

	// Path to the client key file in the container for the targets.
	// +optional
	KeyFile string `json:"keyFile,omitempty" yaml:"key_file,omitempty"`
	// Secret containing the client key file for the targets.
	// +optional
	KeySecret *corev1.SecretKeySelector `json:"keySecret,omitempty" yaml:"-"`

	// Used to verify the hostname for the targets.
	// +optional
	ServerName string `json:"serverName,omitempty" yaml:"server_name,omitempty"`
	// Disable target certificate validation.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty" yaml:"-"`
}

// UnmarshalJSON implements json.Unmarshaller interface
func (c *TLSConfig) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	type pc TLSConfig
	if err := decoder.Decode((*pc)(c)); err != nil {
		return err
	}

	return nil
}

// TLSConfigValidationError is returned by TLSConfig.validate() on semantically
// invalid tls configurations.
// +k8s:openapi-gen=false
type TLSConfigValidationError struct {
	err string
}

func (e *TLSConfigValidationError) Error() string {
	return e.err
}

// validate semantically validates the given TLSConfig.
func (c *TLSConfig) Validate() error {
	if c.CA != (SecretOrConfigMap{}) {
		if c.CAFile != "" {
			return &TLSConfigValidationError{"tls config can not both specify CAFile and CA"}
		}
		if err := c.CA.validate(); err != nil {
			return err
		}
	}

	if c.Cert != (SecretOrConfigMap{}) {
		if c.CertFile != "" {
			return &TLSConfigValidationError{"tls config can not both specify CertFile and Cert"}
		}
		if err := c.Cert.validate(); err != nil {
			return err
		}
	}

	if c.KeyFile != "" && c.KeySecret != nil {
		return &TLSConfigValidationError{"tls config can not both specify KeyFile and KeySecret"}
	}

	return nil
}

// SecretOrConfigMapValidationError is returned by SecretOrConfigMap.validate()
// on semantically invalid configurations.
// +k8s:openapi-gen=false
type SecretOrConfigMapValidationError struct {
	err string
}

func (e *SecretOrConfigMapValidationError) Error() string {
	return e.err
}

// validate semantically validates the given TLSConfig.
func (c *SecretOrConfigMap) validate() error {
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

// BuildAssetPath builds path for usage with assets
func (c *TLSConfig) BuildAssetPath(prefix, name, key string) string {
	if name == "" || key == "" {
		return ""
	}
	return fmt.Sprintf("%s_%s_%s", prefix, name, key)
}

// Certs defines TLS certs configuration
type Certs struct {
	// CertSecretRef defines reference for secret with certificate content under given key
	// mutually exclusive with CertFile
	// +optional
	CertSecretRef *corev1.SecretKeySelector `json:"cert_secret_ref,omitempty"`
	// CertFile defines path to the pre-mounted file with certificate
	// mutually exclusive with CertSecretRef
	// +optional
	CertFile string `json:"cert_file,omitempty"`
	// Key defines reference for secret with certificate key content under given key
	// mutually exclusive with KeyFile
	// +optional
	KeySecretRef *corev1.SecretKeySelector `json:"key_secret_ref,omitempty"`
	// KeyFile defines path to the pre-mounted file with certificate key
	// mutually exclusive with KeySecretRef
	// +optional
	KeyFile string `json:"key_file,omitempty"`
}

// TLSServerConfig defines TLS configuration for the application's server
type TLSServerConfig struct {
	// ClientCASecretRef defines reference for secret with CA content under given key
	// mutually exclusive with ClientCAFile
	// +optional
	ClientCASecretRef *corev1.SecretKeySelector `json:"client_ca_secret_ref,omitempty"`
	// ClientCAFile defines path to the pre-mounted file with CA
	// mutually exclusive with ClientCASecretRef
	// +optional
	ClientCAFile string `json:"client_ca_file,omitempty"`
	// Cert defines reference for secret with CA content under given key
	// mutually exclusive with CertFile
	// ClientAuthType defines server policy for client authentication
	// If you want to enable client authentication (aka mTLS), you need to use RequireAndVerifyClientCert
	// Note, mTLS is supported only at enterprise version of VictoriaMetrics components
	// +optional
	// +kubebuilder:validation:Enum=NoClientCert;RequireAndVerifyClientCert
	ClientAuthType string `json:"client_auth_type,omitempty"`
	// MinVersion minimum TLS version that is acceptable.
	// +optional
	// +kubebuilder:validation:Enum=TLS10;TLS11;TLS12;TLS13
	MinVersion string `json:"min_version,omitempty"`
	// MaxVersion maximum TLS version that is acceptable.
	// +optional
	// +kubebuilder:validation:Enum=TLS10;TLS11;TLS12;TLS13
	MaxVersion string `json:"max_version,omitempty"`
	// CipherSuites defines list of supported cipher suites for TLS versions up to TLS 1.2
	// https://golang.org/pkg/crypto/tls/#pkg-constants
	// +optional
	CipherSuites []string `json:"cipher_suites,omitempty"`
	// CurvePreferences defines elliptic curves that will be used in an ECDHE handshake, in preference order.
	// https://golang.org/pkg/crypto/tls/#CurveID
	// +optional
	CurvePreferences []string `json:"curve_preferences,omitempty"`
	// PreferServerCipherSuites controls whether the server selects the
	// client's most preferred ciphersuite
	// +optional
	PreferServerCipherSuites bool `json:"prefer_server_cipher_suites,omitempty"`
	// Certs defines cert, CA and key for TLS auth
	Certs `json:",inline"`
}

// TLSClientConfig defines TLS configuration for the application's client
type TLSClientConfig struct {
	// CA defines reference for secret with CA content under given key
	// mutually exclusive with CAFile
	// +optional
	CASecretRef *corev1.SecretKeySelector `json:"ca_secret_ref,omitempty"`
	// CAFile defines path to the pre-mounted file with CA
	// mutually exclusive with CASecretRef
	// +optional
	CAFile string `json:"ca_file,omitempty"`
	// Cert defines reference for secret with CA content under given key
	// mutually exclusive with CertFile
	// +optional
	InsecureSkipVerify bool `json:"insecure_skip_verify,omitempty"`
	// ServerName indicates a name of a server
	// +optional
	ServerName string `json:"server_name,omitempty"`
	// Certs defines cert, CA and key for TLS auth
	Certs `json:",inline"`
}

// ScrapeObjectStatus defines the observed state of ScrapeObjects
type ScrapeObjectStatus struct {
	StatusMetadata `json:",inline"`
}

type objectWithLastAppliedState[T, ST any] interface {
	GetAnnotations() map[string]string
	SetLastSpec(ST)
}

// ParseLastAppliedStateTo parses spec from provided CR annotations and sets it to the given CR
func ParseLastAppliedStateTo[T objectWithLastAppliedState[T, ST], ST any](cr T) error {
	lastAppliedSpecJSON := cr.GetAnnotations()[lastAppliedSpecAnnotationName]
	if len(lastAppliedSpecJSON) == 0 {
		return nil
	}
	var dst ST
	if err := json.Unmarshal([]byte(lastAppliedSpecJSON), &dst); err != nil {
		return fmt.Errorf("cannot parse last applied spec annotation=%q, remove this annotation manually from object : %w", lastAppliedSpecAnnotationName, err)
	}
	cr.SetLastSpec(dst)
	return nil
}

// HasSpecChanges compares single spec with last applied single spec stored in annotation
func HasStateChanges(crMeta metav1.ObjectMeta, spec any) (bool, error) {
	lastAppliedSpecJSON := crMeta.GetAnnotations()[lastAppliedSpecAnnotationName]
	if len(lastAppliedSpecJSON) == 0 {
		return true, nil
	}

	instanceSpecData, err := json.Marshal(spec)
	if err != nil {
		return false, err
	}
	if !bytes.Equal([]byte(lastAppliedSpecJSON), instanceSpecData) {
		return true, nil
	}

	return false, nil
}

// LastAppliedChangesAsPatch builds patch request from provided spec
func LastAppliedChangesAsPatch(crMeta metav1.ObjectMeta, spec any) (client.Patch, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("possible bug, cannot serialize single specification as json :%w", err)
	}
	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q: %q }}}`, lastAppliedSpecAnnotationName, data)
	return client.RawPatch(types.MergePatchType, []byte(patch)), nil

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
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
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
	// +optional
	DisableSelfServiceScrape *bool `json:"disableSelfServiceScrape,omitempty"`
}

type CommonConfigReloaderParams struct {
	// UseVMConfigReloader replaces prometheus-like config-reloader
	// with vm one. It uses secrets watch instead of file watch
	// which greatly increases speed of config updates
	// +optional
	UseVMConfigReloader *bool `json:"useVMConfigReloader,omitempty"`
	// ConfigReloaderImageTag defines image:tag for config-reloader container
	// +optional
	ConfigReloaderImageTag string `json:"configReloaderImageTag,omitempty"`
	// ConfigReloaderResources config-reloader container resource request and limits, https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// if not defined default resources from operator config will be used
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resources",xDescriptors="urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	ConfigReloaderResources corev1.ResourceRequirements `json:"configReloaderResources,omitempty"`
	// ConfigReloaderExtraArgs that will be passed to  VMAuths config-reloader container
	// for example resyncInterval: "30s"
	// +optional
	ConfigReloaderExtraArgs map[string]string `json:"configReloaderExtraArgs,omitempty"`
	// ConfigReloadAuthKeySecret defines optional secret reference authKey for /-/reload API requests.
	// Given secret reference will be added to the application and vm-config-reloader as volume
	// available since v0.57.0 version
	// +optional
	ConfigReloadAuthKeySecret *corev1.SecretKeySelector `json:"configReloadAuthKeySecret,omitempty"`
}

// CommonApplicationDeploymentParams defines common params
// for deployment and statefulset specifications
type CommonApplicationDeploymentParams struct {
	// Affinity If specified, the pod's scheduling constraints.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// Tolerations If specified, the pod's tolerations.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
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
	HostAliases []corev1.HostAlias `json:"hostAliases,omitempty"`
	// HostAliasesUnderScore provides mapping for ip and hostname,
	// that would be propagated to pod,
	// cannot be used with HostNetwork.
	// Has Priority over hostAliases field
	// +optional
	HostAliasesUnderScore []corev1.HostAlias `json:"host_aliases,omitempty"`
	// PriorityClassName class assigned to the Pods
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// HostNetwork controls whether the pod may use the node network namespace
	// +optional
	HostNetwork bool `json:"hostNetwork,omitempty"`
	// DNSPolicy sets DNS policy for the pod
	// +optional
	DNSPolicy corev1.DNSPolicy `json:"dnsPolicy,omitempty"`
	// Specifies the DNS parameters of a pod.
	// Parameters specified here will be merged to the generated DNS
	// configuration based on DNSPolicy.
	// +optional
	DNSConfig *corev1.PodDNSConfig `json:"dnsConfig,omitempty"`
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
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see https://kubernetes.io/docs/concepts/containers/images/#referring-to-an-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// TerminationGracePeriodSeconds period for container graceful termination
	// +optional
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
	// ReadinessGates defines pod readiness gates
	ReadinessGates []corev1.PodReadinessGate `json:"readinessGates,omitempty"`
	// MinReadySeconds defines a minimum number of seconds to wait before starting update next pod
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

	// Containers property allows to inject additions sidecars or to patch existing containers.
	// It can be useful for proxies, backup, etc.
	// +optional
	Containers []corev1.Container `json:"containers,omitempty"`
	// InitContainers allows adding initContainers to the pod definition.
	// Any errors during the execution of an initContainer will lead to a restart of the Pod.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/init-containers/
	// +optional
	InitContainers []corev1.Container `json:"initContainers,omitempty"`
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
	Volumes []corev1.Volume `json:"volumes,omitempty"`
	// VolumeMounts allows configuration of additional VolumeMounts on the output Deployment/StatefulSet definition.
	// VolumeMounts specified will be appended to other VolumeMounts in the Application container
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
	// ExtraArgs that will be passed to the application container
	// for example remoteWrite.tmpDataPath: /tmp
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// ExtraEnvs that will be passed to the application container
	// +optional
	ExtraEnvs []corev1.EnvVar `json:"extraEnvs,omitempty"`
	// Paused If set to true all actions on the underlying managed objects are not
	// going to be performed, except for delete actions.
	// +optional
	Paused bool `json:"paused,omitempty"`
	// DisableAutomountServiceAccountToken whether to disable serviceAccount auto mount by Kubernetes (available from v0.54.0).
	// Operator will conditionally create volumes and volumeMounts for containers if it requires k8s API access.
	// For example, vmagent and vm-config-reloader requires k8s API access.
	// Operator creates volumes with name: "kube-api-access", which can be used as volumeMount for extraContainers if needed.
	// And also adds VolumeMounts at /var/run/secrets/kubernetes.io/serviceaccount.
	// +optional
	DisableAutomountServiceAccountToken bool `json:"disableAutomountServiceAccountToken,omitempty"`

	// ExtraEnvsFrom defines source of env variables for the application container
	// could either be secret or configmap
	// +optional
	ExtraEnvsFrom []corev1.EnvFromSource `json:"extraEnvsFrom,omitempty"`
	// ForceSubdomain explicitly sets subdomain into a podspec equal to a primary service name
	// +optional
	ForceSubdomain bool `json:"forceSubdomain,omitempty"`
}

// SecurityContext extends PodSecurityContext with ContainerSecurityContext
// It allows to globally configure security params for pod and all containers
type SecurityContext struct {
	*corev1.PodSecurityContext `json:",inline"`
	*ContainerSecurityContext  `json:",inline"`
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
	Capabilities *corev1.Capabilities `json:"capabilities,omitempty"`
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
	ProcMount *corev1.ProcMountType `json:"procMount,omitempty"`
}

// ExternalConfig defines external source of configuration
type ExternalConfig struct {
	// SecretRef defines selector for externally managed secret which contains configuration
	// +optional
	SecretRef *corev1.SecretKeySelector `json:"secretRef,omitempty" yaml:"secretRef,omitempty"`
	// LocalPath contains static path to a config, which is managed externally for cases
	// when using secrets is not applicable, e.g.: Vault sidecar.
	// +optional
	LocalPath string `json:"localPath,omitempty" yaml:"localPath,omitempty"`
}

// StatusMetadata holds metadata of application update status
// +k8s:openapi-gen=true
type StatusMetadata struct {
	// UpdateStatus defines a status for update rollout
	//
	UpdateStatus UpdateStatus `json:"updateStatus,omitempty"`
	// Reason defines human readable error reason
	//
	Reason string `json:"reason,omitempty"`
	// ObservedGeneration defines current generation picked by operator for the
	// reconcile
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// CurrentSyncError holds an error occurred during reconcile loop
	CurrentSyncError string `json:"-"`
	// Known .status.conditions.type are: "Available", "Progressing", and "Degraded"
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []Condition `json:"conditions,omitempty"`
}

// ManagedObjectsMetadata contains Labels and Annotations
type ManagedObjectsMetadata struct {
	// Labels Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Image defines docker image settings
type Image struct {
	// Repository contains name of docker image + it's repository if needed
	Repository string `json:"repository,omitempty"`
	// Tag contains desired docker image version
	Tag string `json:"tag,omitempty"`
	// PullPolicy describes how to pull docker image
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`
}

// Condition defines status condition of the resource
type Condition struct {
	// Type of condition in CamelCase or in name.namespace.resource.victoriametrics.com/CamelCase.
	// +required
	// +kubebuilder:validation:MaxLength=316
	Type string `json:"type"`
	// status of the condition, one of True, False, Unknown.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=True;False;Unknown
	Status metav1.ConditionStatus `json:"status"`
	// observedGeneration represents the .metadata.generation that the condition was set based upon.
	// For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9, the condition is out of date
	// with respect to the current state of the instance.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,3,opt,name=observedGeneration"`
	// lastTransitionTime is the last time the condition transitioned from one status to another.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
	// LastUpdateTime is the last time of given type update.
	// This value is used for status TTL update and removal
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`

	// reason contains a programmatic identifier indicating the reason for the condition's last transition.
	// Producers of specific condition types may define expected values and meanings for this field,
	// and whether the values are considered a guaranteed API.
	// The value should be a CamelCase string.
	// This field may not be empty.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=1024
	// +kubebuilder:validation:MinLength=1
	Reason string `json:"reason"`
	// message is a human readable message indicating details about the transition.
	// This may be an empty string.
	// +optional
	// +kubebuilder:validation:MaxLength=32768
	Message string `json:"message,omitempty"`
}

// BytesString represents bytes value defined directly as integer
// or as a string with suffix - kb,mb,gb,tb,KiB,MiB,GiB,TiB
type BytesString string

var bytesStringRe = regexp.MustCompile(`^[0-9]+(kb|mb|gb|tb|KB|MB|GB|TB|KiB|MiB|GiB|TiB)?$`)

// UnmarshalJSON implements json.Unmarshaller interface
func (bs *BytesString) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if data[0] == '"' {
		var strV string
		if err := json.Unmarshal(data, &strV); err != nil {
			return fmt.Errorf("cannot parse string value: %w", err)
		}
		if !bytesStringRe.MatchString(strV) {
			return fmt.Errorf("bytes value=%q must match pattern=%q", strV, bytesStringRe)
		}
		*bs = BytesString(strV)
		return nil
	}

	var iv int64
	if err := json.Unmarshal(data, &iv); err != nil {
		return fmt.Errorf("cannot parse BytesString as integer or string: %w", err)
	}
	sv := strconv.FormatInt(iv, 10)
	*bs = BytesString(sv)
	return nil
}

// MarshalJSON implements json.Marshaller interface
func (bs *BytesString) MarshalJSON() ([]byte, error) {
	if bs == nil || len(*bs) == 0 {
		return nil, nil
	}
	bsStr := string(*bs)
	iv, err := strconv.ParseInt(bsStr, 10, 64)
	if err == nil {
		return json.Marshal(iv)
	}
	return json.Marshal(bsStr)
}

// String implements Stringer interface
func (bs *BytesString) String() string {
	if bs == nil {
		return ""
	}
	return string(*bs)
}
