package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestAddStrictSecuritySettingsToPod(t *testing.T) {
	type opts struct {
		setup          func()
		teardown       func()
		params         *vmv1beta1.CommonAppsParams
		expected       *corev1.PodSecurityContext
		kubeletVersion version.Info
	}

	f := func(o opts) {
		t.Helper()
		if o.setup != nil {
			o.setup()
		}
		if o.teardown != nil {
			defer o.teardown()
		}
		assert.NoError(t, k8stools.SetKubernetesVersionWithDefaults(&o.kubeletVersion, 0, 0))
		defer func() {
			// return back defaults after test
			restoreVersion := version.Info{Major: "0", Minor: "0"}
			assert.NoError(t, k8stools.SetKubernetesVersionWithDefaults(&restoreVersion, 0, 0))
		}()
		res := addStrictSecuritySettingsToPod(o.params)
		assert.Equal(t, res, o.expected)
	}

	// enforce strict security
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(true),
		},
		expected: &corev1.PodSecurityContext{
			RunAsNonRoot:        ptr.To(true),
			RunAsUser:           ptr.To(int64(65534)),
			RunAsGroup:          ptr.To(int64(65534)),
			FSGroup:             ptr.To(int64(65534)),
			FSGroupChangePolicy: (*corev1.PodFSGroupChangePolicy)(ptr.To("OnRootMismatch")),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		kubeletVersion: version.Info{Major: "1", Minor: "27"},
	})

	// disable enableStrictSecurity
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(false),
		},
		expected:       nil,
		kubeletVersion: version.Info{Major: "1", Minor: "27"},
	})

	// use custom security
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(true),
			SecurityContext: &vmv1beta1.SecurityContext{
				PodSecurityContext: &corev1.PodSecurityContext{
					RunAsNonRoot: ptr.To(false),
				},
				ContainerSecurityContext: nil,
			},
		},
		expected: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr.To(false),
		},
		kubeletVersion: version.Info{Major: "1", Minor: "27"},
	})

	// openshift mode strips user/group/fsgroup from generated pod SC
	f(opts{
		setup: func() {
			config.MustGetBaseConfig().OpenshiftCompatibilityMode = "enabled"
		},
		teardown: func() {
			config.MustGetBaseConfig().OpenshiftCompatibilityMode = "auto"
		},
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(true),
		},
		expected: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr.To(true),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		kubeletVersion: version.Info{Major: "1", Minor: "27"},
	})

	// openshift mode leaves user-specified pod SC untouched
	f(opts{
		setup: func() {
			config.MustGetBaseConfig().OpenshiftCompatibilityMode = "enabled"
		},
		teardown: func() {
			config.MustGetBaseConfig().OpenshiftCompatibilityMode = "auto"
		},
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(true),
			SecurityContext: &vmv1beta1.SecurityContext{
				PodSecurityContext: &corev1.PodSecurityContext{
					RunAsUser: ptr.To[int64](1000),
				},
			},
		},
		expected: &corev1.PodSecurityContext{
			RunAsUser: ptr.To[int64](1000),
		},
		kubeletVersion: version.Info{Major: "1", Minor: "27"},
	})
}

func TestAddStrictSecuritySettingsToContainers(t *testing.T) {
	type opts struct {
		setup      func()
		teardown   func()
		params     *vmv1beta1.CommonAppsParams
		containers []corev1.Container
		expected   []corev1.Container
	}

	f := func(o opts) {
		t.Helper()
		if o.setup != nil {
			o.setup()
		}
		if o.teardown != nil {
			defer o.teardown()
		}
		AddStrictSecuritySettingsToContainers(o.containers, o.params)
		assert.Equal(t, o.expected, o.containers)
	}

	// default security
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(true),
		},
		containers: []corev1.Container{
			{
				Name: "c1",
			},
			{
				Name: "c2",
			},
		},
		expected: []corev1.Container{
			{
				Name:            "c1",
				SecurityContext: getDefaultSecurityContext(false),
			},
			{
				Name:            "c2",
				SecurityContext: getDefaultSecurityContext(false),
			},
		},
	})

	// add from spec
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(true),
			SecurityContext: &vmv1beta1.SecurityContext{
				PodSecurityContext: &corev1.PodSecurityContext{
					RunAsUser:    ptr.To[int64](1),
					RunAsNonRoot: ptr.To(false),
				},
				ContainerSecurityContext: &vmv1beta1.ContainerSecurityContext{
					Privileged: ptr.To(true),
				},
			},
		},
		containers: []corev1.Container{
			{
				Name: "c1",
			},
			{
				Name: "c2",
			},
		},
		expected: []corev1.Container{
			{
				Name: "c1",
				SecurityContext: &corev1.SecurityContext{
					RunAsUser:    ptr.To[int64](1),
					RunAsNonRoot: ptr.To(false),
					Privileged:   ptr.To(true),
				},
			},
			{
				Name: "c2",
				SecurityContext: &corev1.SecurityContext{
					RunAsUser:    ptr.To[int64](1),
					RunAsNonRoot: ptr.To(false),
					Privileged:   ptr.To(true),
				},
			},
		},
	})

	// replace defined context
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(true),
		},
		containers: []corev1.Container{
			{
				Name: "c1",
				SecurityContext: &corev1.SecurityContext{
					ReadOnlyRootFilesystem: ptr.To(false),
				},
			},
			{
				Name: "c2",
				SecurityContext: &corev1.SecurityContext{
					ReadOnlyRootFilesystem: ptr.To(false),
					RunAsUser:              ptr.To[int64](1000),
					RunAsGroup:             ptr.To[int64](1000),
				},
			},
		},
		expected: []corev1.Container{
			{
				Name:            "c1",
				SecurityContext: getDefaultSecurityContext(false),
			},
			{
				Name:            "c2",
				SecurityContext: getDefaultSecurityContext(false),
			},
		},
	})

	// replace partial security context
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(true),
		},
		containers: []corev1.Container{
			{
				Name: "c1",
				SecurityContext: &corev1.SecurityContext{
					ReadOnlyRootFilesystem: ptr.To(false),
				},
			},
			{
				Name: "c2",
			},
		},
		expected: []corev1.Container{
			{
				Name:            "c1",
				SecurityContext: getDefaultSecurityContext(false),
			},
			{
				Name:            "c2",
				SecurityContext: getDefaultSecurityContext(false),
			},
		},
	})

	// replace security context if external defined
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(true),
			SecurityContext: &vmv1beta1.SecurityContext{
				PodSecurityContext: &corev1.PodSecurityContext{
					RunAsUser: ptr.To[int64](1000),
				},
			},
		},
		containers: []corev1.Container{
			{
				Name: "c1",
				SecurityContext: &corev1.SecurityContext{
					ReadOnlyRootFilesystem: ptr.To(false),
				},
			},
			{
				Name: "c2",
			},
		},
		expected: []corev1.Container{
			{
				Name: "c1",
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: ptr.To[int64](1000),
				},
			},
			{
				Name: "c2",
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: ptr.To[int64](1000),
				},
			},
		},
	})

	// insecure mode
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(false),
		},
		containers: []corev1.Container{
			{
				Name: "c1",
			},
			{
				Name: "c2",
			},
		},
		expected: []corev1.Container{
			{
				Name: "c1",
			},
			{
				Name: "c2",
			},
		},
	})

	// add external if useStrict is false
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(false),
			SecurityContext: &vmv1beta1.SecurityContext{
				PodSecurityContext: &corev1.PodSecurityContext{
					RunAsUser: ptr.To[int64](1000),
				},
			},
		},
		containers: []corev1.Container{
			{
				Name: "c1",
			},
			{
				Name: "c2",
			},
		},
		expected: []corev1.Container{
			{
				Name: "c1",
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: ptr.To[int64](1000),
				},
			},
			{
				Name: "c2",
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: ptr.To[int64](1000),
				},
			},
		},
	})

	// replace with external if useStrict is false
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(false),
			SecurityContext: &vmv1beta1.SecurityContext{
				PodSecurityContext: &corev1.PodSecurityContext{
					RunAsUser: ptr.To[int64](1000),
				},
			},
		},
		containers: []corev1.Container{
			{
				Name: "c1",
				SecurityContext: &corev1.SecurityContext{
					ReadOnlyRootFilesystem: ptr.To(false),
				},
			},
			{
				Name: "c2",
			},
		},
		expected: []corev1.Container{
			{
				Name: "c1",
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: ptr.To[int64](1000),
				},
			},
			{
				Name: "c2",
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: ptr.To[int64](1000),
				},
			},
		},
	})

	// openshift mode strips runAsUser and runAsGroup from generated container SC
	f(opts{
		setup: func() {
			cfg := config.MustGetBaseConfig()
			cfg.OpenshiftCompatibilityMode = "enabled"
		},
		teardown: func() {
			cfg := config.MustGetBaseConfig()
			cfg.OpenshiftCompatibilityMode = "auto"
		},
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(true),
		},
		containers: []corev1.Container{{Name: "c1"}},
		expected: []corev1.Container{
			{
				Name: "c1",
				SecurityContext: &corev1.SecurityContext{
					Privileged:               ptr.To(false),
					ReadOnlyRootFilesystem:   ptr.To(true),
					AllowPrivilegeEscalation: ptr.To(false),
					RunAsNonRoot:             ptr.To(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			},
		},
	})

	// openshift mode leaves user-specified container SC untouched
	f(opts{
		setup: func() {
			cfg := config.MustGetBaseConfig()
			cfg.OpenshiftCompatibilityMode = "enabled"
		},
		teardown: func() {
			cfg := config.MustGetBaseConfig()
			cfg.OpenshiftCompatibilityMode = "auto"
		},
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(true),
			SecurityContext: &vmv1beta1.SecurityContext{
				PodSecurityContext: &corev1.PodSecurityContext{
					RunAsUser: ptr.To[int64](1000),
				},
			},
		},
		containers: []corev1.Container{{Name: "c1"}},
		expected: []corev1.Container{
			{
				Name: "c1",
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: ptr.To[int64](1000),
				},
			},
		},
	})
}

func TestWarnOpenShiftSecurityContext(t *testing.T) {
	type opts struct {
		setup      func()
		teardown   func()
		sc         *vmv1beta1.SecurityContext
		wantFields []string
	}
	f := func(o opts) {
		t.Helper()
		if o.setup != nil {
			o.setup()
		}
		if o.teardown != nil {
			defer o.teardown()
		}
		got := WarnOpenShiftSecurityContext(o.sc)
		for _, field := range o.wantFields {
			found := false
			for _, w := range got {
				if len(w) >= len(field) && w[:len(field)] == field {
					found = true
					break
				}
			}
			assert.True(t, found, "expected warning mentioning %q", field)
		}
		if len(o.wantFields) == 0 {
			assert.Empty(t, got)
		}
	}

	// not active — no warnings regardless of SC content
	f(opts{
		sc: &vmv1beta1.SecurityContext{
			PodSecurityContext: &corev1.PodSecurityContext{
				RunAsUser: ptr.To[int64](65534),
				FSGroup:   ptr.To[int64](65534),
			},
		},
	})

	// active, nil SC — no warnings
	f(opts{
		setup:    func() { config.MustGetBaseConfig().OpenshiftCompatibilityMode = "enabled" },
		teardown: func() { config.MustGetBaseConfig().OpenshiftCompatibilityMode = "auto" },
		sc:       nil,
	})

	// active, all three fields set — three warnings
	f(opts{
		setup:    func() { config.MustGetBaseConfig().OpenshiftCompatibilityMode = "enabled" },
		teardown: func() { config.MustGetBaseConfig().OpenshiftCompatibilityMode = "auto" },
		sc: &vmv1beta1.SecurityContext{
			PodSecurityContext: &corev1.PodSecurityContext{
				RunAsUser:  ptr.To[int64](65534),
				RunAsGroup: ptr.To[int64](65534),
				FSGroup:    ptr.To[int64](65534),
			},
		},
		wantFields: []string{
			"spec.securityContext.runAsUser",
			"spec.securityContext.runAsGroup",
			"spec.securityContext.fsGroup",
		},
	})

	// active via auto + detection, only runAsUser set — one warning
	f(opts{
		setup:    func() { k8stools.SetIsOpenShiftDetected(true) },
		teardown: func() { k8stools.SetIsOpenShiftDetected(false) },
		sc: &vmv1beta1.SecurityContext{
			PodSecurityContext: &corev1.PodSecurityContext{
				RunAsUser: ptr.To[int64](1000),
			},
		},
		wantFields: []string{"spec.securityContext.runAsUser"},
	})
}
