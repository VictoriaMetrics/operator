package vmauth

import (
	"context"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func TestCreateOrUpdateVMAuth(t *testing.T) {
	mutateConf := func(cb func(c *config.BaseOperatorConf)) *config.BaseOperatorConf {
		c := config.MustGetBaseConfig()
		cb(c)
		return c
	}
	type args struct {
		cr *victoriametricsv1beta1.VMAuth
		c  *config.BaseOperatorConf
	}
	tests := []struct {
		name              string
		args              args
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "simple-unmanaged",
			args: args{
				cr: &victoriametricsv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmauth-test", "default"),
			},
		},
		{
			name: "simple-with-external-config",
			args: args{
				cr: &victoriametricsv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAuthSpec{
						ConfigSecret: "external-cfg",
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmauth-test", "default"),
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "external-cfg",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"config.yaml": {},
					},
				},
			},
		},
		{
			name: "with-match-all",
			args: args{
				cr: &victoriametricsv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAuthSpec{
						SelectAllByDefault: true,
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmauth-test", "default"),
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						UserName: ptr.To("user-1"),
						Password: ptr.To("password-1"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: &victoriametricsv1beta1.StaticRef{
									URLs: []string{"http://url-1", "http://url-2"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with customer config reloader",
			args: args{
				cr: &victoriametricsv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAuthSpec{
						SelectAllByDefault: true,
					},
				},
				c: mutateConf(func(c *config.BaseOperatorConf) {
					c.UseCustomConfigReloader = true
				}),
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmauth-test", "default"),
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						UserName: ptr.To("user-1"),
						Password: ptr.To("password-1"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: &victoriametricsv1beta1.StaticRef{
									URLs: []string{"http://url-1", "http://url-2"},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tc := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			if err := CreateOrUpdateVMAuth(ctx, tt.args.cr, tc, tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMAuth() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
