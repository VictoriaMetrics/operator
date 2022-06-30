package factory

import (
	"context"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	"github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
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
							{LastTerminationState: corev1.ContainerState{
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

			err := waitExpanding(context.Background(), fclient, tt.args.namespace, tt.args.lbs, tt.args.desiredCount)
			if (err != nil) != tt.wantErr {
				t.Errorf("waitExpanding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func Test_genVMStorageService(t *testing.T) {
	type args struct {
		cr *v1beta1.VMCluster
		c  *config.BaseOperatorConf
	}
	tests := []struct {
		name string
		args args
		want *corev1.Service
	}{
		{
			name: "get vmStorage svc",
			args: args{
				cr: &v1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "some-cluster",
						Namespace: "default",
					},
					Spec: v1beta1.VMClusterSpec{
						VMStorage: &v1beta1.VMStorage{},
					},
				},
				c: &config.BaseOperatorConf{},
			},
			want: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "vmstorage-some-cluster",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmstorage",
						"app.kubernetes.io/instance":  "some-cluster",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := genVMStorageService(tt.args.cr, tt.args.c)

			if !reflect.DeepEqual(got.Labels, tt.want.Labels) || got.Name != tt.want.Name {
				t.Errorf("genVMStorageService() = %v, want %v", got, tt.want)
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
			want: v1beta1.ClusterStatusExpanding,
			predefinedObjects: []runtime.Object{
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-1", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
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
			want: v1beta1.ClusterStatusExpanding,
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
						}},
				},
			},
			want: v1beta1.ClusterStatusExpanding,
			predefinedObjects: []runtime.Object{
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "select-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmselect", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "select-1", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmselect", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
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
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-1", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-2", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-3", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-4", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-5", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-6", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-7", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-8", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-9", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
			},
			want: v1beta1.ClusterStatusExpanding,
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
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-1", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-2", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-3", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-4", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-5", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-6", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-7", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-8", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "storage-9", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmstorage", "app.kubernetes.io/instance": "cluster-1", "managed-by": "vm-operator"}},
					Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "true"}}}},
			},
			want: v1beta1.ClusterStatusExpanding,
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
