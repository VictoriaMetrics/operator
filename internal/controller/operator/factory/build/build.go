package build

import (
	"fmt"
	"path"

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
