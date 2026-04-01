package vmdistributed

import (
	"context"
	"fmt"
	"slices"
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

func newVMAgent(name, namespace string, owner metav1.OwnerReference) *vmv1beta1.VMAgent {
	return &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.Now(),
			OwnerReferences:   []metav1.OwnerReference{owner},
		},
		Spec: vmv1beta1.VMAgentSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To(int32(1)),
			},
		},
	}
}

func newVMCluster(name, namespace, version string, owner metav1.OwnerReference) *vmv1beta1.VMCluster {
	return &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			Labels:            map[string]string{"tenant": "default"},
			CreationTimestamp: metav1.Now(),
			OwnerReferences:   []metav1.OwnerReference{owner},
		},
		Spec: vmv1beta1.VMClusterSpec{
			ClusterVersion: version,
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
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
	}
}

type opts struct {
	prepare  func(d *testData)
	preRun   func(c client.Client, d *testData)
	validate func(ctx context.Context, rclient client.Client, d *testData)
	actions  func(d *testData) []action
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
			Zones: make([]vmv1alpha1.VMDistributedZone, zonesCount),
			VMAuth: vmv1alpha1.VMDistributedAuth{
				Name: "vmauth-proxy",
			},
		},
	}
	var predefinedObjects []runtime.Object
	owner := cr.AsOwner()
	for i := range cr.Spec.Zones {
		name := fmt.Sprintf("vmcluster-%d", i+1)
		vmCluster := newVMCluster(name, namespace, "v1.0.0", owner)
		vmAgent := newVMAgent(name, namespace, owner)
		zs.vmclusters = append(zs.vmclusters, vmCluster)
		zs.vmagents = append(zs.vmagents, vmAgent)
		predefinedObjects = append(predefinedObjects, vmAgent, vmCluster)
		cr.Spec.Zones[i] = vmv1alpha1.VMDistributedZone{
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
		if o.preRun != nil {
			o.preRun(rclient, d)
			actions = nil
		}
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
			d.cr.Spec.VMAuth.Spec.LogLevel = "ERROR"
		},
		preRun: func(c client.Client, d *testData) {
			clusters := []*vmv1beta1.VMCluster{d.zones.vmclusters[0]}
			lb := buildVMAuthLB(d.cr, d.zones.vmagents, clusters)
			c.Scheme().Default(lb)
			assert.NoError(t, c.Create(context.TODO(), lb))
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
		},
		preRun: func(c client.Client, d *testData) {
			clusters := []*vmv1beta1.VMCluster{d.zones.vmclusters[0]}
			lb := buildVMAuthLB(d.cr, d.zones.vmagents, clusters)
			c.Scheme().Default(lb)
			assert.NoError(t, c.Create(context.TODO(), lb))
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
			var vmClusterObjs, vmAgentObjs []vmv1beta1.NamespacedName
			for i := range d.zones.vmagents {
				vmAgentObjs = append(vmAgentObjs, vmv1beta1.NamespacedName{
					Name:      d.zones.vmagents[i].Name,
					Namespace: d.zones.vmagents[i].Namespace,
				})
			}
			for i := range d.zones.vmclusters {
				vmClusterObjs = append(vmClusterObjs, vmv1beta1.NamespacedName{
					Name:      d.zones.vmclusters[i].Name,
					Namespace: d.zones.vmclusters[i].Namespace,
				})
			}
			// oldest generations are listed first in vmauth
			slices.Reverse(vmClusterObjs)
			targetRefs := []vmv1beta1.TargetRef{
				{
					Name:  "write",
					Paths: []string{"/insert/.+", "/api/v1/write"},
					CRD: &vmv1beta1.CRDRef{
						Kind:    "VMAgent",
						Objects: vmAgentObjs,
					},
				},
				{
					Name:  "read",
					Paths: []string{"/select/.+", "/admin/tenants"},
					CRD: &vmv1beta1.CRDRef{
						Kind:    "VMCluster/vmselect",
						Objects: vmClusterObjs,
					},
				},
			}
			for i := range targetRefs {
				targetRef := &targetRefs[i]
				targetRef.LoadBalancingPolicy = ptr.To("first_available")
				targetRef.RetryStatusCodes = []int{500, 502, 503}
			}
			var got vmv1beta1.VMAuth
			nsn := types.NamespacedName{
				Name:      vmAuth.Name,
				Namespace: vmAuth.Namespace,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Equal(t, targetRefs, got.Spec.DefaultTargetRefs)
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
		},
		preRun: func(c client.Client, d *testData) {
			clusters := []*vmv1beta1.VMCluster{d.zones.vmclusters[0]}
			lb := buildVMAuthLB(d.cr, d.zones.vmagents, clusters)
			c.Scheme().Default(lb)
			lb.OwnerReferences = nil
			assert.NoError(t, c.Create(context.TODO(), lb))
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
			var vmClusterObjs, vmAgentObjs []vmv1beta1.NamespacedName
			for i := range d.zones.vmagents {
				vmAgentObjs = append(vmAgentObjs, vmv1beta1.NamespacedName{
					Name:      d.zones.vmagents[i].Name,
					Namespace: d.zones.vmagents[i].Namespace,
				})
			}
			for i := range d.zones.vmclusters {
				vmClusterObjs = append(vmClusterObjs, vmv1beta1.NamespacedName{
					Name:      d.zones.vmclusters[i].Name,
					Namespace: d.zones.vmclusters[i].Namespace,
				})
			}
			// oldest generations are listed first in vmauth
			slices.Reverse(vmClusterObjs)
			targetRefs := []vmv1beta1.TargetRef{
				{
					Name:  "write",
					Paths: []string{"/insert/.+", "/api/v1/write"},
					CRD: &vmv1beta1.CRDRef{
						Kind:    "VMAgent",
						Objects: vmAgentObjs,
					},
				},
				{
					Name:  "read",
					Paths: []string{"/select/.+", "/admin/tenants"},
					CRD: &vmv1beta1.CRDRef{
						Kind:    "VMCluster/vmselect",
						Objects: vmClusterObjs,
					},
				},
			}
			for i := range targetRefs {
				targetRef := &targetRefs[i]
				targetRef.LoadBalancingPolicy = ptr.To("first_available")
				targetRef.RetryStatusCodes = []int{500, 502, 503}
			}
			var got vmv1beta1.VMAuth
			nsn := types.NamespacedName{
				Name:      vmAuth.Name,
				Namespace: vmAuth.Namespace,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Equal(t, targetRefs, got.Spec.DefaultTargetRefs)
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
