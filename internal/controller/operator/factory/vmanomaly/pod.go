package vmanomaly

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

var reloadMinVersion = semver.MustParse("v1.25.0")

const (
	anomalyDir             = "/etc/vmanomaly"
	confDir                = anomalyDir + "/config"
	confOutDir             = anomalyDir + "/config_out"
	tlsAssetsDir           = anomalyDir + "/tls"
	storageDir             = "/storage"
	configVolumeName       = "config"
	gzippedFilename        = "vmanomaly.yaml.gz"
	configEnvsubstFilename = "vmanomaly.env.yaml"
)

func reloadSupported(cr *vmv1.VMAnomaly) bool {
	anomalyVersion, err := semver.NewVersion(cr.Spec.Image.Tag)
	if err == nil {
		return anomalyVersion.GreaterThanEqual(reloadMinVersion)
	}
	return false
}

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

	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			ContainerPort: int32(port),
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
		{
			Name: "config-out",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      cr.GetVolumeName(),
			MountPath: storageDir,
		},
	}
	var configReloaderMounts []corev1.VolumeMount
	var initContainers []corev1.Container
	var containers []corev1.Container

	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	if reloadSupported(cr) {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "config-out",
			MountPath: confOutDir,
		})
		configReloader := buildConfigReloaderContainer(cr, configReloaderMounts)
		containers = append(containers, configReloader)
		initContainers = append(initContainers, buildInitConfigContainer(ptr.Deref(cr.Spec.UseVMConfigReloader, false), cr, configReloader.Args)...)
		build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, initContainers, useStrictSecurity)
	} else {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      configVolumeName,
			MountPath: confDir,
			ReadOnly:  true,
		})
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
	if reloadSupported(cr) {
		args = append(args, "--watch")
		args = append(args, path.Join(confOutDir, configEnvsubstFilename))
	} else {
		args = append(args, path.Join(confDir, configEnvsubstFilename))
	}

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
	containers = append(containers, container)
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

func buildConfigReloaderContainer(cr *vmv1.VMAnomaly, extraWatchsMounts []corev1.VolumeMount) corev1.Container {
	var configReloadVolumeMounts []corev1.VolumeMount
	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)

	configReloadVolumeMounts = append(configReloadVolumeMounts,
		corev1.VolumeMount{
			Name:      "config-out",
			MountPath: confOutDir,
		},
	)
	if !useVMConfigReloader {
		configReloadVolumeMounts = append(configReloadVolumeMounts,
			corev1.VolumeMount{
				Name:      "config",
				MountPath: confDir,
			})
	}

	configReloadArgs := buildConfigReloaderArgs(cr, extraWatchsMounts)

	configReloadVolumeMounts = append(configReloadVolumeMounts, extraWatchsMounts...)
	cntr := corev1.Container{
		Name:                     "config-reloader",
		Image:                    cr.Spec.ConfigReloaderImageTag,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		Env: []corev1.EnvVar{
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			},
		},
		Command:      []string{"/bin/prometheus-config-reloader"},
		Args:         configReloadArgs,
		VolumeMounts: configReloadVolumeMounts,
		Resources:    cr.Spec.ConfigReloaderResources,
	}
	if useVMConfigReloader {
		cntr.Command = nil
		build.AddServiceAccountTokenVolumeMount(&cntr, &cr.Spec.CommonApplicationDeploymentParams)
	}
	build.AddsPortProbesToConfigReloaderContainer(useVMConfigReloader, &cntr)
	build.AddConfigReloadAuthKeyToReloader(&cntr, &cr.Spec.CommonConfigReloaderParams)
	return cntr
}

func buildConfigReloaderArgs(cr *vmv1.VMAnomaly, extraWatchVolumes []corev1.VolumeMount) []string {
	// by default use watched-dir
	// it should simplify parsing for latest and empty version tags.
	dirsArg := "watched-dir"

	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)

	args := []string{
		fmt.Sprintf("--reload-url=http://localhost:%s/metrics", cr.Spec.Port),
		fmt.Sprintf("--config-envsubst-file=%s", path.Join(confOutDir, configEnvsubstFilename)),
	}
	if useVMConfigReloader {
		args = append(args, fmt.Sprintf("--config-secret-name=%s/%s", cr.Namespace, cr.PrefixedName()))
		args = append(args, fmt.Sprintf("--config-secret-key=%s", gzippedFilename))
	} else {
		args = append(args, fmt.Sprintf("--config-file=%s", path.Join(confDir, gzippedFilename)))
	}
	for _, vl := range extraWatchVolumes {
		args = append(args, fmt.Sprintf("--%s=%s", dirsArg, vl.MountPath))
	}
	if len(cr.Spec.ConfigReloaderExtraArgs) > 0 {
		for idx, arg := range args {
			cleanArg := strings.Split(strings.TrimLeft(arg, "-"), "=")[0]
			if replacement, ok := cr.Spec.ConfigReloaderExtraArgs[cleanArg]; ok {
				delete(cr.Spec.ConfigReloaderExtraArgs, cleanArg)
				args[idx] = fmt.Sprintf(`--%s=%s`, cleanArg, replacement)
			}
		}
		for k, v := range cr.Spec.ConfigReloaderExtraArgs {
			args = append(args, fmt.Sprintf(`--%s=%s`, k, v))
		}
		sort.Strings(args)
	}

	return args
}

func buildInitConfigContainer(useVMConfigReloader bool, cr *vmv1.VMAnomaly, configReloaderArgs []string) []corev1.Container {
	var initReloader corev1.Container
	baseImage := cr.Spec.ConfigReloaderImageTag
	resources := cr.Spec.ConfigReloaderResources
	if useVMConfigReloader {
		initReloader = corev1.Container{
			Image: baseImage,
			Name:  "config-init",
			Args:  append(configReloaderArgs, "--only-init-config"),
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "config-out",
					MountPath: confOutDir,
				},
			},
			Resources: resources,
		}
		build.AddServiceAccountTokenVolumeMount(&initReloader, &cr.Spec.CommonApplicationDeploymentParams)
		return []corev1.Container{initReloader}
	}
	initReloader = corev1.Container{
		Image: baseImage,
		Name:  "config-init",
		Command: []string{
			"/bin/sh",
		},
		Args: []string{
			"-c",
			fmt.Sprintf("gunzip -c %s > %s", path.Join(confDir, gzippedFilename), path.Join(confOutDir, configEnvsubstFilename)),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "config",
				MountPath: confDir,
			},
			{
				Name:      "config-out",
				MountPath: confOutDir,
			},
		},
		Resources: resources,
	}

	return []corev1.Container{initReloader}
}
