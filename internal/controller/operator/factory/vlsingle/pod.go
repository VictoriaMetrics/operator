package vlsingle

import (
	"fmt"
	"path"
	"sort"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta "github.com/VictoriaMetrics/operator/api/operator/v1beta1"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const (
	dataDir        = "/victoria-logs-data"
	dataVolumeName = "data"
)

func newPodSpec(r *vmv1.VLSingle) (*corev1.PodSpec, error) {
	var args []string

	if len(r.Spec.RetentionPeriod) > 0 {
		args = append(args, fmt.Sprintf("-retentionPeriod=%s", r.Spec.RetentionPeriod))
	}
	if len(r.Spec.RetentionMaxDiskSpaceUsageBytes) > 0 {
		args = append(args, fmt.Sprintf("-retention.maxDiskSpaceUsageBytes=%s", r.Spec.RetentionMaxDiskSpaceUsageBytes))
	}

	// if customStorageDataPath is not empty, do not add pvc.
	shouldAddPVC := r.Spec.StorageDataPath == ""

	storagePath := dataDir
	if r.Spec.StorageDataPath != "" {
		storagePath = r.Spec.StorageDataPath
	}
	args = append(args, fmt.Sprintf("-storageDataPath=%s", storagePath))
	if r.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", r.Spec.LogLevel))
	}
	if r.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", r.Spec.LogFormat))
	}
	if len(r.Spec.FutureRetention) > 0 {
		args = append(args, fmt.Sprintf("-futureRetention=%s", r.Spec.FutureRetention))
	}
	if r.Spec.LogNewStreams {
		args = append(args, "-logNewStreams")
	}
	if r.Spec.LogIngestedRows {
		args = append(args, "-logIngestedRows")
	}
	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", r.Spec.Port))
	if len(r.Spec.ExtraEnvs) > 0 || len(r.Spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, r.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(r.Spec.Port).IntVal})
	volumes := []corev1.Volume{}

	storageSpec := r.Spec.Storage

	if storageSpec == nil {
		volumes = append(volumes, corev1.Volume{
			Name: dataVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	} else if shouldAddPVC {
		volumes = append(volumes, corev1.Volume{
			Name: dataVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: r.PrefixedName(),
				},
			},
		})
	}
	volumes = append(volumes, r.Spec.Volumes...)
	vmMounts := []corev1.VolumeMount{
		{
			Name:      dataVolumeName,
			MountPath: storagePath,
		},
	}

	vmMounts = append(vmMounts, r.Spec.VolumeMounts...)

	for _, s := range r.Spec.Secrets {
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
			MountPath: path.Join(vmv1beta.SecretsDir, s),
		})
	}

	for _, c := range r.Spec.ConfigMaps {
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
			MountPath: path.Join(vmv1beta.ConfigMapsDir, c),
		})
	}

	args = build.AddExtraArgsOverrideDefaults(args, r.Spec.ExtraArgs, "-")
	sort.Strings(args)
	container := corev1.Container{
		Name:                     "vlsingle",
		Image:                    fmt.Sprintf("%s:%s", r.Spec.Image.Repository, r.Spec.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                r.Spec.Resources,
		Env:                      envs,
		EnvFrom:                  r.Spec.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          r.Spec.Image.PullPolicy,
	}

	container = build.Probe(container, r)

	containers := []corev1.Container{container}

	build.AddStrictSecuritySettingsToContainers(r.Spec.SecurityContext, containers, ptr.Deref(r.Spec.UseStrictSecurity, false))

	containers, err := k8stools.MergePatchContainers(containers, r.Spec.Containers)
	if err != nil {
		return nil, err
	}

	return &corev1.PodSpec{
		Volumes:            volumes,
		InitContainers:     r.Spec.InitContainers,
		Containers:         containers,
		ServiceAccountName: r.GetServiceAccountName(),
	}, nil
}
