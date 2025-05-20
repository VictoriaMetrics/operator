package vlsingle

import (
	"context"
	"reflect"
	"testing"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func TestCreateOrUpdateVLSingle(t *testing.T) {
	type args struct {
		cr *vmv1.VLSingle
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
			name: "base gen",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &vmv1.VLSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "base",
						Namespace: "default",
					},
					Spec: vmv1.VLSingleSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To(int32(1)),
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "vlsingle-0",
						Labels: map[string]string{
							"app.kubernetes.io/component": "monitoring",
							"app.kubernetes.io/name":      "vlsingle",
							"app.kubernetes.io/instance":  "base",
							"managed-by":                  "vm-operator",
						},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
				},
				k8stools.NewReadyDeployment("vlsingle-base", "default"),
			},
			want: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "vlsingle-base", Namespace: "default"}},
		},
		{
			name: "base with specific port",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &vmv1.VLSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "base",
						Namespace: "default",
					},
					Spec: vmv1.VLSingleSpec{

						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To(int32(1)),
						},
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
							Port: "8435",
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "vlsingle-0",
						Labels: map[string]string{
							"app.kubernetes.io/component": "monitoring",
							"app.kubernetes.io/name":      "vlsingle",
							"app.kubernetes.io/instance":  "base",
							"managed-by":                  "vm-operator",
						},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
				},
				k8stools.NewReadyDeployment("vlsingle-base", "default"),
			},
			want: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "vlsingle-base", Namespace: "default"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			err := CreateOrUpdate(context.TODO(), fclient, tt.args.cr)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVLogs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestCreateOrUpdateVLSingleService(t *testing.T) {
	type args struct {
		cr *vmv1.VLSingle

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
				cr: &vmv1.VLSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "logs-1",
						Namespace: "default",
					},
				},
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vlsingle-logs-1",
					Namespace: "default",
				},
			},
			wantPortsLen: 1,
		},
		{
			name: "with extra service nodePort",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &vmv1.VLSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "logs-1",
						Namespace: "default",
					},
					Spec: vmv1.VLSingleSpec{
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
					Name:      "vlsingle-logs-1",
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
							"app.kubernetes.io/name":      "vlsingle",
							"app.kubernetes.io/instance":  "logs-1",
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
			got, err := createOrUpdateService(context.TODO(), fclient, tt.args.cr, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVLogsService() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !reflect.DeepEqual(got.Name, tt.want.Name) {
				t.Errorf("CreateOrUpdateVLogsService(): %s", cmp.Diff(got, tt.want))
			}
			if len(got.Spec.Ports) != tt.wantPortsLen {
				t.Fatalf("unexpected number of ports: %d, want: %d", len(got.Spec.Ports), tt.wantPortsLen)
			}
		})
	}
}
