package build

import (
	"fmt"
	"path"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// MustSkipRuntimeValidation defines whether runtime object validation must be skipped
// the most usual case for it, if webhook validation is configured
var MustSkipRuntimeValidation bool

// SetSkipRuntimeValidation configures MustSkipRuntimeValidation param
func SetSkipRuntimeValidation(mustSkip bool) {
	MustSkipRuntimeValidation = mustSkip
}

type builderOpts interface {
	client.Object
	PrefixedName() string
	AnnotationsFiltered() map[string]string
	AllLabels() map[string]string
	SelectorLabels() map[string]string
	AsOwner() []metav1.OwnerReference
	GetNamespace() string
	GetAdditionalService() *vmv1beta1.AdditionalServiceSpec
}

// PodDNSAddress formats pod dns address with optional domain name
func PodDNSAddress(baseName string, podIndex int32, namespace string, portName string, domain string) string {
	// The default DNS search path is .svc.<cluster domain>
	if domain == "" {
		return fmt.Sprintf("%s-%d.%s.%s:%s,", baseName, podIndex, baseName, namespace, portName)
	}
	return fmt.Sprintf("%s-%d.%s.%s.svc.%s:%s,", baseName, podIndex, baseName, namespace, domain, portName)
}

// LicenseArgsTo conditionally adds license commandline args into given args
func LicenseArgsTo(args []string, l *vmv1beta1.License, secretMountDir string) []string {
	return licenseArgsTo(args, l, secretMountDir, "-")
}

// LicenseDoubleDashArgsTo conditionally adds double-dash license commandline args into given args
func LicenseDoubleDashArgsTo(args []string, l *vmv1beta1.License, secretMountDir string) []string {
	return licenseArgsTo(args, l, secretMountDir, "--")
}

func licenseArgsTo(args []string, l *vmv1beta1.License, secretMountDir string, dashes string) []string {
	if l == nil || !l.IsProvided() {
		return args
	}
	if l.Key != nil {
		args = append(args, fmt.Sprintf("%slicense=%s", dashes, *l.Key))
	}
	if l.KeyRef != nil {
		args = append(args, fmt.Sprintf("%slicenseFile=%s", dashes, path.Join(secretMountDir, l.KeyRef.Name, l.KeyRef.Key)))
	}
	if l.ForceOffline != nil {
		args = append(args, fmt.Sprintf("%slicense.forceOffline=%v", dashes, *l.ForceOffline))
	}
	if l.ReloadInterval != nil {
		args = append(args, fmt.Sprintf("%slicenseFile.reloadInterval=%s", dashes, *l.ReloadInterval))
	}
	return args
}

// LicenseVolumeTo conditionally mounts secret with license key into given volumes and volume mounts
func LicenseVolumeTo(volumes []corev1.Volume, mounts []corev1.VolumeMount, l *vmv1beta1.License, secretMountDir string) ([]corev1.Volume, []corev1.VolumeMount) {
	if l == nil || l.KeyRef == nil {
		return volumes, mounts
	}
	volumes = append(volumes, corev1.Volume{
		Name: "license",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: l.KeyRef.Name,
			},
		},
	})
	mounts = append(mounts, corev1.VolumeMount{
		Name:      "license",
		ReadOnly:  true,
		MountPath: path.Join(secretMountDir, l.KeyRef.Name),
	})
	return volumes, mounts
}

// StreamAggrArgsTo conditionally adds stream aggregation commandline args into given args
func StreamAggrArgsTo(args []string, c *vmv1beta1.StreamAggrConfig, prefix, key string) []string {
	if c == nil {
		return args
	}
	if c.HasAnyRule() {
		args = append(args, fmt.Sprintf("--%s.config=%s", prefix, path.Join(vmv1beta1.StreamAggrConfigDir, key)))
		if c.KeepInput {
			args = append(args, fmt.Sprintf("--%s.keepInput=true", prefix))
		}
		if c.DropInput {
			args = append(args, fmt.Sprintf("--%s.dropInput=true", prefix))
		}
		if c.IgnoreFirstIntervals > 0 {
			args = append(args, fmt.Sprintf("--%s.ignoreFirstIntervals=%d", prefix, c.IgnoreFirstIntervals))
		}
		if c.IgnoreOldSamples {
			args = append(args, fmt.Sprintf("--%s.ignoreOldSamples=true", prefix))
		}
		if c.EnableWindows {
			args = append(args, fmt.Sprintf("--%s.enableWindows=true", prefix))
		}
	}
	// deduplication can work without stream aggregation rules
	if len(c.DedupInterval) > 0 {
		args = append(args, fmt.Sprintf("--%s.dedupInterval=%s", prefix, c.DedupInterval))
	}
	if len(c.DropInputLabels) > 0 {
		args = append(args, fmt.Sprintf("--%s.dropInputLabels=%s", prefix, strings.Join(c.DropInputLabels, ",")))
	}
	return args
}

// StreamAggrVolumeTo conditionally mounts configmap with stream aggregation config into given volumes and volume mounts
func StreamAggrVolumeTo(volumes []corev1.Volume, mounts []corev1.VolumeMount, c *vmv1beta1.StreamAggrConfig, name string) ([]corev1.Volume, []corev1.VolumeMount) {
	if c.HasAnyRule() {
		volumes = append(volumes, corev1.Volume{
			Name: "stream-aggr-conf",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: name,
					},
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "stream-aggr-conf",
			ReadOnly:  true,
			MountPath: vmv1beta1.StreamAggrConfigDir,
		})
	}
	return volumes, mounts
}

// RelabelArgsTo conditionally adds relabel commandline args into given args
func RelabelArgsTo(args []string, c *vmv1beta1.CommonRelabelParams, flag, key string) []string {
	if c.RelabelConfig != nil || len(c.InlineRelabelConfig) > 0 {
		args = append(args, fmt.Sprintf("-%s=%s", flag, path.Join(vmv1beta1.RelabelingConfigDir, key)))
	}
	return args
}

// RelabelVolumeTo conditionally mounts configmap with relabel config into given volumes and volume mounts
func RelabelVolumeTo(volumes []corev1.Volume, mounts []corev1.VolumeMount, c *vmv1beta1.CommonRelabelParams, name string) ([]corev1.Volume, []corev1.VolumeMount) {
	if c.HasAnyRelabellingConfigs() {
		volumes = append(volumes,
			corev1.Volume{
				Name: "relabeling-assets",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: name,
						},
					},
				},
			},
		)
		mounts = append(mounts,
			corev1.VolumeMount{
				Name:      "relabeling-assets",
				ReadOnly:  true,
				MountPath: vmv1beta1.RelabelingConfigDir,
			},
		)
	}
	return volumes, mounts
}
