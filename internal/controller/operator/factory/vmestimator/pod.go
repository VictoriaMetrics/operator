package vmestimator

import (
	"fmt"
	"path"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const (
	containerName        = "vmestimator"
	configDir            = "/etc/vmestimator/config"
	configVolumeName     = "config"
	configHashAnnotation = "operator.victoriametrics.com/config-hash"
)

// probeCRD defines component, which could be used to build container probes
type probeCRD interface {
	ProbePath() string
	ProbeScheme() string
	ProbePort() string
	ProbeNeedLiveness() bool
	UseProxyProtocol() bool
}

type podOpts struct {
	kind      vmv1beta1.ClusterComponent
	params    *vmv1beta1.CommonAppsParams
	probe     probeCRD
	logLevel  string
	logFormat string
	// args contains component specific command-line flags
	args []string
	// configHash defines hash of the streams configuration.
	// Configuration is mounted into the container only if hash is set.
	configHash string
}

// buildPodTemplate builds pod template for the given vmestimator component
func buildPodTemplate(cr *vmv1.VMEstimator, o *podOpts) (*corev1.PodTemplateSpec, error) {
	cfg := config.MustGetBaseConfig()
	p := o.params
	args := []string{fmt.Sprintf("-httpListenAddr=:%s", p.Port)}
	args = append(args, o.args...)
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if o.logLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", o.logLevel))
	}
	if o.logFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", o.logFormat))
	}
	if len(p.ExtraEnvs) > 0 || len(p.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var volumes []corev1.Volume
	var vmMounts []corev1.VolumeMount
	annotations := cr.PodAnnotations(o.kind)
	if o.configHash != "" {
		args = append(args, fmt.Sprintf("-config=%s", path.Join(configDir, configFileName)))
		volumes = append(volumes, corev1.Volume{
			Name: configVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName(cr),
					},
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      configVolumeName,
			MountPath: configDir,
			ReadOnly:  true,
		})
		// vmestimator doesn't support configuration reload,
		// so pods must be restarted on configuration change
		annotations = labels.Merge(annotations, map[string]string{
			configHashAnnotation: o.configHash,
		})
	}
	volumes = append(volumes, p.Volumes...)
	vmMounts = append(vmMounts, p.VolumeMounts...)

	for _, s := range p.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.SecretsDir, s),
		})
	}

	for _, c := range p.ConfigMaps {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("configmap-" + c),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		})
	}

	args = build.AddExtraArgsOverrideDefaults(args, p.ExtraArgs, "-")
	sort.Strings(args)

	var envs []corev1.EnvVar
	envs = append(envs, p.ExtraEnvs...)

	container := corev1.Container{
		Name:            containerName,
		Image:           p.Image.Reference(),
		ImagePullPolicy: p.Image.PullPolicy,
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				Protocol:      corev1.ProtocolTCP,
				ContainerPort: intstr.Parse(p.Port).IntVal,
			},
		},
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                p.Resources,
		Env:                      envs,
		EnvFrom:                  p.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	build.Probe(&container, o.probe, p)
	build.Lifecycle(&container, p)

	operatorContainers := []corev1.Container{container}
	build.AddStrictSecuritySettingsToContainers(operatorContainers, p)
	containers, err := k8stools.MergePatchContainers(operatorContainers, p.Containers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch containers: %w", err)
	}

	for i := range p.TopologySpreadConstraints {
		if p.TopologySpreadConstraints[i].LabelSelector == nil {
			p.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(o.kind),
			}
		}
	}

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(o.kind),
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     p.InitContainers,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
		},
	}, nil
}
