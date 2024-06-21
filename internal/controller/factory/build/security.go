package build

import (
	"github.com/VictoriaMetrics/operator/internal/controller/factory/k8stools"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// AddStrictSecuritySettingsToContainers conditionally adds Security settings to given containers
func AddStrictSecuritySettingsToContainers(containers []corev1.Container, enableStrictSecurity bool) []corev1.Container {
	if !enableStrictSecurity {
		return containers
	}
	for idx := range containers {
		container := &containers[idx]
		if container.SecurityContext == nil {
			container.SecurityContext = &corev1.SecurityContext{
				ReadOnlyRootFilesystem:   ptr.To(true),
				AllowPrivilegeEscalation: ptr.To(false),
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{
						"ALL",
					},
				},
			}
		}
	}
	return containers
}

// AddStrictSecuritySettingsToPod conditionally creates security context for pod or returns predefined one
func AddStrictSecuritySettingsToPod(p *corev1.PodSecurityContext, enableStrictSecurity bool) *corev1.PodSecurityContext {
	if !enableStrictSecurity || p != nil {
		return p
	}
	securityContext := corev1.PodSecurityContext{
		RunAsNonRoot: ptr.To(true),
		// '65534' refers to 'nobody' in all the used default images like alpine, busybox
		RunAsUser:  ptr.To(int64(65534)),
		RunAsGroup: ptr.To(int64(65534)),
		FSGroup:    ptr.To(int64(65534)),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
	if k8stools.IsFSGroupChangePolicySupported() {
		onRootMismatch := corev1.FSGroupChangeOnRootMismatch
		securityContext.FSGroupChangePolicy = &onRootMismatch
	}
	return &securityContext
}
