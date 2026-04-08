package converter

import (
	"fmt"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

type GlobalValues struct {
	ImagePullSecrets []corev1.LocalObjectReference `yaml:"imagePullSecrets,omitempty"`
	Image            ImageValues                   `yaml:"image,omitempty"`
}

type ServiceAccount struct {
	Name string `yaml:"name,omitempty"`
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
func UnmarshalValues(data []byte) (any, error) {
	var detector map[string]any
	if err := yaml.Unmarshal(data, &detector); err != nil {
		return nil, err
	}

	if _, ok := detector["server"]; ok {
		var values VMSingleHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	}

	if _, ok := detector["vmselect"]; ok {
		var values VMClusterHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	}
	if _, ok := detector["vminsert"]; ok {
		var values VMClusterHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	}
	if _, ok := detector["vmstorage"]; ok {
		var values VMClusterHelmValues
		if err := yaml.Unmarshal(data, &values); err != nil {
			return nil, err
		}
		return &values, nil
	}

	return nil, fmt.Errorf("cannot detect type from helm values")
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
