package vmsingle

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const (
	dataDir             = "/victoria-metrics-data"
	dataVolumeName      = "data"
	streamAggrSecretKey = "config.yaml"
)

func newPodSpec(ctx context.Context, cr *vmv1beta1.VMSingle) (*corev1.PodSpec, error) {
	var args []string

	if cr.Spec.RetentionPeriod != "" {
		args = append(args, fmt.Sprintf("-retentionPeriod=%s", cr.Spec.RetentionPeriod))
	}

	// if customStorageDataPath is not empty, do not add volumes
	// and volumeMounts
	// it's user responsobility to provide correct values
	mustAddVolumeMounts := cr.Spec.StorageDataPath == ""

	storagePath := dataDir
	if cr.Spec.StorageDataPath != "" {
		storagePath = cr.Spec.StorageDataPath
	}
	args = append(args, fmt.Sprintf("-storageDataPath=%s", storagePath))
	if cr.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.LogLevel))
	}
	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.LogFormat))
	}

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))
	if len(cr.Spec.ExtraEnvs) > 0 || len(cr.Spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}
	args = build.AppendArgsForInsertPorts(args, cr.Spec.InsertPorts)

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	ports = build.AppendInsertPorts(ports, cr.Spec.InsertPorts)

	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	volumes, volumeMounts = addVolumeMountsTo(volumes, volumeMounts, cr, mustAddVolumeMounts, storagePath)

	if cr.Spec.VMBackup != nil && cr.Spec.VMBackup.CredentialsSecret != nil {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + cr.Spec.VMBackup.CredentialsSecret.Name),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.Spec.VMBackup.CredentialsSecret.Name,
				},
			},
		})
	}

	volumes = append(volumes, cr.Spec.Volumes...)
	volumeMounts = append(volumeMounts, cr.Spec.VolumeMounts...)

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
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		})
	}

	if cr.HasAnyStreamAggrRule() {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("stream-aggr-conf"),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cr.StreamAggrConfigName(),
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("stream-aggr-conf"),
			ReadOnly:  true,
			MountPath: vmv1beta1.StreamAggrConfigDir,
		})

		args = append(args, fmt.Sprintf("--streamAggr.config=%s", path.Join(vmv1beta1.StreamAggrConfigDir, streamAggrSecretKey)))
		if cr.Spec.StreamAggrConfig.KeepInput {
			args = append(args, "--streamAggr.keepInput=true")
		}
		if cr.Spec.StreamAggrConfig.DropInput {
			args = append(args, "--streamAggr.dropInput=true")
		}
		if len(cr.Spec.StreamAggrConfig.DropInputLabels) > 0 {
			args = append(args, fmt.Sprintf("--streamAggr.dropInputLabels=%s", strings.Join(cr.Spec.StreamAggrConfig.DropInputLabels, ",")))
		}
		if cr.Spec.StreamAggrConfig.IgnoreFirstIntervals > 0 {
			args = append(args, fmt.Sprintf("--streamAggr.ignoreFirstIntervals=%d", cr.Spec.StreamAggrConfig.IgnoreFirstIntervals))
		}
		if cr.Spec.StreamAggrConfig.IgnoreOldSamples {
			args = append(args, "--streamAggr.ignoreOldSamples=true")
		}
		if cr.Spec.StreamAggrConfig.EnableWindows {
			args = append(args, "--streamAggr.enableWindows=true")
		}
	}

	// deduplication can work without stream aggregation rules
	if cr.Spec.StreamAggrConfig != nil && cr.Spec.StreamAggrConfig.DedupInterval != "" {
		args = append(args, fmt.Sprintf("--streamAggr.dedupInterval=%s", cr.Spec.StreamAggrConfig.DedupInterval))
	}

	volumes, volumeMounts = cr.Spec.License.MaybeAddToVolumes(volumes, volumeMounts, vmv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)
	container := corev1.Container{
		Name:                     "vmsingle",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             volumeMounts,
		Resources:                cr.Spec.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
	}

	container = build.Probe(container, cr)

	containers := []corev1.Container{container}
	var initContainers []corev1.Container

	if cr.Spec.VMBackup != nil {
		vmBackupManagerContainer, err := build.VMBackupManager(ctx, cr.Spec.VMBackup, cr.Spec.Port, storagePath, dataVolumeName, cr.Spec.ExtraArgs, false, cr.Spec.License)
		if err != nil {
			return nil, err
		}
		if vmBackupManagerContainer != nil {
			containers = append(containers, *vmBackupManagerContainer)
		}
		if cr.Spec.VMBackup.Restore != nil &&
			cr.Spec.VMBackup.Restore.OnStart != nil &&
			cr.Spec.VMBackup.Restore.OnStart.Enabled {
			vmRestore, err := build.VMRestore(cr.Spec.VMBackup, storagePath, dataVolumeName)
			if err != nil {
				return nil, err
			}
			if vmRestore != nil {
				initContainers = append(initContainers, *vmRestore)
			}
		}
	}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, initContainers, ptr.Deref(cr.Spec.UseStrictSecurity, false))
	ic, err := k8stools.MergePatchContainers(initContainers, cr.Spec.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot apply initContainer patch: %w", err)
	}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, containers, ptr.Deref(cr.Spec.UseStrictSecurity, false))
	containers, err = k8stools.MergePatchContainers(containers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}

	return &corev1.PodSpec{
		Volumes:            volumes,
		InitContainers:     ic,
		Containers:         containers,
		ServiceAccountName: cr.GetServiceAccountName(),
	}, nil
}

func addVolumeMountsTo(volumes []corev1.Volume, volumeMounts []corev1.VolumeMount, cr *vmv1beta1.VMSingle, mustAddVolumeMounts bool, storagePath string) ([]corev1.Volume, []corev1.VolumeMount) {

	switch {
	case mustAddVolumeMounts:
		// add volume and mount point by operator directly
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      dataVolumeName,
			MountPath: storagePath},
		)

		vlSource := corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
		if cr.Spec.Storage != nil {
			vlSource = corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: cr.PrefixedName(),
				},
			}
		}
		volumes = append(volumes, corev1.Volume{
			Name:         dataVolumeName,
			VolumeSource: vlSource})

	case len(cr.Spec.Volumes) > 0:
		// add missing volumeMount point for backward compatibility
		// it simplifies management of external PVCs
		var volumeNamePresent bool
		for _, volume := range cr.Spec.Volumes {
			if volume.Name == dataVolumeName {
				volumeNamePresent = true
				break
			}
		}
		if volumeNamePresent {
			var mustSkipVolumeAdd bool
			for _, volumeMount := range cr.Spec.VolumeMounts {
				if volumeMount.Name == dataVolumeName {
					mustSkipVolumeAdd = true
					break
				}
			}
			if !mustSkipVolumeAdd {
				volumeMounts = append(volumeMounts, corev1.VolumeMount{
					Name:      dataVolumeName,
					MountPath: storagePath,
				})
			}
		}

	}

	return volumes, volumeMounts
}
