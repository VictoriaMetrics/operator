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
	f := func(psp *vmv1beta1.SecurityContext, enableStrictSecurity bool, expected *corev1.PodSecurityContext, k8sVersion version.Info) {
		t.Helper()
		if err := k8stools.SetKubernetesVersionWithDefaults(&k8sVersion, 0, 0); err != nil {
			t.Fatalf("cannot set k8s version for testing: %q", err)
		}
		defer func() {
			// return back defaults after test
			restoreVersion := version.Info{Major: "0", Minor: "0"}
			if err := k8stools.SetKubernetesVersionWithDefaults(&restoreVersion, 0, 0); err != nil {
				t.Fatalf("cannot set k8s version for testing: %q", err)
			}
		}()
		res := AddStrictSecuritySettingsToPod(psp, enableStrictSecurity)
		if diff := deep.Equal(res, expected); len(diff) > 0 {
			t.Fatalf("got unexpected result: %v, expect: %v", res, expected)
		}
	}

	// enforce strict security
	f(nil, true, &corev1.PodSecurityContext{
		RunAsNonRoot:        ptr.To(true),
		RunAsUser:           ptr.To(int64(65534)),
		RunAsGroup:          ptr.To(int64(65534)),
		FSGroup:             ptr.To(int64(65534)),
		FSGroupChangePolicy: (*corev1.PodFSGroupChangePolicy)(ptr.To("OnRootMismatch")),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}, version.Info{Major: "1", Minor: "27"})

	// disable enableStrictSecurity
	f(nil, false, nil, version.Info{Major: "1", Minor: "27"})

	// use custom security
	f(&vmv1beta1.SecurityContext{
		PodSecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr.To(false),
		},
		ContainerSecurityContext: nil,
	}, true, &corev1.PodSecurityContext{
		RunAsNonRoot: ptr.To(false),
	}, version.Info{Major: "1", Minor: "27"})
}

func TestAddStrictSecuritySettingsToContainers(t *testing.T) {
	f := func(sc *vmv1beta1.SecurityContext, containers []corev1.Container, useStrictSecurity bool, expected []corev1.Container) {
		t.Helper()
		AddStrictSecuritySettingsToContainers(sc, containers, useStrictSecurity)
		assert.Equal(t, expected, containers)
	}

	// default security
	f(nil, []corev1.Container{
		{
			Name: "c1",
		},
		{
			Name: "c2",
		},
	}, true, []corev1.Container{
		{
			Name:            "c1",
			SecurityContext: defaultSecurityContext,
		},
		{
			Name:            "c2",
			SecurityContext: defaultSecurityContext,
		},
	})

	// add from spec
	f(&vmv1beta1.SecurityContext{
		PodSecurityContext: &corev1.PodSecurityContext{
			RunAsUser:    ptr.To[int64](1),
			RunAsNonRoot: ptr.To(false),
		},
		ContainerSecurityContext: &vmv1beta1.ContainerSecurityContext{
			Privileged: ptr.To(true),
		},
	}, []corev1.Container{
		{
			Name: "c1",
		},
		{
			Name: "c2",
		},
	}, true, []corev1.Container{
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
	})

	// replace defined context
	f(nil, []corev1.Container{
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
	}, true, []corev1.Container{
		{
			Name:            "c1",
			SecurityContext: defaultSecurityContext,
		},
		{
			Name:            "c2",
			SecurityContext: defaultSecurityContext,
		},
	})

	// replace partial security context
	f(nil, []corev1.Container{
		{
			Name: "c1",
			SecurityContext: &corev1.SecurityContext{
				ReadOnlyRootFilesystem: ptr.To(false),
			},
		},
		{
			Name: "c2",
		},
	}, true, []corev1.Container{
		{
			Name:            "c1",
			SecurityContext: defaultSecurityContext,
		},
		{
			Name:            "c2",
			SecurityContext: defaultSecurityContext,
		},
	})

	// replace security context if external defined
	f(&vmv1beta1.SecurityContext{
		PodSecurityContext: &corev1.PodSecurityContext{
			RunAsUser: ptr.To[int64](1000),
		},
	}, []corev1.Container{
		{
			Name: "c1",
			SecurityContext: &corev1.SecurityContext{
				ReadOnlyRootFilesystem: ptr.To(false),
			},
		},
		{
			Name: "c2",
		},
	}, true, []corev1.Container{
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
	})

	// insecure mode
	f(nil, []corev1.Container{
		{
			Name: "c1",
		},
		{
			Name: "c2",
		},
	}, false, []corev1.Container{
		{
			Name: "c1",
		},
		{
			Name: "c2",
		},
	})

	// add external if useStrict is false
	f(&vmv1beta1.SecurityContext{
		PodSecurityContext: &corev1.PodSecurityContext{
			RunAsUser: ptr.To[int64](1000),
		},
	}, []corev1.Container{
		{
			Name: "c1",
		},
		{
			Name: "c2",
		},
	}, false, []corev1.Container{
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
	})

	// replace with external if useStrict is false
	f(&vmv1beta1.SecurityContext{
		PodSecurityContext: &corev1.PodSecurityContext{
			RunAsUser: ptr.To[int64](1000),
		},
	}, []corev1.Container{
		{
			Name: "c1",
			SecurityContext: &corev1.SecurityContext{
				ReadOnlyRootFilesystem: ptr.To(false),
			},
		},
		{
			Name: "c2",
		},
	}, false, []corev1.Container{
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
	})
}
