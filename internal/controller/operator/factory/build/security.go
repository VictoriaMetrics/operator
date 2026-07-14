package build

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
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

// IsOpenShiftCompatibilityActive reports whether OpenShift-specific adaptations should be applied.
func IsOpenShiftCompatibilityActive() bool {
	mode := config.MustGetBaseConfig().OpenshiftCompatibilityMode
	return mode == "enabled" || (mode == "auto" && k8stools.IsOpenShiftDetected)
}

// WarnOpenShiftClusterSpec returns OpenShift security context warnings for all sub-components
// of a VMClusterSpec (VMSelect, VMInsert, VMStorage, RequestsLoadBalancer).
func WarnOpenShiftClusterSpec(spec *vmv1beta1.VMClusterSpec) []string {
	var w []string
	if c := spec.VMSelect; c != nil {
		w = append(w, WarnOpenShiftSecurityContext(c.SecurityContext)...)
	}
	if c := spec.VMInsert; c != nil {
		w = append(w, WarnOpenShiftSecurityContext(c.SecurityContext)...)
	}
	if c := spec.VMStorage; c != nil {
		w = append(w, WarnOpenShiftSecurityContext(c.SecurityContext)...)
	}
	if spec.RequestsLoadBalancer.Enabled {
		w = append(w, WarnOpenShiftSecurityContext(spec.RequestsLoadBalancer.Spec.SecurityContext)...)
	}
	return w
}

// WarnOpenShiftVLClusterSpec returns OpenShift security context warnings for all sub-components
// of a VLClusterSpec (VLInsert, VLSelect, VLStorage, RequestsLoadBalancer).
func WarnOpenShiftVLClusterSpec(spec *vmv1.VLClusterSpec) []string {
	var w []string
	if c := spec.VLInsert; c != nil {
		w = append(w, WarnOpenShiftSecurityContext(c.SecurityContext)...)
	}
	if c := spec.VLSelect; c != nil {
		w = append(w, WarnOpenShiftSecurityContext(c.SecurityContext)...)
	}
	if c := spec.VLStorage; c != nil {
		w = append(w, WarnOpenShiftSecurityContext(c.SecurityContext)...)
	}
	if spec.RequestsLoadBalancer.Enabled {
		w = append(w, WarnOpenShiftSecurityContext(spec.RequestsLoadBalancer.Spec.SecurityContext)...)
	}
	return w
}

// WarnOpenShiftVTClusterSpec returns OpenShift security context warnings for all sub-components
// of a VTClusterSpec (Insert, Select, Storage, RequestsLoadBalancer).
func WarnOpenShiftVTClusterSpec(spec *vmv1.VTClusterSpec) []string {
	var w []string
	if c := spec.Insert; c != nil {
		w = append(w, WarnOpenShiftSecurityContext(c.SecurityContext)...)
	}
	if c := spec.Select; c != nil {
		w = append(w, WarnOpenShiftSecurityContext(c.SecurityContext)...)
	}
	if c := spec.Storage; c != nil {
		w = append(w, WarnOpenShiftSecurityContext(c.SecurityContext)...)
	}
	if spec.RequestsLoadBalancer.Enabled {
		w = append(w, WarnOpenShiftSecurityContext(spec.RequestsLoadBalancer.Spec.SecurityContext)...)
	}
	return w
}

// WarnOpenShiftSecurityContext returns warning messages for each security context field
// that is explicitly set but would cause pod admission failures on OpenShift when the
// restricted-v2 SCC assigns UIDs/GIDs from the namespace-allocated range.
// Returns nil when OpenShift compatibility is not active or sc is nil.
func WarnOpenShiftSecurityContext(sc *vmv1beta1.SecurityContext) []string {
	if !IsOpenShiftCompatibilityActive() || sc == nil || sc.PodSecurityContext == nil {
		return nil
	}
	psc := sc.PodSecurityContext
	var warnings []string
	if psc.RunAsUser != nil {
		warnings = append(warnings, "spec.securityContext.runAsUser is explicitly set; in OpenShift setting  this value may require additional SCC permission - or fall within the namespace-allocated UID range otherwise pod admission will fail; consider removing it and letting the controller assign the UID")
	}
	if psc.RunAsGroup != nil {
		warnings = append(warnings, "spec.securityContext.runAsGroup is explicitly set; in OpenShift setting  this value may require additional SCC permissions - or fall within the namespace-allocated GID range otherwise pod admission will fail; consider removing it and letting the controller assign the GID")
	}
	if psc.FSGroup != nil {
		warnings = append(warnings, "spec.securityContext.fsGroup is explicitly set; in OpenShift setting  this value may require additional SCC permissions - or fall within the namespace-allocated supplemental group range otherwise pod admission will fail; consider removing it and letting the controller assign the fsGroup")
	}
	return warnings
}

// AddStrictSecuritySettingsToContainers conditionally adds Security settings to given containers
func AddStrictSecuritySettingsToContainers(containers []corev1.Container, params *vmv1beta1.CommonAppsParams) {
	if !ptr.Deref(params.UseStrictSecurity, false) && (params == nil || params.SecurityContext == nil) {
		return
	}
	openShift := params.SecurityContext == nil && IsOpenShiftCompatibilityActive()
	for idx := range containers {
		container := &containers[idx]
		container.SecurityContext = containerSecurityContext(params.SecurityContext, false)
		if openShift {
			container.SecurityContext.RunAsUser = nil
			container.SecurityContext.RunAsGroup = nil
		}
	}
}

// AddStrictSecuritySettingsWithRootToContainers conditionally adds Security settings to given containers
func AddStrictSecuritySettingsWithRootToContainers(containers []corev1.Container, params *vmv1beta1.CommonAppsParams) {
	if !ptr.Deref(params.UseStrictSecurity, false) && (params == nil || params.SecurityContext == nil) {
		return
	}
	openShift := params.SecurityContext == nil && IsOpenShiftCompatibilityActive()
	for idx := range containers {
		container := &containers[idx]
		container.SecurityContext = containerSecurityContext(params.SecurityContext, true)
		if openShift {
			container.SecurityContext.RunAsUser = nil
			container.SecurityContext.RunAsGroup = nil
		}
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

// addStrictSecuritySettingsToPod conditionally creates security context for pod or returns predefined one
func addStrictSecuritySettingsToPod(params *vmv1beta1.CommonAppsParams) *corev1.PodSecurityContext {
	if params != nil && params.SecurityContext != nil {
		return params.SecurityContext.PodSecurityContext
	}
	if !ptr.Deref(params.UseStrictSecurity, false) {
		return nil
	}
	securityContext := getDefaultPodSecurityContext(false)
	if k8stools.IsFSGroupChangePolicySupported() {
		onRootMismatch := corev1.FSGroupChangeOnRootMismatch
		securityContext.FSGroupChangePolicy = &onRootMismatch
	}
	if IsOpenShiftCompatibilityActive() {
		securityContext.RunAsUser = nil
		securityContext.RunAsGroup = nil
		securityContext.FSGroup = nil
		securityContext.FSGroupChangePolicy = nil
	}
	return securityContext
}

// addStrictSecuritySettingsWithRootToPod conditionally creates security context for pod or returns predefined one
func addStrictSecuritySettingsWithRootToPod(params *vmv1beta1.CommonAppsParams) *corev1.PodSecurityContext {
	if params != nil && params.SecurityContext != nil {
		return params.SecurityContext.PodSecurityContext
	}
	if !ptr.Deref(params.UseStrictSecurity, false) {
		return nil
	}
	securityContext := getDefaultPodSecurityContext(true)
	if k8stools.IsFSGroupChangePolicySupported() {
		onRootMismatch := corev1.FSGroupChangeOnRootMismatch
		securityContext.FSGroupChangePolicy = &onRootMismatch
	}
	if IsOpenShiftCompatibilityActive() {
		securityContext.FSGroup = nil
		securityContext.FSGroupChangePolicy = nil
	}
	return securityContext
}

// AddDefaultStorageFSGroupToPod ensures a pod that mounts a PersistentVolume can write to it.
// When neither useStrictSecurity nor a user securityContext is set, the pod gets no securityContext
// and the container process (uid 65534 in the default images) cannot write to the volume.
// It defaults fsGroup to 65534 in that case and leaves any operator- or user-provided pod
// securityContext untouched. It is a no-op under OpenShift, where the assigned SCC manages fsGroup.
func AddDefaultStorageFSGroupToPod(dst *corev1.PodTemplateSpec, params *vmv1beta1.CommonAppsParams) {
	if dst.Spec.SecurityContext != nil {
		return
	}
	if params != nil && params.SecurityContext != nil {
		return
	}
	if ptr.Deref(params.UseStrictSecurity, false) {
		return
	}
	if IsOpenShiftCompatibilityActive() {
		return
	}
	securityContext := &corev1.PodSecurityContext{
		FSGroup: &containerUserGroup,
	}
	if k8stools.IsFSGroupChangePolicySupported() {
		onRootMismatch := corev1.FSGroupChangeOnRootMismatch
		securityContext.FSGroupChangePolicy = &onRootMismatch
	}
	dst.Spec.SecurityContext = securityContext
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
