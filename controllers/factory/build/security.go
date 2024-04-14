package build

import (
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// AddStrictSecuritySettingsToContainers conditionally adds Security settings to given containers
func AddStrictSecuritySettingsToContainers(containers []v1.Container, enableStrictSecurity bool) []v1.Container {
	if !enableStrictSecurity {
		return containers
	}
	for idx := range containers {
		container := &containers[idx]
		if container.SecurityContext == nil {
			container.SecurityContext = &v1.SecurityContext{
				ReadOnlyRootFilesystem:   ptr.To(true),
				AllowPrivilegeEscalation: ptr.To(false),
				Capabilities: &v1.Capabilities{
					Drop: []v1.Capability{
						"ALL",
					},
				},
			}
		}
	}
	return containers
}

// AddStrictSecuritySettingsToPod conditionally creates security context for pod or returns predefined one
func AddStrictSecuritySettingsToPod(p *v1.PodSecurityContext, enableStrictSecurity bool) *v1.PodSecurityContext {
	if !enableStrictSecurity || p != nil {
		return p
	}
	securityContext := v1.PodSecurityContext{
		RunAsNonRoot: ptr.To(true),
		// '65534' refers to 'nobody' in all the used default images like alpine, busybox
		RunAsUser:  ptr.To(int64(65534)),
		RunAsGroup: ptr.To(int64(65534)),
		FSGroup:    ptr.To(int64(65534)),
		SeccompProfile: &v1.SeccompProfile{
			Type: v1.SeccompProfileTypeRuntimeDefault,
		},
	}
	if k8stools.IsFSGroupChangePolicySupported() {
		onRootMismatch := v1.FSGroupChangeOnRootMismatch
		securityContext.FSGroupChangePolicy = &onRootMismatch
	}
	return &securityContext
}
