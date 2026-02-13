package vmcluster

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMCluster
		want              string
		wantErr           bool
		predefinedObjects []runtime.Object
		validate          func(vminsert *appsv1.Deployment, vmselect, vmstorage *appsv1.StatefulSet) error
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var wg sync.WaitGroup
		eventuallyUpdateStatusToOk := func(cb func() error) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tc := time.NewTicker(time.Millisecond * 100)
				for {
					select {
					case <-ctx.Done():
						return
					case <-tc.C:
						if err := cb(); err != nil {
							if k8serrors.IsNotFound(err) {
								continue
							}
							t.Errorf("callback error: %s", err)
							return
						}
						return
					}
				}
			}()
		}
		if o.cr.Spec.RequestsLoadBalancer.Enabled {
			var vmauthLB appsv1.Deployment
			name := o.cr.PrefixedName(vmv1beta1.ClusterComponentBalancer)
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: name, Namespace: o.cr.Namespace}, &vmauthLB); err != nil {
					return err
				}
				vmauthLB.Status.Conditions = append(vmauthLB.Status.Conditions, appsv1.DeploymentCondition{
					Type:   appsv1.DeploymentProgressing,
					Reason: "NewReplicaSetAvailable",
					Status: "True",
				})
				if err := fclient.Status().Update(ctx, &vmauthLB); err != nil {
					return err
				}

				return nil
			})

		}
		if o.cr.Spec.VMInsert != nil {
			var vminsert appsv1.Deployment
			name := o.cr.PrefixedName(vmv1beta1.ClusterComponentInsert)
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: name, Namespace: o.cr.Namespace}, &vminsert); err != nil {
					return err
				}
				vminsert.Status.Conditions = append(vminsert.Status.Conditions, appsv1.DeploymentCondition{
					Type:   appsv1.DeploymentProgressing,
					Reason: "NewReplicaSetAvailable",
					Status: "True",
				})
				if err := fclient.Status().Update(ctx, &vminsert); err != nil {
					return err
				}

				return nil
			})
		}
		if o.cr.Spec.VMSelect != nil {
			var vmselect appsv1.StatefulSet
			name := o.cr.PrefixedName(vmv1beta1.ClusterComponentSelect)
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: name, Namespace: o.cr.Namespace}, &vmselect); err != nil {
					return err
				}
				vmselect.Status.ReadyReplicas = *o.cr.Spec.VMSelect.ReplicaCount
				vmselect.Status.UpdatedReplicas = *o.cr.Spec.VMSelect.ReplicaCount
				if err := fclient.Status().Update(ctx, &vmselect); err != nil {
					return err
				}
				return nil
			})
		}
		if o.cr.Spec.VMStorage != nil {
			var vmstorage appsv1.StatefulSet
			eventuallyUpdateStatusToOk(func() error {
				name := o.cr.PrefixedName(vmv1beta1.ClusterComponentStorage)
				if err := fclient.Get(ctx, types.NamespacedName{Name: name, Namespace: o.cr.Namespace}, &vmstorage); err != nil {
					return err
				}
				vmstorage.Status.ReadyReplicas = *o.cr.Spec.VMStorage.ReplicaCount
				vmstorage.Status.UpdatedReplicas = *o.cr.Spec.VMStorage.ReplicaCount
				if err := fclient.Status().Update(ctx, &vmstorage); err != nil {
					return err
				}

				return nil
			})
		}
		err := CreateOrUpdate(ctx, o.cr, fclient)
		if (err != nil) != o.wantErr {
			t.Errorf("CreateOrUpdate() error = %v, wantErr %v", err, o.wantErr)
			return
		}
		if o.validate != nil {
			var vmselect, vmstorage appsv1.StatefulSet
			var vminsert appsv1.Deployment
			if o.cr.Spec.VMInsert != nil {
				name := o.cr.PrefixedName(vmv1beta1.ClusterComponentStorage)
				if err := fclient.Get(ctx, types.NamespacedName{Name: name, Namespace: o.cr.Namespace}, &vminsert); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if o.cr.Spec.VMSelect != nil {
				name := o.cr.PrefixedName(vmv1beta1.ClusterComponentSelect)
				if err := fclient.Get(ctx, types.NamespacedName{Name: name, Namespace: o.cr.Namespace}, &vmselect); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if o.cr.Spec.VMStorage != nil {
				name := o.cr.PrefixedName(vmv1beta1.ClusterComponentStorage)
				if err := fclient.Get(ctx, types.NamespacedName{Name: name, Namespace: o.cr.Namespace}, &vmstorage); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			if err := o.validate(&vminsert, &vmselect, &vmstorage); err != nil {
				t.Fatalf("validation for cluster failed: %v", err)
			}
		}
	}

	// base-vmstorage-test
	f(opts{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "cluster-1",
				Labels:    map[string]string{"label": "value"},
			},
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod:   "2",
				ReplicationFactor: ptr.To(int32(0)),
				VMInsert: &vmv1beta1.VMInsert{
					PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{
						Annotations: map[string]string{"key": "value"},
					},
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(0)),
					},
				},
				VMStorage: &vmv1beta1.VMStorage{
					PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{
						Annotations: map[string]string{"key": "value"},
						Labels:      map[string]string{"label": "value2"},
					},
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{

						ReplicaCount: ptr.To(int32(2))},
				},
				VMSelect: &vmv1beta1.VMSelect{
					PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{
						Annotations: map[string]string{"key": "value"},
					},
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{

						ReplicaCount: ptr.To(int32(2))},
				},
			},
		},
		want: string(vmv1beta1.UpdateStatusExpanding),
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
	})

	// base-vminsert-with-ports
	f(opts{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "cluster-1",
			},
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod:   "2",
				ReplicationFactor: ptr.To(int32(2)),
				VMInsert: &vmv1beta1.VMInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(0))},
					InsertPorts: &vmv1beta1.InsertPorts{
						GraphitePort:     "8025",
						OpenTSDBHTTPPort: "3311",
						InfluxPort:       "5511",
					},
					HPA: &vmv1beta1.EmbeddedHPA{
						MinReplicas: ptr.To(int32(0)),
						MaxReplicas: 3,
					},
				},
			},
		},
		want: string(vmv1beta1.UpdateStatusExpanding),
	})

	f(opts{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "cluster-1",
			},
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod:   "2",
				ReplicationFactor: ptr.To(int32(2)),
				VMInsert: &vmv1beta1.VMInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(0))},
					InsertPorts: &vmv1beta1.InsertPorts{
						GraphitePort:     "8025",
						OpenTSDBHTTPPort: "3311",
						InfluxPort:       "5511",
					},
					HPA: &vmv1beta1.EmbeddedHPA{
						MinReplicas: ptr.To(int32(0)),
						MaxReplicas: 3,
					},
				},
				VMStorage: &vmv1beta1.VMStorage{
					HPA: &vmv1beta1.EmbeddedHPA{
						MinReplicas: ptr.To(int32(0)),
						MaxReplicas: 3,
						Behaviour: &autoscalingv2.HorizontalPodAutoscalerBehavior{
							ScaleDown: &autoscalingv2.HPAScalingRules{},
						},
					},
				},
			},
		},
		wantErr: true,
		want:    string(vmv1beta1.UpdateStatusFailed),
	})

	// base-vmselect
	f(opts{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "cluster-1",
			},
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod:   "2",
				ReplicationFactor: ptr.To(int32(2)),
				VMSelect: &vmv1beta1.VMSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(2))},
					HPA: &vmv1beta1.EmbeddedHPA{
						MinReplicas: ptr.To(int32(1)),
						MaxReplicas: 3,
					},
				},
			},
		},
		want: string(vmv1beta1.UpdateStatusExpanding),
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
	})

	// base-vmstorage-with-maintenance
	f(opts{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "cluster-1",
			},
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod:   "2",
				ReplicationFactor: ptr.To(int32(2)),
				VMInsert: &vmv1beta1.VMInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(0))},
				},
				VMStorage: &vmv1beta1.VMStorage{
					MaintenanceSelectNodeIDs: []int32{1, 3},
					MaintenanceInsertNodeIDs: []int32{0, 1, 2},
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(10))},
				},
				VMSelect: &vmv1beta1.VMSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(2))},
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
		want: string(vmv1beta1.UpdateStatusExpanding),
	})

	// base-vmstorage-with-maintenance
	f(opts{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "cluster-1",
			},
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod:   "2",
				ReplicationFactor: ptr.To(int32(2)),
				VMInsert: &vmv1beta1.VMInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(0))},
				},
				VMStorage: &vmv1beta1.VMStorage{
					MaintenanceSelectNodeIDs: []int32{1, 3},
					MaintenanceInsertNodeIDs: []int32{0, 1, 2},
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(10))},
				},
				VMSelect: &vmv1beta1.VMSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(2))},
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
		want: string(vmv1beta1.UpdateStatusExpanding),
	})

	// vmcluster with load-balancing
	f(opts{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "cluster-1",
			},
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod:   "2",
				ReplicationFactor: ptr.To(int32(2)),
				RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
					Enabled: true,
					Spec: vmv1beta1.VMAuthLoadBalancerSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To(int32(0))},
					},
				},
				VMSelect: &vmv1beta1.VMSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(0))},
				},
				VMStorage: &vmv1beta1.VMStorage{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(0))},
				},
				VMInsert: &vmv1beta1.VMInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(0))},
					InsertPorts: &vmv1beta1.InsertPorts{
						GraphitePort:     "8025",
						OpenTSDBHTTPPort: "3311",
						InfluxPort:       "5511",
					},
				},
			},
		},
		want: string(vmv1beta1.UpdateStatusExpanding),
	})
}

func TestCreatOrUpdateClusterServices(t *testing.T) {
	f := func(component vmv1beta1.ClusterComponent, cr *vmv1beta1.VMCluster, wantSvcYAML string, predefinedObjects ...runtime.Object) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(cr)

		var builderF func(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error
		var svc *corev1.Service
		switch component {
		case vmv1beta1.ClusterComponentInsert:
			builderF = createOrUpdateVMInsertService
			svc = buildVMInsertService(cr)
		case vmv1beta1.ClusterComponentStorage:
			builderF = createOrUpdateVMStorageService
			svc = buildVMStorageService(cr)
		case vmv1beta1.ClusterComponentSelect:
			builderF = createOrUpdateVMSelectService
			svc = buildVMSelectService(cr)

		default:
			t.Fatalf("BUG not expected component for test: %q", component)
		}
		assert.NoError(t, builderF(ctx, fclient, cr, nil))
		var actualService corev1.Service
		if err := fclient.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}, &actualService); err != nil {
			t.Fatalf("create service not found: %q", err)
		}
		var wantService corev1.Service
		if err := yaml.Unmarshal([]byte(wantSvcYAML), &wantService); err != nil {
			t.Fatalf("BUG: expect service definition at yaml: %q", err)
		}
		assert.Equal(t, wantService, actualService)
	}

	f(vmv1beta1.ClusterComponentStorage, &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{},
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
        app.kubernetes.io/part-of: vmcluster
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
    publishnotreadyaddresses: true
`)
	// with vmbackup and additional service ports
	f(vmv1beta1.ClusterComponentStorage, &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: vmv1beta1.VMClusterSpec{
			License: &vmv1beta1.License{
				Key: ptr.To("test-key"),
			},
			VMStorage: &vmv1beta1.VMStorage{
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					UseAsDefault: true,
					Spec: corev1.ServiceSpec{
						PublishNotReadyAddresses: true,
						Ports: []corev1.ServicePort{
							{
								Name:       "web-rpc",
								Port:       8011,
								TargetPort: intstr.FromInt(8011),
							},
						},
					},
				},
				VMBackup: &vmv1beta1.VMBackup{},
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
        app.kubernetes.io/part-of: vmcluster
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
    publishnotreadyaddresses: true
`)

	f(vmv1beta1.ClusterComponentSelect, &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{},
			VMSelect: &vmv1beta1.VMSelect{
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
					Port: "8352",
				},
			},
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
        app.kubernetes.io/part-of: vmcluster
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
    publishnotreadyaddresses: true
`)
	// with native and extra service
	f(vmv1beta1.ClusterComponentSelect, &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{},
			VMSelect: &vmv1beta1.VMSelect{CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{Port: "8352"},
				ClusterNativePort: "8477", ServiceSpec: &vmv1beta1.AdditionalServiceSpec{Spec: corev1.ServiceSpec{Type: "LoadBalancer"}}},
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
        app.kubernetes.io/part-of: vmcluster
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
    publishnotreadyaddresses: true
`)
	f(vmv1beta1.ClusterComponentInsert, &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: vmv1beta1.VMClusterSpec{
			VMInsert: &vmv1beta1.VMInsert{
				InsertPorts: &vmv1beta1.InsertPorts{
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
        app.kubernetes.io/part-of: vmcluster
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
	f(vmv1beta1.ClusterComponentInsert, &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: vmv1beta1.VMClusterSpec{
			VMInsert: &vmv1beta1.VMInsert{
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					UseAsDefault: true,
					Spec: corev1.ServiceSpec{
						ClusterIP: "None",
						Type:      "ClusterIP",
					},
				},
				ClusterNativePort: "8055",
				InsertPorts: &vmv1beta1.InsertPorts{
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
        app.kubernetes.io/part-of: vmcluster
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
	f(vmv1beta1.ClusterComponentInsert, &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: vmv1beta1.VMClusterSpec{
			VMInsert: &vmv1beta1.VMInsert{
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					UseAsDefault: true,
					EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
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
				InsertPorts: &vmv1beta1.InsertPorts{
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
        app.kubernetes.io/part-of: vmcluster
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
	// insert with load-balanacer
	f(vmv1beta1.ClusterComponentInsert, &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: vmv1beta1.VMClusterSpec{
			RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
				Enabled: true,
			},
			VMInsert: &vmv1beta1.VMInsert{
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					UseAsDefault: true,
					EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
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
				InsertPorts: &vmv1beta1.InsertPorts{
					OpenTSDBHTTPPort: "8087",
				},
			},
		},
	}, `
objectmeta:
    name: vminsertinternal-test
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vminsert
        managed-by: vm-operator
        app.kubernetes.io/part-of: vmcluster
        operator.victoriametrics.com/vmauthlb-proxy-job-name: vminsert-test
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
    type: ClusterIP
    clusterip: "None"
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
