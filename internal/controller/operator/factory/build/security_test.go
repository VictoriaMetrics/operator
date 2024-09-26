package build

import (
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/stretchr/testify/assert"

	"github.com/go-test/deep"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/utils/ptr"
)

func TestAddStrictSecuritySettingsToPod(t *testing.T) {
	type args struct {
		podSecurityPolicy    *vmv1beta1.SecurityContext
		enableStrictSecurity bool
		exp                  *corev1.PodSecurityContext
		kubeletVersion       version.Info
	}
	tests := []struct {
		name     string
		args     args
		validate func(svc *corev1.Service) error
	}{
		{
			name: "enforce strict security",
			args: args{
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
		},
		{
			name: "disable enableStrictSecurity",
			args: args{
				enableStrictSecurity: false,
				exp:                  nil,
				kubeletVersion:       version.Info{Major: "1", Minor: "27"},
			},
		},
		{
			name: "use custom security",
			args: args{
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
		},
	}
	for _, tt := range tests {
		if err := k8stools.SetKubernetesVersionWithDefaults(&tt.args.kubeletVersion, 0, 0); err != nil {
			t.Fatalf("cannot set k8s version for testing: %q", err)
		}
		defer func() {
			// return back defaults after test
			restoreVersion := version.Info{Major: "0", Minor: "0"}
			if err := k8stools.SetKubernetesVersionWithDefaults(&restoreVersion, 0, 0); err != nil {
				t.Fatalf("cannot set k8s version for testing: %q", err)
			}
		}()
		res := AddStrictSecuritySettingsToPod(tt.args.podSecurityPolicy, tt.args.enableStrictSecurity)
		if diff := deep.Equal(res, tt.args.exp); len(diff) > 0 {
			t.Fatalf("got unexpected result: %v, expect: %v", res, tt.args.exp)
		}
	}
}

func TestAddStrictSecuritySettingsToContainers(t *testing.T) {

	type args struct {
		sc         *vmv1beta1.SecurityContext
		containers []corev1.Container
	}
	tests := []struct {
		name     string
		args     args
		expected []corev1.Container
	}{
		{
			name: "default security",
			args: args{
				containers: []corev1.Container{
					{
						Name: "c1",
					},
					{
						Name: "c2",
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
			name: "add from spec",
			args: args{
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
			name: "keep defined context",
			args: args{
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
			},
			expected: []corev1.Container{
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
		},
		{
			name: "partially add security context",
			args: args{
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
			},
			expected: []corev1.Container{
				{
					Name: "c1",
					SecurityContext: &corev1.SecurityContext{
						ReadOnlyRootFilesystem: ptr.To(false),
					},
				},
				{
					Name:            "c2",
					SecurityContext: defaultSecurityContext,
				},
			},
		},
		{
			name: "keep security context if external defined",
			args: args{
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
			},
			expected: []corev1.Container{
				{
					Name: "c1",
					SecurityContext: &corev1.SecurityContext{
						ReadOnlyRootFilesystem: ptr.To(false),
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
			res := AddStrictSecuritySettingsToContainers(tt.args.sc, tt.args.containers, true)
			assert.Equal(t, tt.expected, res)
		})
	}
}
