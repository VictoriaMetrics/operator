package vmsingle

import (
	"context"
	"reflect"
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func TestCreateOrUpdateVMSingle(t *testing.T) {
	type args struct {
		cr *vmv1beta1.VMSingle
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
				cr: &vmv1beta1.VMSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmsingle-base",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMSingleSpec{ReplicaCount: ptr.To(int32(1))},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "vmsingle-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmsingle", "app.kubernetes.io/instance": "vmsingle-base", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
				},
				k8stools.NewReadyDeployment("vmsingle-vmsingle-base", "default"),
			},
			want: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "vmsingle-vmsingle-base", Namespace: "default"}},
		},
		{
			name: "base-vmsingle-with-ports",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &vmv1beta1.VMSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmsingle-base",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMSingleSpec{
						InsertPorts: &vmv1beta1.InsertPorts{
							InfluxPort:       "8051",
							OpenTSDBHTTPPort: "8052",
							GraphitePort:     "8053",
							OpenTSDBPort:     "8054",
						},
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "vmsingle-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmsingle", "app.kubernetes.io/instance": "vmsingle-base", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
				},
				k8stools.NewReadyDeployment("vmsingle-vmsingle-base", "default"),
			},
			want: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "vmsingle-vmsingle-base", Namespace: "default"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			err := CreateOrUpdateVMSingle(context.TODO(), tt.args.cr, fclient, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMSingle() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestCreateOrUpdateVMSingleService(t *testing.T) {
	type args struct {
		cr *vmv1beta1.VMSingle

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
				cr: &vmv1beta1.VMSingle{
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
				cr: &vmv1beta1.VMSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "single-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMSingleSpec{
						InsertPorts: &vmv1beta1.InsertPorts{
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
		{
			name: "with extra service nodePort",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &vmv1beta1.VMSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "single-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMSingleSpec{
						ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
							EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "additional-service"},
							Spec: corev1.ServiceSpec{
								Type: corev1.ServiceTypeNodePort,
							},
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
			wantPortsLen: 1,
			predefinedObjects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "some-svc",
						Namespace: "default",
						Labels: map[string]string{
							"app.kubernetes.io/name":      "vmsingle",
							"app.kubernetes.io/instance":  "single-1",
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
					Spec: corev1.ServiceSpec{},
				},
			},
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
