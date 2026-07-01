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

func newVMSingle(name, namespace string, owner metav1.OwnerReference) *vmv1beta1.VMSingle {
	return &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.Now(),
			OwnerReferences:   []metav1.OwnerReference{owner},
		},
		Spec: vmv1beta1.VMSingleSpec{},
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
	vmclusters        []*vmv1beta1.VMCluster
	cr                *vmv1alpha1.VMDistributed
	predefinedObjects []runtime.Object
}

func beforeEach(o opts) *testData {
	zonesCount := 3
	zs := &zones{
		backends: make([]vmBackend, 0, zonesCount),
		vmagents: make([]*vmv1beta1.VMAgent, 0, zonesCount),
	}
	namespace := "default"
	cr := &vmv1alpha1.VMDistributed{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VMDistributed",
			APIVersion: vmv1alpha1.SchemeGroupVersion.String(),
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
	var vmclusters []*vmv1beta1.VMCluster
	owner := cr.AsOwner()
	for i := range cr.Spec.Zones {
		name := fmt.Sprintf("vmcluster-%d", i+1)
		vmCluster := newVMCluster(name, namespace, "v1.0.0", owner)
		vmAgent := newVMAgent(name, namespace, owner)
		zs.backends = append(zs.backends, vmBackend{obj: vmCluster})
		zs.vmagents = append(zs.vmagents, vmAgent)
		vmclusters = append(vmclusters, vmCluster)
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
		vmclusters:        vmclusters,
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
				for _, vmCluster := range d.vmclusters {
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
			for i, vmCluster := range d.vmclusters {
				for _, vmAgent := range zs.vmagents {
					assert.Len(t, vmAgent.Spec.RemoteWrite, len(d.vmclusters))
					assert.Equal(t, vmCluster.GetRemoteWriteURL(), vmAgent.Spec.RemoteWrite[i].URL)
				}
			}
		},
	})

	// validate remote write urls is appended to vmagent in a valid order
	f(opts{
		prepare: func(d *testData) {
			for _, vmAgent := range d.zones.vmagents {
				vmCluster := d.vmclusters[2]
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
				vmCluster0 := d.vmclusters[0]
				vmCluster1 := d.vmclusters[1]
				vmCluster2 := d.vmclusters[2]
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
			vmAuth := buildVMAuthLB(d.cr, d.zones)
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
		},
	})

	// should update VMAuth if spec changes
	f(opts{
		prepare: func(d *testData) {
			d.vmclusters[0].Spec.VMSelect.Port = "8481"
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.VMAuth.Spec.LogLevel = "ERROR"
		},
		preRun: func(c client.Client, d *testData) {
			clusters := []*vmv1beta1.VMCluster{d.vmclusters[0]}
			lb := buildVMAuthLB(d.cr, newTestLBZones(d.zones.vmagents, clusters, nil))
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
			clusters := []*vmv1beta1.VMCluster{d.vmclusters[0]}
			vmAuth := buildVMAuthLB(d.cr, newTestLBZones(d.zones.vmagents, clusters, nil))
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
		},
	})

	// should not update VMAuth if spec matches
	f(opts{
		prepare: func(d *testData) {
			d.vmclusters[0].Spec.VMSelect.Port = "8481"
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.VMAuth.Spec = vmv1beta1.VMAuthSpec{
				LogLevel: "INFO",
			}
		},
		preRun: func(c client.Client, d *testData) {
			clusters := []*vmv1beta1.VMCluster{d.vmclusters[0]}
			lb := buildVMAuthLB(d.cr, newTestLBZones(d.zones.vmagents, clusters, nil))
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
			clusters := []*vmv1beta1.VMCluster{d.vmclusters[0]}
			vmAuth := buildVMAuthLB(d.cr, newTestLBZones(d.zones.vmagents, clusters, nil))
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
		},
	})

	// should create VMAuth if it does not exist
	f(opts{
		prepare: func(d *testData) {
			for i := range d.vmclusters {
				d.vmclusters[i].Spec.VMSelect.Port = "8481"
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
			vmAuth := buildVMAuthLB(d.cr, d.zones)
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
			var vmClusterObjs, vmAgentObjs []vmv1beta1.NamespacedName
			for i := range d.zones.vmagents {
				vmAgentObjs = append(vmAgentObjs, vmv1beta1.NamespacedName{
					Name:      d.zones.vmagents[i].Name,
					Namespace: d.zones.vmagents[i].Namespace,
				})
			}
			for i := range d.vmclusters {
				vmClusterObjs = append(vmClusterObjs, vmv1beta1.NamespacedName{
					Name:      d.vmclusters[i].Name,
					Namespace: d.vmclusters[i].Namespace,
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
					URLMapCommon: vmv1beta1.URLMapCommon{
						LoadBalancingPolicy: ptr.To("least_loaded"),
					},
				},
				{
					Name:  "read",
					Paths: []string{"/flags", "/metrics", "/select/.+", "/admin/tenants"},
					CRD: &vmv1beta1.CRDRef{
						Kind:    "VMCluster/vmselect",
						Objects: vmClusterObjs,
					},
					URLMapCommon: vmv1beta1.URLMapCommon{
						LoadBalancingPolicy: ptr.To("first_available"),
					},
				},
			}
			for i := range targetRefs {
				targetRef := &targetRefs[i]
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

	// should create VMAuth with VMSingle read target
	f(opts{
		prepare: func(d *testData) {
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.BackendType = vmv1alpha1.VMDistributedBackendTypeVMSingle
			owner := d.cr.AsOwner()
			// Pre-create VMSingle objects with names matching z.VMSingleName(cr) = "{cr.Name}-{zone.Name}"
			for i := range d.cr.Spec.Zones {
				name := fmt.Sprintf("%s-%s", d.cr.Name, d.cr.Spec.Zones[i].Name)
				vmSingle := newVMSingle(name, d.cr.Namespace, owner)
				d.predefinedObjects = append(d.predefinedObjects, vmSingle)
			}
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			zs, err := getZones(ctx, rclient, d.cr)
			assert.NoError(t, err)
			vmAuth := buildVMAuthLB(d.cr, zs)
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))

			var got vmv1beta1.VMAuth
			nsn := types.NamespacedName{Name: vmAuth.Name, Namespace: vmAuth.Namespace}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Len(t, got.Spec.DefaultTargetRefs, 2)
			assert.Equal(t, "write", got.Spec.DefaultTargetRefs[0].Name)
			assert.Equal(t, "VMAgent", got.Spec.DefaultTargetRefs[0].CRD.Kind)
			assert.Equal(t, "read", got.Spec.DefaultTargetRefs[1].Name)
			assert.Equal(t, "VMSingle", got.Spec.DefaultTargetRefs[1].CRD.Kind)
			// sorted alphabetically then reversed: vmcluster-3, vmcluster-2, vmcluster-1
			assert.Equal(t, []vmv1beta1.NamespacedName{
				{Name: "test-vdc-vmcluster-3", Namespace: d.cr.Namespace},
				{Name: "test-vdc-vmcluster-2", Namespace: d.cr.Namespace},
				{Name: "test-vdc-vmcluster-1", Namespace: d.cr.Namespace},
			}, got.Spec.DefaultTargetRefs[1].CRD.Objects)
		},
	})

	// should adopt existing VMAuth if owner reference is missing
	f(opts{
		prepare: func(d *testData) {
			d.vmclusters[0].Spec.VMSelect.Port = "8481"
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.VMAuth.Spec = vmv1beta1.VMAuthSpec{
				LogLevel: "INFO",
			}
		},
		preRun: func(c client.Client, d *testData) {
			clusters := []*vmv1beta1.VMCluster{d.vmclusters[0]}
			lb := buildVMAuthLB(d.cr, newTestLBZones(d.zones.vmagents, clusters, nil))
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
			clusters := []*vmv1beta1.VMCluster{d.vmclusters[0]}
			vmAuth := buildVMAuthLB(d.cr, newTestLBZones(d.zones.vmagents, clusters, nil))
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
			for i := range d.vmclusters {
				d.vmclusters[i].Spec.VMSelect.Port = "8481"
			}

			// Enable load balancer for vmcluster[0]
			d.vmclusters[0].Spec.RequestsLoadBalancer.Enabled = true
			d.vmclusters[0].Spec.RequestsLoadBalancer.Spec.Port = "8427"
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
			vmAuth := buildVMAuthLB(d.cr, d.zones)
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
			var vmClusterObjs, vmAgentObjs []vmv1beta1.NamespacedName
			for i := range d.zones.vmagents {
				vmAgentObjs = append(vmAgentObjs, vmv1beta1.NamespacedName{
					Name:      d.zones.vmagents[i].Name,
					Namespace: d.zones.vmagents[i].Namespace,
				})
			}
			for i := range d.vmclusters {
				vmClusterObjs = append(vmClusterObjs, vmv1beta1.NamespacedName{
					Name:      d.vmclusters[i].Name,
					Namespace: d.vmclusters[i].Namespace,
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
					URLMapCommon: vmv1beta1.URLMapCommon{
						LoadBalancingPolicy: ptr.To("least_loaded"),
					},
				},
				{
					Name:  "read",
					Paths: []string{"/flags", "/metrics", "/select/.+", "/admin/tenants"},
					CRD: &vmv1beta1.CRDRef{
						Kind:    "VMCluster/vmselect",
						Objects: vmClusterObjs,
					},
					URLMapCommon: vmv1beta1.URLMapCommon{
						LoadBalancingPolicy: ptr.To("first_available"),
					},
				},
			}
			for i := range targetRefs {
				targetRef := &targetRefs[i]
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

	// should always have static fallback for vmauth
	f(opts{
		prepare: func(d *testData) {
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
		},
		preRun: func(c client.Client, d *testData) {
			// Create initial VMAuth backed by CRD targets
			lb := buildVMAuthLB(d.cr, d.zones)
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
				{
					verb: "Get",
					key:  "*v1beta1.VMAuth/default/vmauth-lb",
				},
			}
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			// Simulate removing both backends by passing empty slices
			vmAuth := buildVMAuthLB(d.cr, &zones{})
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))

			var got vmv1beta1.VMAuth
			nsn := types.NamespacedName{
				Name:      vmAuth.Name,
				Namespace: vmAuth.Namespace,
			}
			assert.NoError(t, rclient.Get(ctx, nsn, &got))

			expectedTargetRefs := []vmv1beta1.TargetRef{
				{
					Name:  "write",
					Paths: []string{"/insert/.+", "/api/v1/write"},
					URLMapCommon: vmv1beta1.URLMapCommon{
						LoadBalancingPolicy: ptr.To("least_loaded"),
					},
				},
				{
					Name:  "read",
					Paths: []string{"/flags", "/metrics", "/select/.+", "/admin/tenants"},
					URLMapCommon: vmv1beta1.URLMapCommon{
						LoadBalancingPolicy: ptr.To("first_available"),
					},
				},
			}
			for i := range expectedTargetRefs {
				expectedTargetRefs[i].RetryStatusCodes = []int{500, 502, 503}
				expectedTargetRefs[i].Static = &vmv1beta1.StaticRef{URL: defaultStubAddr}
			}
			assert.Equal(t, expectedTargetRefs, got.Spec.DefaultTargetRefs)
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
