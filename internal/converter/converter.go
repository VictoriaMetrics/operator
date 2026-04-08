package converter

import (
	"fmt"

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

type GlobalValues struct {
	ImagePullSecrets []corev1.LocalObjectReference `yaml:"imagePullSecrets,omitempty"`
	Image            ImageValues                   `yaml:"image,omitempty"`
}

type ServiceAccount struct {
	Name string `yaml:"name,omitempty"`
}

type ServerValues struct {
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

// ConvertVMSingle converts helm values to VMSingle CRD
func ConvertVMSingle(name, namespace string, values *VMSingleHelmValues) *vmv1beta1.VMSingle {
	cr := &vmv1beta1.VMSingle{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMSingle",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	spec := &vmv1beta1.VMSingleSpec{}

	if values.Server.ReplicaCount != nil {
		spec.ReplicaCount = values.Server.ReplicaCount
	}

	// Image
	repo := values.Server.Image.Repository
	registry := values.Server.Image.Registry
	if registry == "" && values.Global.Image.Registry != "" {
		registry = values.Global.Image.Registry
	}
	if registry != "" {
		repo = fmt.Sprintf("%s/%s", registry, repo)
	}
	spec.Image.Repository = repo

	tag := values.Server.Image.Tag
	if values.Server.Image.Variant != "" {
		tag = fmt.Sprintf("%s-%s", tag, values.Server.Image.Variant)
	}
	spec.Image.Tag = tag
	if values.Server.Image.PullPolicy != "" {
		spec.Image.PullPolicy = corev1.PullPolicy(values.Server.Image.PullPolicy)
	}

	// ExtraArgs
	if len(values.Server.ExtraArgs) > 0 {
		spec.ExtraArgs = make(map[string]string)
		for k, v := range values.Server.ExtraArgs {
			spec.ExtraArgs[k] = fmt.Sprint(v)
		}
	}

	// ExtraEnvs
	if len(values.Server.ExtraEnvs) > 0 {
		spec.ExtraEnvs = values.Server.ExtraEnvs
	}

	// RetentionPeriod
	if values.Server.RetentionPeriod != nil {
		spec.RetentionPeriod = fmt.Sprint(values.Server.RetentionPeriod)
	}

	// Persistent Volume
	if values.Server.PersistentVolume != nil && values.Server.PersistentVolume.Enabled {
		spec.Storage = &corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		}
		if values.Server.PersistentVolume.StorageClass != "" {
			if values.Server.PersistentVolume.StorageClass == "-" {
				storageClass := ""
				spec.Storage.StorageClassName = &storageClass
			} else {
				spec.Storage.StorageClassName = &values.Server.PersistentVolume.StorageClass
			}
		}
		if values.Server.PersistentVolume.Size != "" {
			q, err := resource.ParseQuantity(values.Server.PersistentVolume.Size)
			if err == nil {
				spec.Storage.Resources = corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: q,
					},
				}
			}
		}
	}

	// Common Apps Params
	if values.Server.Resources != nil {
		spec.Resources = *values.Server.Resources
	}
	if len(values.Server.NodeSelector) > 0 {
		spec.NodeSelector = values.Server.NodeSelector
	}
	if len(values.Server.Tolerations) > 0 {
		spec.Tolerations = values.Server.Tolerations
	}
	if values.Server.Affinity != nil {
		spec.Affinity = values.Server.Affinity
	}

	// Security context
	if values.Server.PodSecurityContext != nil || values.Server.SecurityContext != nil {
		spec.SecurityContext = &vmv1beta1.SecurityContext{
			PodSecurityContext:       values.Server.PodSecurityContext,
			ContainerSecurityContext: values.Server.SecurityContext,
		}
	}

	if len(values.Server.ImagePullSecrets) > 0 {
		spec.ImagePullSecrets = values.Server.ImagePullSecrets
	} else if len(values.Global.ImagePullSecrets) > 0 {
		spec.ImagePullSecrets = values.Global.ImagePullSecrets
	}

	// Annotations and Labels
	if len(values.Server.PodAnnotations) > 0 || len(values.Server.Labels) > 0 {
		spec.PodMetadata = &vmv1beta1.EmbeddedObjectMetadata{
			Annotations: values.Server.PodAnnotations,
			Labels:      values.Server.Labels,
		}
	}

	if values.ServiceAccount != nil && values.ServiceAccount.Name != "" {
		spec.ServiceAccountName = values.ServiceAccount.Name
	}

	cr.Spec = *spec
	return cr
}
