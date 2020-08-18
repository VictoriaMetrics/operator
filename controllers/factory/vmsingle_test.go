package factory

import (
	"context"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := []runtime.Object{}
			obj = append(obj, tt.predefinedObjects...)
			fclient := fake.NewFakeClientWithScheme(testGetScheme(), obj...)
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := []runtime.Object{}
			obj = append(obj, tt.predefinedObjects...)
			fclient := fake.NewFakeClientWithScheme(testGetScheme(), obj...)
			got, err := CreateOrUpdateVMSingleService(context.TODO(), tt.args.cr, fclient, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMSingleService() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Name, tt.want.Name) {
				t.Errorf("CreateOrUpdateVMSingleService() got = %v, want %v", got, tt.want)
			}
		})
	}
}
