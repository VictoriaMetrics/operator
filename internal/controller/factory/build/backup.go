package build

import (
	"context"
	"fmt"
	"sort"
	"strings"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/logger"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const vmBackuperCreds = "/etc/vm/creds"

// VMBackupManager conditionally creates vmbackupmanager container
func VMBackupManager(
	ctx context.Context,
	cr *vmv1beta1.VMBackup,
	c *config.BaseOperatorConf,
	port string,
	storagePath, dataVolumeName string,
	extraArgs map[string]string,
	isCluster bool,
	license *vmv1beta1.License,
) (*corev1.Container, error) {
	if !cr.AcceptEULA && !license.IsProvided() {
		logger.WithContext(ctx).Info("EULA or license wasn't defined, update your backup settings. Follow https://docs.victoriametrics.com/enterprise.html for further instructions.")
		return nil, nil
	}
	if cr.Image.Repository == "" {
		cr.Image.Repository = c.VMBackup.Image
	}
	if cr.Image.Tag == "" {
		cr.Image.Tag = c.VMBackup.Version
	}
	if cr.Image.PullPolicy == "" {
		cr.Image.PullPolicy = corev1.PullIfNotPresent
	}
	if cr.Port == "" {
		cr.Port = c.VMBackup.Port
	}

	snapshotCreateURL := cr.SnapshotCreateURL
	snapshotDeleteURL := cr.SnapShotDeleteURL
	if snapshotCreateURL == "" {
		// http://localhost:port/snaphsot/create
		snapshotCreateURL = cr.SnapshotCreatePathWithFlags(port, extraArgs)
	}
	if snapshotDeleteURL == "" {
		// http://localhost:port/snaphsot/delete
		snapshotDeleteURL = cr.SnapshotDeletePathWithFlags(port, extraArgs)
	}
	backupDst := cr.Destination
	// add suffix with pod name for cluster backupmanager
	// it's needed to create consistent backup across cluster nodes
	if isCluster && !cr.DestinationDisableSuffixAdd {
		backupDst = strings.TrimSuffix(backupDst, "/") + "/$(POD_NAME)/"
	}
	args := []string{
		fmt.Sprintf("-storageDataPath=%s", storagePath),
		fmt.Sprintf("-dst=%s", backupDst),
		fmt.Sprintf("-snapshot.createURL=%s", snapshotCreateURL),
		fmt.Sprintf("-snapshot.deleteURL=%s", snapshotDeleteURL),
		"-eula",
	}

	if cr.LogLevel != nil {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", *cr.LogLevel))
	}
	if cr.LogFormat != nil {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", *cr.LogFormat))
	}
	for arg, value := range cr.ExtraArgs {
		args = append(args, fmt.Sprintf("-%s=%s", arg, value))
	}
	if cr.Concurrency != nil {
		args = append(args, fmt.Sprintf("-concurrency=%d", *cr.Concurrency))
	}
	if cr.CustomS3Endpoint != nil {
		args = append(args, fmt.Sprintf("-customS3Endpoint=%s", *cr.CustomS3Endpoint))
	}
	if cr.DisableHourly != nil && *cr.DisableHourly {
		args = append(args, "-disableHourly")
	}
	if cr.DisableDaily != nil && *cr.DisableDaily {
		args = append(args, "-disableDaily")
	}
	if cr.DisableMonthly != nil && *cr.DisableMonthly {
		args = append(args, "-disableMonthly")
	}
	if cr.DisableWeekly != nil && *cr.DisableWeekly {
		args = append(args, "-disableWeekly")
	}

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Port).IntVal})

	mounts := []corev1.VolumeMount{
		{
			Name:      dataVolumeName,
			MountPath: storagePath,
			ReadOnly:  false,
		},
	}
	mounts = append(mounts, cr.VolumeMounts...)

	if cr.CredentialsSecret != nil {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + cr.CredentialsSecret.Name),
			MountPath: vmBackuperCreds,
			ReadOnly:  true,
		})
		args = append(args, fmt.Sprintf("-credsFilePath=%s/%s", vmBackuperCreds, cr.CredentialsSecret.Key))
	}

	_, mounts = license.MaybeAddToVolumes(nil, mounts, vmv1beta1.SecretsDir)
	args = license.MaybeAddToArgs(args, vmv1beta1.SecretsDir)

	extraEnvs := cr.ExtraEnvs
	if len(cr.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}
	// expose POD_NAME information by default
	// its needed to create uniq path for backup
	extraEnvs = append(extraEnvs, corev1.EnvVar{
		Name: "POD_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "metadata.name",
			},
		},
	})

	livenessProbeHandler := corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Port),
			Scheme: "HTTP",
			Path:   "/health",
		},
	}
	readinessProbeHandler := corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Port),
			Scheme: "HTTP",
			Path:   "/health",
		},
	}
	livenessFailureThreshold := int32(3)
	livenessProbe := &corev1.Probe{
		ProbeHandler:     livenessProbeHandler,
		PeriodSeconds:    5,
		TimeoutSeconds:   5,
		FailureThreshold: livenessFailureThreshold,
	}
	readinessProbe := &corev1.Probe{
		ProbeHandler:     readinessProbeHandler,
		TimeoutSeconds:   5,
		PeriodSeconds:    5,
		FailureThreshold: 10,
	}

	sort.Strings(args)
	vmBackuper := &corev1.Container{
		Name:                     "vmbackuper",
		Image:                    fmt.Sprintf("%s:%s", FormatContainerImage(c.ContainerRegistry, cr.Image.Repository), cr.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		Env:                      extraEnvs,
		VolumeMounts:             mounts,
		LivenessProbe:            livenessProbe,
		ReadinessProbe:           readinessProbe,
		Resources:                Resources(cr.Resources, config.Resource(c.VMBackup.Resource), c.VMBackup.UseDefaultResources),
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	return vmBackuper, nil
}

// VMRestore conditionally creates vmrestore container
func VMRestore(
	cr *vmv1beta1.VMBackup,
	c *config.BaseOperatorConf,
	storagePath, dataVolumeName string,
) (*corev1.Container, error) {
	if cr.Image.Repository == "" {
		cr.Image.Repository = c.VMBackup.Image
	}
	if cr.Image.Tag == "" {
		cr.Image.Tag = c.VMBackup.Version
	}
	if cr.Image.PullPolicy == "" {
		cr.Image.PullPolicy = corev1.PullIfNotPresent
	}
	if cr.Port == "" {
		cr.Port = c.VMBackup.Port
	}

	args := []string{
		fmt.Sprintf("-storageDataPath=%s", storagePath),
		"-eula",
	}

	if cr.LogLevel != nil {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", *cr.LogLevel))
	}
	if cr.LogFormat != nil {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", *cr.LogFormat))
	}
	for arg, value := range cr.ExtraArgs {
		args = append(args, fmt.Sprintf("-%s=%s", arg, value))
	}
	if cr.Concurrency != nil {
		args = append(args, fmt.Sprintf("-concurrency=%d", *cr.Concurrency))
	}
	if cr.CustomS3Endpoint != nil {
		args = append(args, fmt.Sprintf("-customS3Endpoint=%s", *cr.CustomS3Endpoint))
	}

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Port).IntVal})

	mounts := []corev1.VolumeMount{
		{
			Name:      dataVolumeName,
			MountPath: storagePath,
			ReadOnly:  false,
		},
	}
	mounts = append(mounts, cr.VolumeMounts...)

	if cr.CredentialsSecret != nil {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + cr.CredentialsSecret.Name),
			MountPath: vmBackuperCreds,
			ReadOnly:  true,
		})
		args = append(args, fmt.Sprintf("-credsFilePath=%s/%s", vmBackuperCreds, cr.CredentialsSecret.Key))
	}
	extraEnvs := cr.ExtraEnvs
	if len(cr.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	sort.Strings(args)

	args = append([]string{"restore"}, args...)

	vmRestore := &corev1.Container{
		Name:                     "vmbackuper-restore",
		Image:                    fmt.Sprintf("%s:%s", FormatContainerImage(c.ContainerRegistry, cr.Image.Repository), cr.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		Env:                      extraEnvs,
		VolumeMounts:             mounts,
		Resources:                Resources(cr.Resources, config.Resource(c.VMBackup.Resource), c.VMBackup.UseDefaultResources),
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	return vmRestore, nil
}
