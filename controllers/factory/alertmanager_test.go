package factory

import (
	"context"
	"reflect"
	"testing"

	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/pointer"

	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func Test_createDefaultAMConfig(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *victoriametricsv1beta1.VMAlertmanager
	}
	tests := []struct {
		name              string
		args              args
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "create alertmanager config",
			args: args{
				ctx: context.TODO(),
				cr: &victoriametricsv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-am",
					},
					Spec: victoriametricsv1beta1.VMAlertmanagerSpec{},
				},
			},
			predefinedObjects: []runtime.Object{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			if err := createDefaultAMConfig(tt.args.ctx, tt.args.cr, fclient); (err != nil) != tt.wantErr {
				t.Fatalf("createDefaultAMConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			var createdSecret v1.Secret
			err := fclient.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.PrefixedName()}, &createdSecret)
			if err != nil {
				t.Fatalf("config for alertmanager not exist, err: %v", err)
			}
		})
	}
}

func TestCreateOrUpdateAlertManager(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *victoriametricsv1beta1.VMAlertmanager
		c   *config.BaseOperatorConf
	}
	tests := []struct {
		name             string
		args             args
		want             *appsv1.StatefulSet
		wantErr          bool
		predefinedObjets []runtime.Object
	}{
		{
			name: "simple alertmanager",
			args: args{
				ctx: context.TODO(),
				c:   config.MustGetBaseConfig(),
				cr: &victoriametricsv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "test-am",
						Namespace:   "monitoring",
						Annotations: map[string]string{"not": "touch"},
						Labels:      map[string]string{"main": "system"},
					},
					Spec: victoriametricsv1beta1.VMAlertmanagerSpec{
						ReplicaCount: pointer.Int32Ptr(1),
					},
				},
			},
			wantErr: false,
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "vmalertmanager-test-am",
					Namespace:   "monitoring",
					Annotations: map[string]string{"not": "touch"},
					Labels: map[string]string{
						"app.kubernetes.io/component": "monitoring",
						"app.kubernetes.io/instance":  "test-am",
						"app.kubernetes.io/name":      "vmalertmanager",
						"managed-by":                  "vm-operator",
						"main":                        "system",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: pointer.Int32Ptr(1),
					Template: v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{Name: "alertmanager"},
								{Name: "config-reloader"},
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjets)
			got, err := CreateOrUpdateAlertManager(tt.args.ctx, tt.args.cr, fclient, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CreateOrUpdateAlertManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			k8stools.CompareObjectMeta(t, got.ObjectMeta, tt.want.ObjectMeta)
			if !reflect.DeepEqual(len(got.Spec.Template.Spec.Containers), len(tt.want.Spec.Template.Spec.Containers)) {
				t.Errorf("unexpected containers count: \ngot = %v, \nwant %v, \n got containers: %v", len(got.Spec.Template.Spec.Containers), len(tt.want.Spec.Template.Spec.Containers), got.Spec.Template.Spec.Containers)
			}
		})
	}
}
