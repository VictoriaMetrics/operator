package vldistributed

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr     *vmv1alpha1.VLDistributed
		preRun func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1alpha1.VLDistributed)
	}
	type want struct {
		actions []k8stools.ClientAction
		err     error
	}

	name := "test-dist"
	namespace := "default"
	zoneName := "zone-1"
	vlClusterName := types.NamespacedName{Namespace: namespace, Name: "test-dist-zone-1"}
	vlAgentName := types.NamespacedName{Namespace: namespace, Name: "test-dist-zone-1"}
	vmAuthLBName := types.NamespacedName{Namespace: namespace, Name: name}

	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}

	f := func(args args, want want) {
		synctest.Test(t, func(t *testing.T) {
			mux := http.NewServeMux()
			handler := func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `%s{url="http://vl-insert-test-dist-zone-1.default.svc:9481"} 0`, vlAgentQueueMetricName)
			}
			mux.HandleFunc("/metrics", handler)
			ts := httptest.NewServer(mux)
			defer ts.Close()
			tsURL, err := url.Parse(ts.URL)
			assert.NoError(t, err)
			tsHost, tsPortStr, err := net.SplitHostPort(tsURL.Host)
			if err != nil {
				assert.NoError(t, err)
			}
			tsPort, err := strconv.ParseInt(tsPortStr, 10, 32)
			if err != nil {
				assert.NoError(t, err)
			}
			endpointSlice := discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "random-endpoint-name",
					Namespace: namespace,
					Labels:    map[string]string{discoveryv1.LabelServiceName: "vlagent-" + vlAgentName.Name},
				},
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{tsHost},
						Conditions: discoveryv1.EndpointConditions{
							Ready: ptr.To(true),
						},
					},
				},
				Ports: []discoveryv1.EndpointPort{
					{
						Name: ptr.To("http"),
						Port: ptr.To[int32](int32(tsPort)),
					},
				},
			}
			fclient := k8stools.GetTestClientWithOptsActionsAndObjects([]runtime.Object{&endpointSlice}, &k8stools.ClientOpts{
				SetCreationTimestamp: true,
			})
			ctx := context.TODO()
			build.AddDefaults(fclient.Scheme())
			fclient.Scheme().Default(args.cr)
			if args.preRun != nil {
				args.preRun(ctx, fclient, args.cr)
			}

			err = CreateOrUpdate(ctx, args.cr, fclient)
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

	zoneSpec := vmv1alpha1.VLDistributedZoneCluster{
		Spec: vmv1.VLClusterSpec{
			VLStorage: &vmv1.VLStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
			},
			VLSelect: &vmv1.VLSelect{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
			},
			VLInsert: &vmv1.VLInsert{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
			},
		},
	}
	vmAuthSpec := vmv1alpha1.VLDistributedAuth{
		Spec: vmv1beta1.VMAuthSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
		},
	}

	// default
	f(args{
		cr: &vmv1alpha1.VLDistributed{
			ObjectMeta: objectMeta,
			Spec: vmv1alpha1.VLDistributedSpec{
				Zones: []vmv1alpha1.VLDistributedZone{
					{
						Name:      zoneName,
						VLCluster: zoneSpec,
						VLAgent: vmv1alpha1.VLDistributedZoneAgent{
							Spec: vmv1alpha1.VLDistributedZoneAgentSpec{
								PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{},
							},
						},
					},
				},
				VMAuth: vmAuthSpec,
			},
		},
	}, want{
		actions: []k8stools.ClientAction{
			// getZones
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},

			// reconcile VLCluster
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Create", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},

			// reconcile VLAgent
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},
			{Verb: "Create", Kind: "VLAgent", Resource: vlAgentName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},

			// reconcile VMAuth
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Create", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
		},
	})

	// create vlagent only
	f(args{
		cr: &vmv1alpha1.VLDistributed{
			ObjectMeta: objectMeta,
			Spec: vmv1alpha1.VLDistributedSpec{
				Zones: []vmv1alpha1.VLDistributedZone{
					{
						Name:      zoneName,
						VLCluster: zoneSpec,
						VLAgent: vmv1alpha1.VLDistributedZoneAgent{
							Spec: vmv1alpha1.VLDistributedZoneAgentSpec{
								PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{},
							},
						},
					},
				},
				VMAuth: vmv1alpha1.VLDistributedAuth{
					Enabled: ptr.To(false),
				},
			},
		},
	}, want{
		actions: []k8stools.ClientAction{
			// getZones
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},

			// reconcile VLCluster
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Create", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},

			// reconcile VLAgent
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},
			{Verb: "Create", Kind: "VLAgent", Resource: vlAgentName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},
		},
	})

	// no change on status update
	f(args{
		cr: &vmv1alpha1.VLDistributed{
			ObjectMeta: objectMeta,
			Spec: vmv1alpha1.VLDistributedSpec{
				Zones: []vmv1alpha1.VLDistributedZone{
					{
						Name:      zoneName,
						VLCluster: zoneSpec,
						VLAgent: vmv1alpha1.VLDistributedZoneAgent{
							Spec: vmv1alpha1.VLDistributedZoneAgentSpec{
								PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{},
							},
						},
					},
				},
				VMAuth: vmAuthSpec,
			},
		},
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1alpha1.VLDistributed) {
			// Create objects first
			assert.NoError(t, CreateOrUpdate(ctx, cr.DeepCopy(), c))

			// clear actions
			c.Actions = nil

			// Update status to simulate consistency
			cr.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
		},
	}, want{
		actions: []k8stools.ClientAction{
			// getZones
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},

			// reconcile VLCluster
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},

			// reconcile VLAgent
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},

			// reconcile VMAuth
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
		},
	})

	// update VMAuth even, when no healthy zones available
	f(args{
		cr: &vmv1alpha1.VLDistributed{
			ObjectMeta: objectMeta,
			Spec: vmv1alpha1.VLDistributedSpec{
				Zones: []vmv1alpha1.VLDistributedZone{
					{
						Name:      zoneName,
						VLCluster: zoneSpec,
						VLAgent: vmv1alpha1.VLDistributedZoneAgent{
							Spec: vmv1alpha1.VLDistributedZoneAgentSpec{
								PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{},
							},
						},
					},
				},
				VMAuth: vmAuthSpec,
			},
		},
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1alpha1.VLDistributed) {
			// Create objects first
			assert.NoError(t, CreateOrUpdate(ctx, cr.DeepCopy(), c))

			// clear actions
			c.Actions = nil

			// Update status to simulate consistency
			cr.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational

			cr.Spec.ZoneCommon.VLCluster.Spec.ClusterVersion = "v1.51.0"
		},
	}, want{
		actions: []k8stools.ClientAction{
			// getZones
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},

			// update LB config
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Update", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},

			// reconcile VLCluster
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Update", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},

			// reconcile VLAgent
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},

			// reconcile VMAuth
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Update", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
		},
	})

	// switching from read-write to read-only: pre-reconcile PQ drain, no post-reconcile drain
	f(args{
		cr: &vmv1alpha1.VLDistributed{
			ObjectMeta: objectMeta,
			Spec: vmv1alpha1.VLDistributedSpec{
				Zones: []vmv1alpha1.VLDistributedZone{
					{
						Name:        zoneName,
						TrafficMode: vmv1alpha1.VLDistributedTrafficModeReadOnly,
						VLCluster:   zoneSpec,
						VLAgent: vmv1alpha1.VLDistributedZoneAgent{
							Spec: vmv1alpha1.VLDistributedZoneAgentSpec{
								PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{},
							},
						},
					},
				},
				VMAuth: vmAuthSpec,
			},
		},
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1alpha1.VLDistributed) {
			crCopy := cr.DeepCopy()
			crCopy.Spec.Zones[0].TrafficMode = ""
			assert.NoError(t, CreateOrUpdate(ctx, crCopy, c))
			c.Actions = nil
		},
	}, want{
		actions: []k8stools.ClientAction{
			// getZones
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},

			// prevAcceptsWrites=true: drain PQ (no k8s actions), then exclude zone from LB
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Update", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},

			// reconcile VLCluster: VLInsert.ReplicaCount 1 → 0
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Update", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},

			// reconcile VLAgent: no changes
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},

			// newAcceptsWrites=false: no PQ drain; restore LB with read-only config
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Update", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
		},
	})

	// switching from read-only to read-write: no pre-reconcile drain, post-reconcile PQ drain
	f(args{
		cr: &vmv1alpha1.VLDistributed{
			ObjectMeta: objectMeta,
			Spec: vmv1alpha1.VLDistributedSpec{
				Zones: []vmv1alpha1.VLDistributedZone{
					{
						Name:        zoneName,
						TrafficMode: vmv1alpha1.VLDistributedTrafficModeReadWrite,
						VLCluster:   zoneSpec,
						VLAgent: vmv1alpha1.VLDistributedZoneAgent{
							Spec: vmv1alpha1.VLDistributedZoneAgentSpec{
								PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{},
							},
						},
					},
				},
				VMAuth: vmAuthSpec,
			},
		},
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1alpha1.VLDistributed) {
			crCopy := cr.DeepCopy()
			crCopy.Spec.Zones[0].TrafficMode = vmv1alpha1.VLDistributedTrafficModeReadOnly
			assert.NoError(t, CreateOrUpdate(ctx, crCopy, c))
			c.Actions = nil
		},
	}, want{
		actions: []k8stools.ClientAction{
			// getZones
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},

			// prevAcceptsWrites=false: no pre-reconcile drain; exclude zone from LB
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Update", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},

			// reconcile VLCluster: VLInsert.ReplicaCount 0 → 1
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Update", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},

			// reconcile VLAgent: no changes
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},

			// newAcceptsWrites=true: drain PQ (no k8s actions), then restore LB
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Update", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
		},
	})

	// read-only mode with no spec changes: no reconcile, no PQ drains
	f(args{
		cr: &vmv1alpha1.VLDistributed{
			ObjectMeta: objectMeta,
			Spec: vmv1alpha1.VLDistributedSpec{
				Zones: []vmv1alpha1.VLDistributedZone{
					{
						Name:        zoneName,
						TrafficMode: vmv1alpha1.VLDistributedTrafficModeReadOnly,
						VLCluster:   zoneSpec,
						VLAgent: vmv1alpha1.VLDistributedZoneAgent{
							Spec: vmv1alpha1.VLDistributedZoneAgentSpec{
								PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{},
							},
						},
					},
				},
				VMAuth: vmAuthSpec,
			},
		},
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1alpha1.VLDistributed) {
			assert.NoError(t, CreateOrUpdate(ctx, cr.DeepCopy(), c))
			c.Actions = nil
		},
	}, want{
		actions: []k8stools.ClientAction{
			// getZones
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},

			// no spec changes: no LB exclude, no PQ drains
			// reconcile VLCluster: no changes
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},
			{Verb: "Get", Kind: "VLCluster", Resource: vlClusterName},

			// reconcile VLAgent: no changes
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},
			{Verb: "Get", Kind: "VLAgent", Resource: vlAgentName},

			// restore LB: no changes
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
		},
	})
}

func Test_CreateOrUpdate_Paused(t *testing.T) {
	cr := &vmv1alpha1.VLDistributed{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dist-paused",
			Namespace: "default",
		},
		Spec: vmv1alpha1.VLDistributedSpec{
			Paused: true,
			Zones: []vmv1alpha1.VLDistributedZone{
				{
					Name: "zone-1",
					VLCluster: vmv1alpha1.VLDistributedZoneCluster{
						Spec: vmv1.VLClusterSpec{
							VLStorage: &vmv1.VLStorage{
								CommonAppsParams: vmv1beta1.CommonAppsParams{
									ReplicaCount: ptr.To(int32(1)),
								},
							},
							VLSelect: &vmv1.VLSelect{
								CommonAppsParams: vmv1beta1.CommonAppsParams{
									ReplicaCount: ptr.To(int32(1)),
								},
							},
							VLInsert: &vmv1.VLInsert{
								CommonAppsParams: vmv1beta1.CommonAppsParams{
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
	var vlCluster vmv1.VLCluster
	err := fclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "test-dist-paused-zone-1"}, &vlCluster)
	assert.Error(t, err)
	assert.True(t, k8serrors.IsNotFound(err))

	// unpause and verify reconciliation
	cr.Spec.Paused = false
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
	err = fclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "test-dist-paused-zone-1"}, &vlCluster)
	assert.NoError(t, err)

	// pause and update cluster version
	cr.Spec.Paused = true
	cr.Spec.Zones[0].VLCluster.Spec.ClusterVersion = "v2.0.0"
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

	// check that cluster version is not updated
	err = fclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: "test-dist-paused-zone-1"}, &vlCluster)
	assert.NoError(t, err)
	assert.Equal(t, "", vlCluster.Spec.ClusterVersion)
}
