package build

import (
	"testing"

	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/go-test/deep"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/utils/ptr"
)

func TestAddStrictSecuritySettingsToPod(t *testing.T) {
	type args struct {
		podSecurityPolicy    *v1.PodSecurityContext
		enableStrictSecurity bool
		exp                  *v1.PodSecurityContext
		kubeletVersion       version.Info
	}
	tests := []struct {
		name     string
		args     args
		validate func(svc *v1.Service) error
	}{
		{
			name: "enforce strict security",
			args: args{
				enableStrictSecurity: true,
				exp: &v1.PodSecurityContext{
					RunAsNonRoot:        ptr.To(true),
					RunAsUser:           ptr.To(int64(65534)),
					RunAsGroup:          ptr.To(int64(65534)),
					FSGroup:             ptr.To(int64(65534)),
					FSGroupChangePolicy: (*v1.PodFSGroupChangePolicy)(ptr.To("OnRootMismatch")),
					SeccompProfile: &v1.SeccompProfile{
						Type: v1.SeccompProfileTypeRuntimeDefault,
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
				podSecurityPolicy: &v1.PodSecurityContext{
					RunAsNonRoot: ptr.To(false),
				},
				enableStrictSecurity: true,
				exp: &v1.PodSecurityContext{
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
		containers           []v1.Container
		enableStrictSecurity bool
		exp                  []v1.Container
	}
	tests := []struct {
		name     string
		args     args
		validate func(svc *v1.Service) error
	}{
		{
			name: "enforce strict security",
			args: args{
				containers: []v1.Container{
					{
						Name: "c1",
						SecurityContext: &v1.SecurityContext{
							ReadOnlyRootFilesystem: ptr.To(false),
						},
					},
					{
						Name: "c2",
					},
				},
				enableStrictSecurity: true,
				exp: []v1.Container{
					{
						Name: "c1",
						SecurityContext: &v1.SecurityContext{
							ReadOnlyRootFilesystem: ptr.To(false),
						},
					},
					{
						Name: "c2",
						SecurityContext: &v1.SecurityContext{
							ReadOnlyRootFilesystem:   ptr.To(true),
							AllowPrivilegeEscalation: ptr.To(false),
							Capabilities: &v1.Capabilities{
								Drop: []v1.Capability{
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
				containers: []v1.Container{
					{
						Name: "c2",
					},
				},
				enableStrictSecurity: false,
				exp: []v1.Container{
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
