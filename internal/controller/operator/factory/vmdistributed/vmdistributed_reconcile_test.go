package vmdistributed

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr     *vmv1alpha1.VMDistributed
		preRun func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1alpha1.VMDistributed)
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

		synctest.Test(t, func(t *testing.T) {
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
		})
	}

	name := "test-dist"
	namespace := "default"
	zoneName := "zone-1"
	vmClusterName := types.NamespacedName{Namespace: namespace, Name: "test-dist-zone-1"}
	vmAgentName := types.NamespacedName{Namespace: namespace, Name: "test-dist-zone-1"}
	vmAuthLBName := types.NamespacedName{Namespace: namespace, Name: name}

	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}

	// default
	f(args{
		cr: &vmv1alpha1.VMDistributed{
			ObjectMeta: objectMeta,
			Spec: vmv1alpha1.VMDistributedSpec{
				Zones: []vmv1alpha1.VMDistributedZone{
					{
						Name: zoneName,
						VMCluster: vmv1alpha1.VMDistributedZoneCluster{
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
						},
						VMAgent: vmv1alpha1.VMDistributedZoneAgent{
							Spec: vmv1alpha1.VMDistributedZoneAgentSpec{
								PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{},
							},
						},
					},
				},
				VMAuth: vmv1alpha1.VMDistributedAuth{
					Spec: vmv1beta1.VMAuthSpec{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To(int32(1)),
						},
					},
				},
			},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				// getZones
				{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},
				{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},

				// reconcile VMCluster
				{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},
				{Verb: "Create", Kind: "VMCluster", Resource: vmClusterName},
				{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},

				// reconcile VMAgent
				{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},
				{Verb: "Create", Kind: "VMAgent", Resource: vmAgentName},
				{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},

				// reconcile VMAuth
				{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
				{Verb: "Create", Kind: "VMAuth", Resource: vmAuthLBName},
				{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			},
		})

	// create vmagent
	f(args{
		cr: &vmv1alpha1.VMDistributed{
			ObjectMeta: objectMeta,
			Spec: vmv1alpha1.VMDistributedSpec{
				Zones: []vmv1alpha1.VMDistributedZone{
					{
						Name: zoneName,
						VMCluster: vmv1alpha1.VMDistributedZoneCluster{
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
						},
						VMAgent: vmv1alpha1.VMDistributedZoneAgent{
							Spec: vmv1alpha1.VMDistributedZoneAgentSpec{
								PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{},
							},
						},
					},
				},
				VMAuth: vmv1alpha1.VMDistributedAuth{
					Enabled: ptr.To(false),
				},
			},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				// getZones
				{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},
				{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},

				// reconcile VMCluster
				{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},
				{Verb: "Create", Kind: "VMCluster", Resource: vmClusterName},
				{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},

				// reconcile VMAgent
				{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},
				{Verb: "Create", Kind: "VMAgent", Resource: vmAgentName},
				{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},
			},
		})

	// no change on status update
	f(args{
		cr: &vmv1alpha1.VMDistributed{
			ObjectMeta: objectMeta,
			Spec: vmv1alpha1.VMDistributedSpec{
				Zones: []vmv1alpha1.VMDistributedZone{
					{
						Name: zoneName,
						VMCluster: vmv1alpha1.VMDistributedZoneCluster{
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
						},
						VMAgent: vmv1alpha1.VMDistributedZoneAgent{
							Spec: vmv1alpha1.VMDistributedZoneAgentSpec{
								PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{},
							},
						},
					},
				},
				VMAuth: vmv1alpha1.VMDistributedAuth{
					Spec: vmv1beta1.VMAuthSpec{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To(int32(1)),
						},
					},
				},
			},
		},
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1alpha1.VMDistributed) {
			// Create objects first
			assert.NoError(t, CreateOrUpdate(ctx, cr.DeepCopy(), c))

			// clear actions
			c.Actions = nil

			// Update status to simulate consistency
			cr.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
		},
	},
		want{
			actions: []k8stools.ClientAction{
				// getZones
				{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},
				{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},

				// reconcile VMCluster
				{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},
				{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},

				// reconcile VMAgent
				{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},
				{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},

				// reconcile VMAuth
				{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
				{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			},
		})
}

func Test_CreateOrUpdate_Paused(t *testing.T) {
	cr := &vmv1alpha1.VMDistributed{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dist-paused",
			Namespace: "default",
		},
		Spec: vmv1alpha1.VMDistributedSpec{
			Paused: true,
			Zones: []vmv1alpha1.VMDistributedZone{
				{
					Name: "zone-1",
					VMCluster: vmv1alpha1.VMDistributedZoneCluster{
						Spec: vmv1beta1.VMClusterSpec{
							RetentionPeriod: "1",
							VMStorage: &vmv1beta1.VMStorage{
								CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
									ReplicaCount: ptr.To(int32(1)),
								},
							},
							VMSelect: &vmv1beta1.VMSelect{
								CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
									ReplicaCount: ptr.To(int32(1)),
								},
							},
							VMInsert: &vmv1beta1.VMInsert{
								CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
									ReplicaCount: ptr.To(int32(1)),
								},
							},
						},
					},
				},
			},
		},
	}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr})
	ctx := context.TODO()
	build.AddDefaults(fclient.Scheme())
	fclient.Scheme().Default(cr)

	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

	// check that child objects are not created
	var vmCluster vmv1beta1.VMCluster
	err := fclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "test-dist-paused-zone-1"}, &vmCluster)
	assert.Error(t, err)
	assert.True(t, k8serrors.IsNotFound(err))

	// unpause and verify reconciliation
	cr.Spec.Paused = false
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
	err = fclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "test-dist-paused-zone-1"}, &vmCluster)
	assert.NoError(t, err)

	// pause and update retention
	cr.Spec.Paused = true
	cr.Spec.Zones[0].VMCluster.Spec.RetentionPeriod = "2"
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

	// check that retention is not updated
	err = fclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "test-dist-paused-zone-1"}, &vmCluster)
	assert.NoError(t, err)
	assert.Equal(t, "1", vmCluster.Spec.RetentionPeriod)
}
