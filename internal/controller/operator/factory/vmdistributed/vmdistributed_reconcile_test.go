package vmdistributed

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

	name := "test-dist"
	namespace := "default"
	zoneName := "zone-1"
	vmClusterName := types.NamespacedName{Namespace: namespace, Name: "test-dist-zone-1"}
	vmAgentName := types.NamespacedName{Namespace: namespace, Name: "test-dist-zone-1"}
	vmAuthLBName := types.NamespacedName{Namespace: namespace, Name: name}

	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}

	f := func(args args, want want) {
		synctest.Test(t, func(t *testing.T) {
			mux := http.NewServeMux()
			handler := func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `%s{path="/tmp/1_19F3EC170A0DA31A"} 0`, vmAgentQueueMetricName)
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
					Labels:    map[string]string{discoveryv1.LabelServiceName: "vmagent-" + vmAgentName.Name},
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
	}, want{
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
	}, want{
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
	}, want{
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

	// update VMAuth even, when no healthy zones available
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

			cr.Spec.ZoneCommon.VMCluster.Spec.ClusterVersion = "v1.0.0-cluster"
		},
	}, want{
		actions: []k8stools.ClientAction{
			// getZones
			{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},
			{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},

			// update LB config
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Update", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},

			// reconcile VMCluster
			{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},
			{Verb: "Update", Kind: "VMCluster", Resource: vmClusterName},
			{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},

			// reconcile VMAgent
			{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},
			{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},

			// reconcile VMAuth
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Update", Kind: "VMAuth", Resource: vmAuthLBName},
			{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
		},
	})
}
