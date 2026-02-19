package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestAddStrictSecuritySettingsToPod(t *testing.T) {
	type opts struct {
		psp                  *vmv1beta1.SecurityContext
		enableStrictSecurity bool
		expected             *corev1.PodSecurityContext
		kubeletVersion       version.Info
	}

	f := func(o opts) {
		t.Helper()
		assert.NoError(t, k8stools.SetKubernetesVersionWithDefaults(&o.kubeletVersion, 0, 0))
		defer func() {
			// return back defaults after test
			restoreVersion := version.Info{Major: "0", Minor: "0"}
			assert.NoError(t, k8stools.SetKubernetesVersionWithDefaults(&restoreVersion, 0, 0))
		}()
		res := AddStrictSecuritySettingsToPod(o.psp, o.enableStrictSecurity)
		assert.Equal(t, res, o.expected)
	}

	// enforce strict security
	f(opts{
		enableStrictSecurity: true,
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
		enableStrictSecurity: false,
		expected:             nil,
		kubeletVersion:       version.Info{Major: "1", Minor: "27"},
	})

	// use custom security
	f(opts{
		psp: &vmv1beta1.SecurityContext{
			PodSecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: ptr.To(false),
			},
			ContainerSecurityContext: nil,
		},
		enableStrictSecurity: true,
		expected: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr.To(false),
		},
		kubeletVersion: version.Info{Major: "1", Minor: "27"},
	})
}

func TestAddStrictSecuritySettingsToContainers(t *testing.T) {
	type opts struct {
		sc                *vmv1beta1.SecurityContext
		containers        []corev1.Container
		useStrictSecurity bool
		expected          []corev1.Container
	}

	f := func(o opts) {
		t.Helper()
		AddStrictSecuritySettingsToContainers(o.sc, o.containers, o.useStrictSecurity)
		assert.Equal(t, o.expected, o.containers)
	}

	// default security
	f(opts{
		useStrictSecurity: true,
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
		useStrictSecurity: true,
		sc: &vmv1beta1.SecurityContext{
			PodSecurityContext: &corev1.PodSecurityContext{
				RunAsUser:    ptr.To[int64](1),
				RunAsNonRoot: ptr.To(false),
			},
			ContainerSecurityContext: &vmv1beta1.ContainerSecurityContext{
				Privileged: ptr.To(true),
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
		useStrictSecurity: true,
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
		useStrictSecurity: true,
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
		useStrictSecurity: true,
		sc: &vmv1beta1.SecurityContext{
			PodSecurityContext: &corev1.PodSecurityContext{
				RunAsUser: ptr.To[int64](1000),
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
		useStrictSecurity: false,
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
		useStrictSecurity: false,
		sc: &vmv1beta1.SecurityContext{
			PodSecurityContext: &corev1.PodSecurityContext{
				RunAsUser: ptr.To[int64](1000),
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
		useStrictSecurity: false,
		sc: &vmv1beta1.SecurityContext{
			PodSecurityContext: &corev1.PodSecurityContext{
				RunAsUser: ptr.To[int64](1000),
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
}
