package factory

import (
	"context"
	"reflect"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestCreateOrUpdateVMSingle(t *testing.T) {
	type args struct {
		cr *victoriametricsv1beta1.VMSingle
		c  *config.BaseOperatorConf
	}
	tests := []struct {
		name              string
		args              args
		want              *appsv1.Deployment
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "base-vmsingle-gen",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &victoriametricsv1beta1.VMSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmsingle-base",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMSingleSpec{},
				},
			},
			want: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "vmsingle-vmsingle-base", Namespace: "default"}},
		},
		{
			name: "base-vmsingle-with-ports",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &victoriametricsv1beta1.VMSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmsingle-base",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMSingleSpec{
						InsertPorts: &victoriametricsv1beta1.InsertPorts{
							InfluxPort:       "8051",
							OpenTSDBHTTPPort: "8052",
							GraphitePort:     "8053",
							OpenTSDBPort:     "8054",
						},
					},
				},
			},
			want: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "vmsingle-vmsingle-base", Namespace: "default"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := CreateOrUpdateVMSingle(context.TODO(), tt.args.cr, fclient, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMSingle() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Name, tt.want.Name) {
				t.Errorf("CreateOrUpdateVMSingle() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateOrUpdateVMSingleService(t *testing.T) {
	type args struct {
		cr *victoriametricsv1beta1.VMSingle

		c *config.BaseOperatorConf
	}
	tests := []struct {
		name              string
		args              args
		want              *corev1.Service
		wantErr           bool
		wantPortsLen      int
		predefinedObjects []runtime.Object
	}{
		{
			name: "base service test",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &victoriametricsv1beta1.VMSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "single-1",
						Namespace: "default",
					},
				},
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmsingle-single-1",
					Namespace: "default",
				},
			},
			wantPortsLen: 1,
		},
		{
			name: "base service test-with ports",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &victoriametricsv1beta1.VMSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "single-1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMSingleSpec{
						InsertPorts: &victoriametricsv1beta1.InsertPorts{
							InfluxPort:       "8051",
							OpenTSDBHTTPPort: "8052",
							GraphitePort:     "8053",
							OpenTSDBPort:     "8054",
						},
					},
				},
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmsingle-single-1",
					Namespace: "default",
				},
			},
			wantPortsLen: 8,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := CreateOrUpdateVMSingleService(context.TODO(), tt.args.cr, fclient, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMSingleService() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Name, tt.want.Name) {
				t.Errorf("CreateOrUpdateVMSingleService() got = %v, want %v", got, tt.want)
			}
			if len(got.Spec.Ports) != tt.wantPortsLen {
				t.Fatalf("unexpected number of ports: %d, want: %d", len(got.Spec.Ports), tt.wantPortsLen)
			}
		})
	}
}
