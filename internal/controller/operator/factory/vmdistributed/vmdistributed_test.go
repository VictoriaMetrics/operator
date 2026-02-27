package vmdistributed

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

func newVMAgent(name, namespace string) *vmv1beta1.VMAgent {
	return &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.Now(),
		},
		Spec: vmv1beta1.VMAgentSpec{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptr.To(int32(1)),
			},
		},
	}
}

func newVMCluster(name, namespace, version string) *vmv1beta1.VMCluster {
	return &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			Labels:            map[string]string{"tenant": "default"},
			CreationTimestamp: metav1.Now(),
		},
		Spec: vmv1beta1.VMClusterSpec{
			ClusterVersion: version,
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
			VMStorage: &vmv1beta1.VMStorage{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
	}
}

type opts struct {
	prepare  func(*testData)
	validate func(context.Context, client.Client, *testData)
	actions  func(*testData) []action
}

type testData struct {
	zones             *zones
	cr                *vmv1alpha1.VMDistributed
	predefinedObjects []runtime.Object
}

func beforeEach(o opts) *testData {
	zonesCount := 3
	zs := &zones{
		vmclusters: make([]*vmv1beta1.VMCluster, 0, zonesCount),
		vmagents:   make([]*vmv1beta1.VMAgent, 0, zonesCount),
	}
	namespace := "default"
	dzs := make([]vmv1alpha1.VMDistributedZone, zonesCount)
	var predefinedObjects []runtime.Object
	for i := range dzs {
		name := fmt.Sprintf("vmcluster-%d", i+1)
		vmCluster := newVMCluster(name, namespace, "v1.0.0")
		vmAgent := newVMAgent(name, namespace)
		zs.vmclusters = append(zs.vmclusters, vmCluster)
		zs.vmagents = append(zs.vmagents, vmAgent)
		predefinedObjects = append(predefinedObjects, vmAgent, vmCluster)
		dzs[i] = vmv1alpha1.VMDistributedZone{
			Name: name,
			VMCluster: vmv1alpha1.VMDistributedZoneCluster{
				Name: name,
				Spec: vmCluster.Spec,
			},
			VMAgent: vmv1alpha1.VMDistributedZoneAgent{
				Name: name,
			},
		}
	}
	cr := &vmv1alpha1.VMDistributed{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VMDistributed",
			APIVersion: vmv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vdc",
			Namespace: namespace,
		},
		Spec: vmv1alpha1.VMDistributedSpec{
			Zones: dzs,
			VMAuth: vmv1alpha1.VMDistributedAuth{
				Name: "vmauth-proxy",
			},
		},
	}

	predefinedObjects = append(predefinedObjects, cr)
	d := &testData{
		zones:             zs,
		cr:                cr,
		predefinedObjects: predefinedObjects,
	}
	o.prepare(d)
	return d
}

type action struct {
	verb string
	key  string
}

func TestCreateOrUpdate(t *testing.T) {
	f := func(o opts) {
		t.Helper()
		d := beforeEach(o)
		var actions []action
		rclient := k8stools.GetTestClientWithObjectsAndInterceptors(d.predefinedObjects, interceptor.Funcs{
			Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				actions = append(actions, action{
					verb: "Create",
					key:  fmt.Sprintf("%T/%s/%s", obj, obj.GetNamespace(), obj.GetName()),
				})
				switch v := obj.(type) {
				case *vmv1beta1.VMCluster:
					v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
					v.Status.ObservedGeneration = 1
				case *vmv1beta1.VMAuth:
					v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				case *vmv1beta1.VMAgent:
					v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
					v.Status.ObservedGeneration = 1
				}
				return cl.Create(ctx, obj, opts...)
			},
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				actions = append(actions, action{
					verb: "Get",
					key:  fmt.Sprintf("%T/%s/%s", obj, key.Namespace, key.Name),
				})
				return cl.Get(ctx, key, obj, opts...)
			},
			Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				actions = append(actions, action{
					verb: "Update",
					key:  fmt.Sprintf("%T/%s/%s", obj, obj.GetNamespace(), obj.GetName()),
				})
				return cl.Update(ctx, obj, opts...)
			},
		})
		ctx := context.Background()
		synctest.Test(t, func(t *testing.T) {
			o.validate(ctx, rclient, d)
		})
		if o.actions != nil {
			expectedActions := o.actions(d)
			assert.Equal(t, expectedActions, actions)
		}
	}

	// paused CR should do nothing
	f(opts{
		prepare: func(d *testData) {
			d.cr.Spec.Paused = true
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			assert.NoError(t, CreateOrUpdate(ctx, d.cr, rclient))
		},
	})

	// check existing remote write urls order
	f(opts{
		prepare: func(d *testData) {
			for _, vmAgent := range d.zones.vmagents {
				for _, vmCluster := range d.zones.vmclusters {
					vmAgent.Spec.RemoteWrite = append(vmAgent.Spec.RemoteWrite, vmv1beta1.VMAgentRemoteWriteSpec{
						URL: vmCluster.GetRemoteWriteURL(),
					})
				}
			}
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			zs, err := getZones(ctx, rclient, d.cr)
			assert.NoError(t, err)

			// Verify urls order is preserved
			for i, vmCluster := range d.zones.vmclusters {
				for _, vmAgent := range zs.vmagents {
					assert.Len(t, vmAgent.Spec.RemoteWrite, len(d.zones.vmclusters))
					assert.Equal(t, vmCluster.GetRemoteWriteURL(), vmAgent.Spec.RemoteWrite[i].URL)
				}
			}
		},
	})

	// validate remote write urls is appended to vmagent in a valid order
	f(opts{
		prepare: func(d *testData) {
			for _, vmAgent := range d.zones.vmagents {
				vmCluster := d.zones.vmclusters[2]
				vmAgent.Spec.RemoteWrite = []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: vmCluster.GetRemoteWriteURL()},
				}
			}
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			zs, err := getZones(ctx, rclient, d.cr)
			assert.NoError(t, err)

			// Verify urls order is preserved
			for _, vmAgent := range zs.vmagents {
				assert.Len(t, vmAgent.Spec.RemoteWrite, 3)
				vmCluster0 := d.zones.vmclusters[0]
				vmCluster1 := d.zones.vmclusters[1]
				vmCluster2 := d.zones.vmclusters[2]
				assert.Equal(t, vmCluster2.GetRemoteWriteURL(), vmAgent.Spec.RemoteWrite[0].URL)
				assert.Equal(t, vmCluster0.GetRemoteWriteURL(), vmAgent.Spec.RemoteWrite[1].URL)
				assert.Equal(t, vmCluster1.GetRemoteWriteURL(), vmAgent.Spec.RemoteWrite[2].URL)
			}
		},
	})

	// should create VMAuth with default name
	f(opts{
		prepare: func(d *testData) {
			d.cr.Spec.VMAuth.Enabled = ptr.To(true)
			d.cr.Spec.VMAuth.Name = ""
		},
		actions: func(d *testData) []action {
			return []action{
				{
					verb: "Get",
					key:  fmt.Sprintf("*v1beta1.VMAuth/%s/%s", d.cr.Namespace, d.cr.Name),
				},
				{
					verb: "Create",
					key:  fmt.Sprintf("*v1beta1.VMAuth/%s/%s", d.cr.Namespace, d.cr.Name),
				},
				{
					verb: "Get",
					key:  fmt.Sprintf("*v1beta1.VMAuth/%s/%s", d.cr.Namespace, d.cr.Name),
				},
			}
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			vmAuth := buildVMAuthLB(d.cr, d.zones.vmagents, d.zones.vmclusters)
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
		},
	})

	// should update VMAuth if spec changes
	f(opts{
		prepare: func(d *testData) {
			d.zones.vmclusters[0].Spec.VMSelect.Port = "8481"
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.predefinedObjects = append(d.predefinedObjects, &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmauth-lb",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{
					LogLevel: "ERROR",
				},
				Status: vmv1beta1.VMAuthStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{
						UpdateStatus: vmv1beta1.UpdateStatusOperational,
					},
				},
			})
		},
		actions: func(d *testData) []action {
			return []action{
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
				{
					verb: "Update",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
			}
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			d.cr.Spec.VMAuth.Spec = vmv1beta1.VMAuthSpec{
				LogLevel: "INFO",
			}
			clusters := []*vmv1beta1.VMCluster{d.zones.vmclusters[0]}
			vmAuth := buildVMAuthLB(d.cr, d.zones.vmagents, clusters)
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
		},
	})

	// should not update VMAuth if spec matches
	f(opts{
		prepare: func(d *testData) {
			d.zones.vmclusters[0].Spec.VMSelect.Port = "8481"
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.VMAuth.Spec = vmv1beta1.VMAuthSpec{
				LogLevel: "INFO",
			}
			lb := &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:            d.cr.Spec.VMAuth.Name,
					Namespace:       d.cr.Namespace,
					OwnerReferences: []metav1.OwnerReference{d.cr.AsOwner()},
				},
				Spec: d.cr.Spec.VMAuth.Spec,
				Status: vmv1beta1.VMAuthStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{
						UpdateStatus: vmv1beta1.UpdateStatusOperational,
					},
				},
			}
			var targetRefs []vmv1beta1.TargetRef
			targetRefs = appendVMAgentTargetRefs(targetRefs, d.zones.vmagents)
			targetRefs = appendVMClusterTargetRefs(targetRefs, []*vmv1beta1.VMCluster{d.zones.vmclusters[0]})
			lb.Spec.UnauthorizedUserAccessSpec = &vmv1beta1.VMAuthUnauthorizedUserAccessSpec{
				TargetRefs: targetRefs,
			}
			d.predefinedObjects = append(d.predefinedObjects, lb)
		},
		actions: func(d *testData) []action {
			return []action{
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
			}
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			clusters := []*vmv1beta1.VMCluster{d.zones.vmclusters[0]}
			vmAuth := buildVMAuthLB(d.cr, d.zones.vmagents, clusters)
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
		},
	})

	// should create VMAuth if it does not exist
	f(opts{
		prepare: func(d *testData) {
			for i := range d.zones.vmclusters {
				d.zones.vmclusters[i].Spec.VMSelect.Port = "8481"
			}
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.VMAuth.Spec = vmv1beta1.VMAuthSpec{
				LogLevel: "INFO",
			}
		},
		actions: func(d *testData) []action {
			return []action{
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
				{
					verb: "Create",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
			}
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			vmAuth := buildVMAuthLB(d.cr, d.zones.vmagents, d.zones.vmclusters)
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
			targetRefs := make([]vmv1beta1.TargetRef, 6)
			for i := range targetRefs {
				targetRef := &targetRefs[i]
				targetRef.LoadBalancingPolicy = ptr.To("first_available")
				targetRef.RetryStatusCodes = []int{500, 502, 503}
				if i < len(d.zones.vmagents) {
					targetRef.CRD = &vmv1beta1.CRDRef{
						Name:      d.zones.vmagents[i].Name,
						Namespace: d.zones.vmagents[i].Namespace,
						Kind:      "VMAgent",
					}
					targetRef.Paths = []string{"/insert/.+", "/api/v1/write"}
				} else {
					idx := i - len(d.zones.vmagents)
					targetRef.CRD = &vmv1beta1.CRDRef{
						Name:      d.zones.vmclusters[idx].Name,
						Namespace: d.zones.vmclusters[idx].Namespace,
						Kind:      "VMCluster/vmselect",
					}
					targetRef.Paths = []string{"/select/.+", "/admin/tenants"}
				}
			}
			var got vmv1beta1.VMAuth
			nsn := types.NamespacedName{
				Name:      vmAuth.Name,
				Namespace: vmAuth.Namespace,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Equal(t, targetRefs, got.Spec.UnauthorizedUserAccessSpec.TargetRefs)
		},
	})

	// should adopt existing VMAuth if owner reference is missing
	f(opts{
		prepare: func(d *testData) {
			d.zones.vmclusters[0].Spec.VMSelect.Port = "8481"
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.VMAuth.Spec = vmv1beta1.VMAuthSpec{
				LogLevel: "INFO",
			}
			d.predefinedObjects = append(d.predefinedObjects, &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmauth-lb",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{
					// We intentionally don't set a full spec here,
					// so the update will populate the missing fields (like Port)
					// AND set the owner ref.
					LogLevel: "INFO",
				},
				Status: vmv1beta1.VMAuthStatus{
					StatusMetadata: vmv1beta1.StatusMetadata{
						UpdateStatus: vmv1beta1.UpdateStatusOperational,
					},
				},
			})
		},
		actions: func(d *testData) []action {
			return []action{
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
				{
					verb: "Update",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
			}
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			clusters := []*vmv1beta1.VMCluster{d.zones.vmclusters[0]}
			vmAuth := buildVMAuthLB(d.cr, d.zones.vmagents, clusters)
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
			var got vmv1beta1.VMAuth
			nsn := types.NamespacedName{
				Name:      vmAuth.Name,
				Namespace: vmAuth.Namespace,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Equal(t, []metav1.OwnerReference{d.cr.AsOwner()}, got.OwnerReferences)
		},
	})

	// check if works with RequestsLoadBalancer
	f(opts{
		prepare: func(d *testData) {
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			for i := range d.zones.vmclusters {
				d.zones.vmclusters[i].Spec.VMSelect.Port = "8481"
			}

			// Enable load balancer for vmcluster[0]
			d.zones.vmclusters[0].Spec.RequestsLoadBalancer.Enabled = true
			d.zones.vmclusters[0].Spec.RequestsLoadBalancer.Spec.Port = "8427"
		},
		actions: func(d *testData) []action {
			return []action{
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
				{
					verb: "Create",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
			}
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			vmAuth := buildVMAuthLB(d.cr, d.zones.vmagents, d.zones.vmclusters)
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
			targetRefs := make([]vmv1beta1.TargetRef, 6)
			for i := range targetRefs {
				targetRef := &targetRefs[i]
				targetRef.LoadBalancingPolicy = ptr.To("first_available")
				targetRef.RetryStatusCodes = []int{500, 502, 503}
				if i < len(d.zones.vmagents) {
					targetRef.CRD = &vmv1beta1.CRDRef{
						Name:      d.zones.vmagents[i].Name,
						Namespace: d.zones.vmagents[i].Namespace,
						Kind:      "VMAgent",
					}
					targetRef.Paths = []string{"/insert/.+", "/api/v1/write"}
				} else {
					idx := i - len(d.zones.vmagents)
					targetRef.CRD = &vmv1beta1.CRDRef{
						Name:      d.zones.vmclusters[idx].Name,
						Namespace: d.zones.vmclusters[idx].Namespace,
						Kind:      "VMCluster/vmselect",
					}
					targetRef.Paths = []string{"/select/.+", "/admin/tenants"}
				}
			}
			var got vmv1beta1.VMAuth
			nsn := types.NamespacedName{
				Name:      vmAuth.Name,
				Namespace: vmAuth.Namespace,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Equal(t, targetRefs, got.Spec.UnauthorizedUserAccessSpec.TargetRefs)
		},
	})

	// should not create VMAuth if it is disabled
	f(opts{
		prepare: func(d *testData) {
			d.cr.Spec.VMAuth.Enabled = ptr.To(false)
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			var vmAuthList vmv1beta1.VMAuthList
			assert.NoError(t, rclient.List(ctx, &vmAuthList))
			assert.Len(t, vmAuthList.Items, 0)
		},
	})
}
