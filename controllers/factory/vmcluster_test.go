package factory

import (
	"context"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"k8s.io/utils/ptr"
)

func Test_waitForExpanding(t *testing.T) {
	type args struct {
		namespace    string
		lbs          map[string]string
		desiredCount int32
	}
	tests := []struct {
		name              string
		args              args
		want              bool
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "is not expanding",
			args: args{
				namespace:    "default",
				lbs:          map[string]string{"app": "example-app"},
				desiredCount: 2,
			},
			predefinedObjects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod1",
						Labels:    map[string]string{"app": "example-app"},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: "True"},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod2",
						Labels:    map[string]string{"app": "example-app"},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: "True"},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "another-pod",
						Labels:    map[string]string{"app": "some-other-app"},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: "True"},
						},
					},
				},
			},
			wantErr: false,
			want:    false,
		},
		{
			name: "pods is failed",
			args: args{
				namespace:    "default",
				lbs:          map[string]string{"app": "example-app"},
				desiredCount: 2,
			},
			predefinedObjects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod1",
						Labels:    map[string]string{"app": "example-app"},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: "True"},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod2",
						Labels:    map[string]string{"app": "example-app"},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodFailed,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: "False"},
						},
						ContainerStatuses: []corev1.ContainerStatus{
							{
								LastTerminationState: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										Reason:  "fail",
										Message: "Incorrect flag",
									},
								},
							},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "another-pod",
						Labels:    map[string]string{"app": "some-other-app"},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodFailed,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: "False"},
						},
					},
				},
			},
			wantErr: true,
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			err := waitExpanding(context.Background(), fclient, tt.args.namespace, tt.args.lbs, tt.args.desiredCount, 0, time.Second*2)
			if (err != nil) != tt.wantErr {
				t.Errorf("waitExpanding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestCreateOrUpdateVMCluster(t *testing.T) {
	type args struct {
		cr *v1beta1.VMCluster
		c  *config.BaseOperatorConf
	}
	tests := []struct {
		name              string
		args              args
		want              string
		wantErr           bool
		predefinedObjects []runtime.Object
		validate          func(vminsert *appsv1.Deployment, vmselect, vmstorage *appsv1.StatefulSet) error
	}{
		{
			name: "base-vmstorage-test",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &v1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "cluster-1",
						Labels:    map[string]string{"label": "value"},
					},
					Spec: v1beta1.VMClusterSpec{
						RetentionPeriod:   "2",
						ReplicationFactor: pointer.Int32Ptr(0),
						VMInsert: &v1beta1.VMInsert{
							PodMetadata: &v1beta1.EmbeddedObjectMetadata{
								Annotations: map[string]string{"key": "value"},
							},
							ReplicaCount: pointer.Int32Ptr(0),
						},
						VMStorage: &v1beta1.VMStorage{
							PodMetadata: &v1beta1.EmbeddedObjectMetadata{
								Annotations: map[string]string{"key": "value"},
								Labels:      map[string]string{"label": "value2"},
							},
							ReplicaCount: pointer.Int32Ptr(2),
						},
						VMSelect: &v1beta1.VMSelect{
							PodMetadata: &v1beta1.EmbeddedObjectMetadata{
								Annotations: map[string]string{"key": "value"},
							},
							ReplicaCount: pointer.Int32Ptr(2),
						},
					},
				},
			},
			want: string(v1beta1.UpdateStatusExpanding),
			predefinedObjects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-1", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
			},
		},
		{
			name: "base-vminsert-with-ports",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &v1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "cluster-1",
					},
					Spec: v1beta1.VMClusterSpec{
						RetentionPeriod:   "2",
						ReplicationFactor: pointer.Int32Ptr(2),
						VMInsert: &v1beta1.VMInsert{
							ReplicaCount: pointer.Int32Ptr(0),
							InsertPorts: &v1beta1.InsertPorts{
								GraphitePort:     "8025",
								OpenTSDBHTTPPort: "3311",
								InfluxPort:       "5511",
							},
							HPA: &v1beta1.EmbeddedHPA{
								MinReplicas: pointer.Int32Ptr(0),
								MaxReplicas: 3,
							},
						},
					},
				},
			},
			want: string(v1beta1.UpdateStatusExpanding),
		},
		{
			name: "base-vmselect",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &v1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "cluster-1",
					},
					Spec: v1beta1.VMClusterSpec{
						RetentionPeriod:   "2",
						ReplicationFactor: pointer.Int32Ptr(2),
						VMSelect: &v1beta1.VMSelect{
							ReplicaCount: pointer.Int32Ptr(2),
							HPA: &v1beta1.EmbeddedHPA{
								MinReplicas: pointer.Int32Ptr(1),
								MaxReplicas: 3,
							},
						},
					},
				},
			},
			want: string(v1beta1.UpdateStatusExpanding),
			predefinedObjects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "select-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmselect", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "select-1", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmselect", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
			},
		},
		{
			name: "base-vmstorage-with-maintenance",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &v1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "cluster-1",
					},
					Spec: v1beta1.VMClusterSpec{
						RetentionPeriod:   "2",
						ReplicationFactor: pointer.Int32Ptr(2),
						VMInsert: &v1beta1.VMInsert{
							ReplicaCount: pointer.Int32Ptr(0),
						},
						VMStorage: &v1beta1.VMStorage{
							MaintenanceSelectNodeIDs: []int32{1, 3},
							MaintenanceInsertNodeIDs: []int32{0, 1, 2},
							ReplicaCount:             pointer.Int32Ptr(10),
						},
						VMSelect: &v1beta1.VMSelect{
							ReplicaCount: pointer.Int32Ptr(2),
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-1", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-2", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-3", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-4", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-5", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-6", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-7", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-8", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-9", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
			},
			want: string(v1beta1.UpdateStatusExpanding),
		},
		{
			name: "base-vmstorage-with-maintenance",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &v1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "cluster-1",
					},
					Spec: v1beta1.VMClusterSpec{
						RetentionPeriod:   "2",
						ReplicationFactor: pointer.Int32Ptr(2),
						VMInsert: &v1beta1.VMInsert{
							ReplicaCount: pointer.Int32Ptr(0),
						},
						VMStorage: &v1beta1.VMStorage{
							MaintenanceSelectNodeIDs: []int32{1, 3},
							MaintenanceInsertNodeIDs: []int32{0, 1, 2},
							ReplicaCount:             pointer.Int32Ptr(10),
						},
						VMSelect: &v1beta1.VMSelect{
							ReplicaCount: pointer.Int32Ptr(2),
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-1", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-2", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-3", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-4", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-5", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-6", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-7", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-8", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-9", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}},
				},
			},
			want: string(v1beta1.UpdateStatusExpanding),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			err := CreateOrUpdateVMCluster(context.TODO(), tt.args.cr, fclient, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMCluster() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.validate != nil {
				var vmselect, vmstorage appsv1.StatefulSet
				var vminsert appsv1.Deployment
				if tt.args.cr.Spec.VMInsert != nil {
					if err := fclient.Get(context.TODO(), types.NamespacedName{Name: tt.args.cr.Spec.VMInsert.GetNameWithPrefix(tt.args.cr.Name), Namespace: tt.args.cr.Namespace}, &vminsert); err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
				}
				if tt.args.cr.Spec.VMSelect != nil {
					if err := fclient.Get(context.TODO(), types.NamespacedName{Name: tt.args.cr.Spec.VMSelect.GetNameWithPrefix(tt.args.cr.Name), Namespace: tt.args.cr.Namespace}, &vmselect); err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
				}
				if tt.args.cr.Spec.VMStorage != nil {
					if err := fclient.Get(context.TODO(), types.NamespacedName{Name: tt.args.cr.Spec.VMStorage.GetNameWithPrefix(tt.args.cr.Name), Namespace: tt.args.cr.Namespace}, &vmstorage); err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
				}

				if err := tt.validate(&vminsert, &vmselect, &vmstorage); err != nil {
					t.Fatalf("validation for cluster failed: %v", err)
				}
			}
		})
	}
}

func TestCreatOrUpdateClusterServices(t *testing.T) {
	f := func(component string, cr *v1beta1.VMCluster, wantSvcYAML string, predefinedObjects ...runtime.Object) {
		t.Helper()
		ctx := context.Background()
		cfg := config.MustGetBaseConfig()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)

		var builderF func(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error)
		switch component {
		case "insert":
			builderF = createOrUpdateVMInsertService
		case "storage":
			builderF = createOrUpdateVMStorageService
		case "select":
			builderF = createOrUpdateVMSelectService

		default:
			t.Fatalf("BUG not expected component for test: %q", component)
		}
		svc, err := builderF(ctx, cr, fclient, cfg)
		if err != nil {
			t.Fatalf("not expected error= %q", err)
		}
		var actualService corev1.Service
		if err := fclient.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}, &actualService); err != nil {
			t.Fatalf("create service not found: %q", err)
		}
		var wantService corev1.Service
		if err := yaml.Unmarshal([]byte(wantSvcYAML), &wantService); err != nil {
			t.Fatalf("BUG: expect service definition at yaml: %q", err)
		}
		gotYAML, err := yaml.Marshal(actualService)
		if err != nil {
			t.Fatalf("BUG: cannot serialize service as yaml")
		}
		if !cmp.Equal(&actualService, &wantService) {
			diff := cmp.Diff(&actualService, &wantService)
			t.Fatalf("not expected service, diff: \n%s\ngot yaml:\n%s", diff, string(gotYAML))
		}
	}

	f("storage", &v1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: v1beta1.VMClusterSpec{
			VMStorage: &v1beta1.VMStorage{},
		},
	}, `
objectmeta:
    name: vmstorage-test
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmstorage
        managed-by: vm-operator
    ownerreferences:
        - apiversion: ""
          name: test
          controller: true
          blockownerdeletion: true
    finalizers:
        - apps.victoriametrics.com/finalizer
spec:
    ports:
        - name: http
          protocol: TCP
          port: 8482
          targetport:
            intval: 8482
        - name: vminsert
          protocol: TCP
          port: 8400
          targetport:
            intval: 8400
        - name: vmselect
          protocol: TCP
          port: 8401
          targetport:
            intval: 8401
    selector:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmstorage
        managed-by: vm-operator
    clusterip: None
    type: ClusterIP
`)
	// with vmbackup and additional service ports
	f("storage", &v1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: v1beta1.VMClusterSpec{
			VMStorage: &v1beta1.VMStorage{
				ServiceSpec: &v1beta1.AdditionalServiceSpec{
					UseAsDefault: true,
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name:       "web-rpc",
								Port:       8011,
								TargetPort: intstr.FromInt(8011),
							},
						},
					},
				},
				VMBackup: &v1beta1.VMBackup{
					AcceptEULA: true,
				},
			},
		},
	}, `
objectmeta:
    name: vmstorage-test
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmstorage
        managed-by: vm-operator
    ownerreferences:
        - apiversion: ""
          name: test
          controller: true
          blockownerdeletion: true
    finalizers:
        - apps.victoriametrics.com/finalizer
spec:
    ports:
        - name: web-rpc
          port: 8011
          targetport:
            intval: 8011
        - name: http
          protocol: TCP
          port: 8482
          targetport:
            intval: 8482
        - name: vminsert
          protocol: TCP
          port: 8400
          targetport:
            intval: 8400
        - name: vmselect
          protocol: TCP
          port: 8401
          targetport:
            intval: 8401
        - name: vmbackupmanager
          protocol: TCP
          port: 8300
          targetport:
            intval: 8300
    selector:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmstorage
        managed-by: vm-operator
    clusterip: None
    type: ClusterIP
`)

	f("select", &v1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: v1beta1.VMClusterSpec{
			VMStorage: &v1beta1.VMStorage{},
			VMSelect:  &v1beta1.VMSelect{Port: "8352"},
		},
	}, `
objectmeta:
    name: vmselect-test
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmselect
        managed-by: vm-operator
    ownerreferences:
        - apiversion: ""
          name: test
          controller: true
          blockownerdeletion: true
    finalizers:
        - apps.victoriametrics.com/finalizer
spec:
    ports:
        - name: http
          protocol: TCP
          port: 8352
          targetport:
            intval: 8352
    selector:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmselect
        managed-by: vm-operator
    clusterip: None
    type: ClusterIP
`)
	// with native and extra service
	f("select", &v1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: v1beta1.VMClusterSpec{
			VMStorage: &v1beta1.VMStorage{},
			VMSelect:  &v1beta1.VMSelect{Port: "8352", ClusterNativePort: "8477", ServiceSpec: &v1beta1.AdditionalServiceSpec{Spec: corev1.ServiceSpec{Type: "LoadBalancer"}}},
		},
	}, `
objectmeta:
    name: vmselect-test
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmselect
        managed-by: vm-operator
    ownerreferences:
        - apiversion: ""
          name: test
          controller: true
          blockownerdeletion: true
    finalizers:
        - apps.victoriametrics.com/finalizer
spec:
    ports:
        - name: http
          protocol: TCP
          port: 8352
          targetport:
            intval: 8352
        - name: clusternative
          protocol: TCP
          port: 8477
          targetport:
            intval: 8477
    selector:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmselect
        managed-by: vm-operator
    clusterip: None
    type: ClusterIP
`)
	f("insert", &v1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: v1beta1.VMClusterSpec{
			VMInsert: &v1beta1.VMInsert{
				InsertPorts: &v1beta1.InsertPorts{
					OpenTSDBHTTPPort: "8087",
				},
			},
		},
	}, `
objectmeta:
    name: vminsert-test
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vminsert
        managed-by: vm-operator
    ownerreferences:
        - apiversion: ""
          name: test
          controller: true
          blockownerdeletion: true
    finalizers:
        - apps.victoriametrics.com/finalizer
spec:
    ports:
        - name: http
          protocol: TCP
          port: 8480
          targetport:
            intval: 8480
        - name: opentsdb-http
          protocol: TCP
          port: 8087
          targetport:
            intval: 8087
    selector:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vminsert
        managed-by: vm-operator
    clusterip: ""
    type: ClusterIP
`)
	// transit to headless
	f("insert", &v1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: v1beta1.VMClusterSpec{
			VMInsert: &v1beta1.VMInsert{
				ServiceSpec: &v1beta1.AdditionalServiceSpec{
					UseAsDefault: true,
					Spec: corev1.ServiceSpec{
						ClusterIP: "None",
						Type:      "ClusterIP",
					},
				},
				ClusterNativePort: "8055",
				InsertPorts: &v1beta1.InsertPorts{
					OpenTSDBHTTPPort: "8087",
				},
			},
		},
	}, `
objectmeta:
    name: vminsert-test
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vminsert
        managed-by: vm-operator
    ownerreferences:
        - apiversion: ""
          name: test
          controller: true
          blockownerdeletion: true
    finalizers:
        - apps.victoriametrics.com/finalizer
spec:
    ports:
        - name: http
          protocol: TCP
          port: 8480
          targetport:
            intval: 8480
        - name: opentsdb-http
          protocol: TCP
          port: 8087
          targetport:
            intval: 8087
        - name: clusternative
          protocol: TCP
          port: 8055
          targetport:
            intval: 8055
    selector:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vminsert
        managed-by: vm-operator
    clusterip: "None"
    type: ClusterIP
`, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vminsert-test",
			Namespace: "default-1",
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "10.0.0.5",
			Selector: map[string]string{
				"app.kubernetes.io/component": "monitoring",
				"app.kubernetes.io/instance":  "test",
				"app.kubernetes.io/name":      "vminsert",
				"managed-by":                  "vm-operator",
			},
		},
	})
	// transit to loadbalancer
	f("insert", &v1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: v1beta1.VMClusterSpec{
			VMInsert: &v1beta1.VMInsert{
				ServiceSpec: &v1beta1.AdditionalServiceSpec{
					UseAsDefault: true,
					EmbeddedObjectMetadata: v1beta1.EmbeddedObjectMetadata{
						Labels: map[string]string{
							"app.kubernetes.io/instance": "incorrect-label",
						},
						Annotations: map[string]string{
							"service.beta.kubernetes.io/aws-load-balancer-type": "external",
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP:         "",
						Type:              "LoadBalancer",
						LoadBalancerClass: ptr.To("service.k8s.aws/nlb"),
					},
				},
				ClusterNativePort: "8055",
				InsertPorts: &v1beta1.InsertPorts{
					OpenTSDBHTTPPort: "8087",
				},
			},
		},
	}, `
objectmeta:
    name: vminsert-test
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vminsert
        managed-by: vm-operator
    annotations:
      "service.beta.kubernetes.io/aws-load-balancer-type": "external"
    ownerreferences:
        - apiversion: ""
          name: test
          controller: true
          blockownerdeletion: true
    finalizers:
        - apps.victoriametrics.com/finalizer
spec:
    ports:
        - name: http
          protocol: TCP
          port: 8480
          targetport:
            intval: 8480
        - name: opentsdb-http
          protocol: TCP
          port: 8087
          targetport:
            intval: 8087
        - name: clusternative
          protocol: TCP
          port: 8055
          targetport:
            intval: 8055
    selector:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vminsert
        managed-by: vm-operator
    type: LoadBalancer
    loadbalancerclass: service.k8s.aws/nlb
`, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vminsert-test",
			Namespace: "default-1",
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "10.0.0.5",
			Selector: map[string]string{
				"app.kubernetes.io/component": "monitoring",
				"app.kubernetes.io/instance":  "test",
				"app.kubernetes.io/name":      "vminsert",
				"managed-by":                  "vm-operator",
			},
		},
	})
}
