package build

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

// it's a work-around for kubernetes defaults
var _ = equality.Semantic.AddFunc(compareAppAromr)

func getDefaultPodSecurityContext(requireRoot bool) *corev1.PodSecurityContext {
	sc := &corev1.PodSecurityContext{
		FSGroup: &containerUserGroup,
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
	if !requireRoot {
		sc.RunAsNonRoot = &runNonRoot
		sc.RunAsUser = &containerUserGroup
		sc.RunAsGroup = &containerUserGroup
	}
	return sc
}

func getDefaultSecurityContext(requireRoot bool) *corev1.SecurityContext {
	sc := &corev1.SecurityContext{
		Privileged:               ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		AllowPrivilegeEscalation: ptr.To(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{
				"ALL",
			},
		},
	}
	if !requireRoot {
		sc.RunAsUser = &containerUserGroup
		sc.RunAsGroup = &containerUserGroup
		sc.RunAsNonRoot = &runNonRoot
	}
	return sc
}

var (
	// '65534' refers to 'nobody' in all the used default images like alpine, busybox
	containerUserGroup int64 = 65534
	runNonRoot               = true
)

// AddStrictSecuritySettingsToContainers conditionally adds Security settings to given containers
func AddStrictSecuritySettingsToContainers(p *vmv1beta1.SecurityContext, containers []corev1.Container, enableStrictSecurity bool) {
	if !enableStrictSecurity && p == nil {
		return
	}
	for idx := range containers {
		container := &containers[idx]
		container.SecurityContext = containerSecurityContext(p, false)
	}
}

// AddStrictSecuritySettingsWithRootToContainers conditionally adds Security settings to given containers
func AddStrictSecuritySettingsWithRootToContainers(p *vmv1beta1.SecurityContext, containers []corev1.Container, enableStrictSecurity bool) {
	if !enableStrictSecurity && p == nil {
		return
	}
	for idx := range containers {
		container := &containers[idx]
		container.SecurityContext = containerSecurityContext(p, true)
	}
}

func containerSecurityContext(p *vmv1beta1.SecurityContext, requireRoot bool) *corev1.SecurityContext {
	if p == nil {
		return getDefaultSecurityContext(requireRoot)
	}
	var sc corev1.SecurityContext
	if p.ContainerSecurityContext != nil {
		sc.Privileged = p.Privileged
		sc.Capabilities = p.Capabilities
		sc.ReadOnlyRootFilesystem = p.ReadOnlyRootFilesystem
		sc.AllowPrivilegeEscalation = p.AllowPrivilegeEscalation
		sc.ProcMount = p.ProcMount
	}
	if p.PodSecurityContext != nil {
		sc.RunAsUser = p.RunAsUser
		sc.RunAsGroup = p.RunAsGroup
		sc.RunAsNonRoot = p.RunAsNonRoot
		sc.AppArmorProfile = p.AppArmorProfile
		sc.SeccompProfile = p.SeccompProfile

	}
	return &sc
}

// AddStrictSecuritySettingsToPod conditionally creates security context for pod or returns predefined one
func AddStrictSecuritySettingsToPod(p *vmv1beta1.SecurityContext, enableStrictSecurity bool) *corev1.PodSecurityContext {
	if p != nil {
		return p.PodSecurityContext
	}
	if !enableStrictSecurity {
		return nil
	}
	securityContext := getDefaultPodSecurityContext(false)
	if k8stools.IsFSGroupChangePolicySupported() {
		onRootMismatch := corev1.FSGroupChangeOnRootMismatch
		securityContext.FSGroupChangePolicy = &onRootMismatch
	}
	return securityContext
}

// AddStrictSecuritySettingsWithRootToPod conditionally creates security context for pod or returns predefined one
func AddStrictSecuritySettingsWithRootToPod(p *vmv1beta1.SecurityContext, enableStrictSecurity bool) *corev1.PodSecurityContext {
	if p != nil {
		return p.PodSecurityContext
	}
	if !enableStrictSecurity {
		return nil
	}
	securityContext := getDefaultPodSecurityContext(true)
	if k8stools.IsFSGroupChangePolicySupported() {
		onRootMismatch := corev1.FSGroupChangeOnRootMismatch
		securityContext.FSGroupChangePolicy = &onRootMismatch
	}
	return securityContext
}

// Kubernetes acts tricky with AppArmorProfile
// it doesn't assign it into the Statefulset if it has default values
func compareAppAromr(left, right *corev1.AppArmorProfile) bool {
	if left == nil && right == nil {
		// fast path
		return true
	}
	if left != nil && right != nil {
		// compare
		return equality.Semantic.DeepDerivative(left.Type, right.Type) && equality.Semantic.DeepDerivative(left.LocalhostProfile, right.LocalhostProfile)
	}

	// check in-variants
	if left != nil &&
		(left.Type == corev1.AppArmorProfileTypeRuntimeDefault ||
			left.Type == corev1.AppArmorProfileTypeUnconfined) {
		return true
	}
	if right != nil &&
		(right.Type == corev1.AppArmorProfileTypeRuntimeDefault ||
			right.Type == corev1.AppArmorProfileTypeUnconfined) {
		return true
	}

	return false
}
