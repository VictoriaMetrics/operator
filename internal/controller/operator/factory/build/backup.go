package build

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

const vmBackuperCreds = "/etc/vm/creds"

// backupCRD is implemented by applications that support a vmbackupmanager sidecar.
type backupCRD interface {
	Backup() *vmv1beta1.VMBackup
	SnapshotCreatePath(host string) string
	SnapshotDeletePath(host string) string
}

// VMBackupManager conditionally creates vmbackupmanager container
func VMBackupManager(
	ctx context.Context,
	cr backupCRD,
	storagePath string,
	mounts []corev1.VolumeMount,
	isCluster bool,
	license *vmv1beta1.License,
) (*corev1.Container, error) {
	vmBackup := cr.Backup()
	if vmBackup == nil {
		return nil, nil
	}
	if !vmBackup.AcceptEULA && !license.IsProvided() {
		logger.WithContext(ctx).Info("EULA or license wasn't defined, update your backup settings." +
			" Follow https://docs.victoriametrics.com/victoriametrics/enterprise for further instructions.")
		return nil, nil
	}
	snapshotCreateURL := vmBackup.SnapshotCreateURL
	snapshotDeleteURL := vmBackup.SnapshotDeleteURL
	if snapshotCreateURL == "" {
		// http://localhost:port/snapshot/create
		snapshotCreateURL = cr.SnapshotCreatePath(config.GetLocalhost())
	}
	if snapshotDeleteURL == "" {
		// http://localhost:port/snapshot/delete
		snapshotDeleteURL = cr.SnapshotDeletePath(config.GetLocalhost())
	}
	backupDst := vmBackup.Destination
	// add suffix with pod name for cluster backupmanager
	// it's needed to create consistent backup across cluster nodes
	if isCluster && !vmBackup.DestinationDisableSuffixAdd {
		backupDst = strings.TrimSuffix(backupDst, "/") + "/$(POD_NAME)/"
	}
	args := []string{
		fmt.Sprintf("-storageDataPath=%s", storagePath),
		fmt.Sprintf("-dst=%s", backupDst),
		fmt.Sprintf("-snapshot.createURL=%s", snapshotCreateURL),
		fmt.Sprintf("-snapshot.deleteURL=%s", snapshotDeleteURL),
	}
	if vmBackup.AcceptEULA {
		args = append(args, "-eula")
	}
	if vmBackup.LogLevel != nil {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", *vmBackup.LogLevel))
	}
	if vmBackup.LogFormat != nil {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", *vmBackup.LogFormat))
	}
	for key, value := range vmBackup.ExtraArgs {
		arg := fmt.Sprintf("-%s", key)
		if len(value) != 0 {
			arg = fmt.Sprintf("%s=%s", arg, value)
		}
		args = append(args, arg)
	}
	if vmBackup.Concurrency != nil {
		args = append(args, fmt.Sprintf("-concurrency=%d", *vmBackup.Concurrency))
	}
	if vmBackup.CustomS3Endpoint != nil {
		args = append(args, fmt.Sprintf("-customS3Endpoint=%s", *vmBackup.CustomS3Endpoint))
	}
	if vmBackup.DisableHourly != nil && *vmBackup.DisableHourly {
		args = append(args, "-disableHourly")
	}
	if vmBackup.DisableDaily != nil && *vmBackup.DisableDaily {
		args = append(args, "-disableDaily")
	}
	if vmBackup.DisableMonthly != nil && *vmBackup.DisableMonthly {
		args = append(args, "-disableMonthly")
	}
	if vmBackup.DisableWeekly != nil && *vmBackup.DisableWeekly {
		args = append(args, "-disableWeekly")
	}

	var ports []corev1.ContainerPort
	portName := "http-backup"
	if config.UseOldBackupRestorePortNames() {
		portName = "http"
	}
	ports = append(ports, corev1.ContainerPort{Name: portName, Protocol: "TCP", ContainerPort: intstr.Parse(vmBackup.Port).IntVal})
	mounts = append(mounts, vmBackup.VolumeMounts...)
	if vmBackup.CredentialsSecret != nil {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + vmBackup.CredentialsSecret.Name),
			MountPath: vmBackuperCreds,
			ReadOnly:  true,
		})
		args = append(args, fmt.Sprintf("-credsFilePath=%s/%s", vmBackuperCreds, vmBackup.CredentialsSecret.Key))
	}

	_, mounts = LicenseVolumeTo(nil, mounts, license, vmv1beta1.SecretsDir)
	args = LicenseArgsTo(args, license, vmv1beta1.SecretsDir)

	extraEnvs := vmBackup.ExtraEnvs
	if len(vmBackup.ExtraEnvs) > 0 || len(vmBackup.ExtraEnvsFrom) > 0 {
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
			Port:   intstr.Parse(vmBackup.Port),
			Scheme: "HTTP",
			Path:   "/health",
		},
	}
	readinessProbeHandler := corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(vmBackup.Port),
			Scheme: "HTTP",
			Path:   "/health",
		},
	}
	livenessFailureThreshold := int32(3)
	livenessProbe := &corev1.Probe{
		ProbeHandler:     livenessProbeHandler,
		PeriodSeconds:    5,
		TimeoutSeconds:   5,
		SuccessThreshold: 1,
		FailureThreshold: livenessFailureThreshold,
	}
	readinessProbe := &corev1.Probe{
		ProbeHandler:     readinessProbeHandler,
		TimeoutSeconds:   5,
		PeriodSeconds:    5,
		SuccessThreshold: 1,
		FailureThreshold: 10,
	}

	sort.Strings(args)
	vmBackuper := &corev1.Container{
		Name:                     "vmbackuper",
		Image:                    vmBackup.Image.Reference(),
		Ports:                    ports,
		Args:                     args,
		Env:                      extraEnvs,
		VolumeMounts:             mounts,
		LivenessProbe:            livenessProbe,
		ReadinessProbe:           readinessProbe,
		Resources:                vmBackup.Resources,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	return vmBackuper, nil
}

// VMRestore conditionally creates vmrestore container
func VMRestore(
	cr *vmv1beta1.VMBackup,
	storagePath string,
	mounts []corev1.VolumeMount,
) (*corev1.Container, error) {
	args := []string{
		fmt.Sprintf("-storageDataPath=%s", storagePath),
	}
	if cr.AcceptEULA {
		args = append(args, "-eula")
	}
	if cr.LogLevel != nil {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", *cr.LogLevel))
	}
	if cr.LogFormat != nil {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", *cr.LogFormat))
	}
	for key, value := range cr.ExtraArgs {
		arg := fmt.Sprintf("-%s", key)
		if len(value) != 0 {
			arg = fmt.Sprintf("%s=%s", arg, value)
		}
		args = append(args, arg)
	}
	if cr.Concurrency != nil {
		args = append(args, fmt.Sprintf("-concurrency=%d", *cr.Concurrency))
	}
	if cr.CustomS3Endpoint != nil {
		args = append(args, fmt.Sprintf("-customS3Endpoint=%s", *cr.CustomS3Endpoint))
	}

	var ports []corev1.ContainerPort
	portName := "http-restore"
	if config.UseOldBackupRestorePortNames() {
		portName = "http"
	}
	ports = append(ports, corev1.ContainerPort{Name: portName, Protocol: "TCP", ContainerPort: intstr.Parse(cr.Port).IntVal})
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
	if len(cr.ExtraEnvs) > 0 || len(cr.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	sort.Strings(args)

	args = append([]string{"restore"}, args...)

	vmRestore := &corev1.Container{
		Name:                     "vmbackuper-restore",
		Image:                    cr.Image.Reference(),
		Ports:                    ports,
		Args:                     args,
		Env:                      extraEnvs,
		EnvFrom:                  cr.ExtraEnvsFrom,
		VolumeMounts:             mounts,
		Resources:                cr.Resources,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	return vmRestore, nil
}
