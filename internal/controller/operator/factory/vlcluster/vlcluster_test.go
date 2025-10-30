package vlcluster

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	f := func(cr *vmv1.VLCluster, validate func(ctx context.Context, rclient client.Client) error, predefinedObjects []runtime.Object) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(cr)
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
		if cr.Spec.VLStorage != nil {
			var vlst appsv1.StatefulSet
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: cr.GetVLStorageName(), Namespace: cr.Namespace}, &vlst); err != nil {
					return err
				}
				vlst.Status.ReadyReplicas = *cr.Spec.VLStorage.ReplicaCount
				vlst.Status.UpdatedReplicas = *cr.Spec.VLStorage.ReplicaCount
				if err := fclient.Status().Update(ctx, &vlst); err != nil {
					return err
				}

				return nil
			})
		}
		if cr.Spec.VLSelect != nil {
			var vls appsv1.Deployment
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: cr.GetVLSelectName(), Namespace: cr.Namespace}, &vls); err != nil {
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
		if cr.Spec.VLInsert != nil {
			var vli appsv1.Deployment
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: cr.GetVLInsertName(), Namespace: cr.Namespace}, &vli); err != nil {
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
		if cr.Spec.RequestsLoadBalancer.Enabled {
			var vmauthLB appsv1.Deployment
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: cr.GetVMAuthLBName(), Namespace: cr.Namespace}, &vmauthLB); err != nil {
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
		if err := CreateOrUpdate(ctx, fclient, cr); err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if validate != nil {
			if err := validate(ctx, fclient); err != nil {
				t.Fatalf("unexpected validation error: %s", err)
			}
		}
	}

	// base cluster
	cr := &vmv1.VLCluster{
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
	}
	validate := func(ctx context.Context, rclient client.Client) error {
		// ensure SA created
		var sa corev1.ServiceAccount
		assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetServiceAccountName(), Namespace: cr.Namespace}, &sa))
		assert.Nil(t, sa.Annotations)
		assert.Equal(t, sa.Labels, cr.FinalLabels(cr.SelectorLabels()))

		// check insert
		var dep appsv1.Deployment
		assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetVLInsertName(), Namespace: cr.Namespace}, &dep))
		assert.Len(t, dep.Spec.Template.Spec.Containers, 1)
		cnt := dep.Spec.Template.Spec.Containers[0]
		assert.Equal(t, cnt.Args, []string{"-httpListenAddr=:9481", "-internalselect.disable=true", "-storageNode=vlstorage-base-0.vlstorage-base.default:9491,vlstorage-base-1.vlstorage-base.default:9491"})
		assert.Nil(t, dep.Annotations)
		assert.Equal(t, dep.Labels, cr.FinalLabels(cr.VLInsertSelectorLabels()))

		// check select
		assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetVLSelectName(), Namespace: cr.Namespace}, &dep))
		assert.Len(t, dep.Spec.Template.Spec.Containers, 1)
		cnt = dep.Spec.Template.Spec.Containers[0]
		assert.Equal(t, cnt.Args, []string{"-httpListenAddr=:9471", "-internalinsert.disable=true", "-storageNode=vlstorage-base-0.vlstorage-base.default:9491,vlstorage-base-1.vlstorage-base.default:9491"})
		assert.Nil(t, dep.Annotations)
		assert.Equal(t, dep.Labels, cr.FinalLabels(cr.VLSelectSelectorLabels()))

		// check storage
		var sts appsv1.StatefulSet
		assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetVLStorageName(), Namespace: cr.Namespace}, &sts))
		assert.Len(t, sts.Spec.Template.Spec.Containers, 1)
		cnt = sts.Spec.Template.Spec.Containers[0]
		assert.Equal(t, cnt.Args, []string{"-httpListenAddr=:9491", "-storageDataPath=/vlstorage-data"})
		assert.Nil(t, sts.Annotations)
		assert.Equal(t, sts.Labels, cr.FinalLabels(cr.VLStorageSelectorLabels()))

		return nil
	}

	f(cr, validate, nil)

	// with storage retention
	cr = &vmv1.VLCluster{
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
	}
	validate = func(ctx context.Context, rclient client.Client) error {

		// check storage
		var sts appsv1.StatefulSet
		assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetVLStorageName(), Namespace: cr.Namespace}, &sts))
		assert.Len(t, sts.Spec.Template.Spec.Containers, 1)
		cnt := sts.Spec.Template.Spec.Containers[0]
		assert.Equal(t, cnt.Args, []string{"-futureRetention=2d", "-httpListenAddr=:9491", "-retention.maxDiskSpaceUsageBytes=5GB", "-retentionPeriod=1w", "-storageDataPath=/vlstorage-data"})

		return nil
	}

	f(cr, validate, nil)

	// with extra read-only storages
	cr = &vmv1.VLCluster{
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
	}
	validate = func(ctx context.Context, rclient client.Client) error {

		// check select
		var d appsv1.Deployment
		assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.GetVLSelectName(), Namespace: cr.Namespace}, &d))
		assert.Len(t, d.Spec.Template.Spec.Containers, 1)
		cnt := d.Spec.Template.Spec.Containers[0]
		assert.Equal(t, cnt.Args, []string{
			"-httpListenAddr=:9471",
			"-internalinsert.disable=true",
			"-storageNode=vlstorage-read-only-0.vlstorage-read-only.default:9491,localhost:10101",
		})
		return nil
	}

	f(cr, validate, nil)

}
