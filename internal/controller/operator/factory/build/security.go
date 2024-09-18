package build

import (
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

var (
	// '65534' refers to 'nobody' in all the used default images like alpine, busybox
	containerUserGroup     int64 = 65534
	runNonRoot                   = true
	defaultSecurityContext       = &corev1.SecurityContext{
		RunAsUser:    &containerUserGroup,
		RunAsGroup:   &containerUserGroup,
		RunAsNonRoot: &runNonRoot,
		AppArmorProfile: &corev1.AppArmorProfile{
			Type: corev1.AppArmorProfileTypeRuntimeDefault,
		},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
		ReadOnlyRootFilesystem:   ptr.To(true),
		AllowPrivilegeEscalation: ptr.To(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{
				"ALL",
			},
		},
	}
	defaultPodSecurityContext = &corev1.PodSecurityContext{
		RunAsNonRoot: &runNonRoot,
		RunAsUser:    &containerUserGroup,
		RunAsGroup:   &containerUserGroup,
		FSGroup:      &containerUserGroup,
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
)

// AddStrictSecuritySettingsToContainers conditionally adds Security settings to given containers
func AddStrictSecuritySettingsToContainers(p *vmv1beta1.SecurityContext, containers []corev1.Container, enableStrictSecurity bool) []corev1.Container {
	if !enableStrictSecurity {
		return containers
	}
	for idx := range containers {
		container := &containers[idx]
		if container.SecurityContext == nil {
			container.SecurityContext = defaultSecurityContext
		}
	}
	return containers
}

// AddStrictSecuritySettingsToPod conditionally creates security context for pod or returns predefined one
func AddStrictSecuritySettingsToPod(p *vmv1beta1.SecurityContext, enableStrictSecurity bool) *corev1.PodSecurityContext {
	if p != nil {
		return p.PodSecurityContext
	}
	if !enableStrictSecurity {
		return nil
	}
	securityContext := defaultPodSecurityContext.DeepCopy()
	if k8stools.IsFSGroupChangePolicySupported() {
		onRootMismatch := corev1.FSGroupChangeOnRootMismatch
		securityContext.FSGroupChangePolicy = &onRootMismatch
	}
	return securityContext
}
