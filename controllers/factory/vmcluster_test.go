package factory

import (
	"context"
	"reflect"
	"testing"
	"time"

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

func Test_waitForPodReady(t *testing.T) {

	type args struct {
		ns      string
		podName string
		c       *config.BaseOperatorConf
	}
	tests := []struct {
		name              string
		args              args
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "testing pod with unready status",
			args: args{
				ns:      "default",
				podName: "vmselect-example-0",
				c:       &config.BaseOperatorConf{PodWaitReadyIntervalCheck: time.Second * 1, PodWaitReadyInitDelay: time.Second, PodWaitReadyTimeout: time.Second * 4},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-example-0",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{},
						Phase:      corev1.PodPending,
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-example-1",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{},
						Phase:      corev1.PodPending,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "testing pod with ready status",
			args: args{
				ns:      "default",
				podName: "vmselect-example-0",
				c:       &config.BaseOperatorConf{PodWaitReadyIntervalCheck: time.Second * 1, PodWaitReadyInitDelay: time.Second, PodWaitReadyTimeout: time.Second * 4},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-example-0",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Status: "True", Type: corev1.PodReady},
						},
						Phase: corev1.PodRunning,
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-example-1",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{},
						Phase:      corev1.PodPending,
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			if err := waitForPodReady(context.Background(), fclient, tt.args.ns, tt.args.podName, tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("waitForPodReady() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_podIsReady(t *testing.T) {
	type args struct {
		pod corev1.Pod
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "pod is ready",
			args: args{
				pod: corev1.Pod{
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{
								Type:   corev1.PodInitialized,
								Status: "False",
							},
							{
								Type:   corev1.PodReady,
								Status: "True",
							},
						},
						Phase: corev1.PodRunning,
					},
				},
			},
			want: true,
		},
		{
			name: "pod is unready",
			args: args{
				pod: corev1.Pod{
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{
								Type:   corev1.PodInitialized,
								Status: "False",
							},
							{
								Type:   corev1.PodReady,
								Status: "True",
							},
						},
						Phase: corev1.PodPending,
					},
				},
			},
			want: false,
		},
		{
			name: "pod is deleted",
			args: args{
				pod: corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						DeletionTimestamp: &metav1.Time{},
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{
								Type:   corev1.PodInitialized,
								Status: "False",
							},
							{
								Type:   corev1.PodReady,
								Status: "True",
							},
						},
						Phase: corev1.PodSucceeded,
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PodIsReady(tt.args.pod); got != tt.want {
				t.Errorf("PodIsReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_performRollingUpdateOnSts(t *testing.T) {
	type args struct {
		stsName   string
		ns        string
		podLabels map[string]string
		c         *config.BaseOperatorConf
	}
	tests := []struct {
		name                string
		args                args
		wantErr             bool
		predefinedObjects   []runtime.Object
		updatePodRevByIndex *int32
		neededPodRev        string
	}{
		{
			name: "rolling update is not needed",
			args: args{
				stsName:   "vmselect-sts",
				ns:        "default",
				c:         &config.BaseOperatorConf{},
				podLabels: map[string]string{"app": "vmselect"},
			},
			predefinedObjects: []runtime.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-sts",
						Namespace: "default",
						Labels:    map[string]string{"app": "vmselect"},
					},
					Status: appsv1.StatefulSetStatus{
						CurrentRevision: "rev1",
						UpdateRevision:  "rev1",
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-sts-0",
						Namespace: "default",
						Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev1"},
					},
					Status: corev1.PodStatus{},
				},
			},
		},
		{
			name: "rolling update is timeout",
			args: args{
				stsName: "vmselect-sts",
				ns:      "default",
				c: &config.BaseOperatorConf{
					PodWaitReadyTimeout:       time.Second * 2,
					PodWaitReadyInitDelay:     time.Millisecond,
					PodWaitReadyIntervalCheck: time.Second,
				},
				podLabels: map[string]string{"app": "vmselect"},
			},
			predefinedObjects: []runtime.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-sts",
						Namespace: "default",
						Labels:    map[string]string{"app": "vmselect"},
					},
					Status: appsv1.StatefulSetStatus{
						CurrentRevision: "rev1",
						UpdateRevision:  "rev2",
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-sts-0",
						Namespace: "default",
						Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev1"},
					},
					Status: corev1.PodStatus{},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-sts-1",
						Namespace: "default",
						Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev2"},
					},
					Status: corev1.PodStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "rolling update is needed with update",
			args: args{
				stsName: "vmselect-sts",
				ns:      "default",
				c: &config.BaseOperatorConf{
					PodWaitReadyTimeout:       time.Second * 2,
					PodWaitReadyInitDelay:     time.Millisecond,
					PodWaitReadyIntervalCheck: time.Second,
				},
				podLabels: map[string]string{"app": "vmselect"},
			},

			predefinedObjects: []runtime.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-sts",
						Namespace: "default",
						Labels:    map[string]string{"app": "vmselect"},
					},
					Spec: appsv1.StatefulSetSpec{Replicas: pointer.Int32Ptr(2)},
					Status: appsv1.StatefulSetStatus{
						CurrentRevision: "rev1",
						UpdateRevision:  "rev2",
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-sts-0",
						Namespace: "default",
						Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev1"},
					},
					Status: corev1.PodStatus{},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-sts-1",
						Namespace: "default",
						Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev2"},
					},
					Status: corev1.PodStatus{},
				},
			},
			updatePodRevByIndex: pointer.Int32Ptr(1),
			neededPodRev:        "rev2",
			wantErr:             false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			if tt.updatePodRevByIndex != nil {
				podInd := tt.predefinedObjects[int(*tt.updatePodRevByIndex)]
				pod := podInd.(*corev1.Pod)
				go func(pod *corev1.Pod, rev string) {
					time.Sleep(time.Millisecond * 1200)
					pod.ObjectMeta.Labels[podRevisionLabel] = rev
					err := fclient.Update(context.Background(), pod)
					if err != nil {
						t.Errorf("cannot update pod for rolling update check")
					}
				}(pod, tt.neededPodRev)
			}

			if err := performRollingUpdateOnSts(context.Background(), fclient, tt.args.stsName, tt.args.ns, tt.args.podLabels, tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("performRollingUpdateOnSts() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

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
			name: "pods is expanding",
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
							{Type: corev1.PodReady, Status: "False"},
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
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			got, err := waitForExpanding(context.Background(), fclient, tt.args.namespace, tt.args.lbs, tt.args.desiredCount)
			if (err != nil) != tt.wantErr {
				t.Errorf("waitForExpanding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("waitForExpanding() got = %v, want %v", got, tt.want)
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
					},
					Spec: v1beta1.VMClusterSpec{
						RetentionPeriod:   "2",
						ReplicationFactor: pointer.Int32Ptr(2),
						VMInsert: &v1beta1.VMInsert{
							ReplicaCount: pointer.Int32Ptr(2),
						},
						VMStorage: &v1beta1.VMStorage{
							ReplicaCount: pointer.Int32Ptr(2),
						},
						VMSelect: &v1beta1.VMSelect{
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
							ReplicaCount: pointer.Int32Ptr(2),
							InsertPorts: &v1beta1.InsertPorts{
								GraphitePort:     "8025",
								OpenTSDBHTTPPort: "3311",
								InfluxPort:       "5511",
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
							ReplicaCount: pointer.Int32Ptr(2),
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
			got, err := CreateOrUpdateVMCluster(context.TODO(), tt.args.cr, fclient, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMCluster() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("CreateOrUpdateVMCluster() got = %v, want %v", got, tt.want)
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
