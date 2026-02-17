package vlcluster

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
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
		fclient.Scheme().Default(o.cr)
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		defer func() {
			cancel()
			wg.Wait()
		}()
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
		if o.cr.Spec.VLStorage != nil {
			var vlst appsv1.StatefulSet
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: o.cr.PrefixedName(vmv1beta1.ClusterComponentStorage), Namespace: o.cr.Namespace}, &vlst); err != nil {
					return err
				}
				vlst.Status.ReadyReplicas = *o.cr.Spec.VLStorage.ReplicaCount
				vlst.Status.UpdatedReplicas = *o.cr.Spec.VLStorage.ReplicaCount
				if err := fclient.Status().Update(ctx, &vlst); err != nil {
					return err
				}

				return nil
			})
		}
		if o.cr.Spec.VLSelect != nil {
			var vls appsv1.Deployment
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: o.cr.PrefixedName(vmv1beta1.ClusterComponentSelect), Namespace: o.cr.Namespace}, &vls); err != nil {
					return err
				}
				vls.Status.Conditions = append(vls.Status.Conditions, appsv1.DeploymentCondition{
					Type:   appsv1.DeploymentProgressing,
					Reason: "NewReplicaSetAvailable",
					Status: "True",
				})
				vls.Status.UpdatedReplicas = *vls.Spec.Replicas
				vls.Status.AvailableReplicas = vls.Status.UpdatedReplicas
				if err := fclient.Status().Update(ctx, &vls); err != nil {
					return err
				}

				return nil
			})

		}
		if o.cr.Spec.VLInsert != nil {
			var vli appsv1.Deployment
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: o.cr.PrefixedName(vmv1beta1.ClusterComponentInsert), Namespace: o.cr.Namespace}, &vli); err != nil {
					return err
				}
				vli.Status.Conditions = append(vli.Status.Conditions, appsv1.DeploymentCondition{
					Type:   appsv1.DeploymentProgressing,
					Reason: "NewReplicaSetAvailable",
					Status: "True",
				})
				vli.Status.UpdatedReplicas = *vli.Spec.Replicas
				vli.Status.AvailableReplicas = vli.Status.UpdatedReplicas
				if err := fclient.Status().Update(ctx, &vli); err != nil {
					return err
				}
				return nil
			})

		}
		if o.cr.Spec.RequestsLoadBalancer.Enabled {
			var vmauthLB appsv1.Deployment
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: o.cr.PrefixedName(vmv1beta1.ClusterComponentBalancer), Namespace: o.cr.Namespace}, &vmauthLB); err != nil {
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
		err := CreateOrUpdate(ctx, fclient, o.cr.DeepCopy())
		if (err != nil) != o.wantErr {
			t.Fatalf("unexpected error: %s", err)
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
			assert.Equal(t, cnt.Args, []string{"-httpListenAddr=:9481", "-internalselect.disable=true", "-storageNode=vlstorage-base-0.vlstorage-base.default:9491,vlstorage-base-1.vlstorage-base.default:9491"})
			assert.Nil(t, dep.Annotations)
			assert.Equal(t, dep.Labels, cr.FinalLabels(vmv1beta1.ClusterComponentInsert))

			// check select
			assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(vmv1beta1.ClusterComponentSelect), Namespace: cr.Namespace}, &dep))
			assert.Len(t, dep.Spec.Template.Spec.Containers, 1)
			cnt = dep.Spec.Template.Spec.Containers[0]
			assert.Equal(t, cnt.Args, []string{"-httpListenAddr=:9471", "-internalinsert.disable=true", "-storageNode=vlstorage-base-0.vlstorage-base.default:9491,vlstorage-base-1.vlstorage-base.default:9491"})
			assert.Nil(t, dep.Annotations)
			assert.Equal(t, dep.Labels, cr.FinalLabels(vmv1beta1.ClusterComponentSelect))

			// check storage
			var sts appsv1.StatefulSet
			assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(vmv1beta1.ClusterComponentStorage), Namespace: cr.Namespace}, &sts))
			assert.Len(t, sts.Spec.Template.Spec.Containers, 1)
			cnt = sts.Spec.Template.Spec.Containers[0]
			assert.Equal(t, cnt.Args, []string{"-httpListenAddr=:9491", "-storageDataPath=/vlstorage-data"})
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
			assert.Equal(t, cnt.Args, []string{"-futureRetention=2d", "-httpListenAddr=:9491", "-retention.maxDiskSpaceUsageBytes=5GB", "-retentionPeriod=1w", "-storageDataPath=/vlstorage-data"})
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
					Finalizers:      []string{vmv1beta1.FinalizerName},
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
			if !cmp.Equal(got, expected) {
				diff := cmp.Diff(got, expected)
				t.Fatal("not expected output with diff: ", diff)
			}
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
					Finalizers:      []string{vmv1beta1.FinalizerName},
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
			if !cmp.Equal(got, expected) {
				diff := cmp.Diff(got, expected)
				t.Fatal("not expected output with diff: ", diff)
			}
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
					Finalizers:      []string{vmv1beta1.FinalizerName},
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
			if !cmp.Equal(got, expected) {
				diff := cmp.Diff(got, expected)
				t.Fatal("not expected output with diff: ", diff)
			}
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
					Finalizers:      []string{vmv1beta1.FinalizerName},
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
			if !cmp.Equal(got, expected) {
				diff := cmp.Diff(got, expected)
				t.Fatal("not expected output with diff: ", diff)
			}
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
					Finalizers:      []string{vmv1beta1.FinalizerName},
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
			ParsedLastAppliedSpec: &vmv1.VLClusterSpec{},
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
