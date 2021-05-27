package factory

import (
	"testing"

	"github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	"k8s.io/utils/pointer"
)

func Test_genUserCfg(t *testing.T) {
	type args struct {
		user        *v1beta1.VMUser
		crdUrlCache map[string]string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "basic user cfg",
			args: args{
				user: &v1beta1.VMUser{
					Spec: v1beta1.VMUserSpec{
						UserName: pointer.StringPtr("basic"),
						Password: pointer.StringPtr("pass"),
						TargetRefs: []v1beta1.TargetRef{
							{
								Static: &v1beta1.StaticRef{
									URL: "http://vmselect",
								},
								Paths: []string{
									"/select/0/prometheus",
									"/select/0/graphite",
								},
							},
						},
					},
				},
			},
			want: `url_map:
- url_prefix: http://vmselect
  src_paths:
  - /select/0/prometheus
  - /select/0/graphite
username: basic
password: pass
`,
		},
		{
			name: "with crd",
			args: args{
				user: &v1beta1.VMUser{
					Spec: v1beta1.VMUserSpec{
						BearerToken: pointer.StringPtr("secret-token"),
						TargetRefs: []v1beta1.TargetRef{
							{
								CRD: &v1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "base",
									Namespace: "monitoring",
								},
								Paths: []string{
									"/api/v1/write",
									"/api/v1/targets",
									"/targets",
								},
							},
							{
								CRD: &v1beta1.CRDRef{
									Kind:      "VMSingle",
									Namespace: "monitoring",
									Name:      "db",
								},
							},
						},
					},
				},
				crdUrlCache: map[string]string{
					"VMAgent/monitoring/base": "http://vmagent-base.monitoring.svc:8429",
					"VMSingle/monitoring/db":  "http://vmsingle-b.monitoring.svc:8429",
				},
			},
			want: `url_map:
- url_prefix: http://vmagent-base.monitoring.svc:8429
  src_paths:
  - /api/v1/write
  - /api/v1/targets
  - /targets
- url_prefix: http://vmsingle-b.monitoring.svc:8429
  src_paths:
  - /
bearer_token: secret-token
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := genUserCfg(tt.args.user, tt.args.crdUrlCache)
			szd, err := yaml.Marshal(got)
			if err != nil {
				t.Fatalf("cannot serialize resutl: %v", err)
			}
			assert.Equal(t, tt.want, string(szd))
		})
	}
}
