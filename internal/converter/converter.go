package converter

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8syaml "sigs.k8s.io/yaml"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

type VMSingleHelmValues struct {
	Global         GlobalValues    `yaml:"global,omitempty" json:"global,omitempty"`
	Server         ServerValues    `yaml:"server" json:"server"`
	ServiceAccount *ServiceAccount `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"`
}

type VMClusterHelmValues struct {
	Global         GlobalValues    `yaml:"global,omitempty" json:"global,omitempty"`
	VMSelect       ServerValues    `yaml:"vmselect" json:"vmselect"`
	VMInsert       ServerValues    `yaml:"vminsert" json:"vminsert"`
	VMStorage      ServerValues    `yaml:"vmstorage" json:"vmstorage"`
	ServiceAccount *ServiceAccount `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"`
}

type VLogsHelmValues struct {
	Global         GlobalValues    `yaml:"global,omitempty" json:"global,omitempty"`
	Server         ServerValues    `yaml:"server" json:"server"`
	ServiceAccount *ServiceAccount `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"`
}

type VTSingleHelmValues struct {
	Global         GlobalValues    `yaml:"global,omitempty" json:"global,omitempty"`
	Server         ServerValues    `yaml:"server" json:"server"`
	ServiceAccount *ServiceAccount `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"`
}

type VTClusterHelmValues struct {
	Global         GlobalValues    `yaml:"global,omitempty" json:"global,omitempty"`
	VTSelect       ServerValues    `yaml:"vtselect" json:"vtselect"`
	VTInsert       ServerValues    `yaml:"vtinsert" json:"vtinsert"`
	VTStorage      ServerValues    `yaml:"vtstorage" json:"vtstorage"`
	ServiceAccount *ServiceAccount `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"`
}

type VLClusterHelmValues struct {
	Global         GlobalValues    `yaml:"global,omitempty" json:"global,omitempty"`
	VLSelect       ServerValues    `yaml:"vlselect" json:"vlselect"`
	VLInsert       ServerValues    `yaml:"vlinsert" json:"vlinsert"`
	VLStorage      ServerValues    `yaml:"vlstorage" json:"vlstorage"`
	ServiceAccount *ServiceAccount `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"`
}

type VMAgentHelmValues struct {
	Global             GlobalValues                  `yaml:"global,omitempty" json:"global,omitempty"`
	ReplicaCount       *int32                        `yaml:"replicaCount,omitempty" json:"replicaCount,omitempty"`
	Image              ImageValues                   `yaml:"image" json:"image"`
	ImagePullSecrets   []corev1.LocalObjectReference `yaml:"imagePullSecrets,omitempty" json:"imagePullSecrets,omitempty"`
	ExtraArgs          map[string]interface{}        `yaml:"extraArgs,omitempty" json:"extraArgs,omitempty"`
	ExtraEnvs          []corev1.EnvVar               `yaml:"env,omitempty" json:"env,omitempty"`
	Resources          *corev1.ResourceRequirements  `yaml:"resources,omitempty" json:"resources,omitempty"`
	NodeSelector       map[string]string             `yaml:"nodeSelector,omitempty" json:"nodeSelector,omitempty"`
	Tolerations        []corev1.Toleration           `yaml:"tolerations,omitempty" json:"tolerations,omitempty"`
	Affinity           *corev1.Affinity              `yaml:"affinity,omitempty" json:"affinity,omitempty"`
	PodAnnotations     map[string]string             `yaml:"podAnnotations,omitempty" json:"podAnnotations,omitempty"`
	Labels             map[string]string             `yaml:"labels,omitempty" json:"labels,omitempty"`
	PodSecurityContext *corev1.PodSecurityContext    `yaml:"podSecurityContext,omitempty" json:"podSecurityContext,omitempty"`
	SecurityContext    *corev1.SecurityContext       `yaml:"securityContext,omitempty" json:"securityContext,omitempty"`
	RemoteWrite        []VMAgentRemoteWriteValues    `yaml:"remoteWrite,omitempty" json:"remoteWrite,omitempty"`
	ServiceAccount     *ServiceAccount               `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"`
}

type VMAuthHelmValues struct {
	ServerValues   `yaml:",inline"`
	Global         GlobalValues        `yaml:"global,omitempty" json:"global,omitempty"`
	Env            []corev1.EnvVar     `yaml:"env,omitempty" json:"env,omitempty"`
	ServiceAccount *ServiceAccount     `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"`
	Config         *VMAuthConfigValues `yaml:"config,omitempty" json:"config,omitempty"`
}

// VMAuthConfigValues represents vmauth's own native config file (the chart's `config`
// value). UnauthorizedUser reuses VMAuthUnauthorizedUserAccessSpec directly since its field
// names already match vmauth's native `unauthorized_user` keys.
type VMAuthConfigValues struct {
	Users            []VMAuthConfigUser                          `yaml:"users,omitempty" json:"users,omitempty"`
	UnauthorizedUser *vmv1beta1.VMAuthUnauthorizedUserAccessSpec `yaml:"unauthorized_user,omitempty" json:"unauthorized_user,omitempty"`
}

// VMAuthConfigUser represents one entry of vmauth's native config `users` list, reusing
// operator types whose field names already match vmauth's native keys. Backend TLS
// (tls_ca_file etc.) isn't covered: it doesn't map onto the operator's nested tlsConfig.
type VMAuthConfigUser struct {
	Username     string                                     `yaml:"username,omitempty" json:"username,omitempty"`
	Password     string                                     `yaml:"password,omitempty" json:"password,omitempty"`
	BearerToken  string                                     `yaml:"bearer_token,omitempty" json:"bearer_token,omitempty"`
	URLPrefix    vmv1beta1.StringOrArray                    `yaml:"url_prefix,omitempty" json:"url_prefix,omitempty"`
	URLMap       []vmv1beta1.UnauthorizedAccessConfigURLMap `yaml:"url_map,omitempty" json:"url_map,omitempty"`
	MetricLabels map[string]string                          `yaml:"metric_labels,omitempty" json:"metric_labels,omitempty"`

	vmv1beta1.VMUserConfigOptions `yaml:",inline" json:",inline"`
}

type VMAlertHelmValues struct {
	Global         GlobalValues        `yaml:"global,omitempty" json:"global,omitempty"`
	Server         VMAlertServerValues `yaml:"server" json:"server"`
	ServiceAccount *ServiceAccount     `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"`
}

type VMAnomalyHelmValues struct {
	Global             GlobalValues                  `yaml:"global,omitempty" json:"global,omitempty"`
	ReplicaCount       *int32                        `yaml:"replicaCount,omitempty" json:"replicaCount,omitempty"`
	Image              ImageValues                   `yaml:"image" json:"image"`
	ImagePullSecrets   []corev1.LocalObjectReference `yaml:"imagePullSecrets,omitempty" json:"imagePullSecrets,omitempty"`
	ExtraArgs          map[string]interface{}        `yaml:"extraArgs,omitempty" json:"extraArgs,omitempty"`
	ExtraEnvs          []corev1.EnvVar               `yaml:"env,omitempty" json:"env,omitempty"`
	Resources          *corev1.ResourceRequirements  `yaml:"resources,omitempty" json:"resources,omitempty"`
	NodeSelector       map[string]string             `yaml:"nodeSelector,omitempty" json:"nodeSelector,omitempty"`
	Tolerations        []corev1.Toleration           `yaml:"tolerations,omitempty" json:"tolerations,omitempty"`
	Affinity           *corev1.Affinity              `yaml:"affinity,omitempty" json:"affinity,omitempty"`
	PodAnnotations     map[string]string             `yaml:"podAnnotations,omitempty" json:"podAnnotations,omitempty"`
	Labels             map[string]string             `yaml:"labels,omitempty" json:"labels,omitempty"`
	PodSecurityContext *corev1.PodSecurityContext    `yaml:"podSecurityContext,omitempty" json:"podSecurityContext,omitempty"`
	SecurityContext    *corev1.SecurityContext       `yaml:"securityContext,omitempty" json:"securityContext,omitempty"`
	Reader             *VMAnomalyReaderValues        `yaml:"reader,omitempty" json:"reader,omitempty"`
	Writer             *VMAnomalyWriterValues        `yaml:"writer,omitempty" json:"writer,omitempty"`
	ServiceAccount     *ServiceAccount               `yaml:"serviceAccount,omitempty" json:"serviceAccount,omitempty"`
}

type VMAnomalyReaderValues struct {
	DatasourceURL  string `yaml:"datasourceURL,omitempty" json:"datasourceURL,omitempty"`
	SamplingPeriod string `yaml:"samplingPeriod,omitempty" json:"samplingPeriod,omitempty"`
}

type VMAnomalyWriterValues struct {
	DatasourceURL string `yaml:"datasourceURL,omitempty" json:"datasourceURL,omitempty"`
}

type VMAlertServerValues struct {
	Image              ImageValues                      `yaml:"image" json:"image"`
	ImagePullSecrets   []corev1.LocalObjectReference    `yaml:"imagePullSecrets,omitempty" json:"imagePullSecrets,omitempty"`
	ReplicaCount       *int32                           `yaml:"replicaCount,omitempty" json:"replicaCount,omitempty"`
	ExtraArgs          map[string]interface{}           `yaml:"extraArgs,omitempty" json:"extraArgs,omitempty"`
	ExtraEnvs          []corev1.EnvVar                  `yaml:"env,omitempty" json:"env,omitempty"`
	Resources          *corev1.ResourceRequirements     `yaml:"resources,omitempty" json:"resources,omitempty"`
	NodeSelector       map[string]string                `yaml:"nodeSelector,omitempty" json:"nodeSelector,omitempty"`
	Tolerations        []corev1.Toleration              `yaml:"tolerations,omitempty" json:"tolerations,omitempty"`
	Affinity           *corev1.Affinity                 `yaml:"affinity,omitempty" json:"affinity,omitempty"`
	PodAnnotations     map[string]string                `yaml:"podAnnotations,omitempty" json:"podAnnotations,omitempty"`
	Labels             map[string]string                `yaml:"labels,omitempty" json:"labels,omitempty"`
	PodSecurityContext *corev1.PodSecurityContext       `yaml:"podSecurityContext,omitempty" json:"podSecurityContext,omitempty"`
	SecurityContext    *corev1.SecurityContext          `yaml:"securityContext,omitempty" json:"securityContext,omitempty"`
	Notifier           *vmv1beta1.VMAlertNotifierSpec   `yaml:"notifier,omitempty" json:"notifier,omitempty"`
	Notifiers          []vmv1beta1.VMAlertNotifierSpec  `yaml:"notifiers,omitempty" json:"notifiers,omitempty"`
	RemoteWrite        *VMAlertRemoteWriteValues        `yaml:"remoteWrite,omitempty" json:"remoteWrite,omitempty"`
	RemoteRead         *vmv1beta1.VMAlertRemoteReadSpec `yaml:"remoteRead,omitempty" json:"remoteRead,omitempty"`
	Datasource         vmv1beta1.VMAlertDatasourceSpec  `yaml:"datasource,omitempty" json:"datasource,omitempty"`
}

type GlobalValues struct {
	ImagePullSecrets []corev1.LocalObjectReference `yaml:"imagePullSecrets,omitempty" json:"imagePullSecrets,omitempty"`
	Image            ImageValues                   `yaml:"image,omitempty" json:"image,omitempty"`
}

type ServiceAccount struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
}

type VLAgentHelmValues struct {
	Image                     ImageValues                       `yaml:"image" json:"image"`
	ReplicaCount              *int32                            `yaml:"replicaCount,omitempty" json:"replicaCount,omitempty"`
	Annotations               map[string]string                 `yaml:"annotations,omitempty" json:"annotations,omitempty"`
	PodAnnotations            map[string]string                 `yaml:"podAnnotations,omitempty" json:"podAnnotations,omitempty"`
	PodLabels                 map[string]string                 `yaml:"podLabels,omitempty" json:"podLabels,omitempty"`
	NodeSelector              map[string]string                 `yaml:"nodeSelector,omitempty" json:"nodeSelector,omitempty"`
	Tolerations               []corev1.Toleration               `yaml:"tolerations,omitempty" json:"tolerations,omitempty"`
	Affinity                  *corev1.Affinity                  `yaml:"affinity,omitempty" json:"affinity,omitempty"`
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `yaml:"topologySpreadConstraints,omitempty" json:"topologySpreadConstraints,omitempty"`
	SecurityContext           *corev1.SecurityContext           `yaml:"securityContext,omitempty" json:"securityContext,omitempty"`
	PodSecurityContext        *corev1.PodSecurityContext        `yaml:"podSecurityContext,omitempty" json:"podSecurityContext,omitempty"`
	PriorityClassName         string                            `yaml:"priorityClassName,omitempty" json:"priorityClassName,omitempty"`
	ExtraArgs                 map[string]string                 `yaml:"extraArgs,omitempty" json:"extraArgs,omitempty"`
	Env                       []corev1.EnvVar                   `yaml:"env,omitempty" json:"env,omitempty"`
	ExtraVolumes              []corev1.Volume                   `yaml:"extraVolumes,omitempty" json:"extraVolumes,omitempty"`
	ExtraVolumeMounts         []corev1.VolumeMount              `yaml:"extraVolumeMounts,omitempty" json:"extraVolumeMounts,omitempty"`
	Resources                 *corev1.ResourceRequirements      `yaml:"resources,omitempty" json:"resources,omitempty"`
	RemoteWrite               []VLAgentRemoteWriteValues        `yaml:"remoteWrite" json:"remoteWrite"`
	MaxDiskUsagePerURL        string                            `yaml:"maxDiskUsagePerURL,omitempty" json:"maxDiskUsagePerURL,omitempty"`
	PersistentVolume          *PersistentVolumeValues           `yaml:"persistentVolume,omitempty" json:"persistentVolume,omitempty"`
}

type VLCollectorHelmValues struct {
	Image                     ImageValues                       `yaml:"image" json:"image"`
	Annotations               map[string]string                 `yaml:"annotations,omitempty" json:"annotations,omitempty"`
	PodAnnotations            map[string]string                 `yaml:"podAnnotations,omitempty" json:"podAnnotations,omitempty"`
	PodLabels                 map[string]string                 `yaml:"podLabels,omitempty" json:"podLabels,omitempty"`
	NodeSelector              map[string]string                 `yaml:"nodeSelector,omitempty" json:"nodeSelector,omitempty"`
	Tolerations               []corev1.Toleration               `yaml:"tolerations,omitempty" json:"tolerations,omitempty"`
	Affinity                  *corev1.Affinity                  `yaml:"affinity,omitempty" json:"affinity,omitempty"`
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `yaml:"topologySpreadConstraints,omitempty" json:"topologySpreadConstraints,omitempty"`
	SecurityContext           *corev1.SecurityContext           `yaml:"securityContext,omitempty" json:"securityContext,omitempty"`
	PodSecurityContext        *corev1.PodSecurityContext        `yaml:"podSecurityContext,omitempty" json:"podSecurityContext,omitempty"`
	PriorityClassName         string                            `yaml:"priorityClassName,omitempty" json:"priorityClassName,omitempty"`
	ExtraArgs                 map[string]string                 `yaml:"extraArgs,omitempty" json:"extraArgs,omitempty"`
	Env                       []corev1.EnvVar                   `yaml:"env,omitempty" json:"env,omitempty"`
	ExtraVolumes              []corev1.Volume                   `yaml:"extraVolumes,omitempty" json:"extraVolumes,omitempty"`
	ExtraVolumeMounts         []corev1.VolumeMount              `yaml:"extraVolumeMounts,omitempty" json:"extraVolumeMounts,omitempty"`
	Resources                 *corev1.ResourceRequirements      `yaml:"resources,omitempty" json:"resources,omitempty"`
	RemoteWrite               []VLAgentRemoteWriteValues        `yaml:"remoteWrite" json:"remoteWrite"`
	Collector                 VLCollectorSettings               `yaml:"collector" json:"collector"`
}

type VLCollectorSettings struct {
	TimeField              []string `yaml:"timeField,omitempty" json:"timeField,omitempty"`
	MsgField               []string `yaml:"msgField,omitempty" json:"msgField,omitempty"`
	StreamFields           []string `yaml:"streamFields,omitempty" json:"streamFields,omitempty"`
	ExcludeFilter          string   `yaml:"excludeFilter,omitempty" json:"excludeFilter,omitempty"`
	IncludePodLabels       *bool    `yaml:"includePodLabels,omitempty" json:"includePodLabels,omitempty"`
	IncludePodAnnotations  *bool    `yaml:"includePodAnnotations,omitempty" json:"includePodAnnotations,omitempty"`
	IncludeNodeLabels      *bool    `yaml:"includeNodeLabels,omitempty" json:"includeNodeLabels,omitempty"`
	IncludeNodeAnnotations *bool    `yaml:"includeNodeAnnotations,omitempty" json:"includeNodeAnnotations,omitempty"`
	ExtraFields            string   `yaml:"extraFields,omitempty" json:"extraFields,omitempty"`
	IgnoreFields           []string `yaml:"ignoreFields,omitempty" json:"ignoreFields,omitempty"`
	TenantID               string   `yaml:"tenantID,omitempty" json:"tenantID,omitempty"`
	CheckpointsPath        *string  `yaml:"checkpointsPath,omitempty" json:"checkpointsPath,omitempty"`
	LogsPath               string   `yaml:"logsPath,omitempty" json:"logsPath,omitempty"`
}

type ServiceValues struct {
	Annotations              map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
	Labels                   map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	ClusterIP                string            `yaml:"clusterIP,omitempty" json:"clusterIP,omitempty"`
	ExternalIPs              []string          `yaml:"externalIPs,omitempty" json:"externalIPs,omitempty"`
	LoadBalancerIP           string            `yaml:"loadBalancerIP,omitempty" json:"loadBalancerIP,omitempty"`
	LoadBalancerSourceRanges []string          `yaml:"loadBalancerSourceRanges,omitempty" json:"loadBalancerSourceRanges,omitempty"`
	Type                     string            `yaml:"type,omitempty" json:"type,omitempty"`
}

type ServerValues struct {
	Enabled            *bool                         `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Name               string                        `yaml:"name,omitempty" json:"name,omitempty"`
	Image              ImageValues                   `yaml:"image" json:"image"`
	ImagePullSecrets   []corev1.LocalObjectReference `yaml:"imagePullSecrets,omitempty" json:"imagePullSecrets,omitempty"`
	ReplicaCount       *int32                        `yaml:"replicaCount,omitempty" json:"replicaCount,omitempty"`
	RetentionPeriod    interface{}                   `yaml:"retentionPeriod,omitempty" json:"retentionPeriod,omitempty"`
	ExtraArgs          map[string]interface{}        `yaml:"extraArgs,omitempty" json:"extraArgs,omitempty"`
	ExtraEnvs          []corev1.EnvVar               `yaml:"extraEnvs,omitempty" json:"extraEnvs,omitempty"`
	Resources          *corev1.ResourceRequirements  `yaml:"resources,omitempty" json:"resources,omitempty"`
	NodeSelector       map[string]string             `yaml:"nodeSelector,omitempty" json:"nodeSelector,omitempty"`
	Tolerations        []corev1.Toleration           `yaml:"tolerations,omitempty" json:"tolerations,omitempty"`
	Affinity           *corev1.Affinity              `yaml:"affinity,omitempty" json:"affinity,omitempty"`
	PodAnnotations     map[string]string             `yaml:"podAnnotations,omitempty" json:"podAnnotations,omitempty"`
	Labels             map[string]string             `yaml:"labels,omitempty" json:"labels,omitempty"`
	PodSecurityContext *corev1.PodSecurityContext    `yaml:"podSecurityContext,omitempty" json:"podSecurityContext,omitempty"`
	SecurityContext    *corev1.SecurityContext       `yaml:"securityContext,omitempty" json:"securityContext,omitempty"`
	PersistentVolume   *PersistentVolumeValues       `yaml:"persistentVolume,omitempty" json:"persistentVolume,omitempty"`
	Service            *ServiceValues                `yaml:"service,omitempty" json:"service,omitempty"`
}

type ImageValues struct {
	Registry   string `yaml:"registry,omitempty" json:"registry,omitempty"`
	Repository string `yaml:"repository" json:"repository"`
	Tag        string `yaml:"tag" json:"tag"`
	Variant    string `yaml:"variant,omitempty" json:"variant,omitempty"`
	PullPolicy string `yaml:"pullPolicy,omitempty" json:"pullPolicy,omitempty"`
}

type PersistentVolumeValues struct {
	Enabled          bool              `yaml:"enabled" json:"enabled"`
	StorageClassName string            `yaml:"storageClassName,omitempty" json:"storageClassName,omitempty"`
	Size             string            `yaml:"size,omitempty" json:"size,omitempty"`
	MountPath        string            `yaml:"mountPath,omitempty" json:"mountPath,omitempty"`
	Annotations      map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

// remoteWriteTLSValues captures the flat tls* keys the helm charts expose as sibling keys on
// each remoteWrite item, rather than the operator CRD's nested tlsConfig object.
type remoteWriteTLSValues struct {
	TLSCAFile             string `yaml:"tlsCAFile,omitempty" json:"tlsCAFile,omitempty"`
	TLSCertFile           string `yaml:"tlsCertFile,omitempty" json:"tlsCertFile,omitempty"`
	TLSKeyFile            string `yaml:"tlsKeyFile,omitempty" json:"tlsKeyFile,omitempty"`
	TLSServerName         string `yaml:"tlsServerName,omitempty" json:"tlsServerName,omitempty"`
	TLSInsecureSkipVerify bool   `yaml:"tlsInsecureSkipVerify,omitempty" json:"tlsInsecureSkipVerify,omitempty"`
}

// isSet reports whether any flat tls* key was provided.
func (v remoteWriteTLSValues) isSet() bool {
	return v.TLSCAFile != "" || v.TLSCertFile != "" || v.TLSKeyFile != "" || v.TLSServerName != "" || v.TLSInsecureSkipVerify
}

// asTLSConfig converts the flat tls* keys to the operator's nested TLSConfig, or nil if none were set.
func (v remoteWriteTLSValues) asTLSConfig() *vmv1beta1.TLSConfig {
	if !v.isSet() {
		return nil
	}
	return &vmv1beta1.TLSConfig{
		CAFile:             v.TLSCAFile,
		CertFile:           v.TLSCertFile,
		KeyFile:            v.TLSKeyFile,
		ServerName:         v.TLSServerName,
		InsecureSkipVerify: v.TLSInsecureSkipVerify,
	}
}

// asVLTLSConfig converts the flat tls* keys to the v1 API's nested TLSConfig, or nil if none were set.
func (v remoteWriteTLSValues) asVLTLSConfig() *vmv1.TLSConfig {
	if !v.isSet() {
		return nil
	}
	return &vmv1.TLSConfig{
		CAFile:             v.TLSCAFile,
		CertFile:           v.TLSCertFile,
		KeyFile:            v.TLSKeyFile,
		ServerName:         v.TLSServerName,
		InsecureSkipVerify: v.TLSInsecureSkipVerify,
	}
}

type VMAgentRemoteWriteValues struct {
	vmv1beta1.VMAgentRemoteWriteSpec `yaml:",inline" json:",inline"`
	remoteWriteTLSValues             `yaml:",inline" json:",inline"`
}

// asSpec merges the flat tls* keys into TLSConfig, unless it was already set explicitly.
func (v VMAgentRemoteWriteValues) asSpec() vmv1beta1.VMAgentRemoteWriteSpec {
	rw := v.VMAgentRemoteWriteSpec
	if rw.TLSConfig == nil {
		rw.TLSConfig = v.asTLSConfig()
	}
	return rw
}

type VLAgentRemoteWriteValues struct {
	vmv1.VLAgentRemoteWriteSpec `yaml:",inline" json:",inline"`
	remoteWriteTLSValues        `yaml:",inline" json:",inline"`
}

// asSpec merges the flat tls* keys into TLSConfig, unless it was already set explicitly.
func (v VLAgentRemoteWriteValues) asSpec() vmv1.VLAgentRemoteWriteSpec {
	rw := v.VLAgentRemoteWriteSpec
	if rw.TLSConfig == nil {
		rw.TLSConfig = v.asVLTLSConfig()
	}
	return rw
}

// VMAlertRemoteWriteValues is singular, unlike VMAgent/VLAgent's list-based remoteWrite.
type VMAlertRemoteWriteValues struct {
	vmv1beta1.VMAlertRemoteWriteSpec `yaml:",inline" json:",inline"`
	remoteWriteTLSValues             `yaml:",inline" json:",inline"`
}

// asSpec merges the flat tls* keys into TLSConfig, unless it was already set explicitly.
func (v VMAlertRemoteWriteValues) asSpec() vmv1beta1.VMAlertRemoteWriteSpec {
	rw := v.VMAlertRemoteWriteSpec
	if rw.TLSConfig == nil {
		rw.TLSConfig = v.asTLSConfig()
	}
	return rw
}

func convertVMAgentRemoteWrite(items []VMAgentRemoteWriteValues) []vmv1beta1.VMAgentRemoteWriteSpec {
	if items == nil {
		return nil
	}
	result := make([]vmv1beta1.VMAgentRemoteWriteSpec, 0, len(items))
	for _, item := range items {
		result = append(result, item.asSpec())
	}
	return result
}

func convertVLAgentRemoteWrite(items []VLAgentRemoteWriteValues) []vmv1.VLAgentRemoteWriteSpec {
	if items == nil {
		return nil
	}
	result := make([]vmv1.VLAgentRemoteWriteSpec, 0, len(items))
	for _, item := range items {
		result = append(result, item.asSpec())
	}
	return result
}

func convertVMAlertRemoteWrite(item *VMAlertRemoteWriteValues) *vmv1beta1.VMAlertRemoteWriteSpec {
	if item == nil {
		return nil
	}
	rw := item.asSpec()
	return &rw
}

var (
	helmChartsRawBaseURL = "https://raw.githubusercontent.com/VictoriaMetrics/helm-charts"
	helmChartsIndexURL   = "https://victoriametrics.github.io/helm-charts/index.yaml"
)

var helmHTTPClient = &http.Client{Timeout: 30 * time.Second}

type helmRepoIndex struct {
	Entries map[string][]struct {
		Version string `yaml:"version"`
	} `yaml:"entries"`
}

// fetchLatestChartVersion fetches the latest version of chart from the helm-charts repository.
func fetchLatestChartVersion(chart string) (string, error) {
	resp, err := helmHTTPClient.Get(helmChartsIndexURL)
	if err != nil {
		return "", fmt.Errorf("cannot fetch helm repo index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cannot fetch helm repo index: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("cannot read helm repo index: %w", err)
	}

	var index helmRepoIndex
	if err := yaml.Unmarshal(data, &index); err != nil {
		return "", fmt.Errorf("cannot parse helm repo index: %w", err)
	}

	entries, ok := index.Entries[chart]
	if !ok || len(entries) == 0 {
		return "", fmt.Errorf("chart %q not found in helm repo index", chart)
	}

	// Index entries are sorted newest-first.
	return entries[0].Version, nil
}

// FetchChartDefaults fetches chart's default values.yaml at its latest released version.
func FetchChartDefaults(chart string) ([]byte, error) {
	version, err := fetchLatestChartVersion(chart)
	if err != nil {
		return nil, err
	}

	ref := fmt.Sprintf("%s-%s", chart, version)
	url := fmt.Sprintf("%s/%s/charts/%s/values.yaml", helmChartsRawBaseURL, ref, chart)

	resp, err := helmHTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("cannot fetch chart defaults from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cannot fetch chart defaults: HTTP %d for %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read chart defaults response: %w", err)
	}
	return data, nil
}

// MergeValues deep-merges base and override YAML, with override taking precedence.
func MergeValues(base, override []byte) ([]byte, error) {
	var baseMap map[string]any
	var overrideMap map[string]any

	if err := k8syaml.Unmarshal(base, &baseMap); err != nil {
		return nil, fmt.Errorf("cannot unmarshal base values: %w", err)
	}
	if err := k8syaml.Unmarshal(override, &overrideMap); err != nil {
		return nil, fmt.Errorf("cannot unmarshal override values: %w", err)
	}

	if baseMap == nil {
		normalizeHeaderMaps(overrideMap)
		return k8syaml.Marshal(overrideMap)
	}
	if err := build.MergeDeep(&baseMap, &overrideMap, false); err != nil {
		return nil, fmt.Errorf("cannot merge values: %w", err)
	}
	normalizeHeaderMaps(baseMap)
	return k8syaml.Marshal(baseMap)
}

// normalizeHeaderMaps rewrites any "headers" key whose value is a map (the shape several
// charts' default values.yaml use, e.g. `datasource.headers: {}`) into the []string
// "key:value" format the operator's HTTPAuth.Headers field expects.
func normalizeHeaderMaps(v any) {
	switch val := v.(type) {
	case map[string]any:
		for k, sub := range val {
			if k == "headers" {
				if m, ok := sub.(map[string]any); ok {
					val[k] = headersMapToSlice(m)
					continue
				}
			}
			normalizeHeaderMaps(sub)
		}
	case []any:
		for _, item := range val {
			normalizeHeaderMaps(item)
		}
	}
}

// headersMapToSlice converts a {headerName: headerValue} map into the sorted
// "headerName:headerValue" string slice HTTPAuth.Headers expects.
func headersMapToSlice(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(m))
	for _, k := range keys {
		result = append(result, fmt.Sprintf("%s:%v", k, m[k]))
	}
	return result
}

func UnmarshalValues(data []byte, chart string) (any, error) {
	switch chart {
	case "victoria-metrics-auth":
		var values VMAuthHelmValues
		if err := k8syaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-metrics-single":
		var values VMSingleHelmValues
		if err := k8syaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-metrics-cluster":
		var values VMClusterHelmValues
		if err := k8syaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-metrics-agent":
		var values VMAgentHelmValues
		if err := k8syaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-metrics-alert":
		var values VMAlertHelmValues
		if err := k8syaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-metrics-anomaly":
		var values VMAnomalyHelmValues
		if err := k8syaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-logs-cluster":
		var values VLClusterHelmValues
		if err := k8syaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-logs-agent":
		var values VLAgentHelmValues
		if err := k8syaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-logs-collector":
		var values VLCollectorHelmValues
		if err := k8syaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-logs-single":
		var values VLogsHelmValues
		if err := k8syaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-traces-single":
		var values VTSingleHelmValues
		if err := k8syaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-traces-cluster":
		var values VTClusterHelmValues
		if err := k8syaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	default:
		return nil, fmt.Errorf("unsupported chart: %s", chart)
	}
}

func Convert(name, namespace string, values any) (any, error) {
	var cr any

	switch v := values.(type) {
	case *VMAuthHelmValues:
		auth := &vmv1beta1.VMAuth{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1beta1",
				Kind:       "VMAuth",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		spec, err := convertVMAuthSpec(v)
		if err != nil {
			return nil, err
		}
		auth.Spec = *spec
		if v.Config != nil && len(v.Config.Users) > 0 {
			// Default userSelector/userNamespaceSelector (both nil) select nothing, so the
			// generated VMUsers would never actually get loaded without this.
			auth.Spec.UserSelector = &metav1.LabelSelector{MatchLabels: vmAuthUserSelectorLabels(name)}
		}
		cr = auth

	case *VMSingleHelmValues:
		single := &vmv1beta1.VMSingle{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1beta1",
				Kind:       "VMSingle",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		spec, err := convertVMSingleSpec(v)
		if err != nil {
			return nil, err
		}
		single.Spec = *spec
		cr = single

	case *VMClusterHelmValues:
		cluster := &vmv1beta1.VMCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1beta1",
				Kind:       "VMCluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		spec, err := convertVMClusterSpec(v)
		if err != nil {
			return nil, err
		}
		cluster.Spec = *spec
		cr = cluster

	case *VMAgentHelmValues:
		agent := &vmv1beta1.VMAgent{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1beta1",
				Kind:       "VMAgent",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		spec, err := convertVMAgentSpec(v)
		if err != nil {
			return nil, err
		}
		agent.Spec = *spec
		cr = agent

	case *VMAlertHelmValues:
		alert := &vmv1beta1.VMAlert{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1beta1",
				Kind:       "VMAlert",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		spec, err := convertVMAlertSpec(v)
		if err != nil {
			return nil, err
		}
		alert.Spec = *spec
		cr = alert

	case *VMAnomalyHelmValues:
		anomaly := &vmv1.VMAnomaly{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1",
				Kind:       "VMAnomaly",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		spec, err := convertVMAnomalySpec(v)
		if err != nil {
			return nil, err
		}
		anomaly.Spec = *spec
		cr = anomaly

	case *VLAgentHelmValues:
		agent := &vmv1.VLAgent{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1",
				Kind:       "VLAgent",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		spec, err := convertVLAgentSpec(v)
		if err != nil {
			return nil, err
		}
		agent.Spec = *spec
		cr = agent

	case *VLClusterHelmValues:
		cluster := &vmv1.VLCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1",
				Kind:       "VLCluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		spec, err := convertVLClusterSpec(v)
		if err != nil {
			return nil, err
		}
		cluster.Spec = *spec
		cr = cluster

	case *VLCollectorHelmValues:
		agent := &vmv1.VLAgent{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1",
				Kind:       "VLAgent",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		spec, err := convertVLCollectorSpec(v)
		if err != nil {
			return nil, err
		}
		agent.Spec = *spec
		cr = agent

	case *VLogsHelmValues:
		vlogs := &vmv1beta1.VLogs{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1beta1",
				Kind:       "VLogs",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		spec, err := convertVLogsSpec(v)
		if err != nil {
			return nil, err
		}
		vlogs.Spec = *spec
		cr = vlogs

	case *VTSingleHelmValues:
		vtsingle := &vmv1.VTSingle{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1",
				Kind:       "VTSingle",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		spec, err := convertVTSingleSpec(v)
		if err != nil {
			return nil, err
		}
		vtsingle.Spec = *spec
		cr = vtsingle

	case *VTClusterHelmValues:
		cluster := &vmv1.VTCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1",
				Kind:       "VTCluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		spec, err := convertVTClusterSpec(v)
		if err != nil {
			return nil, err
		}
		cluster.Spec = *spec
		cr = cluster

	default:
		panic(fmt.Sprintf("unsupported values type: %T", values))
	}

	return cr, nil
}

type commonConfig struct {
	vmv1beta1.CommonAppsParams
	ServiceSpec *vmv1beta1.AdditionalServiceSpec
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata
	Storage     *corev1.PersistentVolumeClaimSpec
}

func convertCommonConfig(values ServerValues, global GlobalValues) (commonConfig, error) {
	var cfg commonConfig

	cfg.ReplicaCount = values.ReplicaCount

	cfg.Image = convertImage(values.Image, global.Image)

	if len(values.ExtraArgs) > 0 {
		cfg.ExtraArgs = make(map[string]string)
		for k, v := range values.ExtraArgs {
			cfg.ExtraArgs[k] = fmt.Sprint(v)
		}
	}

	if len(values.ExtraEnvs) > 0 {
		cfg.ExtraEnvs = values.ExtraEnvs
	}

	storage, err := convertPersistentVolume(values.PersistentVolume)
	if err != nil {
		return cfg, err
	}
	cfg.Storage = storage

	if values.Resources != nil {
		cfg.Resources = *values.Resources
	}
	if len(values.NodeSelector) > 0 {
		cfg.NodeSelector = values.NodeSelector
	}
	if len(values.Tolerations) > 0 {
		cfg.Tolerations = values.Tolerations
	}
	if values.Affinity != nil {
		cfg.Affinity = values.Affinity
	}

	if values.PodSecurityContext != nil || values.SecurityContext != nil {
		cfg.SecurityContext = &vmv1beta1.SecurityContext{
			PodSecurityContext:       mergePodSecurityContext(values.PodSecurityContext, values.SecurityContext),
			ContainerSecurityContext: convertContainerSecurityContext(values.SecurityContext),
		}
	}

	if len(values.ImagePullSecrets) > 0 {
		cfg.ImagePullSecrets = values.ImagePullSecrets
	} else if len(global.ImagePullSecrets) > 0 {
		cfg.ImagePullSecrets = global.ImagePullSecrets
	}

	if len(values.PodAnnotations) > 0 || len(values.Labels) > 0 {
		cfg.PodMetadata = &vmv1beta1.EmbeddedObjectMetadata{
			Annotations: values.PodAnnotations,
			Labels:      values.Labels,
		}
	}

	cfg.ServiceSpec = convertService(values.Service)

	return cfg, nil
}

// convertContainerSecurityContext maps the subset of corev1.SecurityContext fields that the
// operator's CRD can represent at the container level.
func convertContainerSecurityContext(sc *corev1.SecurityContext) *vmv1beta1.ContainerSecurityContext {
	if sc == nil {
		return nil
	}
	return &vmv1beta1.ContainerSecurityContext{
		Privileged:               sc.Privileged,
		Capabilities:             sc.Capabilities,
		ReadOnlyRootFilesystem:   sc.ReadOnlyRootFilesystem,
		AllowPrivilegeEscalation: sc.AllowPrivilegeEscalation,
		ProcMount:                sc.ProcMount,
	}
}

// mergePodSecurityContext promotes RunAsUser/RunAsGroup/RunAsNonRoot/SeccompProfile/
// AppArmorProfile/SELinuxOptions/WindowsOptions from the chart's container-level
// securityContext into the pod-level one when not already set there, since the operator's CRD
// has no container-level equivalents. Note this inverts Kubernetes' own precedence: a pod-level
// value set here always wins, whereas Kubernetes normally lets a container-level value
// override its pod-level counterpart at runtime.
func mergePodSecurityContext(pod *corev1.PodSecurityContext, container *corev1.SecurityContext) *corev1.PodSecurityContext {
	if container == nil {
		return pod
	}
	if container.RunAsUser == nil && container.RunAsGroup == nil && container.RunAsNonRoot == nil &&
		container.SeccompProfile == nil && container.AppArmorProfile == nil &&
		container.SELinuxOptions == nil && container.WindowsOptions == nil {
		return pod
	}
	merged := &corev1.PodSecurityContext{}
	if pod != nil {
		merged = pod.DeepCopy()
	}
	if merged.RunAsUser == nil {
		merged.RunAsUser = container.RunAsUser
	}
	if merged.RunAsGroup == nil {
		merged.RunAsGroup = container.RunAsGroup
	}
	if merged.RunAsNonRoot == nil {
		merged.RunAsNonRoot = container.RunAsNonRoot
	}
	if merged.SeccompProfile == nil {
		merged.SeccompProfile = container.SeccompProfile
	}
	if merged.AppArmorProfile == nil {
		merged.AppArmorProfile = container.AppArmorProfile
	}
	if merged.SELinuxOptions == nil {
		merged.SELinuxOptions = container.SELinuxOptions
	}
	if merged.WindowsOptions == nil {
		merged.WindowsOptions = container.WindowsOptions
	}
	return merged
}

func convertImage(image ImageValues, globalImage ImageValues) vmv1beta1.Image {
	var result vmv1beta1.Image

	repo := image.Repository
	registry := image.Registry
	if registry == "" && globalImage.Registry != "" {
		registry = globalImage.Registry
	}
	if registry != "" {
		repo = fmt.Sprintf("%s/%s", registry, repo)
	}
	result.Repository = repo

	tag := image.Tag
	if image.Variant != "" {
		tag = fmt.Sprintf("%s-%s", tag, image.Variant)
	}
	result.Tag = tag
	if image.PullPolicy != "" {
		result.PullPolicy = corev1.PullPolicy(image.PullPolicy)
	}

	return result
}

func convertService(service *ServiceValues) *vmv1beta1.AdditionalServiceSpec {
	if service == nil {
		return nil
	}

	spec := &vmv1beta1.AdditionalServiceSpec{
		UseAsDefault: true,
	}

	if len(service.Annotations) > 0 || len(service.Labels) > 0 {
		spec.EmbeddedObjectMetadata = vmv1beta1.EmbeddedObjectMetadata{
			Annotations: service.Annotations,
			Labels:      service.Labels,
		}
	}

	if service.Type != "" {
		spec.Spec.Type = corev1.ServiceType(service.Type)
	}
	if service.ClusterIP != "" {
		spec.Spec.ClusterIP = service.ClusterIP
	}
	if service.LoadBalancerIP != "" {
		spec.Spec.LoadBalancerIP = service.LoadBalancerIP
	}
	if len(service.ExternalIPs) > 0 {
		spec.Spec.ExternalIPs = service.ExternalIPs
	}
	if len(service.LoadBalancerSourceRanges) > 0 {
		spec.Spec.LoadBalancerSourceRanges = service.LoadBalancerSourceRanges
	}

	return spec
}

func convertPersistentVolume(pv *PersistentVolumeValues) (*corev1.PersistentVolumeClaimSpec, error) {
	if pv == nil || !pv.Enabled {
		return nil, nil
	}

	storage := &corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
	}
	if pv.StorageClassName != "" {
		if pv.StorageClassName == "-" {
			storageClass := ""
			storage.StorageClassName = &storageClass
		} else {
			storage.StorageClassName = &pv.StorageClassName
		}
	}
	if pv.Size != "" {
		q, err := resource.ParseQuantity(pv.Size)
		if err != nil {
			return nil, fmt.Errorf("cannot parse persistent volume size %q: %w", pv.Size, err)
		}
		storage.Resources = corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: q,
			},
		}
	}
	return storage, nil
}

func convertVMSingleSpec(values *VMSingleHelmValues) (*vmv1beta1.VMSingleSpec, error) {
	spec := &vmv1beta1.VMSingleSpec{}
	cfg, err := convertCommonConfig(values.Server, values.Global)
	if err != nil {
		return nil, err
	}

	spec.ReplicaCount = cfg.ReplicaCount
	spec.Image = cfg.Image
	spec.ExtraArgs = cfg.ExtraArgs
	spec.ExtraEnvs = cfg.ExtraEnvs
	spec.Resources = cfg.Resources
	spec.NodeSelector = cfg.NodeSelector
	spec.Tolerations = cfg.Tolerations
	spec.Affinity = cfg.Affinity
	spec.SecurityContext = cfg.SecurityContext
	spec.ImagePullSecrets = cfg.ImagePullSecrets
	spec.PodMetadata = cfg.PodMetadata
	spec.ServiceSpec = cfg.ServiceSpec
	spec.Storage = cfg.Storage

	if values.Server.RetentionPeriod != nil {
		spec.RetentionPeriod = fmt.Sprint(values.Server.RetentionPeriod)
	}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	return spec, nil
}

func convertVMAnomalySpec(values *VMAnomalyHelmValues) (*vmv1.VMAnomalySpec, error) {
	spec := &vmv1.VMAnomalySpec{}

	cfg, err := convertCommonConfig(ServerValues{
		Image:              values.Image,
		ImagePullSecrets:   values.ImagePullSecrets,
		ReplicaCount:       values.ReplicaCount,
		ExtraArgs:          values.ExtraArgs,
		ExtraEnvs:          values.ExtraEnvs,
		Resources:          values.Resources,
		NodeSelector:       values.NodeSelector,
		Tolerations:        values.Tolerations,
		Affinity:           values.Affinity,
		PodAnnotations:     values.PodAnnotations,
		Labels:             values.Labels,
		PodSecurityContext: values.PodSecurityContext,
		SecurityContext:    values.SecurityContext,
	}, values.Global)
	if err != nil {
		return nil, err
	}

	spec.ReplicaCount = cfg.ReplicaCount
	spec.Image = cfg.Image
	spec.ExtraArgs = cfg.ExtraArgs
	spec.ExtraEnvs = cfg.ExtraEnvs
	spec.Resources = cfg.Resources
	spec.NodeSelector = cfg.NodeSelector
	spec.Tolerations = cfg.Tolerations
	spec.Affinity = cfg.Affinity
	spec.SecurityContext = cfg.SecurityContext
	spec.ImagePullSecrets = cfg.ImagePullSecrets
	spec.PodMetadata = cfg.PodMetadata
	if values.Reader != nil {
		spec.Reader = &vmv1.VMAnomalyReadersSpec{
			DatasourceURL:  values.Reader.DatasourceURL,
			SamplingPeriod: values.Reader.SamplingPeriod,
		}
	}
	if values.Writer != nil {
		spec.Writer = &vmv1.VMAnomalyWritersSpec{
			DatasourceURL: values.Writer.DatasourceURL,
		}
	}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	return spec, nil
}

func convertVMAlertSpec(values *VMAlertHelmValues) (*vmv1beta1.VMAlertSpec, error) {
	spec := &vmv1beta1.VMAlertSpec{}

	cfg, err := convertCommonConfig(ServerValues{
		Image:              values.Server.Image,
		ImagePullSecrets:   values.Server.ImagePullSecrets,
		ReplicaCount:       values.Server.ReplicaCount,
		ExtraArgs:          values.Server.ExtraArgs,
		ExtraEnvs:          values.Server.ExtraEnvs,
		Resources:          values.Server.Resources,
		NodeSelector:       values.Server.NodeSelector,
		Tolerations:        values.Server.Tolerations,
		Affinity:           values.Server.Affinity,
		PodAnnotations:     values.Server.PodAnnotations,
		Labels:             values.Server.Labels,
		PodSecurityContext: values.Server.PodSecurityContext,
		SecurityContext:    values.Server.SecurityContext,
	}, values.Global)
	if err != nil {
		return nil, err
	}

	spec.ReplicaCount = cfg.ReplicaCount
	spec.Image = cfg.Image
	spec.ExtraArgs = cfg.ExtraArgs
	spec.ExtraEnvs = cfg.ExtraEnvs
	spec.Resources = cfg.Resources
	spec.NodeSelector = cfg.NodeSelector
	spec.Tolerations = cfg.Tolerations
	spec.Affinity = cfg.Affinity
	spec.SecurityContext = cfg.SecurityContext
	spec.ImagePullSecrets = cfg.ImagePullSecrets
	spec.PodMetadata = cfg.PodMetadata
	spec.ServiceSpec = cfg.ServiceSpec
	spec.Notifier = values.Server.Notifier
	spec.Notifiers = values.Server.Notifiers
	spec.RemoteWrite = convertVMAlertRemoteWrite(values.Server.RemoteWrite)
	spec.RemoteRead = values.Server.RemoteRead
	spec.Datasource = values.Server.Datasource

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	return spec, nil
}

func convertVMAgentSpec(values *VMAgentHelmValues) (*vmv1beta1.VMAgentSpec, error) {
	spec := &vmv1beta1.VMAgentSpec{}

	cfg, err := convertCommonConfig(ServerValues{
		Image:              values.Image,
		ImagePullSecrets:   values.ImagePullSecrets,
		ReplicaCount:       values.ReplicaCount,
		ExtraArgs:          values.ExtraArgs,
		ExtraEnvs:          values.ExtraEnvs,
		Resources:          values.Resources,
		NodeSelector:       values.NodeSelector,
		Tolerations:        values.Tolerations,
		Affinity:           values.Affinity,
		PodAnnotations:     values.PodAnnotations,
		Labels:             values.Labels,
		PodSecurityContext: values.PodSecurityContext,
		SecurityContext:    values.SecurityContext,
	}, values.Global)
	if err != nil {
		return nil, err
	}

	spec.ReplicaCount = cfg.ReplicaCount
	spec.Image = cfg.Image
	spec.ExtraArgs = cfg.ExtraArgs
	spec.ExtraEnvs = cfg.ExtraEnvs
	spec.Resources = cfg.Resources
	spec.NodeSelector = cfg.NodeSelector
	spec.Tolerations = cfg.Tolerations
	spec.Affinity = cfg.Affinity
	spec.SecurityContext = cfg.SecurityContext
	spec.ImagePullSecrets = cfg.ImagePullSecrets
	spec.PodMetadata = cfg.PodMetadata
	spec.ServiceSpec = cfg.ServiceSpec
	spec.RemoteWrite = convertVMAgentRemoteWrite(values.RemoteWrite)

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	return spec, nil
}

func convertVMClusterSpec(values *VMClusterHelmValues) (*vmv1beta1.VMClusterSpec, error) {
	spec := &vmv1beta1.VMClusterSpec{}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	if values.VMStorage.RetentionPeriod != nil {
		spec.RetentionPeriod = fmt.Sprint(values.VMStorage.RetentionPeriod)
	}

	if values.VMSelect.Enabled == nil || *values.VMSelect.Enabled {
		spec.VMSelect = &vmv1beta1.VMSelect{}
		cfg, err := convertCommonConfig(values.VMSelect, values.Global)
		if err != nil {
			return nil, err
		}
		spec.VMSelect.CommonAppsParams = cfg.CommonAppsParams
		spec.VMSelect.PodMetadata = cfg.PodMetadata
		spec.VMSelect.ServiceSpec = cfg.ServiceSpec
	}

	if values.VMInsert.Enabled == nil || *values.VMInsert.Enabled {
		spec.VMInsert = &vmv1beta1.VMInsert{}
		cfg, err := convertCommonConfig(values.VMInsert, values.Global)
		if err != nil {
			return nil, err
		}
		spec.VMInsert.CommonAppsParams = cfg.CommonAppsParams
		spec.VMInsert.PodMetadata = cfg.PodMetadata
		spec.VMInsert.ServiceSpec = cfg.ServiceSpec
	}

	if values.VMStorage.Enabled == nil || *values.VMStorage.Enabled {
		spec.VMStorage = &vmv1beta1.VMStorage{}
		cfg, err := convertCommonConfig(values.VMStorage, values.Global)
		if err != nil {
			return nil, err
		}
		spec.VMStorage.CommonAppsParams = cfg.CommonAppsParams
		spec.VMStorage.PodMetadata = cfg.PodMetadata
		spec.VMStorage.ServiceSpec = cfg.ServiceSpec
		if cfg.Storage != nil {
			spec.VMStorage.Storage = &vmv1beta1.StorageSpec{
				VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
					Spec: *cfg.Storage,
				},
			}
		}
	}

	return spec, nil
}
func convertVLAgentSpec(values *VLAgentHelmValues) (*vmv1.VLAgentSpec, error) {
	spec := &vmv1.VLAgentSpec{}

	cfg, err := convertCommonConfig(ServerValues{
		Image:              values.Image,
		ReplicaCount:       values.ReplicaCount,
		ExtraArgs:          nil,
		ExtraEnvs:          values.Env,
		Resources:          values.Resources,
		NodeSelector:       values.NodeSelector,
		Tolerations:        values.Tolerations,
		Affinity:           values.Affinity,
		PodAnnotations:     values.PodAnnotations,
		Labels:             values.PodLabels,
		PodSecurityContext: values.PodSecurityContext,
		SecurityContext:    values.SecurityContext,
		PersistentVolume:   values.PersistentVolume,
	}, GlobalValues{})
	if err != nil {
		return nil, err
	}

	spec.Image = cfg.Image
	spec.ReplicaCount = cfg.ReplicaCount
	spec.ExtraArgs = values.ExtraArgs
	spec.ExtraEnvs = cfg.ExtraEnvs
	spec.Resources = cfg.Resources
	spec.NodeSelector = cfg.NodeSelector
	spec.Tolerations = cfg.Tolerations
	spec.Affinity = cfg.Affinity
	spec.PodMetadata = cfg.PodMetadata
	spec.ServiceSpec = cfg.ServiceSpec
	spec.RemoteWrite = convertVLAgentRemoteWrite(values.RemoteWrite)
	spec.SecurityContext = cfg.SecurityContext
	if cfg.Storage != nil {
		spec.Storage = &vmv1beta1.StorageSpec{
			VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
				Spec: *cfg.Storage,
			},
		}
	}

	if values.MaxDiskUsagePerURL != "" {
		if spec.ExtraArgs == nil {
			spec.ExtraArgs = make(map[string]string)
		}
		spec.ExtraArgs["remoteWrite.maxDiskUsagePerURL"] = values.MaxDiskUsagePerURL
	}

	spec.TopologySpreadConstraints = values.TopologySpreadConstraints
	spec.PriorityClassName = values.PriorityClassName
	spec.Volumes = values.ExtraVolumes
	spec.VolumeMounts = values.ExtraVolumeMounts

	return spec, nil
}
func convertVLClusterSpec(values *VLClusterHelmValues) (*vmv1.VLClusterSpec, error) {
	spec := &vmv1.VLClusterSpec{}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	if values.VLSelect.Enabled == nil || *values.VLSelect.Enabled {
		cfg, err := convertCommonConfig(values.VLSelect, values.Global)
		if err != nil {
			return nil, err
		}
		spec.VLSelect = &vmv1.VLSelect{}
		spec.VLSelect.CommonAppsParams = cfg.CommonAppsParams
		spec.VLSelect.PodMetadata = cfg.PodMetadata
		spec.VLSelect.ServiceSpec = cfg.ServiceSpec
	}

	if values.VLInsert.Enabled == nil || *values.VLInsert.Enabled {
		cfg, err := convertCommonConfig(values.VLInsert, values.Global)
		if err != nil {
			return nil, err
		}
		spec.VLInsert = &vmv1.VLInsert{}
		spec.VLInsert.CommonAppsParams = cfg.CommonAppsParams
		spec.VLInsert.PodMetadata = cfg.PodMetadata
		spec.VLInsert.ServiceSpec = cfg.ServiceSpec
	}

	if values.VLStorage.Enabled == nil || *values.VLStorage.Enabled {
		cfg, err := convertCommonConfig(values.VLStorage, values.Global)
		if err != nil {
			return nil, err
		}
		spec.VLStorage = &vmv1.VLStorage{}
		spec.VLStorage.CommonAppsParams = cfg.CommonAppsParams
		spec.VLStorage.PodMetadata = cfg.PodMetadata
		spec.VLStorage.ServiceSpec = cfg.ServiceSpec

		if cfg.Storage != nil {
			spec.VLStorage.Storage = &vmv1beta1.StorageSpec{
				VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
					Spec: *cfg.Storage,
				},
			}
		}
	}

	return spec, nil
}
func convertVLCollectorSpec(values *VLCollectorHelmValues) (*vmv1.VLAgentSpec, error) {
	spec := &vmv1.VLAgentSpec{}

	cfg, err := convertCommonConfig(ServerValues{
		Image:              values.Image,
		ExtraEnvs:          values.Env,
		Resources:          values.Resources,
		NodeSelector:       values.NodeSelector,
		Tolerations:        values.Tolerations,
		Affinity:           values.Affinity,
		PodAnnotations:     values.PodAnnotations,
		Labels:             values.PodLabels,
		PodSecurityContext: values.PodSecurityContext,
		SecurityContext:    values.SecurityContext,
	}, GlobalValues{})
	if err != nil {
		return nil, err
	}

	spec.Image = cfg.Image
	spec.ExtraEnvs = cfg.ExtraEnvs
	spec.Resources = cfg.Resources
	spec.NodeSelector = cfg.NodeSelector
	spec.Tolerations = cfg.Tolerations
	spec.Affinity = cfg.Affinity
	spec.PodMetadata = cfg.PodMetadata
	spec.ServiceSpec = cfg.ServiceSpec
	spec.RemoteWrite = convertVLAgentRemoteWrite(values.RemoteWrite)
	spec.SecurityContext = cfg.SecurityContext

	spec.TopologySpreadConstraints = values.TopologySpreadConstraints
	spec.PriorityClassName = values.PriorityClassName
	spec.Volumes = values.ExtraVolumes
	spec.VolumeMounts = values.ExtraVolumeMounts

	if len(values.ExtraArgs) > 0 {
		spec.ExtraArgs = values.ExtraArgs
	}

	spec.K8sCollector = vmv1.VLAgentK8sCollector{
		Enabled:                true,
		TimeFields:             values.Collector.TimeField,
		MsgFields:              values.Collector.MsgField,
		StreamFields:           values.Collector.StreamFields,
		ExcludeFilter:          values.Collector.ExcludeFilter,
		IncludePodLabels:       values.Collector.IncludePodLabels,
		IncludePodAnnotations:  values.Collector.IncludePodAnnotations,
		IncludeNodeLabels:      values.Collector.IncludeNodeLabels,
		IncludeNodeAnnotations: values.Collector.IncludeNodeAnnotations,
		ExtraFields:            values.Collector.ExtraFields,
		IgnoreFields:           values.Collector.IgnoreFields,
		TenantID:               values.Collector.TenantID,
		CheckpointsPath:        values.Collector.CheckpointsPath,
		LogsPath:               values.Collector.LogsPath,
	}

	return spec, nil
}
func convertVLogsSpec(values *VLogsHelmValues) (*vmv1beta1.VLogsSpec, error) {
	spec := &vmv1beta1.VLogsSpec{}
	cfg, err := convertCommonConfig(values.Server, values.Global)
	if err != nil {
		return nil, err
	}

	spec.CommonAppsParams = cfg.CommonAppsParams
	spec.PodMetadata = cfg.PodMetadata
	spec.ServiceSpec = cfg.ServiceSpec

	if values.Server.RetentionPeriod != nil {
		spec.RetentionPeriod = fmt.Sprint(values.Server.RetentionPeriod)
	}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	if cfg.Storage != nil {
		spec.Storage = cfg.Storage
	}

	return spec, nil
}
func convertVTSingleSpec(values *VTSingleHelmValues) (*vmv1.VTSingleSpec, error) {
	spec := &vmv1.VTSingleSpec{}
	cfg, err := convertCommonConfig(values.Server, values.Global)
	if err != nil {
		return nil, err
	}

	spec.CommonAppsParams = cfg.CommonAppsParams
	spec.PodMetadata = cfg.PodMetadata
	spec.ServiceSpec = cfg.ServiceSpec

	if values.Server.RetentionPeriod != nil {
		spec.RetentionPeriod = fmt.Sprint(values.Server.RetentionPeriod)
	}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	if cfg.Storage != nil {
		spec.Storage = cfg.Storage
	}

	return spec, nil
}
func convertVTClusterSpec(values *VTClusterHelmValues) (*vmv1.VTClusterSpec, error) {
	spec := &vmv1.VTClusterSpec{}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	if values.VTSelect.Enabled == nil || *values.VTSelect.Enabled {
		cfg, err := convertCommonConfig(values.VTSelect, values.Global)
		if err != nil {
			return nil, err
		}
		spec.Select = &vmv1.VTSelect{}
		spec.Select.CommonAppsParams = cfg.CommonAppsParams
		spec.Select.PodMetadata = cfg.PodMetadata
		spec.Select.ServiceSpec = cfg.ServiceSpec
	}

	if values.VTInsert.Enabled == nil || *values.VTInsert.Enabled {
		cfg, err := convertCommonConfig(values.VTInsert, values.Global)
		if err != nil {
			return nil, err
		}
		spec.Insert = &vmv1.VTInsert{}
		spec.Insert.CommonAppsParams = cfg.CommonAppsParams
		spec.Insert.PodMetadata = cfg.PodMetadata
		spec.Insert.ServiceSpec = cfg.ServiceSpec
	}

	if values.VTStorage.Enabled == nil || *values.VTStorage.Enabled {
		cfg, err := convertCommonConfig(values.VTStorage, values.Global)
		if err != nil {
			return nil, err
		}
		spec.Storage = &vmv1.VTStorage{}
		spec.Storage.CommonAppsParams = cfg.CommonAppsParams
		spec.Storage.PodMetadata = cfg.PodMetadata
		spec.Storage.ServiceSpec = cfg.ServiceSpec

		if cfg.Storage != nil {
			spec.Storage.Storage = &vmv1beta1.StorageSpec{
				VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
					Spec: *cfg.Storage,
				},
			}
		}
	}

	return spec, nil
}

func convertVMAuthSpec(values *VMAuthHelmValues) (*vmv1beta1.VMAuthSpec, error) {
	spec := &vmv1beta1.VMAuthSpec{}

	cfg, err := convertCommonConfig(values.ServerValues, values.Global)
	if err != nil {
		return nil, err
	}

	spec.CommonAppsParams = cfg.CommonAppsParams
	spec.PodMetadata = cfg.PodMetadata
	spec.ServiceSpec = cfg.ServiceSpec
	spec.ExtraEnvs = values.Env

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	if values.Config != nil {
		spec.UnauthorizedUserAccessSpec = values.Config.UnauthorizedUser
	}

	return spec, nil
}

// vmAuthUserSelectorLabels returns the labels shared by every VMUser ConvertVMAuthUsers
// generates for vmauthName and that VMAuth's own userSelector, keyed by vmauthName so
// multiple converted releases in one namespace don't pick up each other's users.
func vmAuthUserSelectorLabels(vmauthName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "helm-converter",
		"app.kubernetes.io/instance":   vmauthName,
	}
}

// ConvertVMAuthUsers converts a victoria-metrics-auth chart's config.users entries into
// standalone VMUser CRs. Returned separately from Convert since each user becomes its own CR.
func ConvertVMAuthUsers(vmauthName, namespace string, values *VMAuthHelmValues) ([]*vmv1beta1.VMUser, error) {
	if values.Config == nil || len(values.Config.Users) == 0 {
		return nil, nil
	}
	users := make([]*vmv1beta1.VMUser, 0, len(values.Config.Users))
	for i, u := range values.Config.Users {
		if u.Username == "" {
			return nil, fmt.Errorf("config.users[%d]: username is required", i)
		}
		targetRefs, err := convertVMAuthConfigUserTargetRefs(u)
		if err != nil {
			return nil, fmt.Errorf("config.users[%d] (username=%q): %w", i, u.Username, err)
		}
		spec := vmv1beta1.VMUserSpec{
			Username:            &u.Username,
			TargetRefs:          targetRefs,
			MetricLabels:        u.MetricLabels,
			VMUserConfigOptions: u.VMUserConfigOptions,
		}
		if u.Password != "" {
			spec.Password = &u.Password
		}
		if u.BearerToken != "" {
			spec.BearerToken = &u.BearerToken
		}
		users = append(users, &vmv1beta1.VMUser{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1beta1",
				Kind:       "VMUser",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      sanitizeK8sName(u.Username),
				Namespace: namespace,
				Labels:    vmAuthUserSelectorLabels(vmauthName),
			},
			Spec: spec,
		})
	}
	return users, nil
}

// convertVMAuthConfigUserTargetRefs builds TargetRefs from a config user's url_prefix/
// url_map, which the operator only exposes via TargetRefs rather than as direct spec fields.
func convertVMAuthConfigUserTargetRefs(u VMAuthConfigUser) ([]vmv1beta1.TargetRef, error) {
	if len(u.URLMap) > 0 {
		refs := make([]vmv1beta1.TargetRef, 0, len(u.URLMap))
		for i, m := range u.URLMap {
			static, err := staticRefFromURLPrefix(m.URLPrefix)
			if err != nil {
				return nil, fmt.Errorf("url_map[%d]: %w", i, err)
			}
			refs = append(refs, vmv1beta1.TargetRef{
				Paths:        m.SrcPaths,
				Hosts:        m.SrcHosts,
				Static:       static,
				URLMapCommon: m.URLMapCommon,
			})
		}
		return refs, nil
	}
	static, err := staticRefFromURLPrefix(u.URLPrefix)
	if err != nil {
		return nil, err
	}
	return []vmv1beta1.TargetRef{{Static: static}}, nil
}

// staticRefFromURLPrefix converts url_prefix into a StaticRef. StaticRef.URL/.URLs are
// mutually exclusive, so a single-entry prefix uses URL and a multi-entry one uses URLs.
func staticRefFromURLPrefix(prefix vmv1beta1.StringOrArray) (*vmv1beta1.StaticRef, error) {
	switch len(prefix) {
	case 0:
		return nil, fmt.Errorf("url_prefix is required when url_map is not set")
	case 1:
		return &vmv1beta1.StaticRef{URL: prefix[0]}, nil
	default:
		return &vmv1beta1.StaticRef{URLs: prefix}, nil
	}
}

// sanitizeK8sName converts an arbitrary vmauth username into a valid Kubernetes resource name
// (lowercase alphanumeric and '-', not starting/ending with '-').
func sanitizeK8sName(s string) string {
	lowered := strings.ToLower(s)
	var b strings.Builder
	prevDash := false
	for _, r := range lowered {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	maxLen := validation.DNS1123SubdomainMaxLength
	if len(result) > maxLen {
		result = result[:maxLen]
		// Re-trim in case truncation split on a hyphen
		result = strings.TrimSuffix(result, "-")
	}

	return result
}
