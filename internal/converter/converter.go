package converter

import (
	"fmt"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VMSingleHelmValues represents values from VictoriaMetrics single helm chart
type VMSingleHelmValues struct {
	Global         GlobalValues    `yaml:"global,omitempty"`
	Server         ServerValues    `yaml:"server"`
	ServiceAccount *ServiceAccount `yaml:"serviceAccount,omitempty"`
}

// VMClusterHelmValues represents values from VictoriaMetrics cluster helm chart
type VMClusterHelmValues struct {
	Global         GlobalValues    `yaml:"global,omitempty"`
	VMSelect       ServerValues    `yaml:"vmselect"`
	VMInsert       ServerValues    `yaml:"vminsert"`
	VMStorage      ServerValues    `yaml:"vmstorage"`
	ServiceAccount *ServiceAccount `yaml:"serviceAccount,omitempty"`
}

// VLogsHelmValues represents values from VictoriaLogs single helm chart
type VLogsHelmValues struct {
	Global         GlobalValues    `yaml:"global,omitempty"`
	Server         ServerValues    `yaml:"server"`
	ServiceAccount *ServiceAccount `yaml:"serviceAccount,omitempty"`
}

// VTSingleHelmValues represents values from VictoriaTraces single helm chart
type VTSingleHelmValues struct {
	Global         GlobalValues    `yaml:"global,omitempty"`
	Server         ServerValues    `yaml:"server"`
	ServiceAccount *ServiceAccount `yaml:"serviceAccount,omitempty"`
}

// VTClusterHelmValues represents values from VictoriaTraces cluster helm chart
type VTClusterHelmValues struct {
	Global         GlobalValues    `yaml:"global,omitempty"`
	VTSelect       ServerValues    `yaml:"vtselect"`
	VTInsert       ServerValues    `yaml:"vtinsert"`
	VTStorage      ServerValues    `yaml:"vtstorage"`
	ServiceAccount *ServiceAccount `yaml:"serviceAccount,omitempty"`
}

// VLClusterHelmValues represents values from VictoriaLogs cluster helm chart
type VLClusterHelmValues struct {
	Global         GlobalValues    `yaml:"global,omitempty"`
	VLSelect       ServerValues    `yaml:"vlselect"`
	VLInsert       ServerValues    `yaml:"vlinsert"`
	VLStorage      ServerValues    `yaml:"vlstorage"`
	ServiceAccount *ServiceAccount `yaml:"serviceAccount,omitempty"`
}

// VMAgentHelmValues represents values from VictoriaMetrics agent helm chart
type VMAgentHelmValues struct {
	Global             GlobalValues                        `yaml:"global,omitempty"`
	ReplicaCount       *int32                              `yaml:"replicaCount,omitempty"`
	Image              ImageValues                         `yaml:"image"`
	ImagePullSecrets   []corev1.LocalObjectReference       `yaml:"imagePullSecrets,omitempty"`
	ExtraArgs          map[string]interface{}              `yaml:"extraArgs,omitempty"`
	ExtraEnvs          []corev1.EnvVar                     `yaml:"env,omitempty"`
	Resources          *corev1.ResourceRequirements        `yaml:"resources,omitempty"`
	NodeSelector       map[string]string                   `yaml:"nodeSelector,omitempty"`
	Tolerations        []corev1.Toleration                 `yaml:"tolerations,omitempty"`
	Affinity           *corev1.Affinity                    `yaml:"affinity,omitempty"`
	PodAnnotations     map[string]string                   `yaml:"podAnnotations,omitempty"`
	Labels             map[string]string                   `yaml:"labels,omitempty"`
	PodSecurityContext *corev1.PodSecurityContext          `yaml:"podSecurityContext,omitempty"`
	SecurityContext    *vmv1beta1.ContainerSecurityContext `yaml:"securityContext,omitempty"`
	RemoteWrite        []vmv1beta1.VMAgentRemoteWriteSpec  `yaml:"remoteWrite,omitempty"`
	ServiceAccount     *ServiceAccount                     `yaml:"serviceAccount,omitempty"`
}

// VMAlertHelmValues represents values from VictoriaMetrics alert helm chart
type VMAlertHelmValues struct {
	Global         GlobalValues        `yaml:"global,omitempty"`
	Server         VMAlertServerValues `yaml:"server"`
	ServiceAccount *ServiceAccount     `yaml:"serviceAccount,omitempty"`
}

// VMAnomalyHelmValues represents values from VictoriaMetrics anomaly helm chart
type VMAnomalyHelmValues struct {
	Global             GlobalValues                        `yaml:"global,omitempty"`
	ReplicaCount       *int32                              `yaml:"replicaCount,omitempty"`
	Image              ImageValues                         `yaml:"image"`
	ImagePullSecrets   []corev1.LocalObjectReference       `yaml:"imagePullSecrets,omitempty"`
	ExtraArgs          map[string]interface{}              `yaml:"extraArgs,omitempty"`
	ExtraEnvs          []corev1.EnvVar                     `yaml:"env,omitempty"`
	Resources          *corev1.ResourceRequirements        `yaml:"resources,omitempty"`
	NodeSelector       map[string]string                   `yaml:"nodeSelector,omitempty"`
	Tolerations        []corev1.Toleration                 `yaml:"tolerations,omitempty"`
	Affinity           *corev1.Affinity                    `yaml:"affinity,omitempty"`
	PodAnnotations     map[string]string                   `yaml:"podAnnotations,omitempty"`
	Labels             map[string]string                   `yaml:"labels,omitempty"`
	PodSecurityContext *corev1.PodSecurityContext          `yaml:"podSecurityContext,omitempty"`
	SecurityContext    *vmv1beta1.ContainerSecurityContext `yaml:"securityContext,omitempty"`
	Reader             *VMAnomalyReaderValues              `yaml:"reader,omitempty"`
	Writer             *VMAnomalyWriterValues              `yaml:"writer,omitempty"`
	ServiceAccount     *ServiceAccount                     `yaml:"serviceAccount,omitempty"`
}

type VMAnomalyReaderValues struct {
	DatasourceURL  string `yaml:"datasourceURL,omitempty"`
	SamplingPeriod string `yaml:"samplingPeriod,omitempty"`
}

type VMAnomalyWriterValues struct {
	DatasourceURL string `yaml:"datasourceURL,omitempty"`
}

type VMAlertServerValues struct {
	Image              ImageValues                         `yaml:"image"`
	ImagePullSecrets   []corev1.LocalObjectReference       `yaml:"imagePullSecrets,omitempty"`
	ReplicaCount       *int32                              `yaml:"replicaCount,omitempty"`
	ExtraArgs          map[string]interface{}              `yaml:"extraArgs,omitempty"`
	ExtraEnvs          []corev1.EnvVar                     `yaml:"env,omitempty"`
	Resources          *corev1.ResourceRequirements        `yaml:"resources,omitempty"`
	NodeSelector       map[string]string                   `yaml:"nodeSelector,omitempty"`
	Tolerations        []corev1.Toleration                 `yaml:"tolerations,omitempty"`
	Affinity           *corev1.Affinity                    `yaml:"affinity,omitempty"`
	PodAnnotations     map[string]string                   `yaml:"podAnnotations,omitempty"`
	Labels             map[string]string                   `yaml:"labels,omitempty"`
	PodSecurityContext *corev1.PodSecurityContext          `yaml:"podSecurityContext,omitempty"`
	SecurityContext    *vmv1beta1.ContainerSecurityContext `yaml:"securityContext,omitempty"`
	Notifier           *vmv1beta1.VMAlertNotifierSpec      `yaml:"notifier,omitempty"`
	Notifiers          []vmv1beta1.VMAlertNotifierSpec     `yaml:"notifiers,omitempty"`
	RemoteWrite        *vmv1beta1.VMAlertRemoteWriteSpec   `yaml:"remoteWrite,omitempty"`
	RemoteRead         *vmv1beta1.VMAlertRemoteReadSpec    `yaml:"remoteRead,omitempty"`
	Datasource         vmv1beta1.VMAlertDatasourceSpec     `yaml:"datasource,omitempty"`
}

type GlobalValues struct {
	ImagePullSecrets []corev1.LocalObjectReference `yaml:"imagePullSecrets,omitempty"`
	Image            ImageValues                   `yaml:"image,omitempty"`
}

type ServiceAccount struct {
	Name string `yaml:"name,omitempty"`
}

// VLAgentHelmValues represents values from VictoriaLogs agent helm chart
type VLAgentHelmValues struct {
	Image                     ImageValues                         `yaml:"image"`
	ReplicaCount              *int32                              `yaml:"replicaCount,omitempty"`
	Annotations               map[string]string                   `yaml:"annotations,omitempty"`
	PodAnnotations            map[string]string                   `yaml:"podAnnotations,omitempty"`
	PodLabels                 map[string]string                   `yaml:"podLabels,omitempty"`
	NodeSelector              map[string]string                   `yaml:"nodeSelector,omitempty"`
	Tolerations               []corev1.Toleration                 `yaml:"tolerations,omitempty"`
	Affinity                  *corev1.Affinity                    `yaml:"affinity,omitempty"`
	TopologySpreadConstraints []corev1.TopologySpreadConstraint   `yaml:"topologySpreadConstraints,omitempty"`
	SecurityContext           *vmv1beta1.ContainerSecurityContext `yaml:"securityContext,omitempty"`
	PodSecurityContext        *corev1.PodSecurityContext          `yaml:"podSecurityContext,omitempty"`
	PriorityClassName         string                              `yaml:"priorityClassName,omitempty"`
	ExtraArgs                 map[string]string                   `yaml:"extraArgs,omitempty"`
	Env                       []corev1.EnvVar                     `yaml:"env,omitempty"`
	ExtraVolumes              []corev1.Volume                     `yaml:"extraVolumes,omitempty"`
	ExtraVolumeMounts         []corev1.VolumeMount                `yaml:"extraVolumeMounts,omitempty"`
	Resources                 *corev1.ResourceRequirements        `yaml:"resources,omitempty"`
	RemoteWrite               []vmv1.VLAgentRemoteWriteSpec       `yaml:"remoteWrite"`
	MaxDiskUsagePerURL        string                              `yaml:"maxDiskUsagePerURL,omitempty"`
	PersistentVolume          *PersistentVolumeValues             `yaml:"persistentVolume,omitempty"`
}

type VLCollectorHelmValues struct {
	Image                     ImageValues                         `yaml:"image"`
	Annotations               map[string]string                   `yaml:"annotations,omitempty"`
	PodAnnotations            map[string]string                   `yaml:"podAnnotations,omitempty"`
	PodLabels                 map[string]string                   `yaml:"podLabels,omitempty"`
	NodeSelector              map[string]string                   `yaml:"nodeSelector,omitempty"`
	Tolerations               []corev1.Toleration                 `yaml:"tolerations,omitempty"`
	Affinity                  *corev1.Affinity                    `yaml:"affinity,omitempty"`
	TopologySpreadConstraints []corev1.TopologySpreadConstraint   `yaml:"topologySpreadConstraints,omitempty"`
	SecurityContext           *vmv1beta1.ContainerSecurityContext `yaml:"securityContext,omitempty"`
	PodSecurityContext        *corev1.PodSecurityContext          `yaml:"podSecurityContext,omitempty"`
	PriorityClassName         string                              `yaml:"priorityClassName,omitempty"`
	ExtraArgs                 map[string]string                   `yaml:"extraArgs,omitempty"`
	Env                       []corev1.EnvVar                     `yaml:"env,omitempty"`
	ExtraVolumes              []corev1.Volume                     `yaml:"extraVolumes,omitempty"`
	ExtraVolumeMounts         []corev1.VolumeMount                `yaml:"extraVolumeMounts,omitempty"`
	Resources                 *corev1.ResourceRequirements        `yaml:"resources,omitempty"`
	RemoteWrite               []vmv1.VLAgentRemoteWriteSpec       `yaml:"remoteWrite"`
	Collector                 VLCollectorSettings                 `yaml:"collector"`
}

type VLCollectorSettings struct {
	TimeField              []string `yaml:"timeField,omitempty"`
	MsgField               []string `yaml:"msgField,omitempty"`
	StreamFields           []string `yaml:"streamFields,omitempty"`
	ExcludeFilter          string   `yaml:"excludeFilter,omitempty"`
	IncludePodLabels       *bool    `yaml:"includePodLabels,omitempty"`
	IncludePodAnnotations  *bool    `yaml:"includePodAnnotations,omitempty"`
	IncludeNodeLabels      *bool    `yaml:"includeNodeLabels,omitempty"`
	IncludeNodeAnnotations *bool    `yaml:"includeNodeAnnotations,omitempty"`
	ExtraFields            string   `yaml:"extraFields,omitempty"`
	IgnoreFields           []string `yaml:"ignoreFields,omitempty"`
	TenantID               string   `yaml:"tenantID,omitempty"`
	CheckpointsPath        *string  `yaml:"checkpointsPath,omitempty"`
	LogsPath               string   `yaml:"logsPath,omitempty"`
}

type ServerValues struct {
	Enabled            *bool                               `yaml:"enabled,omitempty"`
	Name               string                              `yaml:"name,omitempty"`
	Image              ImageValues                         `yaml:"image"`
	ImagePullSecrets   []corev1.LocalObjectReference       `yaml:"imagePullSecrets,omitempty"`
	ReplicaCount       *int32                              `yaml:"replicaCount,omitempty"`
	RetentionPeriod    interface{}                         `yaml:"retentionPeriod,omitempty"`
	ExtraArgs          map[string]interface{}              `yaml:"extraArgs,omitempty"`
	ExtraEnvs          []corev1.EnvVar                     `yaml:"extraEnvs,omitempty"`
	Resources          *corev1.ResourceRequirements        `yaml:"resources,omitempty"`
	NodeSelector       map[string]string                   `yaml:"nodeSelector,omitempty"`
	Tolerations        []corev1.Toleration                 `yaml:"tolerations,omitempty"`
	Affinity           *corev1.Affinity                    `yaml:"affinity,omitempty"`
	PodAnnotations     map[string]string                   `yaml:"podAnnotations,omitempty"`
	Labels             map[string]string                   `yaml:"labels,omitempty"`
	PodSecurityContext *corev1.PodSecurityContext          `yaml:"podSecurityContext,omitempty"`
	SecurityContext    *vmv1beta1.ContainerSecurityContext `yaml:"securityContext,omitempty"`
	PersistentVolume   *PersistentVolumeValues             `yaml:"persistentVolume,omitempty"`
}

type ImageValues struct {
	Registry   string `yaml:"registry,omitempty"`
	Repository string `yaml:"repository"`
	Tag        string `yaml:"tag"`
	Variant    string `yaml:"variant,omitempty"`
	PullPolicy string `yaml:"pullPolicy,omitempty"`
}

type PersistentVolumeValues struct {
	Enabled      bool              `yaml:"enabled"`
	StorageClass string            `yaml:"storageClass,omitempty"`
	Size         string            `yaml:"size,omitempty"`
	MountPath    string            `yaml:"mountPath,omitempty"`
	Annotations  map[string]string `yaml:"annotations,omitempty"`
}

// UnmarshalValues unmarshals yaml data into the specified type
func UnmarshalValues(data []byte, chart string) (any, error) {
	switch chart {
	case "victoria-metrics-single":
		var values VMSingleHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-metrics-cluster":
		var values VMClusterHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-metrics-agent":
		var values VMAgentHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-metrics-alert":
		var values VMAlertHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-metrics-anomaly":
		var values VMAnomalyHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-logs-cluster":
		var values VLClusterHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-logs-agent":
		var values VLAgentHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-logs-collector":
		var values VLCollectorHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-logs-single":
		var values VLogsHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-traces-single":
		var values VTSingleHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	case "victoria-traces-cluster":
		var values VTClusterHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	default:
		return nil, fmt.Errorf("unsupported chart: %s", chart)
	}
}

// Convert converts helm values to corresponding CRD
func Convert(name, namespace string, values any) any {
	var cr any

	switch v := values.(type) {
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
		single.Spec = *convertVMSingleSpec(v)
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
		cluster.Spec = *convertVMClusterSpec(v)
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
		agent.Spec = *convertVMAgentSpec(v)
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
		alert.Spec = *convertVMAlertSpec(v)
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
		anomaly.Spec = *convertVMAnomalySpec(v)
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
		agent.Spec = *convertVLAgentSpec(v)
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
		cluster.Spec = *convertVLClusterSpec(v)
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
		agent.Spec = *convertVLCollectorSpec(v)
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
		vlogs.Spec = *convertVLogsSpec(v)
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
		vtsingle.Spec = *convertVTSingleSpec(v)
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
		cluster.Spec = *convertVTClusterSpec(v)
		cr = cluster

	default:
		panic(fmt.Sprintf("unsupported values type: %T", values))
	}

	return cr
}

type commonConfig struct {
	vmv1beta1.CommonAppsParams
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata
	Storage     *corev1.PersistentVolumeClaimSpec
}

func convertCommonConfig(values ServerValues, global GlobalValues) commonConfig {
	var cfg commonConfig

	cfg.ReplicaCount = values.ReplicaCount

	cfg.Image = convertImage(values.Image, global.Image)

	// ExtraArgs
	if len(values.ExtraArgs) > 0 {
		cfg.ExtraArgs = make(map[string]string)
		for k, v := range values.ExtraArgs {
			cfg.ExtraArgs[k] = fmt.Sprint(v)
		}
	}

	// ExtraEnvs
	if len(values.ExtraEnvs) > 0 {
		cfg.ExtraEnvs = values.ExtraEnvs
	}

	// Persistent Volume
	cfg.Storage = convertPersistentVolume(values.PersistentVolume)

	// Common Apps Params
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

	// Security context
	if values.PodSecurityContext != nil || values.SecurityContext != nil {
		cfg.SecurityContext = &vmv1beta1.SecurityContext{
			PodSecurityContext:       values.PodSecurityContext,
			ContainerSecurityContext: values.SecurityContext,
		}
	}

	if len(values.ImagePullSecrets) > 0 {
		cfg.ImagePullSecrets = values.ImagePullSecrets
	} else if len(global.ImagePullSecrets) > 0 {
		cfg.ImagePullSecrets = global.ImagePullSecrets
	}

	// Annotations and Labels
	if len(values.PodAnnotations) > 0 || len(values.Labels) > 0 {
		cfg.PodMetadata = &vmv1beta1.EmbeddedObjectMetadata{
			Annotations: values.PodAnnotations,
			Labels:      values.Labels,
		}
	}

	return cfg
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

func convertPersistentVolume(pv *PersistentVolumeValues) *corev1.PersistentVolumeClaimSpec {
	if pv == nil || !pv.Enabled {
		return nil
	}

	storage := &corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
	}
	if pv.StorageClass != "" {
		if pv.StorageClass == "-" {
			storageClass := ""
			storage.StorageClassName = &storageClass
		} else {
			storage.StorageClassName = &pv.StorageClass
		}
	}
	if pv.Size != "" {
		q, err := resource.ParseQuantity(pv.Size)
		if err == nil {
			storage.Resources = corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: q,
				},
			}
		}
	}
	return storage
}

func convertVMSingleSpec(values *VMSingleHelmValues) *vmv1beta1.VMSingleSpec {
	spec := &vmv1beta1.VMSingleSpec{}
	cfg := convertCommonConfig(values.Server, values.Global)

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
	spec.Storage = cfg.Storage

	if values.Server.RetentionPeriod != nil {
		spec.RetentionPeriod = fmt.Sprint(values.Server.RetentionPeriod)
	}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	return spec
}

func convertVMAnomalySpec(values *VMAnomalyHelmValues) *vmv1.VMAnomalySpec {
	spec := &vmv1.VMAnomalySpec{}

	cfg := convertCommonConfig(ServerValues{
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

	return spec
}

func convertVMAlertSpec(values *VMAlertHelmValues) *vmv1beta1.VMAlertSpec {
	spec := &vmv1beta1.VMAlertSpec{}

	cfg := convertCommonConfig(ServerValues{
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
	spec.Notifier = values.Server.Notifier
	spec.Notifiers = values.Server.Notifiers
	spec.RemoteWrite = values.Server.RemoteWrite
	spec.RemoteRead = values.Server.RemoteRead
	spec.Datasource = values.Server.Datasource

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	return spec
}

func convertVMAgentSpec(values *VMAgentHelmValues) *vmv1beta1.VMAgentSpec {
	spec := &vmv1beta1.VMAgentSpec{}

	cfg := convertCommonConfig(ServerValues{
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
	spec.RemoteWrite = values.RemoteWrite

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	return spec
}

func convertVMClusterSpec(values *VMClusterHelmValues) *vmv1beta1.VMClusterSpec {
	spec := &vmv1beta1.VMClusterSpec{}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	if values.VMStorage.RetentionPeriod != nil {
		spec.RetentionPeriod = fmt.Sprint(values.VMStorage.RetentionPeriod)
	}

	// VMSelect
	if values.VMSelect.Enabled == nil || *values.VMSelect.Enabled {
		spec.VMSelect = &vmv1beta1.VMSelect{}
		cfg := convertCommonConfig(values.VMSelect, values.Global)
		spec.VMSelect.CommonAppsParams = cfg.CommonAppsParams
		spec.VMSelect.PodMetadata = cfg.PodMetadata
	}

	// VMInsert
	if values.VMInsert.Enabled == nil || *values.VMInsert.Enabled {
		spec.VMInsert = &vmv1beta1.VMInsert{}
		cfg := convertCommonConfig(values.VMInsert, values.Global)
		spec.VMInsert.CommonAppsParams = cfg.CommonAppsParams
		spec.VMInsert.PodMetadata = cfg.PodMetadata
	}

	// VMStorage
	if values.VMStorage.Enabled == nil || *values.VMStorage.Enabled {
		spec.VMStorage = &vmv1beta1.VMStorage{}
		cfg := convertCommonConfig(values.VMStorage, values.Global)
		spec.VMStorage.CommonAppsParams = cfg.CommonAppsParams
		spec.VMStorage.PodMetadata = cfg.PodMetadata
		if cfg.Storage != nil {
			spec.VMStorage.Storage = &vmv1beta1.StorageSpec{
				VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
					Spec: *cfg.Storage,
				},
			}
		}
	}

	return spec
}
func convertVLAgentSpec(values *VLAgentHelmValues) *vmv1.VLAgentSpec {
	spec := &vmv1.VLAgentSpec{}

	cfg := convertCommonConfig(ServerValues{
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

	spec.Image = cfg.Image
	spec.ReplicaCount = cfg.ReplicaCount
	spec.ExtraArgs = values.ExtraArgs
	spec.ExtraEnvs = cfg.ExtraEnvs
	spec.Resources = cfg.Resources
	spec.NodeSelector = cfg.NodeSelector
	spec.Tolerations = cfg.Tolerations
	spec.Affinity = cfg.Affinity
	spec.PodMetadata = cfg.PodMetadata
	spec.RemoteWrite = values.RemoteWrite
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

	return spec
}
func convertVLClusterSpec(values *VLClusterHelmValues) *vmv1.VLClusterSpec {
	spec := &vmv1.VLClusterSpec{}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	// VLSelect
	if values.VLSelect.Enabled == nil || *values.VLSelect.Enabled {
		cfg := convertCommonConfig(values.VLSelect, values.Global)
		spec.VLSelect = &vmv1.VLSelect{}
		spec.VLSelect.CommonAppsParams = cfg.CommonAppsParams
		spec.VLSelect.PodMetadata = cfg.PodMetadata
	}

	// VLInsert
	if values.VLInsert.Enabled == nil || *values.VLInsert.Enabled {
		cfg := convertCommonConfig(values.VLInsert, values.Global)
		spec.VLInsert = &vmv1.VLInsert{}
		spec.VLInsert.CommonAppsParams = cfg.CommonAppsParams
		spec.VLInsert.PodMetadata = cfg.PodMetadata
	}

	// VLStorage
	if values.VLStorage.Enabled == nil || *values.VLStorage.Enabled {
		cfg := convertCommonConfig(values.VLStorage, values.Global)
		spec.VLStorage = &vmv1.VLStorage{}
		spec.VLStorage.CommonAppsParams = cfg.CommonAppsParams
		spec.VLStorage.PodMetadata = cfg.PodMetadata

		if cfg.Storage != nil {
			spec.VLStorage.Storage = &vmv1beta1.StorageSpec{
				VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
					Spec: *cfg.Storage,
				},
			}
		}
	}

	return spec
}
func convertVLCollectorSpec(values *VLCollectorHelmValues) *vmv1.VLAgentSpec {
	spec := &vmv1.VLAgentSpec{}

	extraArgs := make(map[string]interface{})
	for k, v := range values.ExtraArgs {
		extraArgs[k] = v
	}

	cfg := convertCommonConfig(ServerValues{
		Image:              values.Image,
		ExtraArgs:          extraArgs,
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

	spec.Image = cfg.Image
	spec.ExtraEnvs = cfg.ExtraEnvs
	spec.Resources = cfg.Resources
	spec.NodeSelector = cfg.NodeSelector
	spec.Tolerations = cfg.Tolerations
	spec.Affinity = cfg.Affinity
	spec.PodMetadata = cfg.PodMetadata
	spec.RemoteWrite = values.RemoteWrite
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

	return spec
}
func convertVLogsSpec(values *VLogsHelmValues) *vmv1beta1.VLogsSpec {
	spec := &vmv1beta1.VLogsSpec{}
	cfg := convertCommonConfig(values.Server, values.Global)

	spec.CommonAppsParams = cfg.CommonAppsParams
	spec.PodMetadata = cfg.PodMetadata

	if values.Server.RetentionPeriod != nil {
		spec.RetentionPeriod = fmt.Sprint(values.Server.RetentionPeriod)
	}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	if cfg.Storage != nil {
		spec.Storage = cfg.Storage
	}

	return spec
}
func convertVTSingleSpec(values *VTSingleHelmValues) *vmv1.VTSingleSpec {
	spec := &vmv1.VTSingleSpec{}
	cfg := convertCommonConfig(values.Server, values.Global)

	spec.CommonAppsParams = cfg.CommonAppsParams
	spec.PodMetadata = cfg.PodMetadata

	if values.Server.RetentionPeriod != nil {
		spec.RetentionPeriod = fmt.Sprint(values.Server.RetentionPeriod)
	}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	if cfg.Storage != nil {
		spec.Storage = cfg.Storage
	}

	return spec
}
func convertVTClusterSpec(values *VTClusterHelmValues) *vmv1.VTClusterSpec {
	spec := &vmv1.VTClusterSpec{}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	// VTSelect
	if values.VTSelect.Enabled == nil || *values.VTSelect.Enabled {
		cfg := convertCommonConfig(values.VTSelect, values.Global)
		spec.Select = &vmv1.VTSelect{}
		spec.Select.CommonAppsParams = cfg.CommonAppsParams
		spec.Select.PodMetadata = cfg.PodMetadata
	}

	// VTInsert
	if values.VTInsert.Enabled == nil || *values.VTInsert.Enabled {
		cfg := convertCommonConfig(values.VTInsert, values.Global)
		spec.Insert = &vmv1.VTInsert{}
		spec.Insert.CommonAppsParams = cfg.CommonAppsParams
		spec.Insert.PodMetadata = cfg.PodMetadata
	}

	// VTStorage
	if values.VTStorage.Enabled == nil || *values.VTStorage.Enabled {
		cfg := convertCommonConfig(values.VTStorage, values.Global)
		spec.Storage = &vmv1.VTStorage{}
		spec.Storage.CommonAppsParams = cfg.CommonAppsParams
		spec.Storage.PodMetadata = cfg.PodMetadata

		if cfg.Storage != nil {
			spec.Storage.Storage = &vmv1beta1.StorageSpec{
				VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
					Spec: *cfg.Storage,
				},
			}
		}
	}

	return spec
}
