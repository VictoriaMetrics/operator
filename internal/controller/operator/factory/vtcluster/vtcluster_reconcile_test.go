package vtcluster

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr     *vmv1.VTCluster
		preRun func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1.VTCluster)
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

		err := CreateOrUpdate(ctx, fclient, args.cr)
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
	saName := types.NamespacedName{Namespace: namespace, Name: "vtcluster-" + name}
	vtstorageName := types.NamespacedName{Namespace: namespace, Name: "vtstorage-" + name}
	vtselectName := types.NamespacedName{Namespace: namespace, Name: "vtselect-" + name}
	vtinsertName := types.NamespacedName{Namespace: namespace, Name: "vtinsert-" + name}

	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}

	// create vtcluster with all components
	f(args{
		cr: &vmv1.VTCluster{
			ObjectMeta: objectMeta,
			Spec: vmv1.VTClusterSpec{
				Storage: &vmv1.VTStorage{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				Select: &vmv1.VTSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				Insert: &vmv1.VTInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				// ServiceAccount
				{Verb: "Get", Kind: "ServiceAccount", Resource: saName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: saName},

				// VTStorage
				{Verb: "Get", Kind: "StatefulSet", Resource: vtstorageName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vtstorageName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vtstorageName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vtstorageName},
				{Verb: "Create", Kind: "Service", Resource: vtstorageName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtstorageName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vtstorageName},

				// VTSelect
				{Verb: "Get", Kind: "Service", Resource: vtselectName},
				{Verb: "Create", Kind: "Service", Resource: vtselectName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtselectName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vtselectName},
				{Verb: "Get", Kind: "Deployment", Resource: vtselectName},
				{Verb: "Create", Kind: "Deployment", Resource: vtselectName},
				{Verb: "Get", Kind: "Deployment", Resource: vtselectName}, // wait for ready

				// VTInsert
				{Verb: "Get", Kind: "Deployment", Resource: vtinsertName},
				{Verb: "Create", Kind: "Deployment", Resource: vtinsertName},
				{Verb: "Get", Kind: "Deployment", Resource: vtinsertName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vtinsertName},
				{Verb: "Create", Kind: "Service", Resource: vtinsertName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtinsertName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vtinsertName},
			},
		})

	// update vtcluster with no changes
	f(args{
		cr: &vmv1.VTCluster{
			ObjectMeta: objectMeta,
			Spec: vmv1.VTClusterSpec{
				Storage: &vmv1.VTStorage{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
					RollingUpdateStrategy: appsv1.RollingUpdateStatefulSetStrategyType,
				},
				Select: &vmv1.VTSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				Insert: &vmv1.VTInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			},
		},
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1.VTCluster) {
			// Create objects first
			assert.NoError(t, CreateOrUpdate(ctx, c, cr.DeepCopy()))

			// Set Ready status
			sts := &appsv1.StatefulSet{}
			if err := c.Get(ctx, vtstorageName, sts); err == nil {
				sts.Status.Replicas = 1
				sts.Status.ReadyReplicas = 1
				sts.Status.UpdatedReplicas = 1
				sts.Status.ObservedGeneration = sts.Generation
				sts.Status.UpdateRevision = "v1"
				_ = c.Status().Update(ctx, sts)

				labels := make(map[string]string)
				for k, v := range sts.Spec.Selector.MatchLabels {
					labels[k] = v
				}
				labels["controller-revision-hash"] = "v1"

				// Create pod for StatefulSet to pass rolling update check
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      sts.Name + "-0",
						Namespace: sts.Namespace,
						Labels:    labels,
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				}
				assert.NoError(t, c.Create(ctx, pod))
			}

			depSelect := &appsv1.Deployment{}
			if err := c.Get(ctx, vtselectName, depSelect); err == nil {
				depSelect.Status.Replicas = 1
				depSelect.Status.ReadyReplicas = 1
				depSelect.Status.UpdatedReplicas = 1
				depSelect.Status.ObservedGeneration = depSelect.Generation
				_ = c.Status().Update(ctx, depSelect)
			}

			depInsert := &appsv1.Deployment{}
			if err := c.Get(ctx, vtinsertName, depInsert); err == nil {
				depInsert.Status.Replicas = 1
				depInsert.Status.ReadyReplicas = 1
				depInsert.Status.UpdatedReplicas = 1
				depInsert.Status.ObservedGeneration = depInsert.Generation
				_ = c.Status().Update(ctx, depInsert)
			}

			// clear actions
			c.Actions = nil

			// set LastAppliedSpec
			cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: saName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vtstorageName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vtstorageName},
				{Verb: "Get", Kind: "Service", Resource: vtstorageName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtstorageName},
				{Verb: "Get", Kind: "Service", Resource: vtselectName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtselectName},
				{Verb: "Get", Kind: "Deployment", Resource: vtselectName},
				{Verb: "Get", Kind: "Deployment", Resource: vtselectName},
				{Verb: "Get", Kind: "Deployment", Resource: vtinsertName},
				{Verb: "Get", Kind: "Deployment", Resource: vtinsertName},
				{Verb: "Get", Kind: "Service", Resource: vtinsertName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtinsertName},
				{Verb: "Get", Kind: "Deployment", Resource: types.NamespacedName{Namespace: namespace, Name: "vtclusterlb-" + name}},
				{Verb: "Get", Kind: "Secret", Resource: types.NamespacedName{Namespace: namespace, Name: "vtclusterlb-" + name}},
				{Verb: "Get", Kind: "PodDisruptionBudget", Resource: types.NamespacedName{Namespace: namespace, Name: "vtclusterlb-" + name}},
			},
		})
}
