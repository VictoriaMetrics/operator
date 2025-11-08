package vtcluster

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
	type opts struct {
		cr                *vmv1.VTCluster
		validate          func(ctx context.Context, rclient client.Client, cr *vmv1.VTCluster) error
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(opts.cr)
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
		if opts.cr.Spec.Storage != nil {
			var vtst appsv1.StatefulSet
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: opts.cr.PrefixedName(vmv1beta1.ClusterComponentStorage), Namespace: opts.cr.Namespace}, &vtst); err != nil {
					return err
				}
				vtst.Status.ReadyReplicas = *opts.cr.Spec.Storage.ReplicaCount
				vtst.Status.UpdatedReplicas = *opts.cr.Spec.Storage.ReplicaCount
				if err := fclient.Status().Update(ctx, &vtst); err != nil {
					return err
				}

				return nil
			})
		}
		if opts.cr.Spec.Select != nil {
			var vts appsv1.Deployment
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: opts.cr.PrefixedName(vmv1beta1.ClusterComponentSelect), Namespace: opts.cr.Namespace}, &vts); err != nil {
					return err
				}
				vts.Status.Conditions = append(vts.Status.Conditions, appsv1.DeploymentCondition{
					Type:   appsv1.DeploymentProgressing,
					Reason: "NewReplicaSetAvailable",
					Status: "True",
				})
				vts.Status.UpdatedReplicas = *vts.Spec.Replicas
				vts.Status.AvailableReplicas = vts.Status.UpdatedReplicas
				if err := fclient.Status().Update(ctx, &vts); err != nil {
					return err
				}

				return nil
			})

		}
		if opts.cr.Spec.Insert != nil {
			var vti appsv1.Deployment
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: opts.cr.PrefixedName(vmv1beta1.ClusterComponentInsert), Namespace: opts.cr.Namespace}, &vti); err != nil {
					return err
				}
				vti.Status.Conditions = append(vti.Status.Conditions, appsv1.DeploymentCondition{
					Type:   appsv1.DeploymentProgressing,
					Reason: "NewReplicaSetAvailable",
					Status: "True",
				})
				vti.Status.UpdatedReplicas = *vti.Spec.Replicas
				vti.Status.AvailableReplicas = vti.Status.UpdatedReplicas
				if err := fclient.Status().Update(ctx, &vti); err != nil {
					return err
				}
				return nil
			})

		}
		if opts.cr.Spec.RequestsLoadBalancer.Enabled {
			var vmauthLB appsv1.Deployment
			eventuallyUpdateStatusToOk(func() error {
				if err := fclient.Get(ctx, types.NamespacedName{Name: opts.cr.PrefixedName(vmv1beta1.ClusterComponentBalancer), Namespace: opts.cr.Namespace}, &vmauthLB); err != nil {
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
		if err := CreateOrUpdate(ctx, fclient, opts.cr); err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if opts.validate != nil {
			if err := opts.validate(ctx, fclient, opts.cr); err != nil {
				t.Fatalf("unexpected validation error: %s", err)
			}
		}
	}

	// base cluster
	o := opts{
		cr: &vmv1.VTCluster{
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
			Spec: vmv1.VTClusterSpec{
				Insert: &vmv1.VTInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(2)),
					},
				},
				Storage: &vmv1.VTStorage{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(2)),
					},
				},
				Select: &vmv1.VTSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(2)),
					},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VTCluster) error {
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
			assert.Equal(t, cnt.Args, []string{"-httpListenAddr=:10481", "-internalselect.disable=true", "-storageNode=vtstorage-base-0.vtstorage-base.default:10491,vtstorage-base-1.vtstorage-base.default:10491"})
			assert.Nil(t, dep.Annotations)
			assert.Equal(t, dep.Labels, cr.FinalLabels(vmv1beta1.ClusterComponentInsert))

			// check select
			assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(vmv1beta1.ClusterComponentSelect), Namespace: cr.Namespace}, &dep))
			assert.Len(t, dep.Spec.Template.Spec.Containers, 1)
			cnt = dep.Spec.Template.Spec.Containers[0]
			assert.Equal(t, cnt.Args, []string{"-httpListenAddr=:10471", "-internalinsert.disable=true", "-storageNode=vtstorage-base-0.vtstorage-base.default:10491,vtstorage-base-1.vtstorage-base.default:10491"})
			assert.Nil(t, dep.Annotations)
			assert.Equal(t, dep.Labels, cr.FinalLabels(vmv1beta1.ClusterComponentSelect))

			// check storage
			var sts appsv1.StatefulSet
			assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(vmv1beta1.ClusterComponentStorage), Namespace: cr.Namespace}, &sts))
			assert.Len(t, sts.Spec.Template.Spec.Containers, 1)
			cnt = sts.Spec.Template.Spec.Containers[0]
			assert.Equal(t, cnt.Args, []string{"-httpListenAddr=:10491", "-storageDataPath=/vtstorage-data"})
			assert.Nil(t, sts.Annotations)
			assert.Equal(t, sts.Labels, cr.FinalLabels(vmv1beta1.ClusterComponentStorage))

			return nil
		},
	}
	f(o)

	// with storage retention
	o = opts{
		cr: &vmv1.VTCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "retention",
				Namespace: "default",
			},
			Spec: vmv1.VTClusterSpec{
				Storage: &vmv1.VTStorage{
					RetentionPeriod:                 "1w",
					RetentionMaxDiskSpaceUsageBytes: "5GB",
					FutureRetention:                 "2d",
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VTCluster) error {

			// check storage
			var sts appsv1.StatefulSet
			assert.Nil(t, rclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(vmv1beta1.ClusterComponentStorage), Namespace: cr.Namespace}, &sts))
			assert.Len(t, sts.Spec.Template.Spec.Containers, 1)
			cnt := sts.Spec.Template.Spec.Containers[0]
			assert.Equal(t, cnt.Args, []string{"-futureRetention=2d", "-httpListenAddr=:10491", "-retention.maxDiskSpaceUsageBytes=5GB", "-retentionPeriod=1w", "-storageDataPath=/vtstorage-data"})

			return nil
		},
	}
	f(o)
}
