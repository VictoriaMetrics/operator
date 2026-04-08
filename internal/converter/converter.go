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

	default:
		panic(fmt.Sprintf("unsupported values type: %T", values))
	}

	return cr
}

type commonConfig struct {
	ReplicaCount     *int32
	Image            vmv1beta1.Image
	ExtraArgs        map[string]string
	ExtraEnvs        []corev1.EnvVar
	Resources        corev1.ResourceRequirements
	NodeSelector     map[string]string
	Tolerations      []corev1.Toleration
	Affinity         *corev1.Affinity
	SecurityContext  *vmv1beta1.SecurityContext
	ImagePullSecrets []corev1.LocalObjectReference
	PodMetadata      *vmv1beta1.EmbeddedObjectMetadata
	Storage          *corev1.PersistentVolumeClaimSpec
}

func convertCommonConfig(values ServerValues, global GlobalValues) commonConfig {
	var cfg commonConfig

	cfg.ReplicaCount = values.ReplicaCount

	// Image
	repo := values.Image.Repository
	registry := values.Image.Registry
	if registry == "" && global.Image.Registry != "" {
		registry = global.Image.Registry
	}
	if registry != "" {
		repo = fmt.Sprintf("%s/%s", registry, repo)
	}
	cfg.Image.Repository = repo

	tag := values.Image.Tag
	if values.Image.Variant != "" {
		tag = fmt.Sprintf("%s-%s", tag, values.Image.Variant)
	}
	cfg.Image.Tag = tag
	if values.Image.PullPolicy != "" {
		cfg.Image.PullPolicy = corev1.PullPolicy(values.Image.PullPolicy)
	}

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
	if values.PersistentVolume != nil && values.PersistentVolume.Enabled {
		cfg.Storage = &corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		}
		if values.PersistentVolume.StorageClass != "" {
			if values.PersistentVolume.StorageClass == "-" {
				storageClass := ""
				cfg.Storage.StorageClassName = &storageClass
			} else {
				cfg.Storage.StorageClassName = &values.PersistentVolume.StorageClass
			}
		}
		if values.PersistentVolume.Size != "" {
			q, err := resource.ParseQuantity(values.PersistentVolume.Size)
			if err == nil {
				cfg.Storage.Resources = corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: q,
					},
				}
			}
		}
	}

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
		spec.VMSelect.ReplicaCount = cfg.ReplicaCount
		spec.VMSelect.Image = cfg.Image
		spec.VMSelect.ExtraArgs = cfg.ExtraArgs
		spec.VMSelect.ExtraEnvs = cfg.ExtraEnvs
		spec.VMSelect.Resources = cfg.Resources
		spec.VMSelect.NodeSelector = cfg.NodeSelector
		spec.VMSelect.Tolerations = cfg.Tolerations
		spec.VMSelect.Affinity = cfg.Affinity
		spec.VMSelect.SecurityContext = cfg.SecurityContext
		spec.VMSelect.ImagePullSecrets = cfg.ImagePullSecrets
		spec.VMSelect.PodMetadata = cfg.PodMetadata
	}

	// VMInsert
	if values.VMInsert.Enabled == nil || *values.VMInsert.Enabled {
		spec.VMInsert = &vmv1beta1.VMInsert{}
		cfg := convertCommonConfig(values.VMInsert, values.Global)
		spec.VMInsert.ReplicaCount = cfg.ReplicaCount
		spec.VMInsert.Image = cfg.Image
		spec.VMInsert.ExtraArgs = cfg.ExtraArgs
		spec.VMInsert.ExtraEnvs = cfg.ExtraEnvs
		spec.VMInsert.Resources = cfg.Resources
		spec.VMInsert.NodeSelector = cfg.NodeSelector
		spec.VMInsert.Tolerations = cfg.Tolerations
		spec.VMInsert.Affinity = cfg.Affinity
		spec.VMInsert.SecurityContext = cfg.SecurityContext
		spec.VMInsert.ImagePullSecrets = cfg.ImagePullSecrets
		spec.VMInsert.PodMetadata = cfg.PodMetadata
	}

	// VMStorage
	if values.VMStorage.Enabled == nil || *values.VMStorage.Enabled {
		spec.VMStorage = &vmv1beta1.VMStorage{}
		cfg := convertCommonConfig(values.VMStorage, values.Global)
		spec.VMStorage.ReplicaCount = cfg.ReplicaCount
		spec.VMStorage.Image = cfg.Image
		spec.VMStorage.ExtraArgs = cfg.ExtraArgs
		spec.VMStorage.ExtraEnvs = cfg.ExtraEnvs
		spec.VMStorage.Resources = cfg.Resources
		spec.VMStorage.NodeSelector = cfg.NodeSelector
		spec.VMStorage.Tolerations = cfg.Tolerations
		spec.VMStorage.Affinity = cfg.Affinity
		spec.VMStorage.SecurityContext = cfg.SecurityContext
		spec.VMStorage.ImagePullSecrets = cfg.ImagePullSecrets
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
		spec.VLSelect.ReplicaCount = cfg.ReplicaCount
		spec.VLSelect.Image = cfg.Image
		spec.VLSelect.ExtraArgs = cfg.ExtraArgs
		spec.VLSelect.ExtraEnvs = cfg.ExtraEnvs
		spec.VLSelect.Resources = cfg.Resources
		spec.VLSelect.NodeSelector = cfg.NodeSelector
		spec.VLSelect.Tolerations = cfg.Tolerations
		spec.VLSelect.Affinity = cfg.Affinity
		spec.VLSelect.SecurityContext = cfg.SecurityContext
		spec.VLSelect.ImagePullSecrets = cfg.ImagePullSecrets
		spec.VLSelect.PodMetadata = cfg.PodMetadata
	}

	// VLInsert
	if values.VLInsert.Enabled == nil || *values.VLInsert.Enabled {
		cfg := convertCommonConfig(values.VLInsert, values.Global)
		spec.VLInsert = &vmv1.VLInsert{}
		spec.VLInsert.ReplicaCount = cfg.ReplicaCount
		spec.VLInsert.Image = cfg.Image
		spec.VLInsert.ExtraArgs = cfg.ExtraArgs
		spec.VLInsert.ExtraEnvs = cfg.ExtraEnvs
		spec.VLInsert.Resources = cfg.Resources
		spec.VLInsert.NodeSelector = cfg.NodeSelector
		spec.VLInsert.Tolerations = cfg.Tolerations
		spec.VLInsert.Affinity = cfg.Affinity
		spec.VLInsert.SecurityContext = cfg.SecurityContext
		spec.VLInsert.ImagePullSecrets = cfg.ImagePullSecrets
		spec.VLInsert.PodMetadata = cfg.PodMetadata
	}

	// VLStorage
	if values.VLStorage.Enabled == nil || *values.VLStorage.Enabled {
		cfg := convertCommonConfig(values.VLStorage, values.Global)
		spec.VLStorage = &vmv1.VLStorage{}
		spec.VLStorage.ReplicaCount = cfg.ReplicaCount
		spec.VLStorage.Image = cfg.Image
		spec.VLStorage.ExtraArgs = cfg.ExtraArgs
		spec.VLStorage.ExtraEnvs = cfg.ExtraEnvs
		spec.VLStorage.Resources = cfg.Resources
		spec.VLStorage.NodeSelector = cfg.NodeSelector
		spec.VLStorage.Tolerations = cfg.Tolerations
		spec.VLStorage.Affinity = cfg.Affinity
		spec.VLStorage.SecurityContext = cfg.SecurityContext
		spec.VLStorage.ImagePullSecrets = cfg.ImagePullSecrets
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
