package vlcluster

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1.VLCluster
		validate          func(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster)
		cfgMutator        func(*config.BaseOperatorConf)
		predefinedObjects []runtime.Object
		wantErr           bool
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
		build.AddDefaults(fclient.Scheme())
		ctx := context.Background()
		fclient.Scheme().Default(o.cr)
		err := CreateOrUpdate(ctx, fclient, o.cr.DeepCopy())
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		if o.validate != nil {
			o.validate(ctx, fclient, o.cr)
		}
	}

	// base cluster
	f(opts{
		cr: &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
				Labels: map[string]string{
					"only-main-object-label": "value",
				},
				Annotations: map[string]string{
					"only-main-object-annotation": "value",
				},
			},
			Spec: vmv1.VLClusterSpec{
				VLInsert: &vmv1.VLInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(2)),
					},
				},
				VLStorage: &vmv1.VLStorage{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(2)),
					},
				},
				VLSelect: &vmv1.VLSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(2)),
					},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster) {
			// ensure SA created
			var sa corev1.ServiceAccount
			assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetServiceAccountName(), Namespace: cr.Namespace}, &sa))
			assert.Nil(t, sa.Annotations)
			assert.Equal(t, sa.Labels, cr.FinalLabels(vmv1beta1.ClusterComponentRoot))

			// check insert
			var dep appsv1.Deployment
			assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(vmv1beta1.ClusterComponentInsert), Namespace: cr.Namespace}, &dep))
			assert.Len(t, dep.Spec.Template.Spec.Containers, 1)
			cnt := dep.Spec.Template.Spec.Containers[0]
			assert.Equal(t, cnt.Args, []string{"-http.shutdownDelay=30s", "-httpListenAddr=:9481", "-internalselect.disable=true", "-storageNode=vlstorage-base-0.vlstorage-base.default:9491,vlstorage-base-1.vlstorage-base.default:9491"})
			assert.Nil(t, dep.Annotations)
			assert.Equal(t, dep.Labels, cr.FinalLabels(vmv1beta1.ClusterComponentInsert))

			// check select
			assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(vmv1beta1.ClusterComponentSelect), Namespace: cr.Namespace}, &dep))
			assert.Len(t, dep.Spec.Template.Spec.Containers, 1)
			cnt = dep.Spec.Template.Spec.Containers[0]
			assert.Equal(t, cnt.Args, []string{"-http.shutdownDelay=30s", "-httpListenAddr=:9471", "-internalinsert.disable=true", "-storageNode=vlstorage-base-0.vlstorage-base.default:9491,vlstorage-base-1.vlstorage-base.default:9491"})
			assert.Nil(t, dep.Annotations)
			assert.Equal(t, dep.Labels, cr.FinalLabels(vmv1beta1.ClusterComponentSelect))

			// check storage
			var sts appsv1.StatefulSet
			assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(vmv1beta1.ClusterComponentStorage), Namespace: cr.Namespace}, &sts))
			assert.Len(t, sts.Spec.Template.Spec.Containers, 1)
			cnt = sts.Spec.Template.Spec.Containers[0]
			assert.Equal(t, cnt.Args, []string{"-http.shutdownDelay=30s", "-httpListenAddr=:9491", "-storageDataPath=/vlstorage-data"})
			assert.Nil(t, sts.Annotations)
			assert.Equal(t, sts.Labels, cr.FinalLabels(vmv1beta1.ClusterComponentStorage))
		},
	})

	// with storage retention
	f(opts{
		cr: &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "retention",
				Namespace: "default",
			},
			Spec: vmv1.VLClusterSpec{
				VLStorage: &vmv1.VLStorage{
					RetentionPeriod:                 "1w",
					RetentionMaxDiskSpaceUsageBytes: "5GB",
					FutureRetention:                 "2d",
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster) {
			// check storage
			var sts appsv1.StatefulSet
			assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(vmv1beta1.ClusterComponentStorage), Namespace: cr.Namespace}, &sts))
			assert.Len(t, sts.Spec.Template.Spec.Containers, 1)
			cnt := sts.Spec.Template.Spec.Containers[0]
			assert.Equal(t, cnt.Args, []string{"-futureRetention=2d", "-http.shutdownDelay=30s", "-httpListenAddr=:9491", "-retention.maxDiskSpaceUsageBytes=5GB", "-retentionPeriod=1w", "-storageDataPath=/vlstorage-data"})
		},
	})

	// with extra read-only storages
	f(opts{
		cr: &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "read-only",
				Namespace: "default",
			},
			Spec: vmv1.VLClusterSpec{
				VLSelect: &vmv1.VLSelect{
					ExtraStorageNodes: []vmv1.VLStorageNode{
						{
							Addr: "localhost:10101",
						},
					},
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				VLStorage: &vmv1.VLStorage{
					RetentionPeriod: "1w",
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster) {
			// check select
			var d appsv1.Deployment
			assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(vmv1beta1.ClusterComponentSelect), Namespace: cr.Namespace}, &d))
			assert.Len(t, d.Spec.Template.Spec.Containers, 1)
			cnt := d.Spec.Template.Spec.Containers[0]
			assert.Equal(t, cnt.Args, []string{
				"-http.shutdownDelay=30s",
				"-httpListenAddr=:9471",
				"-internalinsert.disable=true",
				"-storageNode=vlstorage-read-only-0.vlstorage-read-only.default:9491,localhost:10101",
			})
		},
	})

	// fail with scaledown for storage
	f(opts{
		cr: &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "read-only",
				Namespace: "default",
			},
			Spec: vmv1.VLClusterSpec{
				VLSelect: &vmv1.VLSelect{
					ExtraStorageNodes: []vmv1.VLStorageNode{
						{
							Addr: "localhost:10101",
						},
					},
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				VLStorage: &vmv1.VLStorage{
					RetentionPeriod: "1w",
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
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

	// with insert VPA
	f(opts{
		cr: &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: vmv1.VLClusterSpec{
				VLInsert: &vmv1.VLInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(0)),
					},
					VPA: &vmv1beta1.EmbeddedVPA{
						UpdatePolicy: &vpav1.PodUpdatePolicy{
							UpdateMode: ptr.To(vpav1.UpdateModeInitial),
						},
						ResourcePolicy: &vpav1.PodResourcePolicy{
							ContainerPolicies: []vpav1.ContainerResourcePolicy{
								{ContainerName: "vlinsert"},
							},
						},
					},
				},
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.VPAAPIEnabled = true
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster) {
			var got vpav1.VerticalPodAutoscaler
			vpaName := cr.PrefixedName(vmv1beta1.ClusterComponentInsert)
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: vpaName}, &got))
			expected := vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:            vpaName,
					Namespace:       cr.Namespace,
					Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentInsert),
					ResourceVersion: "1",
					OwnerReferences: []metav1.OwnerReference{{Name: "test", Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(true)}},
				},
				Spec: vpav1.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						Name:       vpaName,
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeInitial),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vlinsert"},
						},
					},
				},
			}
			assert.Equal(t, got, expected)
		},
	})

	// with select VPA
	f(opts{
		cr: &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: vmv1.VLClusterSpec{
				VLSelect: &vmv1.VLSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(0)),
					},
					VPA: &vmv1beta1.EmbeddedVPA{
						UpdatePolicy: &vpav1.PodUpdatePolicy{
							UpdateMode: ptr.To(vpav1.UpdateModeRecreate),
						},
						ResourcePolicy: &vpav1.PodResourcePolicy{
							ContainerPolicies: []vpav1.ContainerResourcePolicy{
								{
									ContainerName: "vlselect",
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
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster) {
			var got vpav1.VerticalPodAutoscaler
			component := vmv1beta1.ClusterComponentSelect
			vpaName := cr.PrefixedName(component)
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: vpaName}, &got))
			expected := vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:            vpaName,
					Namespace:       cr.Namespace,
					Labels:          cr.FinalLabels(component),
					ResourceVersion: "1",
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
						ContainerPolicies: []vpav1.ContainerResourcePolicy{{
							ContainerName: "vlselect",
							Mode:          ptr.To(vpav1.ContainerScalingModeAuto),
						}},
					},
					Recommenders: []*vpav1.VerticalPodAutoscalerRecommenderSelector{
						{Name: "custom-recommender"},
					},
				},
			}
			assert.Equal(t, got, expected)
		},
	})

	// with storage VPA
	f(opts{
		cr: &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: vmv1.VLClusterSpec{
				VLStorage: &vmv1.VLStorage{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(0)),
					},
					VPA: &vmv1beta1.EmbeddedVPA{
						UpdatePolicy: &vpav1.PodUpdatePolicy{
							UpdateMode: ptr.To(vpav1.UpdateModeInitial),
						},
						ResourcePolicy: &vpav1.PodResourcePolicy{
							ContainerPolicies: []vpav1.ContainerResourcePolicy{
								{ContainerName: "vlstorage"},
							},
						},
					},
				},
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.VPAAPIEnabled = true
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster) {
			component := vmv1beta1.ClusterComponentStorage
			var got vpav1.VerticalPodAutoscaler
			vpaName := cr.PrefixedName(component)
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: vpaName}, &got))
			expected := vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:            vpaName,
					Namespace:       cr.Namespace,
					Labels:          cr.FinalLabels(component),
					ResourceVersion: "1",
					OwnerReferences: []metav1.OwnerReference{{Name: "test", Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(true)}},
				},
				Spec: vpav1.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						Name:       vpaName,
						Kind:       "StatefulSet",
						APIVersion: "apps/v1",
					},
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeInitial),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vlstorage"},
						},
					},
				},
			}
			assert.Equal(t, got, expected)
		},
	})

	// update VPA on insert
	f(opts{
		predefinedObjects: []runtime.Object{
			&vpav1.VerticalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vlinsert-test",
					Namespace: "default",
				},
				Spec: vpav1.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						Name:       "vlinsert-test",
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeInitial),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vlinsert"},
						},
					},
				},
			},
		},
		cr: &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: vmv1.VLClusterSpec{
				VLInsert: &vmv1.VLInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(0)),
					},
					VPA: &vmv1beta1.EmbeddedVPA{
						UpdatePolicy: &vpav1.PodUpdatePolicy{
							UpdateMode: ptr.To(vpav1.UpdateModeRecreate),
						},
						ResourcePolicy: &vpav1.PodResourcePolicy{
							ContainerPolicies: []vpav1.ContainerResourcePolicy{
								{ContainerName: "vlinsert"},
							},
						},
					},
				},
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.VPAAPIEnabled = true
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster) {
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
							{ContainerName: "vlinsert"},
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
					Name:            "vlinsert-test",
					Namespace:       "default",
					ResourceVersion: "1",
					OwnerReferences: []metav1.OwnerReference{{Name: "test", Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(true)}},
					Labels: map[string]string{
						"app.kubernetes.io/instance":  "test",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
						"app.kubernetes.io/name":      "vlinsert",
						"app.kubernetes.io/part-of":   "vlcluster",
					},
				},
				Spec: vpav1.VerticalPodAutoscalerSpec{
					TargetRef: &autoscalingv1.CrossVersionObjectReference{
						Name:       "vlinsert-test",
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					UpdatePolicy: &vpav1.PodUpdatePolicy{
						UpdateMode: ptr.To(vpav1.UpdateModeInitial),
					},
					ResourcePolicy: &vpav1.PodResourcePolicy{
						ContainerPolicies: []vpav1.ContainerResourcePolicy{
							{ContainerName: "vlinsert"},
						},
					},
				},
			},
		},
		cr: &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: vmv1.VLClusterSpec{
				VLInsert: &vmv1.VLInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(0)),
					},
				},
			},
			Status: vmv1.VLClusterStatus{
				LastAppliedSpec: &vmv1.VLClusterSpec{},
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.VPAAPIEnabled = true
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster) {
			component := vmv1beta1.ClusterComponentInsert
			var got vpav1.VerticalPodAutoscaler
			vpaName := cr.PrefixedName(component)
			err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: vpaName}, &got)
			assert.Error(t, err)
			assert.True(t, k8serrors.IsNotFound(err))
		},
	})
}
