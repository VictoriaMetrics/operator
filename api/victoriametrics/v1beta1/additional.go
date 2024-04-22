package v1beta1

import (
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
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type UpdateStatus string

const (
	UpdateStatusExpanding   UpdateStatus = "expanding"
	UpdateStatusOperational UpdateStatus = "operational"
	UpdateStatusFailed      UpdateStatus = "failed"
	vmPathPrefixFlagName                 = "http.pathPrefix"
	healthPath                           = "/health"
	metricPath                           = "/metrics"
	reloadPath                           = "/-/reload"
	reloadAuthKey                        = "reloadAuthKey"
	snapshotCreate                       = "/snapshot/create"
	snapshotDelete                       = "/snapshot/delete"
	// FinalizerName name of our finalizer.
	FinalizerName            = "apps.victoriametrics.com/finalizer"
	SkipValidationAnnotation = "operator.victoriametrics.com/skip-validation"
	SkipValidationValue      = "true"
	AdditionalServiceLabel   = "operator.victoriametrics.com/additional-service"
	// PVCExpandableLabel controls checks for storageClass
	PVCExpandableLabel  = "operator.victoriametrics.com/pvc-allow-volume-expansion"
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

func MergeFinalizers(src client.Object, finalizer string) []string {
	if !IsContainsFinalizer(src.GetFinalizers(), finalizer) {
		srcF := src.GetFinalizers()
		srcF = append(srcF, finalizer)
		src.SetFinalizers(srcF)
	}
	return src.GetFinalizers()
}

// IsContainsFinalizer check if finalizers is set.
func IsContainsFinalizer(src []string, finalizer string) bool {
	for _, s := range src {
		if s == finalizer {
			return true
		}
	}
	return false
}

// RemoveFinalizer - removes given finalizer from finalizers list.
func RemoveFinalizer(src []string, finalizer string) []string {
	dst := src[:0]
	for _, s := range src {
		if s == finalizer {
			continue
		}
		dst = append(dst, s)
	}
	return dst
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
	BasicAuth   *BasicAuth `json:"basicAuth,omitempty"`
	OAuth2      *OAuth2    `json:"oauth2,omitempty"`
	TLSConfig   *TLSConfig `json:"tlsConfig,omitempty"`
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
	// The secret in the service scrape namespace that contains the username
	// for authentication.
	// It must be at them same namespace as CRD
	// +optional
	Username v1.SecretKeySelector `json:"username,omitempty"`
	// The secret in the service scrape namespace that contains the password
	// for authentication.
	// It must be at them same namespace as CRD
	// +optional
	Password v1.SecretKeySelector `json:"password,omitempty"`
	// PasswordFile defines path to password file at disk
	// +optional
	PasswordFile string `json:"password_file,omitempty"`
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
	UseAsDefault bool `json:"useAsDefault,omitempty"`
	// EmbeddedObjectMetadata defines objectMeta for additional service.
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
	Rules []StreamAggrRule `json:"rules"`
	// Allows writing both raw and aggregate data
	// +optional
	KeepInput bool `json:"keepInput,omitempty"`
	// Allow drop all the input samples after the aggregation
	DropInput bool `json:"dropInput,omitempty"`
	// Allows setting different de-duplication intervals per each configured remote storage
	// +optional
	DedupInterval string `json:"dedupInterval,omitempty"`
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

	// StalenessInterval defines an interval after which the series state will be reset if no samples have been sent during it.
	StalenessInterval string `json:"staleness_interval,omitempty" yaml:"staleness_interval,omitempty"`

	// FlushOnShutdown defines whether to flush the aggregation state on process termination
	// or config reload. Is `false` by default.
	// It is not recommended changing this setting, unless unfinished aggregations states
	// are preferred to missing data points.
	FlushOnShutdown bool `json:"flush_on_shutdown,omitempty" yaml:"flush_on_shutdown,omitempty"`
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

	// InputRelabelConfigs is an optional relabeling rules, which are applied on the input
	// before aggregation.
	// +optional
	InputRelabelConfigs []RelabelConfig `json:"input_relabel_configs,omitempty" yaml:"input_relabel_configs,omitempty"`

	// OutputRelabelConfigs is an optional relabeling rules, which are applied
	// on the aggregated output before being sent to remote storage.
	// +optional
	OutputRelabelConfigs []RelabelConfig `json:"output_relabel_configs,omitempty" yaml:"output_relabel_configs,omitempty"`
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

func (m StringOrArray) MarshalJSON() ([]byte, error) {
	switch len(m) {
	case 0:
		return json.Marshal("")
	case 1:
		return json.Marshal(m[0])
	default:
		return json.Marshal([]string(m))
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
// Using license key is supported starting from VictoriaMetrics v1.94.0
// See: https://docs.victoriametrics.com/enterprise.html
type License struct {
	// Enterprise license key. This flag is available only in VictoriaMetrics enterprise.
	// Documentation - https://docs.victoriametrics.com/enterprise.html
	// for more information, visit https://victoriametrics.com/products/enterprise/ .
	// To request a trial license, go to https://victoriametrics.com/products/enterprise/trial/
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
