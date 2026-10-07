/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/VictoriaMetrics/metricsql"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

const (
	// VMEstimatorComponentSingle defines the single-node vmestimator component kind
	VMEstimatorComponentSingle vmv1beta1.ClusterComponent = "single"

	vmEstimatorName              = "vmestimator"
	vmEstimatorDefaultPort       = "8490"
	vmEstimatorRemoteWritePath   = "/cardinality/api/v1/write"
	vmEstimatorInsertSuffix      = "-insert"
	vmEstimatorDefaultInterval   = 5 * time.Minute
	vmEstimatorMaxGroupByLabels  = 5
	vmEstimatorStaticLabelPrefix = "by_"
	// pods of StatefulSet with longer name cannot be created,
	// since the name with hash suffix is used as controller-revision-hash label value, which is limited to 63 chars
	vmEstimatorMaxStatefulSetNameLen = 52
)

// vmEstimatorReservedLabels contains label names reserved by vmestimator for the output metrics
var vmEstimatorReservedLabels = []string{"interval", "churn_interval", "filter", "group_by_keys", "group_by_values"}

// VMEstimatorSpec defines the desired state of VMEstimator
// +k8s:openapi-gen=true
type VMEstimatorSpec struct {
	// ComponentVersion defines default images tag for all components.
	// it can be overwritten with component specific image.tag value.
	// +optional
	ComponentVersion string `json:"componentVersion,omitempty"`
	// ClusterDomainName defines domain name suffix for in-cluster dns addresses
	// aka .cluster.local
	// used by select to build storage nodes addresses
	// +optional
	ClusterDomainName string `json:"clusterDomainName,omitempty"`
	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see https://kubernetes.io/docs/concepts/containers/images/#specifying-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// ServiceAccountName is the name of the ServiceAccount to use to run all VMEstimator pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	// +optional
	ManagedMetadata *vmv1beta1.ManagedObjectsMetadata `json:"managedMetadata,omitempty"`
	// UseStrictSecurity enables strict security mode for component
	// it restricts disk writes access
	// uses non-root user out of the box
	// drops not needed security permissions
	// +optional
	UseStrictSecurity *bool `json:"useStrictSecurity,omitempty"`
	// Paused If set to true all actions on the underlying managed objects are not
	// going to be performed, except for delete actions.
	// +optional
	Paused bool `json:"paused,omitempty"`

	// Streams defines cardinality estimation streams.
	// If neither streams nor streamsConfigMap is set, operator uses default streams,
	// which estimate global cardinality and cardinality per job and per metric name over 5m interval.
	// See https://docs.victoriametrics.com/victoriametrics/vmestimator/#configuration
	// +optional
	Streams []VMEstimatorStream `json:"streams,omitempty"`
	// StreamsConfigMap defines a ConfigMap key with vmestimator configuration in YAML format,
	// which must contain a top-level streams list.
	// Streams defined at this ConfigMap are appended to the streams defined at spec.streams.
	// See https://github.com/VictoriaMetrics/vmestimator/blob/main/streams.yaml
	// +optional
	StreamsConfigMap *corev1.ConfigMapKeySelector `json:"streamsConfigMap,omitempty"`

	// Single defines single-node vmestimator, which accepts remote write requests
	// and exposes cardinality estimations.
	// It's mutually exclusive with storage and select.
	// Operator deploys single-node vmestimator if none of single, storage and select is set.
	// It's expected to run with a single replica: the Service spreads remote write requests
	// among replicas, so each replica estimates only its share of the data. Use cluster mode to scale.
	// +optional
	Single *VMEstimatorSingle `json:"single,omitempty"`
	// Storage defines vmestimator storage nodes for the cluster mode.
	// Storage nodes accept remote write requests and maintain local cardinality estimations.
	// See https://docs.victoriametrics.com/victoriametrics/vmestimator/#cluster
	// +optional
	Storage *VMEstimatorStorage `json:"storage,omitempty"`
	// Select defines vmestimator select nodes for the cluster mode.
	// Select nodes query all storage nodes, merge their estimations and expose them as metrics.
	// It requires storage to be set.
	// See https://docs.victoriametrics.com/victoriametrics/vmestimator/#cluster
	// +optional
	Select *VMEstimatorSelect `json:"select,omitempty"`
}

// VMEstimatorStream defines cardinality estimation stream
// See https://docs.victoriametrics.com/victoriametrics/vmestimator/#configuration
type VMEstimatorStream struct {
	// Interval defines the measurement window: how long unique series are retained before the HLL sketch resets.
	// Increases are always reflected immediately, interval only controls how fast the estimate
	// drops after previously seen series disappear.
	// Defaults to 5m.
	// +optional
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
	// ChurnInterval enables cardinality_churn_ratio metric, which measures how quickly the series set changes.
	// It defines the look-back comparison window and must not exceed interval.
	// +optional
	ChurnInterval string `json:"churnInterval,omitempty" yaml:"churn_interval,omitempty"`
	// Filter defines MetricsQL series selector used to pre-filter time series before counting,
	// e.g. '{job="api",env!~"dev|staging"}'.
	// +optional
	Filter string `json:"filter,omitempty" yaml:"filter,omitempty"`
	// GroupBy defines label names used to split the cardinality estimate into per-combination groups.
	// The special pseudo-label "__label__" estimates the number of unique values per label name.
	// Omit it for a single global estimate across all series.
	// +optional
	// +kubebuilder:validation:MaxItems=5
	GroupBy []string `json:"groupBy,omitempty" yaml:"group_by,omitempty"`
	// GroupLimit defines maximum number of distinct groups to track.
	// Excess groups are counted in a single shared "rejected" sketch.
	// Defaults to 10000.
	// +optional
	// +kubebuilder:validation:Minimum=1
	GroupLimit int32 `json:"groupLimit,omitempty" yaml:"group_limit,omitempty"`
	// Buckets defines number of shards used to reduce lock contention during parallel ingestion.
	// Defaults to min(64, 2*availableCPUs).
	// +optional
	// +kubebuilder:validation:Minimum=1
	Buckets int32 `json:"buckets,omitempty" yaml:"buckets,omitempty"`
	// HLLPrecision defines HyperLogLog precision, which determines estimation error and memory usage.
	// Defaults to 14.
	// +optional
	// +kubebuilder:validation:Minimum=4
	// +kubebuilder:validation:Maximum=18
	HLLPrecision int32 `json:"hllPrecision,omitempty" yaml:"hll_precision,omitempty"`
	// HLLSparse defines whether to use the sparse HyperLogLog representation for low-cardinality groups.
	// Defaults to true.
	// +optional
	HLLSparse *bool `json:"hllSparse,omitempty" yaml:"hll_sparse,omitempty"`
	// Labels defines static labels attached to every output metric produced by this stream.
	// +optional
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// Validate checks if stream is correct
func (s *VMEstimatorStream) Validate() error {
	interval := vmEstimatorDefaultInterval
	if s.Interval != "" {
		d, err := time.ParseDuration(s.Interval)
		if err != nil {
			return fmt.Errorf("cannot parse interval=%q: %w", s.Interval, err)
		}
		if d <= 0 {
			return fmt.Errorf("interval=%q must be positive", s.Interval)
		}
		interval = d
	}
	if s.ChurnInterval != "" {
		d, err := time.ParseDuration(s.ChurnInterval)
		if err != nil {
			return fmt.Errorf("cannot parse churnInterval=%q: %w", s.ChurnInterval, err)
		}
		if d < 0 {
			return fmt.Errorf("churnInterval=%q cannot be negative", s.ChurnInterval)
		}
		if d > interval {
			return fmt.Errorf("churnInterval=%s must not exceed interval=%s", d, interval)
		}
	}
	if s.Filter != "" {
		expr, err := metricsql.Parse(s.Filter)
		if err != nil {
			return fmt.Errorf("cannot parse filter=%q: %w", s.Filter, err)
		}
		me, ok := expr.(*metricsql.MetricExpr)
		if !ok {
			return fmt.Errorf("filter=%q must be a series selector", s.Filter)
		}
		if len(me.LabelFilterss) > 1 {
			return fmt.Errorf("filter=%q must not contain `or` filters", s.Filter)
		}
	}
	if len(s.GroupBy) > vmEstimatorMaxGroupByLabels {
		return fmt.Errorf("groupBy must not contain more than %d labels, got %d", vmEstimatorMaxGroupByLabels, len(s.GroupBy))
	}
	seen := make(map[string]struct{}, len(s.GroupBy))
	for _, l := range s.GroupBy {
		switch l {
		case "":
			return fmt.Errorf("groupBy cannot contain empty label name")
		case "__global__", "__group__":
			return fmt.Errorf("groupBy cannot contain reserved label name %q", l)
		}
		if _, ok := seen[l]; ok {
			return fmt.Errorf("groupBy cannot contain label name %q more than once", l)
		}
		seen[l] = struct{}{}
	}
	if s.HLLPrecision != 0 && (s.HLLPrecision < 4 || s.HLLPrecision > 18) {
		return fmt.Errorf("hllPrecision=%d must be in range [4, 18]", s.HLLPrecision)
	}
	for name := range s.Labels {
		for _, reserved := range vmEstimatorReservedLabels {
			if name == reserved {
				return fmt.Errorf("label name %q is reserved and cannot be used in labels", name)
			}
		}
		if strings.HasPrefix(name, vmEstimatorStaticLabelPrefix) {
			return fmt.Errorf("label name %q is reserved: label names with %q prefix cannot be used in labels", name, vmEstimatorStaticLabelPrefix)
		}
	}
	return nil
}

// VMEstimatorSingle defines single-node vmestimator configuration
type VMEstimatorSingle struct {
	// PodMetadata configures Labels and Annotations which are propagated to the single-node vmestimator pods.
	// +optional
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for vmestimator to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for vmestimator to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`

	// ServiceSpec that will be added to single-node vmestimator service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to single-node vmestimator VMServiceScrape spec
	// +optional
	// +kubebuilder:validation:Type=object
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// Configures vertical pod autoscaling.
	// +optional
	VPA *vmv1beta1.EmbeddedVPA `json:"vpa,omitempty"`
	// NetworkPolicy defines network access rules for pods created by this CR.
	// +optional
	NetworkPolicy *vmv1beta1.EmbeddedNetworkPolicy `json:"networkPolicy,omitempty"`

	// UpdateStrategy - overrides default update strategy.
	// +kubebuilder:validation:Enum=Recreate;RollingUpdate
	// +optional
	UpdateStrategy *appsv1.DeploymentStrategyType `json:"updateStrategy,omitempty"`
	// RollingUpdate - overrides deployment update params.
	// +optional
	RollingUpdate *appsv1.RollingUpdateDeployment `json:"rollingUpdate,omitempty"`

	vmv1beta1.CommonAppsParams `json:",inline"`
}

// UseProxyProtocol implements build.probeCRD interface
func (cr *VMEstimatorSingle) UseProxyProtocol() bool {
	return vmv1beta1.UseProxyProtocol(cr.ExtraArgs)
}

// ProbePath implements build.probeCRD interface
func (cr *VMEstimatorSingle) ProbePath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

// ProbeScheme implements build.probeCRD interface
func (cr *VMEstimatorSingle) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.ExtraArgs))
}

// ProbePort implements build.probeCRD interface
func (cr *VMEstimatorSingle) ProbePort() string {
	return cr.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VMEstimatorSingle) ProbeNeedLiveness() bool {
	return true
}

// GetMetricsPath returns prefixed path for metric requests
func (cr *VMEstimatorSingle) GetMetricsPath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, metricsPath)
}

// GetExtraArgs returns additionally configured command-line arguments
func (cr *VMEstimatorSingle) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// UseTLS returns true if TLS is enabled
func (cr *VMEstimatorSingle) UseTLS() bool {
	return vmv1beta1.UseTLS(cr.ExtraArgs)
}

// GetServiceScrape returns overrides for serviceScrape builder
func (cr *VMEstimatorSingle) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// VMEstimatorStorage defines vmestimator storage nodes configuration
type VMEstimatorStorage struct {
	// PodMetadata configures Labels and Annotations which are propagated to the vmestimator storage pods.
	// +optional
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for vmestimator to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for vmestimator to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`

	// ServiceSpec that will be added to vmestimator storage service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmestimator storage VMServiceScrape spec
	// +optional
	// +kubebuilder:validation:Type=object
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// Configures vertical pod autoscaling.
	// +optional
	VPA *vmv1beta1.EmbeddedVPA `json:"vpa,omitempty"`
	// NetworkPolicy defines network access rules for pods created by this CR.
	// +optional
	NetworkPolicy *vmv1beta1.EmbeddedNetworkPolicy `json:"networkPolicy,omitempty"`

	// RollingUpdateStrategy defines strategy for application updates
	// Default is OnDelete, in this case operator handles update process
	// Can be changed for RollingUpdate
	// +optional
	RollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"rollingUpdateStrategy,omitempty"`
	// RollingUpdateStrategyBehavior defines customized behavior for rolling updates.
	// It applies if the RollingUpdateStrategy is set to OnDelete, which is the default.
	// +optional
	RollingUpdateStrategyBehavior *vmv1beta1.StatefulSetUpdateStrategyBehavior `json:"rollingUpdateStrategyBehavior,omitempty"`

	vmv1beta1.CommonAppsParams `json:",inline"`
}

// UseProxyProtocol implements build.probeCRD interface
func (cr *VMEstimatorStorage) UseProxyProtocol() bool {
	return vmv1beta1.UseProxyProtocol(cr.ExtraArgs)
}

// ProbePath implements build.probeCRD interface
func (cr *VMEstimatorStorage) ProbePath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

// ProbeScheme implements build.probeCRD interface
func (cr *VMEstimatorStorage) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.ExtraArgs))
}

// ProbePort implements build.probeCRD interface
func (cr *VMEstimatorStorage) ProbePort() string {
	return cr.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VMEstimatorStorage) ProbeNeedLiveness() bool {
	return true
}

// GetMetricsPath returns prefixed path for metric requests
func (cr *VMEstimatorStorage) GetMetricsPath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, metricsPath)
}

// GetExtraArgs returns additionally configured command-line arguments
func (cr *VMEstimatorStorage) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// UseTLS returns true if TLS is enabled
func (cr *VMEstimatorStorage) UseTLS() bool {
	return vmv1beta1.UseTLS(cr.ExtraArgs)
}

// GetServiceScrape returns overrides for serviceScrape builder
func (cr *VMEstimatorStorage) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// VMEstimatorSelect defines vmestimator select nodes configuration
type VMEstimatorSelect struct {
	// PodMetadata configures Labels and Annotations which are propagated to the vmestimator select pods.
	// +optional
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for vmestimator to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for vmestimator to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`

	// ServiceSpec that will be added to vmestimator select service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmestimator select VMServiceScrape spec
	// +optional
	// +kubebuilder:validation:Type=object
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// Configures horizontal pod autoscaling.
	// +optional
	HPA *vmv1beta1.EmbeddedHPA `json:"hpa,omitempty"`
	// Configures vertical pod autoscaling.
	// +optional
	VPA *vmv1beta1.EmbeddedVPA `json:"vpa,omitempty"`
	// NetworkPolicy defines network access rules for pods created by this CR.
	// +optional
	NetworkPolicy *vmv1beta1.EmbeddedNetworkPolicy `json:"networkPolicy,omitempty"`

	// UpdateStrategy - overrides default update strategy.
	// +kubebuilder:validation:Enum=Recreate;RollingUpdate
	// +optional
	UpdateStrategy *appsv1.DeploymentStrategyType `json:"updateStrategy,omitempty"`
	// RollingUpdate - overrides deployment update params.
	// +optional
	RollingUpdate *appsv1.RollingUpdateDeployment `json:"rollingUpdate,omitempty"`

	vmv1beta1.CommonAppsParams `json:",inline"`
}

// UseProxyProtocol implements build.probeCRD interface
func (cr *VMEstimatorSelect) UseProxyProtocol() bool {
	return vmv1beta1.UseProxyProtocol(cr.ExtraArgs)
}

// ProbePath implements build.probeCRD interface
func (cr *VMEstimatorSelect) ProbePath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

// ProbeScheme implements build.probeCRD interface
func (cr *VMEstimatorSelect) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.ExtraArgs))
}

// ProbePort implements build.probeCRD interface
func (cr *VMEstimatorSelect) ProbePort() string {
	return cr.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VMEstimatorSelect) ProbeNeedLiveness() bool {
	return true
}

// GetMetricsPath returns prefixed path for metric requests
func (cr *VMEstimatorSelect) GetMetricsPath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, metricsPath)
}

// GetExtraArgs returns additionally configured command-line arguments
func (cr *VMEstimatorSelect) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// UseTLS returns true if TLS is enabled
func (cr *VMEstimatorSelect) UseTLS() bool {
	return vmv1beta1.UseTLS(cr.ExtraArgs)
}

// GetServiceScrape returns overrides for serviceScrape builder
func (cr *VMEstimatorSelect) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// VMEstimatorStatus defines the observed state of VMEstimator
type VMEstimatorStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	LastAppliedSpec *VMEstimatorSpec `json:"lastAppliedSpec,omitempty"`
	// ParsingSpecError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingSpecError string `json:"-" yaml:"-"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VMEstimator) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// VMEstimator - is a real-time cardinality estimator for metrics ingested via Prometheus remote write protocol.
// It can be deployed as a single node or as a cluster of storage and select nodes.
// See https://docs.victoriametrics.com/victoriametrics/vmestimator/
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMEstimator App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="StatefulSet,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="ConfigMap,v1"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmestimators,scope=Namespaced
// +kubebuilder:printcolumn:name="Single Count",type="string",JSONPath=".spec.single.replicaCount",description="replicas of single-node vmestimator"
// +kubebuilder:printcolumn:name="Storage Count",type="string",JSONPath=".spec.storage.replicaCount",description="replicas of vmestimator storage"
// +kubebuilder:printcolumn:name="Select Count",type="string",JSONPath=".spec.select.replicaCount",description="replicas of vmestimator select"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of update rollout"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VMEstimator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMEstimatorSpec   `json:"spec,omitempty"`
	Status VMEstimatorStatus `json:"status,omitempty"`
}

// VMEstimatorList contains a list of VMEstimator
// +kubebuilder:object:root=true
type VMEstimatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMEstimator `json:"items"`
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMEstimator) GetStatus() *VMEstimatorStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMEstimator) DefaultStatusFields(vs *VMEstimatorStatus) {
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMEstimator) UnmarshalJSON(src []byte) error {
	type pcr VMEstimator
	type shadow struct {
		*pcr
		Spec json.RawMessage `json:"spec"`
	}
	s := shadow{pcr: (*pcr)(cr)}
	if err := json.Unmarshal(src, &s); err != nil {
		return err
	}
	if len(s.Spec) > 0 {
		if err := vmv1beta1.UnmarshalSpecStrict(s.Spec, &cr.Spec); err != nil {
			cr.Status.ParsingSpecError = fmt.Sprintf("cannot parse VMEstimatorSpec: %s, err: %s", string(s.Spec), err)
		}
	}
	return nil
}

// AsOwner returns owner references with current object as owner
func (cr *VMEstimator) AsOwner() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         cr.APIVersion,
		Kind:               cr.Kind,
		Name:               cr.Name,
		UID:                cr.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
}

// FinalAnnotations returns global annotations to be applied for created objects
func (cr *VMEstimator) FinalAnnotations() map[string]string {
	var v map[string]string
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Annotations, v)
	}
	return v
}

// SelectorLabels returns selector labels for the given component kind
func (cr *VMEstimator) SelectorLabels(kind vmv1beta1.ClusterComponent) map[string]string {
	ls := map[string]string{
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
	switch kind {
	case vmv1beta1.ClusterComponentCommon:
		ls["app.kubernetes.io/part-of"] = vmEstimatorName
	case vmv1beta1.ClusterComponentRoot:
		ls["app.kubernetes.io/name"] = vmEstimatorName
	default:
		ls["app.kubernetes.io/name"] = vmEstimatorName + "-" + string(kind)
	}
	return ls
}

// FinalLabels returns labels to be applied for objects of the given component kind
func (cr *VMEstimator) FinalLabels(kind vmv1beta1.ClusterComponent) map[string]string {
	v := labels.Merge(map[string]string{
		"app.kubernetes.io/part-of": vmEstimatorName,
	}, cr.SelectorLabels(kind))
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Labels, v)
	}
	return v
}

// PodMetadata returns pod metadata for the given component kind
func (cr *VMEstimator) PodMetadata(kind vmv1beta1.ClusterComponent) *vmv1beta1.EmbeddedObjectMetadata {
	switch kind {
	case VMEstimatorComponentSingle:
		if cr.Spec.Single == nil {
			return nil
		}
		return cr.Spec.Single.PodMetadata
	case vmv1beta1.ClusterComponentStorage:
		if cr.Spec.Storage == nil {
			return nil
		}
		return cr.Spec.Storage.PodMetadata
	case vmv1beta1.ClusterComponentSelect:
		if cr.Spec.Select == nil {
			return nil
		}
		return cr.Spec.Select.PodMetadata
	default:
		panic("BUG unsupported vmestimator component kind=" + string(kind))
	}
}

// PodLabels returns pod labels for the given component kind
func (cr *VMEstimator) PodLabels(kind vmv1beta1.ClusterComponent) map[string]string {
	selectorLabels := cr.SelectorLabels(kind)
	podMetadata := cr.PodMetadata(kind)
	if podMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(podMetadata.Labels, selectorLabels)
}

// PodAnnotations returns pod annotations for the given component kind
func (cr *VMEstimator) PodAnnotations(kind vmv1beta1.ClusterComponent) map[string]string {
	podMetadata := cr.PodMetadata(kind)
	if podMetadata == nil {
		return nil
	}
	return podMetadata.Annotations
}

// GetAdditionalService returns AdditionalServiceSpec settings for the given component kind
func (cr *VMEstimator) GetAdditionalService(kind vmv1beta1.ClusterComponent) *vmv1beta1.AdditionalServiceSpec {
	switch kind {
	case VMEstimatorComponentSingle:
		if cr.Spec.Single == nil {
			return nil
		}
		return cr.Spec.Single.ServiceSpec
	case vmv1beta1.ClusterComponentStorage:
		if cr.Spec.Storage == nil {
			return nil
		}
		return cr.Spec.Storage.ServiceSpec
	case vmv1beta1.ClusterComponentSelect:
		if cr.Spec.Select == nil {
			return nil
		}
		return cr.Spec.Select.ServiceSpec
	default:
		return nil
	}
}

// PrefixedName returns prefixed name for the given component kind
func (cr *VMEstimator) PrefixedName(kind vmv1beta1.ClusterComponent) string {
	if kind == vmv1beta1.ClusterComponentRoot {
		return fmt.Sprintf("%s-%s", vmEstimatorName, cr.Name)
	}
	return vmv1beta1.ClusterPrefixedName(kind, cr.Name, vmEstimatorName+"-", false)
}

// PrefixedInternalName returns prefixed internal name for the given component kind
func (cr *VMEstimator) PrefixedInternalName(kind vmv1beta1.ClusterComponent) string {
	return vmv1beta1.ClusterPrefixedName(kind, cr.Name, vmEstimatorName+"-", true)
}

// PrefixedInsertName returns name of the storage service, which load-balances remote write requests among storage nodes
func (cr *VMEstimator) PrefixedInsertName() string {
	return cr.PrefixedName(vmv1beta1.ClusterComponentStorage) + vmEstimatorInsertSuffix
}

// GetServiceAccountName returns service account name for all vmestimator components
func (cr *VMEstimator) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName(vmv1beta1.ClusterComponentRoot)
	}
	return cr.Spec.ServiceAccountName
}

// IsOwnsServiceAccount checks if serviceAccount belongs to the CR
func (cr *VMEstimator) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

// AsURL returns url for http access to the given component service.
// Returns empty string if component is not defined.
func (cr *VMEstimator) AsURL(kind vmv1beta1.ClusterComponent) string {
	var port string
	var svcSpec *vmv1beta1.AdditionalServiceSpec
	var extraArgs map[string]string
	switch kind {
	case VMEstimatorComponentSingle:
		if cr.Spec.Single == nil {
			return ""
		}
		port, svcSpec, extraArgs = cr.Spec.Single.Port, cr.Spec.Single.ServiceSpec, cr.Spec.Single.ExtraArgs
	case vmv1beta1.ClusterComponentStorage:
		if cr.Spec.Storage == nil {
			return ""
		}
		port, svcSpec, extraArgs = cr.Spec.Storage.Port, cr.Spec.Storage.ServiceSpec, cr.Spec.Storage.ExtraArgs
	case vmv1beta1.ClusterComponentSelect:
		if cr.Spec.Select == nil {
			return ""
		}
		port, svcSpec, extraArgs = cr.Spec.Select.Port, cr.Spec.Select.ServiceSpec, cr.Spec.Select.ExtraArgs
	default:
		panic("BUG unsupported vmestimator component kind=" + string(kind))
	}
	if port == "" {
		port = vmEstimatorDefaultPort
	}
	svcName, port := vmv1beta1.ResolveServiceURL(cr.PrefixedName(kind), port, "http", svcSpec, false)
	return fmt.Sprintf("%s://%s.%s.svc:%s", vmv1beta1.HTTPProtoFromFlags(extraArgs), svcName, cr.Namespace, port)
}

// RemoteWriteURL returns url of Prometheus remote write API, which accepts data for cardinality estimation.
// In cluster mode it points to the service, which load-balances requests among storage nodes.
// Returns empty string if neither single nor storage component is defined.
func (cr *VMEstimator) RemoteWriteURL() string {
	switch {
	case cr.Spec.Single != nil:
		return cr.AsURL(VMEstimatorComponentSingle) + vmv1beta1.BuildPathWithPrefixFlag(cr.Spec.Single.ExtraArgs, vmEstimatorRemoteWritePath)
	case cr.Spec.Storage != nil:
		port := cr.Spec.Storage.Port
		if port == "" {
			port = vmEstimatorDefaultPort
		}
		// insert service inherits ports of the default storage service, which could be changed with serviceSpec.useAsDefault
		_, port = vmv1beta1.ResolveServiceURL(cr.PrefixedName(vmv1beta1.ClusterComponentStorage), port, "http", cr.Spec.Storage.ServiceSpec, false)
		extraArgs := cr.Spec.Storage.ExtraArgs
		return fmt.Sprintf("%s://%s.%s.svc:%s%s", vmv1beta1.HTTPProtoFromFlags(extraArgs), cr.PrefixedInsertName(), cr.Namespace, port, vmv1beta1.BuildPathWithPrefixFlag(extraArgs, vmEstimatorRemoteWritePath))
	default:
		return ""
	}
}

// Validate performs semantic validation of VMEstimator
func (cr *VMEstimator) Validate() error {
	if vmv1beta1.MustSkipCRValidation(cr) {
		return nil
	}
	if cr.Spec.Single != nil && (cr.Spec.Storage != nil || cr.Spec.Select != nil) {
		return fmt.Errorf("spec.single cannot be used together with spec.storage or spec.select")
	}
	if cr.Spec.Select != nil && cr.Spec.Storage == nil {
		return fmt.Errorf("spec.select requires spec.storage to be defined")
	}
	for idx := range cr.Spec.Streams {
		if err := cr.Spec.Streams[idx].Validate(); err != nil {
			return fmt.Errorf("spec.streams[%d]: %w", idx, err)
		}
	}
	if cm := cr.Spec.StreamsConfigMap; cm != nil && (cm.Name == "" || cm.Key == "") {
		return fmt.Errorf("spec.streamsConfigMap must have both name and key defined")
	}
	if err := cr.validateNames(); err != nil {
		return err
	}
	if c := cr.Spec.Single; c != nil {
		name := cr.PrefixedName(VMEstimatorComponentSingle)
		if c.ServiceSpec != nil && c.ServiceSpec.Name == name {
			return fmt.Errorf("spec.single.serviceSpec.name cannot be equal to prefixed name=%q", name)
		}
		if c.VPA != nil {
			if err := c.VPA.Validate(); err != nil {
				return fmt.Errorf("spec.single.vpa: %w", err)
			}
		}
		if err := c.Validate(); err != nil {
			return fmt.Errorf("spec.single: %w", err)
		}
	}
	if c := cr.Spec.Storage; c != nil {
		name := cr.PrefixedName(vmv1beta1.ClusterComponentStorage)
		if c.ServiceSpec != nil && (c.ServiceSpec.Name == name || c.ServiceSpec.Name == cr.PrefixedInsertName()) {
			return fmt.Errorf("spec.storage.serviceSpec.name cannot be equal to %q or %q", name, cr.PrefixedInsertName())
		}
		// select nodes reach storage nodes via DNS records of the headless service
		if err := c.ServiceSpec.ValidateHeadlessDefaultService(); err != nil {
			return fmt.Errorf("spec.storage: %w", err)
		}
		if c.ReplicaCount != nil && *c.ReplicaCount == 0 && cr.Spec.Select != nil && cr.Spec.Select.ExtraArgs["storageNode"] == "" {
			return fmt.Errorf("spec.storage.replicaCount must be positive, since spec.select requires at least one storage node")
		}
		if c.VPA != nil {
			if err := c.VPA.Validate(); err != nil {
				return fmt.Errorf("spec.storage.vpa: %w", err)
			}
		}
		if err := c.Validate(); err != nil {
			return fmt.Errorf("spec.storage: %w", err)
		}
	}
	if c := cr.Spec.Select; c != nil {
		name := cr.PrefixedName(vmv1beta1.ClusterComponentSelect)
		if c.ServiceSpec != nil && c.ServiceSpec.Name == name {
			return fmt.Errorf("spec.select.serviceSpec.name cannot be equal to prefixed name=%q", name)
		}
		if c.HPA != nil {
			if err := c.HPA.Validate(); err != nil {
				return fmt.Errorf("spec.select.hpa: %w", err)
			}
		}
		if c.VPA != nil {
			if err := c.VPA.Validate(); err != nil {
				return fmt.Errorf("spec.select.vpa: %w", err)
			}
		}
		if err := c.Validate(); err != nil {
			return fmt.Errorf("spec.select: %w", err)
		}
	}
	return nil
}

// validateNames checks that names of objects, which are derived from VMEstimator name, are accepted by Kubernetes
func (cr *VMEstimator) validateNames() error {
	var services []string
	// single-node is deployed by default
	if cr.Spec.Single != nil || (cr.Spec.Storage == nil && cr.Spec.Select == nil) {
		services = append(services, cr.PrefixedName(VMEstimatorComponentSingle))
	}
	if cr.Spec.Storage != nil {
		sts := cr.PrefixedName(vmv1beta1.ClusterComponentStorage)
		if len(sts) > vmEstimatorMaxStatefulSetNameLen {
			return fmt.Errorf("name=%q is too long for storage: StatefulSet name %q must not exceed %d chars, otherwise its pods cannot be created", cr.Name, sts, vmEstimatorMaxStatefulSetNameLen)
		}
		services = append(services, sts, cr.PrefixedInsertName())
	}
	if cr.Spec.Select != nil {
		services = append(services, cr.PrefixedName(vmv1beta1.ClusterComponentSelect))
	}
	for _, svc := range services {
		if errs := validation.IsDNS1035Label(svc); len(errs) > 0 {
			return fmt.Errorf("name=%q cannot be used: Service name %q is incorrect: %s", cr.Name, svc, strings.Join(errs, ", "))
		}
	}
	return nil
}

// LastSpecUpdated compares spec with last applied spec stored, replaces old spec and returns true if it's updated
func (cr *VMEstimator) LastSpecUpdated() bool {
	updated := cr.Status.LastAppliedSpec == nil || !equality.Semantic.DeepEqual(&cr.Spec, cr.Status.LastAppliedSpec)
	cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
	return updated
}

// Paused checks if resource reconcile should be paused
func (cr *VMEstimator) Paused() bool {
	return cr.Spec.Paused
}
