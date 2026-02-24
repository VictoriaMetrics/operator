package vmcluster

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr     *vmv1beta1.VMCluster
		preRun func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMCluster)
	}
	type want struct {
		actions []k8stools.ClientAction
		err     error
	}

	f := func(args args, want want) {
		t.Helper()

		fclient := k8stools.GetTestClientWithActionsAndObjects(nil)
		ctx := context.TODO()
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(args.cr)

		if args.preRun != nil {
			args.preRun(ctx, fclient, args.cr)
		}

		err := CreateOrUpdate(ctx, args.cr, fclient)
		if want.err != nil {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}

		if !assert.Equal(t, len(want.actions), len(fclient.Actions)) {
			for i, action := range fclient.Actions {
				t.Logf("Action %d: %s %s %s", i, action.Verb, action.Kind, action.Resource)
			}
		}

		for i, action := range want.actions {
			if i >= len(fclient.Actions) {
				break
			}
			assert.Equal(t, action.Verb, fclient.Actions[i].Verb, "idx %d verb", i)
			assert.Equal(t, action.Kind, fclient.Actions[i].Kind, "idx %d kind", i)
			assert.Equal(t, action.Resource, fclient.Actions[i].Resource, "idx %d resource", i)
		}
	}

	name := "example-cluster"
	namespace := "default"
	saName := types.NamespacedName{Namespace: namespace, Name: "vmcluster-" + name}
	vmstorageName := types.NamespacedName{Namespace: namespace, Name: "vmstorage-" + name}
	vmselectName := types.NamespacedName{Namespace: namespace, Name: "vmselect-" + name}
	vminsertName := types.NamespacedName{Namespace: namespace, Name: "vminsert-" + name}

	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}

	defaultCR := &vmv1beta1.VMCluster{
		ObjectMeta: objectMeta,
		Spec: vmv1beta1.VMClusterSpec{
			RetentionPeriod: "1",
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
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
		},
	}

	setupReadyComponents := func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMCluster) {
		// Create objects first
		assert.NoError(t, CreateOrUpdate(ctx, cr.DeepCopy(), c))

		// Create pods for StatefulSets to simulate readiness
		for _, stsName := range []types.NamespacedName{vmstorageName, vmselectName} {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stsName.Name + "-0",
					Namespace: stsName.Namespace,
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmstorage",
						"app.kubernetes.io/instance":  name,
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
						"controller-revision-hash":    "v1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "StatefulSet",
							Name:       stsName.Name,
							Controller: ptr.To(true),
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			}
			if stsName == vmselectName {
				pod.Labels["app.kubernetes.io/name"] = "vmselect"
			}
			assert.NoError(t, c.Create(ctx, pod))

			// Update STS status
			sts := &appsv1.StatefulSet{}
			if err := c.Get(ctx, stsName, sts); err == nil {
				sts.Status.CurrentRevision = "v1"
				sts.Status.UpdateRevision = "v1"
				sts.Status.ObservedGeneration = sts.Generation
				sts.Status.Replicas = 1
				sts.Status.ReadyReplicas = 1
				_ = c.Status().Update(ctx, sts)
			}
		}

		// clear actions
		c.Actions = nil
	}

	// create vmcluster with all components
	f(args{
		cr: defaultCR.DeepCopy(),
	},
		want{
			actions: []k8stools.ClientAction{
				// ServiceAccount
				{Verb: "Get", Kind: "ServiceAccount", Resource: saName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: saName},

				// VMStorage
				{Verb: "Get", Kind: "StatefulSet", Resource: vmstorageName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vmstorageName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmstorageName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vmstorageName},
				{Verb: "Create", Kind: "Service", Resource: vmstorageName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmstorageName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vmstorageName},

				// VMSelect
				{Verb: "Get", Kind: "StatefulSet", Resource: vmselectName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vmselectName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmselectName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vmselectName},
				{Verb: "Create", Kind: "Service", Resource: vmselectName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmselectName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vmselectName},

				// VMInsert
				{Verb: "Get", Kind: "Deployment", Resource: vminsertName},
				{Verb: "Create", Kind: "Deployment", Resource: vminsertName},
				{Verb: "Get", Kind: "Deployment", Resource: vminsertName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vminsertName},
				{Verb: "Create", Kind: "Service", Resource: vminsertName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vminsertName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vminsertName},
			},
		})

	// update vmcluster with no changes
	f(args{
		cr:     defaultCR.DeepCopy(),
		preRun: setupReadyComponents,
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: saName},

				// VMStorage
				{Verb: "Get", Kind: "StatefulSet", Resource: vmstorageName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmstorageName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vmstorageName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmstorageName},

				// VMSelect
				{Verb: "Get", Kind: "StatefulSet", Resource: vmselectName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmselectName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vmselectName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmselectName},

				// VMInsert
				{Verb: "Get", Kind: "Deployment", Resource: vminsertName},
				{Verb: "Get", Kind: "Deployment", Resource: vminsertName},
				{Verb: "Get", Kind: "Service", Resource: vminsertName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vminsertName},
			},
		})

	// no update on status change
	f(args{
		cr: defaultCR.DeepCopy(),
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMCluster) {
			setupReadyComponents(ctx, c, cr)
			// Update status to simulate consistency
			cr.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
		},
	},
		want{
			actions: []k8stools.ClientAction{
				// ServiceAccount
				{Verb: "Get", Kind: "ServiceAccount", Resource: saName},

				// VMStorage
				{Verb: "Get", Kind: "StatefulSet", Resource: vmstorageName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmstorageName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vmstorageName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmstorageName},

				// VMSelect
				{Verb: "Get", Kind: "StatefulSet", Resource: vmselectName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmselectName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vmselectName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmselectName},

				// VMInsert
				{Verb: "Get", Kind: "Deployment", Resource: vminsertName},
				{Verb: "Get", Kind: "Deployment", Resource: vminsertName},
				{Verb: "Get", Kind: "Service", Resource: vminsertName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vminsertName},
			},
		})
}

func TestCreateOrUpdate_Paused(t *testing.T) {
	// Create a paused VMCluster CR and test that it is not reconciled
	cr := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-cluster",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMClusterSpec{
			RetentionPeriod: "1",
			VMStorage: &vmv1beta1.VMStorage{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
			Paused: true,
		},
	}
	stsName := types.NamespacedName{Namespace: cr.Namespace, Name: "vmstorage-" + cr.Name}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr})
	ctx := context.TODO()
	build.AddDefaults(fclient.Scheme())
	fclient.Scheme().Default(cr)

	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

	var sts appsv1.StatefulSet
	err := fclient.Get(ctx, stsName, &sts)
	assert.Error(t, err)
	assert.True(t, k8serrors.IsNotFound(err))

	// unpause and verify reconciliation
	cr.Spec.Paused = false
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
	err = fclient.Get(ctx, stsName, &sts)
	assert.NoError(t, err)

	// pause and update replica count
	cr.Spec.Paused = true
	cr.Spec.VMStorage.ReplicaCount = ptr.To(int32(2))
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

	// check that replicas count is not updated
	err = fclient.Get(ctx, stsName, &sts)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), *sts.Spec.Replicas)
}
