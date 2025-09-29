package vmanomaly

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const (
	secretConfigKey  = "vmanomaly.yaml"
	anomalyDir       = "/etc/vmanomaly"
	confDir          = anomalyDir + "/config"
	confFile         = confDir + "/vmanomaly.yaml"
	tlsAssetsDir     = anomalyDir + "/tls"
	storageDir       = "/storage"
	configVolumeName = "config-volume"
)

func newPodSpec(cr *vmv1.VMAnomaly, ac *build.AssetsCache) (*corev1.PodSpec, error) {
	image := fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag)

	var args []string
	port, err := strconv.ParseInt(cr.Port(), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("cannot reconcile vmanomaly: failed to parse port: %w", err)
	}

	if cr.Spec.LogLevel != "" && cr.Spec.LogLevel != "info" {
		args = append(args, fmt.Sprintf("--loggerLevel=%s", strings.ToUpper(cr.Spec.LogLevel)))
	}

	monitoringPort, err := strconv.ParseInt(cr.Spec.Monitoring.Pull.Port, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("cannot reconcile vmanomaly: failed to parse monitoring port: %w", err)
	}

	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			ContainerPort: int32(port),
			Protocol:      corev1.ProtocolTCP,
		},
		{
			Name:          "monitoring-http",
			ContainerPort: int32(monitoringPort),
			Protocol:      corev1.ProtocolTCP,
		},
	}

	volumes := []corev1.Volume{
		{
			Name: configVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: build.ResourceName(build.SecretConfigResourceKind, cr),
				},
			},
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      configVolumeName,
			MountPath: confDir,
			ReadOnly:  true,
		},
		{
			Name:      cr.GetVolumeName(),
			MountPath: storageDir,
		},
	}

	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.ConfigMaps {
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
		cmVolumeMount := corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		}
		volumeMounts = append(volumeMounts, cmVolumeMount)
	}

	volumeMounts = append(volumeMounts, cr.Spec.VolumeMounts...)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "--")
	sort.Strings(args)

	envs := []corev1.EnvVar{
		{
			Name: "POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
		{
			Name:  "VMANOMALY_MODEL_DUMPS_DIR",
			Value: path.Join(storageDir, "model"),
		},
		{
			Name:  "VMANOMALY_DATA_DUMPS_DIR",
			Value: path.Join(storageDir, "data"),
		},
	}

	envs = append(envs, cr.Spec.ExtraEnvs...)

	var initContainers []corev1.Container

	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, initContainers, useStrictSecurity)

	ic, err := k8stools.MergePatchContainers(initContainers, cr.Spec.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot apply patch for initContainers: %w", err)
	}

	volumes, volumeMounts = build.LicenseVolumeTo(volumes, volumeMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	volumes, volumeMounts = ac.VolumeTo(volumes, volumeMounts)
	args = build.LicenseDoubleDashArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)
	// HACK: vmanomaly doesn't support boolean equal true
	// and false value doesn't make any sense
	if cr.Spec.License != nil && ptr.Deref(cr.Spec.License.ForceOffline, false) {
		for idx, arg := range args {
			if strings.HasPrefix(arg, "--license.forceOffline=") {
				args[idx] = "--license.forceOffline"
			}
		}
	}

	// vmanomaly accepts configuration file as a last element of args
	args = append(args, confFile)

	container := corev1.Container{
		Args:                     args,
		Name:                     "vmanomaly",
		Image:                    image,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		VolumeMounts:             volumeMounts,
		Resources:                cr.Spec.Resources,
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	container = build.Probe(container, cr)
	containers := []corev1.Container{container}
	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, containers, useStrictSecurity)
	containers, err = k8stools.MergePatchContainers(containers, cr.Spec.Containers)
	if err != nil {
		return nil, fmt.Errorf("failed to merge containers spec: %w", err)
	}

	for i := range cr.Spec.TopologySpreadConstraints {
		if cr.Spec.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			}
		}
	}
	return &corev1.PodSpec{
		InitContainers:     ic,
		Containers:         containers,
		Volumes:            volumes,
		ServiceAccountName: cr.GetServiceAccountName(),
	}, nil
}
