package alertmanager

import (
	"context"
	"testing"

	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
)

func TestBuildConfig(t *testing.T) {
	type args struct {
		ctx     context.Context
		baseCfg []byte
		amcfgs  map[string]*operatorv1beta1.VMAlertmanagerConfig
	}
	tests := []struct {
		name              string
		args              args
		predefinedObjects []runtime.Object
		want              []byte
		wantErr           bool
	}{
		{
			name: "simple ok",
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": &operatorv1beta1.VMAlertmanagerConfig{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []operatorv1beta1.Receiver{
								{
									Name: "email",
									EmailConfigs: []operatorv1beta1.EmailConfig{
										{
											SendResolved: pointer.Bool(true),
											From:         "from",
										},
									},
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver:  "email",
								GroupWait: "1min",
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := BuildConfig(tt.args.ctx, testClient, tt.args.baseCfg, tt.args.amcfgs)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, string(tt.want), string(got))

		})
	}
}
