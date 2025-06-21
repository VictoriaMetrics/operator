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
	tests := []struct {
		name                 string
		podSecurityPolicy    *vmv1beta1.SecurityContext
		enableStrictSecurity bool
		exp                  *corev1.PodSecurityContext
		kubeletVersion       version.Info
		validate             func(svc *corev1.Service) error
	}{
		{
			name:                 "enforce strict security",
			enableStrictSecurity: true,
			exp: &corev1.PodSecurityContext{
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
		},
		{
			name:                 "disable enableStrictSecurity",
			enableStrictSecurity: false,
			exp:                  nil,
			kubeletVersion:       version.Info{Major: "1", Minor: "27"},
		},
		{
			name: "use custom security",
			podSecurityPolicy: &vmv1beta1.SecurityContext{
				PodSecurityContext: &corev1.PodSecurityContext{
					RunAsNonRoot: ptr.To(false),
				},
				ContainerSecurityContext: nil,
			},
			enableStrictSecurity: true,
			exp: &corev1.PodSecurityContext{
				RunAsNonRoot: ptr.To(false),
			},
			kubeletVersion: version.Info{Major: "1", Minor: "27"},
		},
	}
	for _, tt := range tests {
		if err := k8stools.SetKubernetesVersionWithDefaults(&tt.kubeletVersion, 0, 0); err != nil {
			t.Fatalf("cannot set k8s version for testing: %q", err)
		}
		defer func() {
			// return back defaults after test
			restoreVersion := version.Info{Major: "0", Minor: "0"}
			if err := k8stools.SetKubernetesVersionWithDefaults(&restoreVersion, 0, 0); err != nil {
				t.Fatalf("cannot set k8s version for testing: %q", err)
			}
		}()
		res := AddStrictSecuritySettingsToPod(tt.podSecurityPolicy, tt.enableStrictSecurity)
		if diff := deep.Equal(res, tt.exp); len(diff) > 0 {
			t.Fatalf("got unexpected result: %v, expect: %v", res, tt.exp)
		}
	}
}

func TestAddStrictSecuritySettingsToContainers(t *testing.T) {
	tests := []struct {
		name              string
		sc                *vmv1beta1.SecurityContext
		containers        []corev1.Container
		useStrictSecurity bool
		expected          []corev1.Container
	}{
		{
			name:              "default security",
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
					SecurityContext: defaultSecurityContext,
				},
				{
					Name:            "c2",
					SecurityContext: defaultSecurityContext,
				},
			},
		},
		{
			name:              "add from spec",
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
		},

		{
			name:              "replace defined context",
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
					SecurityContext: defaultSecurityContext,
				},
				{
					Name:            "c2",
					SecurityContext: defaultSecurityContext,
				},
			},
		},
		{
			name:              "replace partial security context",
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
					SecurityContext: defaultSecurityContext,
				},
				{
					Name:            "c2",
					SecurityContext: defaultSecurityContext,
				},
			},
		},
		{
			name:              "replace security context if external defined",
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
		},
		{
			name:              "insecure mode",
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
		},
		{
			name:              "add external if useStrict is false",
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
		},

		{
			name:              "replace with external if useStrict is false",
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			AddStrictSecuritySettingsToContainers(tt.sc, tt.containers, tt.useStrictSecurity)
			assert.Equal(t, tt.expected, tt.containers)
		})
	}
}
