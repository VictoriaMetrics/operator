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
	"path"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VMAnomalySpec defines the desired state of VMAnomaly.
// +k8s:openapi-gen=true
type VMAnomalySpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// PodMetadata configures Labels and Annotations which are propagated to the vmanomaly pods.
	// +optional
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *vmv1beta1.ManagedObjectsMetadata `json:"managedMetadata,omitempty"`
	// LogLevel for VMAnomaly to be configured with.
	// INFO, WARN, ERROR, FATAL, PANIC
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// ServiceScrapeSpec that will be added to vmanomaly VMPodScrape spec
	// +optional
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// ShardCount - numbers of shards of VMAnomaly
	// in this case operator will use 1 sts per shard with
	// replicas count according to spec.replicas.
	// +optional
	ShardCount *int `json:"shardCount,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget       *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*vmv1beta1.EmbeddedProbes `json:",inline"`
	// ConfigRawYaml - raw configuration for anomaly,
	// it helps it to start without secret.
	// priority -> hardcoded ConfigRaw -> ConfigRaw, provided by user -> ConfigSecret.
	// +optional
	ConfigRawYaml string `json:"configRawYaml,omitempty"`
	// ConfigSecret is the name of a Kubernetes Secret in the same namespace as the
	// VMAnomaly object, which contains configuration for this VMAnomaly,
	// configuration must be inside secret key: anomaly.yaml.
	// It must be created by user.
	// instance. Defaults to 'vmanomaly-<anomaly-name>'
	// The secret is mounted into /etc/anomaly/config.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Secret with anomaly config",xDescriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ConfigSecret *corev1.SecretKeySelector `json:"configSecret,omitempty"`
	// Metrics source for VMAnomaly
	// See https://docs.victoriametrics.com/anomaly-detection/components/reader/
	Reader *VMAnomalyReadersSpec `json:"reader"`
	// Metrics destination for VMAnomaly
	// See https://docs.victoriametrics.com/anomaly-detection/components/writer/
	Writer *VMAnomalyWritersSpec `json:"writer"`
	// Storage configures storage for StatefulSet
	// +optional
	Storage *vmv1beta1.StorageSpec `json:"storage,omitempty"`
	// PersistentVolumeClaimRetentionPolicy allows configuration of PVC retention policy
	// +optional
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`
	// RollingUpdateStrategy allows configuration for strategyType
	// set it to RollingUpdate for disabling operator statefulSet rollingUpdate
	// +optional
	RollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"rollingUpdateStrategy,omitempty"`
	// ClaimTemplates allows adding additional VolumeClaimTemplates for VMAnomaly
	ClaimTemplates []corev1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`
	// Monitoring configures how expose anomaly metrics
	// See https://docs.victoriametrics.com/anomaly-detection/components/monitoring/
	Monitoring *VMAnomalyMonitoringSpec `json:"monitoring,omitempty"`
	// Server configures HTTP server for VMAnomaly
	// +optional
	Server *VMAnomalyServerSpec `json:"server,omitempty"`
	// License allows to configure license key to be used for enterprise features.
	// Using license key is supported starting from VictoriaMetrics v1.94.0.
	// See [here](https://docs.victoriametrics.com/victoriametrics/enterprise/)
	// +optional
	License *vmv1beta1.License `json:"license,omitempty"`
	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName                          string `json:"serviceAccountName,omitempty"`
	vmv1beta1.CommonDefaultableParams           `json:",inline,omitempty"`
	vmv1beta1.CommonApplicationDeploymentParams `json:",inline,omitempty"`
}

// VMAnomalyWritersSpec defines writer configuration for VMAnomaly
type VMAnomalyWritersSpec struct {
	// DatasourceURL defines remote write url for write requests
	// provided endpoint must serve /api/v1/import path
	// vmanomaly joins datasourceURL + "/api/v1/import"
	DatasourceURL string `json:"datasourceURL" yaml:"datasource_url,omitempty"`
	// Metrics to save the output (in metric names or labels)
	// +optional
	MetricFormat VMAnomalyVMWriterMetricFormatSpec `json:"metricFormat,omitempty" yaml:"metric_format,omitempty"`
	// +optional
	VMAnomalyHTTPClientSpec `json:",inline,omitempty" yaml:",inline,omitempty"`
}

// VMAnomalyVMWriterMetricFormatSpec defines the desired state of VMAnomalyVMWriterMetricFormat
type VMAnomalyVMWriterMetricFormatSpec struct {
	// Name of result metric
	// Must have a value with $VAR placeholder in it to distinguish between resulting metrics
	Name string `json:"__name__" yaml:"__name__"`
	// For is a special label with $QUERY_KEY placeholder
	For string `json:"for" yaml:"for"`
	// ExtraLabels defines additional labels to be added to the resulting metrics
	ExtraLabels map[string]string `json:"extraLabels,omitempty" yaml:"extra_labels,omitempty"`
}

// VMAnomalyHTTPClientSpec defines the desired state of VMAnomalyHTTPClient
type VMAnomalyHTTPClientSpec struct {
	// HealthPath defines absolute or relative URL address where to check availability of the remote webserver
	HealthPath string `json:"healthPath,omitempty" yaml:"health_path,omitempty"`
	// Timeout for the requests, passed as a string
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	// TenantID defines for VictoriaMetrics Cluster version only, tenants are identified by accountID, accountID:projectID or multitenant.
	TenantID string `json:"tenantID,omitempty" yaml:"tenant_id,omitempty"`
	// Basic auth defines basic authorization configuration
	BasicAuth *vmv1beta1.BasicAuth `json:"basicAuth,omitempty" yaml:"-"`
	// TLSConfig defines tls connection configuration
	TLSConfig *vmv1beta1.TLSConfig `json:"tlsConfig,omitempty" yaml:"-"`
	// BearerAuth defines authorization with Authorization: Bearer header
	BearerAuth *vmv1beta1.BearerAuth `json:"bearer,omitempty" yaml:"-"`
}

// VMAnomalyReadersSpec defines reader configuration for VMAnomaly
type VMAnomalyReadersSpec struct {
	// DatasourceURL address
	// datasource must serve /api/v1/query and /api/v1/query_range APIs
	DatasourceURL string `json:"datasourceURL" yaml:"datasource_url,omitempty"`
	// Frequency of the points returned
	SamplingPeriod string `json:"samplingPeriod" yaml:"sampling_period,omitempty"`
	// Performs PromQL/MetricsQL range query
	QueryRangePath string `json:"queryRangePath,omitempty" yaml:"query_range_path,omitempty"`
	// List of strings with series selector.
	ExtraFilters []string `json:"extraFilters,omitempty" yaml:"extra_filters,omitempty"`
	// If True, then query will be performed from the last seen timestamp for a given series.
	QueryFromLastSeenTimestamp bool `json:"queryFromLastSeenTimestamp,omitempty" yaml:"query_from_last_seen_timestamp,omitempty"`
	// It allows overriding the default -search.latencyOffsetflag of VictoriaMetrics
	LatencyOffset string `json:"latencyOffset,omitempty" yaml:"latency_offset,omitempty"`
	// Optional argoverrides how search.maxPointsPerTimeseries flagimpacts vmanomaly on splitting long fitWindow queries into smaller sub-intervals
	MaxPointsPerQuery int `json:"maxPointsPerQuery,omitempty" yaml:"max_points_per_query,omitempty"`
	// Optional argumentspecifies the IANA timezone to account for local shifts, like DST, in models sensitive to seasonal patterns
	Timezone string `json:"tz,omitempty" yaml:"tz,omitempty"`
	// Optional argumentallows defining valid data ranges for input of all the queries in queries
	DataRange               []string `json:"dataRange,omitempty" yaml:"data_range,omitempty"`
	VMAnomalyHTTPClientSpec `json:",inline" yaml:",inline"`
}

// VMAnomalyStatus defines the observed state of VMAnomaly.
// +k8s:openapi-gen=true
type VMAnomalyStatus struct {
	// Shards represents total number of vmanomaly statefulsets with uniq scrape targets
	Shards                   int32 `json:"shards,omitempty"`
	vmv1beta1.StatusMetadata `json:",inline"`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	LastAppliedSpec *VMAnomalySpec `json:"lastAppliedSpec,omitempty"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VMAnomalyStatus) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.StatusMetadata
}

// VMAnomaly is the Schema for the vmanomalies API.
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMAnomaly App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="StatefulSet,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmanomalies,scope=Namespaced
// +kubebuilder:subresource:scale:specpath=.spec.shardCount,statuspath=.status.shards,selectorpath=.status.selector
// +kubebuilder:printcolumn:name="Shards Count",type="integer",JSONPath=".status.shards",description="current number of shards"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of update rollout"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VMAnomaly struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalySpec   `json:"spec,omitempty"`
	Status VMAnomalyStatus `json:"status,omitempty"`
}

// VMAnomalyMonitoringSpec defines configuration for VMAnomaly monitoring
// See https://docs.victoriametrics.com/anomaly-detection/components/monitoring/
type VMAnomalyMonitoringSpec struct {
	Pull *VMAnomalyMonitoringPullSpec `json:"pull,omitempty" yaml:"pull,omitempty"`
	Push *VMAnomalyMonitoringPushSpec `json:"push,omitempty" yaml:"push,omitempty"`
}

// VMAnomalyMonitoringPullSpec defines pull monitoring configuration
// which is enabled by default and served at POD_IP:8490/metrics
type VMAnomalyMonitoringPullSpec struct {
	// Port defines a port for metrics scrape
	Port string `json:"port"`
}

// VMAnomalyMonitoringPushSpec defines metrics push configuration
//
// VMAnomaly uses prometheus text exposition format
type VMAnomalyMonitoringPushSpec struct {
	// defines target url for push requests
	URL string `json:"url" yaml:"url"`
	// PushFrequency defines push interval
	PushFrequency string `json:"pushFrequency,omitempty" yaml:"push_frequency,omitempty"`
	// ExtraLabels defines a set of labels to attach to the pushed metrics
	ExtraLabels             map[string]string `json:"extraLabels,omitempty" yaml:"extra_labels,omitempty"`
	VMAnomalyHTTPClientSpec `json:",inline" yaml:",inline"`
}

// VMAnomalyServerSpec defines HTTP server configuration for VMAnomaly
// See docs: https://docs.victoriametrics.com/anomaly-detection/components/server/
type VMAnomalyServerSpec struct {
	// Addr defines IP address to listen on
	// +optional
	Addr string `json:"addr,omitempty" yaml:"addr,omitempty"`
	// Port defines port to listen on
	// +optional
	Port string `json:"port,omitempty" yaml:"port,omitempty"`
	// PathPrefix defines optional URL path prefix for all HTTP routes
	// If set to 'my-app' or '/my-app', routes will be served under '/my-app/...'
	// +optional
	PathPrefix string `json:"pathPrefix,omitempty" yaml:"path_prefix,omitempty"`
	// MaxConcurrentTasks defines maximum number of concurrent anomaly detection tasks
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=20
	MaxConcurrentTasks int `json:"maxConcurrentTasks,omitempty" yaml:"max_concurrent_tasks,omitempty"`
	// UIDefaultState defines default query state for anomaly UI
	// +optional
	UIDefaultState string `json:"uiDefaultState,omitempty" yaml:"ui_default_state,omitempty"`
}

// AsOwner returns owner references with current object as owner
func (cr *VMAnomaly) AsOwner() metav1.OwnerReference {
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
func (cr *VMAnomaly) FinalAnnotations() map[string]string {
	var v map[string]string
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Annotations, v)
	}
	return v
}

// PodAnnotations returns annotations to be applied to Pod
func (cr *VMAnomaly) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMAnomaly) GetStatus() *VMAnomalyStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMAnomaly) DefaultStatusFields(vs *VMAnomalyStatus) {
	var shardCnt int32
	if cr.IsSharded() {
		shardCnt = int32(*cr.Spec.ShardCount)
	}
	vs.Shards = shardCnt
}

// SelectorLabels returns selector labels for vmanomaly
func (cr *VMAnomaly) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmanomaly",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// PodLabels returns labels to applied to Pod
func (cr *VMAnomaly) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}

	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

// FinalLabels returns global labels to be applied for created objects
func (cr *VMAnomaly) FinalLabels() map[string]string {
	v := cr.SelectorLabels()
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Labels, v)
	}
	return v
}

// PrefixedName format name of the component with hard-coded prefix
func (cr *VMAnomaly) PrefixedName() string {
	return fmt.Sprintf("vmanomaly-%s", cr.Name)
}

// GetServiceAccountName returns service account name for components
func (cr *VMAnomaly) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

// IsOwnsServiceAccount checks if serviceAccount belongs to the CR
func (cr *VMAnomaly) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

// HealthPath returns path for health requests
func (cr *VMAnomaly) HealthPath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

// GetMetricsPath returns prefixed path for metric requests
func (cr *VMAnomaly) GetMetricsPath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricsPath)
}

// UseTLS returns true if TLS is enabled
func (cr *VMAnomaly) UseTLS() bool {
	return vmv1beta1.UseTLS(cr.Spec.ExtraArgs)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VMAnomaly) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VMAnomaly) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

// Port returns port for accessing anomaly UI
func (cr *VMAnomaly) Port() string {
	return cr.Spec.Port
}

// GetVolumeName returns volume name for persistent storage
func (cr *VMAnomaly) GetVolumeName() string {
	if cr.Spec.Storage != nil && cr.Spec.Storage.VolumeClaimTemplate.Name != "" {
		return cr.Spec.Storage.VolumeClaimTemplate.Name
	}
	return "vmanomaly-storage"
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMAnomaly) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return nil
}

// Probe implements build.probeCRD interface
func (cr *VMAnomaly) Probe() *vmv1beta1.EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

// ProbePath implements build.probeCRD interface
func (cr *VMAnomaly) ProbePath() string {
	if cr.Spec.Server != nil && cr.Spec.Server.PathPrefix != "" {
		return path.Join("/", cr.Spec.Server.PathPrefix, healthPath)
	}
	return healthPath
}

// ProbeScheme implements build.probeCRD interface
func (cr *VMAnomaly) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.Spec.ExtraArgs))
}

// ProbePort implements build.probeCRD interface
func (cr *VMAnomaly) ProbePort() string {
	return cr.Port()
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VMAnomaly) ProbeNeedLiveness() bool {
	return true
}

// Validate performs semantic validation for component
func (cr *VMAnomaly) Validate() error {
	if vmv1beta1.MustSkipCRValidation(cr) {
		return nil
	}
	if !cr.Spec.License.IsProvided() {
		return fmt.Errorf("no license is provided!. Either spec.license.key or spec.license.keyRef is required")
	}
	return nil
}

// IsSharded returns true if sharding is enabled
func (cr *VMAnomaly) IsSharded() bool {
	return cr != nil && cr.Spec.ShardCount != nil && *cr.Spec.ShardCount > 1
}

// GetShardCount returns shard count for vmanomaly
func (cr *VMAnomaly) GetShardCount() int {
	if !cr.IsSharded() {
		return 1
	}
	return *cr.Spec.ShardCount
}

// LastSpecUpdated compares spec with last applied spec stored, replaces old spec and returns true if it's updated
func (cr *VMAnomaly) LastSpecUpdated() bool {
	updated := cr.Status.LastAppliedSpec == nil || !equality.Semantic.DeepEqual(&cr.Spec, cr.Status.LastAppliedSpec)
	cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
	return updated
}

// Paused checks if given component reconcile loop should be stopped
func (cr *VMAnomaly) Paused() bool {
	return cr.Spec.Paused
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalySpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalySpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vmanomaly spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyList contains a list of VMAnomaly.
type VMAnomalyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomaly `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VMAnomaly{}, &VMAnomalyList{})
}
