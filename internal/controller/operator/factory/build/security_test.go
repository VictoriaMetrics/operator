package build

import (
	"testing"

	"github.com/go-test/deep"
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
		k8sVersion           version.Info
	}
	f := func(opts opts) {
		t.Helper()
		if err := k8stools.SetKubernetesVersionWithDefaults(&opts.k8sVersion, 0, 0); err != nil {
			t.Fatalf("cannot set k8s version for testing: %q", err)
		}
		defer func() {
			// return back defaults after test
			restoreVersion := version.Info{Major: "0", Minor: "0"}
			if err := k8stools.SetKubernetesVersionWithDefaults(&restoreVersion, 0, 0); err != nil {
				t.Fatalf("cannot set k8s version for testing: %q", err)
			}
		}()
		res := AddStrictSecuritySettingsToPod(opts.psp, opts.enableStrictSecurity)
		if diff := deep.Equal(res, opts.expected); len(diff) > 0 {
			t.Fatalf("got unexpected result: %v, expect: %v", res, opts.expected)
		}
	}

	// enforce strict security
	o := opts{
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
		k8sVersion: version.Info{Major: "1", Minor: "27"},
	}
	f(o)

	// disable enableStrictSecurity
	o = opts{
		k8sVersion: version.Info{Major: "1", Minor: "27"},
	}
	f(o)

	// use custom security
	o = opts{
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
		k8sVersion: version.Info{Major: "1", Minor: "27"},
	}
	f(o)
}

func TestAddStrictSecuritySettingsToContainers(t *testing.T) {
	type opts struct {
		sc                *vmv1beta1.SecurityContext
		containers        []corev1.Container
		useStrictSecurity bool
		expected          []corev1.Container
	}
	f := func(opts opts) {
		t.Helper()
		AddStrictSecuritySettingsToContainers(opts.sc, opts.containers, opts.useStrictSecurity)
		assert.Equal(t, opts.expected, opts.containers)
	}

	// default security
	o := opts{
		containers: []corev1.Container{
			{
				Name: "c1",
			},
			{
				Name: "c2",
			},
		},
		useStrictSecurity: true,
		expected: []corev1.Container{
			{
				Name:            "c1",
				SecurityContext: defaultSecurityContext,
			},
			{
				Name:            "c2",
				SecurityContext: defaultSecurityContext,
			},
		},
	}
	f(o)

	// add from spec
	o = opts{
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
		useStrictSecurity: true,
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
	}
	f(o)

	// replace defined context
	o = opts{
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
		useStrictSecurity: true,
		expected: []corev1.Container{
			{
				Name:            "c1",
				SecurityContext: defaultSecurityContext,
			},
			{
				Name:            "c2",
				SecurityContext: defaultSecurityContext,
			},
		},
	}
	f(o)

	// replace partial security context
	o = opts{
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
		useStrictSecurity: true,
		expected: []corev1.Container{
			{
				Name:            "c1",
				SecurityContext: defaultSecurityContext,
			},
			{
				Name:            "c2",
				SecurityContext: defaultSecurityContext,
			},
		},
	}
	f(o)

	// replace security context if external defined
	o = opts{
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
		useStrictSecurity: true,
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
	}
	f(o)

	// insecure mode
	o = opts{
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
	}
	f(o)

	// add external if useStrict is false
	o = opts{
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
	}
	f(o)

	// replace with external if useStrict is false
	o = opts{
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
	}
	f(o)
}
