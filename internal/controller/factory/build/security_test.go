package build

import (
	"testing"

	"github.com/VictoriaMetrics/operator/internal/controller/factory/k8stools"
	"github.com/go-test/deep"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/utils/ptr"
)

func TestAddStrictSecuritySettingsToPod(t *testing.T) {
	type args struct {
		podSecurityPolicy    *corev1.PodSecurityContext
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
				podSecurityPolicy: &corev1.PodSecurityContext{
					RunAsNonRoot: ptr.To(false),
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
		res := AddStrictSecuritySettingsToPod(tt.args.podSecurityPolicy, tt.args.enableStrictSecurity)
		if diff := deep.Equal(res, tt.args.exp); len(diff) > 0 {
			t.Fatalf("got unexpected result: %v, expect: %v", res, tt.args.exp)
		}
	}
}

func TestAddStrictSecuritySettingsToContainers(t *testing.T) {
	type args struct {
		containers           []corev1.Container
		enableStrictSecurity bool
		exp                  []corev1.Container
	}
	tests := []struct {
		name     string
		args     args
		validate func(svc *corev1.Service) error
	}{
		{
			name: "enforce strict security",
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
				enableStrictSecurity: true,
				exp: []corev1.Container{
					{
						Name: "c1",
						SecurityContext: &corev1.SecurityContext{
							ReadOnlyRootFilesystem: ptr.To(false),
						},
					},
					{
						Name: "c2",
						SecurityContext: &corev1.SecurityContext{
							ReadOnlyRootFilesystem:   ptr.To(true),
							AllowPrivilegeEscalation: ptr.To(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{
									"ALL",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "disable enableStrictSecurity",
			args: args{
				containers: []corev1.Container{
					{
						Name: "c2",
					},
				},
				enableStrictSecurity: false,
				exp: []corev1.Container{
					{
						Name: "c2",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		res := AddStrictSecuritySettingsToContainers(tt.args.containers, tt.args.enableStrictSecurity)
		if diff := deep.Equal(res, tt.args.exp); len(diff) > 0 {
			t.Fatalf("got unexpected result: %v, expect: %v", res, tt.args.exp)
		}
	}
}
