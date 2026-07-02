package vmcluster

import (
	"context"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMCluster
		wantErr           bool
		cfgMutator        func(*config.BaseOperatorConf)
		predefinedObjects []runtime.Object
		validate          func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster)
	}

	f := func(o opts) {
		t.Helper()
		cfg := config.MustGetBaseConfig()
		if o.cfgMutator != nil {
			defaultCfg := *cfg
			o.cfgMutator(cfg)
			defer func() {
				*config.MustGetBaseConfig() = defaultCfg
			}()
		}
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		err := CreateOrUpdate(ctx, o.cr, fclient)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		if o.validate != nil {
			o.validate(ctx, fclient, o.cr)
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
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(0)),
					},
				},
				VMStorage: &vmv1beta1.VMStorage{
					PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{
						Annotations: map[string]string{"key": "value"},
						Labels:      map[string]string{"label": "value2"},
					},
					CommonAppsParams: vmv1beta1.CommonAppsParams{

						ReplicaCount: ptr.To(int32(2))},
				},
				VMSelect: &vmv1beta1.VMSelect{
					PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{
						Annotations: map[string]string{"key": "value"},
					},
					CommonAppsParams: vmv1beta1.CommonAppsParams{

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
					CommonAppsParams: vmv1beta1.CommonAppsParams{
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
					CommonAppsParams: vmv1beta1.CommonAppsParams{
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
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(2))},
					HPA: &vmv1beta1.EmbeddedHPA{
						MinReplicas: ptr.To(int32(1)),
						MaxReplicas: 3,
					},
				},
			},
		},
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
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(0))},
				},
				VMStorage: &vmv1beta1.VMStorage{
					MaintenanceSelectNodeIDs: []int32{1, 3},
					MaintenanceInsertNodeIDs: []int32{0, 1, 2},
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(10))},
				},
				VMSelect: &vmv1beta1.VMSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
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
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(0))},
				},
				VMStorage: &vmv1beta1.VMStorage{
					MaintenanceSelectNodeIDs: []int32{1, 3},
					MaintenanceInsertNodeIDs: []int32{0, 1, 2},
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(10))},
				},
				VMSelect: &vmv1beta1.VMSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
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
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To(int32(0))},
					},
				},
				VMSelect: &vmv1beta1.VMSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(0))},
				},
				VMStorage: &vmv1beta1.VMStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(0))},
				},
				VMInsert: &vmv1beta1.VMInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(0))},
					InsertPorts: &vmv1beta1.InsertPorts{
						GraphitePort:     "8025",
						OpenTSDBHTTPPort: "3311",
						InfluxPort:       "5511",
					},
				},
			},
		},
	})

	// vmcluster with load-balancing and HPA
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
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To(int32(1)),
						},
						HPA: &vmv1beta1.EmbeddedHPA{
							MinReplicas: ptr.To(int32(1)),
							MaxReplicas: 3,
						},
					},
				},
				VMSelect: &vmv1beta1.VMSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(0))},
				},
				VMStorage: &vmv1beta1.VMStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(0))},
				},
				VMInsert: &vmv1beta1.VMInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(0))},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster) {
			var hpa autoscalingv2.HorizontalPodAutoscaler
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{
				Namespace: cr.Namespace,
				Name:      cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
			}, &hpa))
			assert.Equal(t, int32(3), hpa.Spec.MaxReplicas)
			assert.Equal(t, "Deployment", hpa.Spec.ScaleTargetRef.Kind)
		},
	})

	// with select VPA
	f(opts{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: vmv1beta1.VMClusterSpec{
				VMSelect: &vmv1beta1.VMSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(0)),
						Volumes: []corev1.Volume{{
							Name: "vmselect-cachedir",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/host/path/cache",
								},
							},
						}},
					},
					CacheMountPath: "/cache",
					VPA: &vmv1beta1.EmbeddedVPA{
						UpdatePolicy: &vpav1.PodUpdatePolicy{
							UpdateMode: ptr.To(vpav1.UpdateModeRecreate),
						},
						ResourcePolicy: &vpav1.PodResourcePolicy{
							ContainerPolicies: []vpav1.ContainerResourcePolicy{
								{
									ContainerName: "vmselect",
									Mode:          ptr.To(vpav1.ContainerScalingModeAuto),
								},
							},
						},
						Recommenders: []*vpav1.VerticalPodAutoscalerRecommenderSelector{
							{Name: "custom-recommender"},
						},
					},
				},
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.VPAAPIEnabled = true
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster) {
			var vpaGot vpav1.VerticalPodAutoscaler
			component := vmv1beta1.ClusterComponentSelect
			selectName := cr.PrefixedName(component)
			nsn := types.NamespacedName{
				Namespace: cr.Namespace,
				Name:      selectName,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &vpaGot))
			vpaExpected := vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      selectName,
					Namespace: cr.Namespace,
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmselect",
						"app.kubernetes.io/part-of":   "vmcluster",
						"app.kubernetes.io/instance":  "test",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
					ResourceVersion: "1",
					OwnerReferences: []metav1.OwnerReference{{Name: "test", Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(true)}},
				},
				Spec: vpav1.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						Name:       selectName,
						Kind:       "StatefulSet",
						APIVersion: "apps/v1",
					},
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeRecreate),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{{
							ContainerName: "vmselect",
							Mode:          ptr.To(vpav1.ContainerScalingModeAuto),
						}},
					},
					Recommenders: []*vpav1.VerticalPodAutoscalerRecommenderSelector{
						{Name: "custom-recommender"},
					},
				},
			}
			assert.Equal(t, vpaGot, vpaExpected)
			var stsSelectGot appsv1.StatefulSet
			assert.NoError(t, rclient.Get(ctx, nsn, &stsSelectGot))
			volumesSelectExpected := []corev1.Volume{{
				Name: "vmselect-cachedir",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/host/path/cache",
					},
				},
			}}
			assert.Equal(t, stsSelectGot.Spec.Template.Spec.Volumes, volumesSelectExpected)
		},
	})

	// with storage VPA
	f(opts{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: vmv1beta1.VMClusterSpec{
				VMStorage: &vmv1beta1.VMStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(0)),
						Volumes: []corev1.Volume{{
							Name: "vmstorage-db",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/host/path/storage",
								},
							},
						}},
					},
					VPA: &vmv1beta1.EmbeddedVPA{
						UpdatePolicy: &vpav1.PodUpdatePolicy{
							UpdateMode: ptr.To(vpav1.UpdateModeInitial),
						},
						ResourcePolicy: &vpav1.PodResourcePolicy{
							ContainerPolicies: []vpav1.ContainerResourcePolicy{
								{ContainerName: "vmstorage"},
							},
						},
					},
				},
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.VPAAPIEnabled = true
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster) {
			component := vmv1beta1.ClusterComponentStorage
			var got vpav1.VerticalPodAutoscaler
			storageName := cr.PrefixedName(component)
			nsn := types.NamespacedName{
				Namespace: cr.Namespace,
				Name:      storageName,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			expected := vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:            storageName,
					Namespace:       cr.Namespace,
					Labels:          cr.FinalLabels(component),
					ResourceVersion: "1",
					OwnerReferences: []metav1.OwnerReference{{Name: "test", Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(true)}},
				},
				Spec: vpav1.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						Name:       storageName,
						Kind:       "StatefulSet",
						APIVersion: "apps/v1",
					},
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeInitial),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vmstorage"},
						},
					},
				},
			}
			assert.Equal(t, got, expected)

			var stsStorageGot appsv1.StatefulSet
			assert.NoError(t, rclient.Get(ctx, nsn, &stsStorageGot))
			volumesStorageExpected := []corev1.Volume{{
				Name: "vmstorage-db",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/host/path/storage",
					},
				},
			}}
			assert.Equal(t, stsStorageGot.Spec.Template.Spec.Volumes, volumesStorageExpected)
		},
	})

	// update VPA on insert
	f(opts{
		predefinedObjects: []runtime.Object{
			&vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vminsert-test",
					Namespace: "default",
				},
				Spec: vpav1.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						Name:       "vminsert-test",
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeInitial),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vminsert"},
						},
					},
				},
			},
		},
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: vmv1beta1.VMClusterSpec{
				VMInsert: &vmv1beta1.VMInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(0)),
					},
					VPA: &vmv1beta1.EmbeddedVPA{
						UpdatePolicy: &vpav1.PodUpdatePolicy{
							UpdateMode: ptr.To(vpav1.UpdateModeRecreate),
						},
						ResourcePolicy: &vpav1.PodResourcePolicy{
							ContainerPolicies: []vpav1.ContainerResourcePolicy{
								{ContainerName: "vminsert"},
							},
						},
					},
				},
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.VPAAPIEnabled = true
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster) {
			component := vmv1beta1.ClusterComponentInsert
			var got vpav1.VerticalPodAutoscaler
			vpaName := cr.PrefixedName(component)
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: vpaName}, &got))
			expected := vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:            vpaName,
					Namespace:       cr.Namespace,
					Labels:          cr.FinalLabels(component),
					ResourceVersion: "1000",
					OwnerReferences: []metav1.OwnerReference{{Name: "test", Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(true)}},
				},
				Spec: vpav1.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						Name:       vpaName,
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeRecreate),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vminsert"},
						},
					},
				},
			}
			assert.Equal(t, got, expected)
		},
	})

	// remove insert VPA
	f(opts{
		predefinedObjects: []runtime.Object{
			&vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "vminsert-test",
					Namespace:       "default",
					ResourceVersion: "1",
					OwnerReferences: []metav1.OwnerReference{{Name: "test", Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(true)}},
					Labels: map[string]string{
						"app.kubernetes.io/instance":  "test",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
						"app.kubernetes.io/name":      "vminsert",
						"app.kubernetes.io/part-of":   "vmcluster",
					},
				},
				Spec: vpav1.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						Name:       "vminsert-test",
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeInitial),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vminsert"},
						},
					},
				},
			},
		},
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMClusterSpec{
				VMInsert: &vmv1beta1.VMInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(0)),
					},
				},
			},
			Status: vmv1beta1.VMClusterStatus{
				LastAppliedSpec: &vmv1beta1.VMClusterSpec{},
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.VPAAPIEnabled = true
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster) {
			component := vmv1beta1.ClusterComponentInsert
			var got vpav1.VerticalPodAutoscaler
			vpaName := cr.PrefixedName(component)
			err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: vpaName}, &got)
			assert.Error(t, err)
			assert.True(t, k8serrors.IsNotFound(err))
		},
	})

	// managed metadata
	f(opts{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMClusterSpec{
				VMSelect: &vmv1beta1.VMSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				VMInsert: &vmv1beta1.VMInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				VMStorage: &vmv1beta1.VMStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{
					Labels:      map[string]string{"env": "prod"},
					Annotations: map[string]string{"controller": "true"},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster) {
			var set appsv1.StatefulSet
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "vmselect-base"}, &set))
			assert.Equal(t, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vmselect",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"app.kubernetes.io/part-of":   "vmcluster",
				"managed-by":                  "vm-operator",
			}, set.Labels)
			assert.Equal(t, map[string]string{"controller": "true"}, set.Annotations)
			var svc corev1.Service
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName(vmv1beta1.ClusterComponentSelect)}, &svc))
			assert.Equal(t, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vmselect",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"app.kubernetes.io/part-of":   "vmcluster",
				"managed-by":                  "vm-operator",
			}, svc.Labels)
		},
	})

	// common labels
	f(opts{
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.CommonLabels = map[string]string{"env": "prod"}
			c.CommonAnnotations = map[string]string{"controller": "true"}
		},
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMClusterSpec{
				VMSelect: &vmv1beta1.VMSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				VMInsert: &vmv1beta1.VMInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				VMStorage: &vmv1beta1.VMStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster) {
			var set appsv1.StatefulSet
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "vmselect-base"}, &set))
			assert.Equal(t, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vmselect",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"app.kubernetes.io/part-of":   "vmcluster",
				"managed-by":                  "vm-operator",
			}, set.Labels)
			assert.Equal(t, map[string]string{"controller": "true"}, set.Annotations)
			var svc corev1.Service
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName(vmv1beta1.ClusterComponentSelect)}, &svc))
			assert.Equal(t, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vmselect",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"app.kubernetes.io/part-of":   "vmcluster",
				"managed-by":                  "vm-operator",
			}, svc.Labels)
		}})

	// pools: two pools with shared vminsert — pool STSes get pool label, instance label stays the cluster name, top-level vmstorage not created
	f(opts{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cluster-1"},
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod: "1",
				VMStorage: &vmv1beta1.VMStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
				},
				VMSelect: &vmv1beta1.VMSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
				},
				VMInsert: &vmv1beta1.VMInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
				},
				Pools: []vmv1beta1.VMClusterPool{
					{Name: "hot"},
					{Name: "cold"},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster) {
			for _, poolName := range []string{"hot", "cold"} {
				stsName := cr.PoolPrefixedName(vmv1beta1.ClusterComponentStorage, poolName)
				var sts appsv1.StatefulSet
				assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: stsName}, &sts), "pool STS %s should exist", stsName)
				// instance label must be the cluster name, not cluster-pool
				assert.Equal(t, cr.Name, sts.Labels["app.kubernetes.io/instance"], "instance label for pool %s", poolName)
				// STS selector must include the pool label so per-pool selectors are disjoint
				assert.Equal(t, poolName, sts.Spec.Selector.MatchLabels["app.kubernetes.io/pool"], "selector pool label for pool %s", poolName)
				// pod template must also carry the pool label so the STS selector matches its pods
				assert.Equal(t, poolName, sts.Spec.Template.Labels["app.kubernetes.io/pool"], "pod template pool label for pool %s", poolName)
			}
			// top-level vmstorage must NOT be created when pools are defined
			var topSts appsv1.StatefulSet
			err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName(vmv1beta1.ClusterComponentStorage)}, &topSts)
			assert.True(t, k8serrors.IsNotFound(err), "top-level vmstorage STS must not exist when pools are defined")
			// shared vminsert must still be created (no pool has a dedicated insert)
			var dep appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName(vmv1beta1.ClusterComponentInsert)}, &dep))
		},
	})

	// pools: pool with dedicated vminsert — pool insert created, top-level vminsert skipped
	f(opts{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cluster-1"},
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod: "1",
				VMStorage: &vmv1beta1.VMStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
				},
				VMSelect: &vmv1beta1.VMSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
				},
				VMInsert: &vmv1beta1.VMInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
				},
				Pools: []vmv1beta1.VMClusterPool{
					{
						Name: "hot",
						VMInsert: &vmv1beta1.VMInsert{
							CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
						},
					},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster) {
			// pool insert must exist with pool-scoped name
			poolInsertName := cr.PoolPrefixedName(vmv1beta1.ClusterComponentInsert, "hot")
			var poolDep appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: poolInsertName}, &poolDep))
			assert.Equal(t, cr.Name, poolDep.Labels["app.kubernetes.io/instance"])
			assert.Equal(t, "hot", poolDep.Labels["app.kubernetes.io/pool"])
			// top-level vminsert must NOT be created when any pool has its own insert
			var topDep appsv1.Deployment
			err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName(vmv1beta1.ClusterComponentInsert)}, &topDep)
			assert.True(t, k8serrors.IsNotFound(err), "top-level vminsert must not exist when a pool has a dedicated insert")
		},
	})

	// pools: VMStorage.RetentionPeriod overrides cluster-level RetentionPeriod in the generated args
	f(opts{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cluster-1"},
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod: "1",
				VMStorage: &vmv1beta1.VMStorage{
					RetentionPeriod:  "90d",
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
				},
				VMSelect: &vmv1beta1.VMSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(0))},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMCluster) {
			var sts appsv1.StatefulSet
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName(vmv1beta1.ClusterComponentStorage)}, &sts))
			var storageArgs []string
			for _, c := range sts.Spec.Template.Spec.Containers {
				if c.Name == "vmstorage" {
					storageArgs = c.Args
				}
			}
			hasRetention90d := false
			hasRetention1 := false
			for _, a := range storageArgs {
				if a == "-retentionPeriod=90d" {
					hasRetention90d = true
				}
				if a == "-retentionPeriod=1" {
					hasRetention1 = true
				}
			}
			assert.True(t, hasRetention90d, "VMStorage.RetentionPeriod should override cluster-level: got args %v", storageArgs)
			assert.False(t, hasRetention1, "cluster-level RetentionPeriod should be overridden: got args %v", storageArgs)
		},
	})
}

func TestCreatOrUpdateClusterServices(t *testing.T) {
	f := func(cr *vmv1beta1.VMCluster, wantSvcYAML string, predefinedObjects ...runtime.Object) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(cr)
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
		var ls corev1.ServiceList
		selector := cr.SelectorLabels(vmv1beta1.ClusterComponentRoot)
		delete(selector, "app.kubernetes.io/name")
		assert.NoError(t, fclient.List(ctx, &ls, &client.ListOptions{LabelSelector: labels.SelectorFromSet(selector)}))
		sort.Slice(ls.Items, func(i, j int) bool {
			return strings.ToLower(ls.Items[i].Name) < strings.ToLower(ls.Items[j].Name)
		})
		decoder := yaml.NewDecoder(strings.NewReader(wantSvcYAML))
		var wantServices []corev1.Service
		for {
			var wantService corev1.Service
			err := decoder.Decode(&wantService)
			if err == io.EOF {
				break
			}
			assert.NoError(t, err)
			wantServices = append(wantServices, wantService)
		}
		assert.Equal(t, wantServices, ls.Items)
	}

	f(&vmv1beta1.VMCluster{
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
        - name: test
          controller: true
          blockownerdeletion: true
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
    sessionaffinity: None
    internaltrafficpolicy: Cluster
    publishnotreadyaddresses: true
`)
	// with vmbackup and additional service ports
	f(&vmv1beta1.VMCluster{
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
        - name: test
          controller: true
          blockownerdeletion: true
spec:
    ports:
        - name: web-rpc
          protocol: TCP
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
    sessionaffinity: None
    internaltrafficpolicy: Cluster
    publishnotreadyaddresses: true
`)

	f(&vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{},
			VMSelect: &vmv1beta1.VMSelect{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
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
        - name: test
          controller: true
          blockownerdeletion: true
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
    sessionaffinity: None
    internaltrafficpolicy: Cluster
    publishnotreadyaddresses: true
---
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
        - name: test
          controller: true
          blockownerdeletion: true
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
    sessionaffinity: None
    internaltrafficpolicy: Cluster
    publishnotreadyaddresses: true
`)
	// with native and extra service
	f(&vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{},
			VMSelect: &vmv1beta1.VMSelect{CommonAppsParams: vmv1beta1.CommonAppsParams{Port: "8352"},
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
        - name: test
          controller: true
          blockownerdeletion: true
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
    sessionaffinity: None
    internaltrafficpolicy: Cluster
    publishnotreadyaddresses: true
---
objectmeta:
    name: vmselect-test-additional-service
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmselect
        app.kubernetes.io/part-of: vmcluster
        managed-by: vm-operator
        operator.victoriametrics.com/additional-service: managed
    ownerreferences:
        - name: test
          controller: true
          blockownerdeletion: true
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
    type: LoadBalancer
    sessionaffinity: None
    externaltrafficpolicy: Cluster
    allocateloadbalancernodeports: true
    internaltrafficpolicy: Cluster
---
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
        - name: test
          controller: true
          blockownerdeletion: true
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
    sessionaffinity: None
    internaltrafficpolicy: Cluster
    publishnotreadyaddresses: true
`)
	f(&vmv1beta1.VMCluster{
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
        - name: test
          controller: true
          blockownerdeletion: true
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
    type: ClusterIP
    sessionaffinity: None
    internaltrafficpolicy: Cluster
`)
	// transit to headless
	f(&vmv1beta1.VMCluster{
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
        - name: test
          controller: true
          blockownerdeletion: true
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
    sessionaffinity: None
    internaltrafficpolicy: Cluster
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
	f(&vmv1beta1.VMCluster{
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
        - name: test
          controller: true
          blockownerdeletion: true
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
    sessionaffinity: None
    externaltrafficpolicy: Cluster
    allocateloadbalancernodeports: true
    internaltrafficpolicy: Cluster
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

	// insert with load-balancer and additional service
	f(&vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default-1"},
		Spec: vmv1beta1.VMClusterSpec{
			RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
				Enabled: true,
			},
			VMInsert: &vmv1beta1.VMInsert{
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
						Labels: map[string]string{
							"app.kubernetes.io/instance": "incorrect-label",
						},
						Annotations: map[string]string{
							"service.beta.kubernetes.io/aws-load-balancer-type": "external",
						},
					},
					Spec: corev1.ServiceSpec{
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
    name: vmclusterlb-test
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmclusterlb-vmauth-balancer
        app.kubernetes.io/part-of: vmcluster
        managed-by: vm-operator
        operator.victoriametrics.com/vmauthlb-proxy-name: vmauth
    ownerreferences:
        - name: test
          controller: true
          blockownerdeletion: true
spec:
    ports:
        - name: http
          protocol: TCP
          port: 8427
          targetport:
            intval: 8427
    selector:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmclusterlb-vmauth-balancer
        managed-by: vm-operator
    type: ClusterIP
    sessionaffinity: None
    internaltrafficpolicy: Cluster
---
objectmeta:
    name: vminsert-test
    namespace: default-1
    resourceversion: "1000"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vminsert
        app.kubernetes.io/part-of: vmcluster
        managed-by: vm-operator
        operator.victoriametrics.com/vmauthlb-proxy-name: insert
    ownerreferences:
        - name: test
          controller: true
          blockownerdeletion: true
spec:
    ports:
        - name: http
          protocol: TCP
          port: 8480
          targetport:
            intval: 8427
    selector:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmclusterlb-vmauth-balancer
        managed-by: vm-operator
    clusterip: 10.0.0.5
    type: ClusterIP
    sessionaffinity: None
    internaltrafficpolicy: Cluster
---
objectmeta:
    name: vminsert-test-additional-service
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vminsert
        app.kubernetes.io/part-of: vmcluster
        managed-by: vm-operator
        operator.victoriametrics.com/additional-service: managed
        operator.victoriametrics.com/vmauthlb-proxy-job-name: vminsert-test
    annotations:
        service.beta.kubernetes.io/aws-load-balancer-type: external
    ownerreferences:
        - name: test
          controller: true
          blockownerdeletion: true
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
    sessionaffinity: None
    externaltrafficpolicy: Cluster
    allocateloadbalancernodeports: true
    internaltrafficpolicy: Cluster
    loadbalancerclass: service.k8s.aws/nlb
---
objectmeta:
    name: vminsertinternal-test
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vminsert
        app.kubernetes.io/part-of: vmcluster
        managed-by: vm-operator
        operator.victoriametrics.com/vmauthlb-proxy-job-name: vminsert-test
    ownerreferences:
        - name: test
          controller: true
          blockownerdeletion: true
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
    clusterip: None
    type: ClusterIP
    sessionaffinity: None
    internaltrafficpolicy: Cluster
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

	// insert with load-balancer and custom service spec
	f(&vmv1beta1.VMCluster{
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
    name: vmclusterlb-test
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmclusterlb-vmauth-balancer
        app.kubernetes.io/part-of: vmcluster
        managed-by: vm-operator
        operator.victoriametrics.com/vmauthlb-proxy-name: vmauth
    ownerreferences:
        - name: test
          controller: true
          blockownerdeletion: true
spec:
    ports:
        - name: http
          protocol: TCP
          port: 8427
          targetport:
            intval: 8427
    selector:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vmclusterlb-vmauth-balancer
        managed-by: vm-operator
    type: ClusterIP
    sessionaffinity: None
    internaltrafficpolicy: Cluster
---
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
        operator.victoriametrics.com/vmauthlb-proxy-name: insert
    annotations:
        service.beta.kubernetes.io/aws-load-balancer-type: external
    ownerreferences:
        - name: test
          controller: true
          blockownerdeletion: true
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
        app.kubernetes.io/name: vmclusterlb-vmauth-balancer
        managed-by: vm-operator
    type: LoadBalancer
    sessionaffinity: None
    externaltrafficpolicy: Cluster
    allocateloadbalancernodeports: true
    internaltrafficpolicy: Cluster
    loadbalancerclass: service.k8s.aws/nlb
---
objectmeta:
    name: vminsertinternal-test
    namespace: default-1
    resourceversion: "1"
    labels:
        app.kubernetes.io/component: monitoring
        app.kubernetes.io/instance: test
        app.kubernetes.io/name: vminsert
        app.kubernetes.io/part-of: vmcluster
        managed-by: vm-operator
        operator.victoriametrics.com/vmauthlb-proxy-job-name: vminsert-test
    annotations:
        service.beta.kubernetes.io/aws-load-balancer-type: external
    ownerreferences:
        - name: test
          controller: true
          blockownerdeletion: true
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
    clusterip: None
    type: ClusterIP
    sessionaffinity: None
    internaltrafficpolicy: Cluster
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

func TestVMClusterDiscoveryArgs(t *testing.T) {
	licenseKey := ptr.To("test-license-key")

	f := func(cr *vmv1beta1.VMCluster, validateSelect, validateInsert func(t *testing.T, args []string)) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(nil)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(cr)
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

		if validateSelect != nil && cr.Spec.VMSelect != nil {
			var sts appsv1.StatefulSet
			assert.NoError(t, fclient.Get(ctx, types.NamespacedName{
				Namespace: cr.Namespace,
				Name:      cr.PrefixedName(vmv1beta1.ClusterComponentSelect),
			}, &sts))
			var args []string
			for _, c := range sts.Spec.Template.Spec.Containers {
				if c.Name == "vmselect" {
					args = c.Args
				}
			}
			validateSelect(t, args)
		}
		if validateInsert != nil && cr.Spec.VMInsert != nil {
			var dep appsv1.Deployment
			assert.NoError(t, fclient.Get(ctx, types.NamespacedName{
				Namespace: cr.Namespace,
				Name:      cr.PrefixedName(vmv1beta1.ClusterComponentInsert),
			}, &dep))
			var args []string
			for _, c := range dep.Spec.Template.Spec.Containers {
				if c.Name == "vminsert" {
					args = c.Args
				}
			}
			validateInsert(t, args)
		}
	}

	hasArg := func(args []string, prefix string) bool {
		for _, a := range args {
			if strings.HasPrefix(a, prefix) {
				return true
			}
		}
		return false
	}

	// global discovery enabled: both vmselect and vminsert get srv+ storageNode
	f(&vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: vmv1beta1.VMClusterSpec{
			License:   &vmv1beta1.License{Key: licenseKey},
			Discovery: &vmv1beta1.VMClusterDiscovery{Enabled: true},
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(2))},
			},
			VMSelect: &vmv1beta1.VMSelect{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
			},
			VMInsert: &vmv1beta1.VMInsert{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
			},
		},
	},
		func(t *testing.T, args []string) {
			assert.True(t, hasArg(args, "-storageNode=srv+"), "vmselect: expected srv+ storageNode, got %v", args)
		},
		func(t *testing.T, args []string) {
			assert.True(t, hasArg(args, "-storageNode=srv+"), "vminsert: expected srv+ storageNode, got %v", args)
		},
	)

	// global discovery disabled: both get individual pod addresses
	f(&vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(2))},
			},
			VMSelect: &vmv1beta1.VMSelect{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
			},
			VMInsert: &vmv1beta1.VMInsert{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
			},
		},
	},
		func(t *testing.T, args []string) {
			assert.False(t, hasArg(args, "-storageNode=srv+"), "vmselect: expected individual pod addresses, got %v", args)
			assert.True(t, hasArg(args, "-storageNode="), "vmselect: expected storageNode flag, got %v", args)
		},
		func(t *testing.T, args []string) {
			assert.False(t, hasArg(args, "-storageNode=srv+"), "vminsert: expected individual pod addresses, got %v", args)
			assert.True(t, hasArg(args, "-storageNode="), "vminsert: expected storageNode flag, got %v", args)
		},
	)

	// component override: vminsert discovery disabled, vmselect inherits global (enabled)
	f(&vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: vmv1beta1.VMClusterSpec{
			License:   &vmv1beta1.License{Key: licenseKey},
			Discovery: &vmv1beta1.VMClusterDiscovery{Enabled: true},
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(2))},
			},
			VMSelect: &vmv1beta1.VMSelect{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
			},
			VMInsert: &vmv1beta1.VMInsert{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
				Discovery:        &vmv1beta1.VMClusterDiscovery{Enabled: false},
			},
		},
	},
		func(t *testing.T, args []string) {
			assert.True(t, hasArg(args, "-storageNode=srv+"), "vmselect: expected srv+ storageNode, got %v", args)
		},
		func(t *testing.T, args []string) {
			assert.False(t, hasArg(args, "-storageNode=srv+"), "vminsert: expected individual pod addresses, got %v", args)
		},
	)

	// discovery with interval and filter flags
	// pod addresses take the form vmstorage-test-N.vmstorage-test.default..., so the filter must include the cluster name
	f(&vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: vmv1beta1.VMClusterSpec{
			License: &vmv1beta1.License{Key: licenseKey},
			Discovery: &vmv1beta1.VMClusterDiscovery{
				Enabled:  true,
				Interval: "5s",
				Filter:   `vmstorage-test-[0-1]\.`,
			},
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(2))},
			},
			VMSelect: &vmv1beta1.VMSelect{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
			},
			VMInsert: &vmv1beta1.VMInsert{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
			},
		},
	},
		func(t *testing.T, args []string) {
			assert.True(t, hasArg(args, "-storageNode=srv+"), "vmselect: expected srv+ storageNode")
			assert.True(t, hasArg(args, "-storageNode.discoveryInterval=5s"), "vmselect: expected discoveryInterval flag, got %v", args)
			assert.True(t, hasArg(args, `-storageNode.filter=vmstorage-test-[0-1]\.`), "vmselect: expected filter flag, got %v", args)
		},
		func(t *testing.T, args []string) {
			assert.True(t, hasArg(args, "-storageNode=srv+"), "vminsert: expected srv+ storageNode")
			assert.True(t, hasArg(args, "-storageNode.discoveryInterval=5s"), "vminsert: expected discoveryInterval flag, got %v", args)
			assert.True(t, hasArg(args, `-storageNode.filter=vmstorage-test-[0-1]\.`), "vminsert: expected filter flag, got %v", args)
		},
	)

	// pools: vmselect gets pool-grouped storage nodes, shared vminsert gets plain addresses
	f(&vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(2))},
			},
			VMSelect: &vmv1beta1.VMSelect{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
			},
			VMInsert: &vmv1beta1.VMInsert{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
			},
			Pools: []vmv1beta1.VMClusterPool{
				{Name: "hot", VMStorage: &vmv1beta1.VMStorage{CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(2))}}},
				{Name: "cold", VMStorage: &vmv1beta1.VMStorage{CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))}}},
			},
		},
	},
		func(t *testing.T, args []string) {
			// vmselect: each pool appears as a named storage group (poolName/addr)
			assert.True(t, hasArg(args, "-storageNode=hot/"), "vmselect: expected hot pool storageNode, got %v", args)
			assert.True(t, hasArg(args, "-storageNode=cold/"), "vmselect: expected cold pool storageNode, got %v", args)
			// no top-level (ungrouped) storage node when pools are defined
			for _, a := range args {
				if strings.HasPrefix(a, "-storageNode=") {
					val := strings.TrimPrefix(a, "-storageNode=")
					assert.True(t, strings.Contains(val, "/"), "vmselect: expected all storageNodes to be pool-grouped, got %q", a)
				}
			}
		},
		func(t *testing.T, args []string) {
			// shared vminsert: pool storage nodes are plain addresses without pool prefix
			assert.True(t, hasArg(args, "-storageNode="), "vminsert: expected storageNode flag, got %v", args)
			assert.False(t, hasArg(args, "-storageNode=hot/"), "vminsert: should not have pool-grouped addresses, got %v", args)
			assert.False(t, hasArg(args, "-storageNode=cold/"), "vminsert: should not have pool-grouped addresses, got %v", args)
		},
	)

	// component-level discovery overrides interval and filter
	f(&vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: vmv1beta1.VMClusterSpec{
			License: &vmv1beta1.License{Key: licenseKey},
			Discovery: &vmv1beta1.VMClusterDiscovery{
				Enabled:  true,
				Interval: "5s",
				Filter:   `vmstorage-test-[0-1]\.`,
			},
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(4))},
			},
			VMSelect: &vmv1beta1.VMSelect{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
				Discovery: &vmv1beta1.VMClusterDiscovery{
					Enabled:  true,
					Interval: "10s",
					Filter:   `vmstorage-test-[2-3]\.`,
				},
			},
			VMInsert: &vmv1beta1.VMInsert{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
			},
		},
	},
		func(t *testing.T, args []string) {
			assert.True(t, hasArg(args, "-storageNode.discoveryInterval=10s"), "vmselect: expected overridden interval, got %v", args)
			assert.True(t, hasArg(args, `-storageNode.filter=vmstorage-test-[2-3]\.`), "vmselect: expected overridden filter, got %v", args)
		},
		func(t *testing.T, args []string) {
			assert.True(t, hasArg(args, "-storageNode.discoveryInterval=5s"), "vminsert: expected global interval, got %v", args)
			assert.True(t, hasArg(args, `-storageNode.filter=vmstorage-test-[0-1]\.`), "vminsert: expected global filter, got %v", args)
		},
	)
}
