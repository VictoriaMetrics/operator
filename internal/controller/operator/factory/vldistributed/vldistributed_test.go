package vldistributed

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

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

func newVLAgent(name, namespace string, owner metav1.OwnerReference) *vmv1.VLAgent {
	return &vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.Now(),
			OwnerReferences:   []metav1.OwnerReference{owner},
		},
		Spec: vmv1.VLAgentSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To[int32](1),
			},
		},
	}
}

func newVLCluster(name, namespace, version string, owner metav1.OwnerReference) *vmv1.VLCluster {
	return &vmv1.VLCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			Labels:            map[string]string{"tenant": "default"},
			CreationTimestamp: metav1.Now(),
			OwnerReferences:   []metav1.OwnerReference{owner},
		},
		Spec: vmv1.VLClusterSpec{
			ClusterVersion: version,
			VLSelect: &vmv1.VLSelect{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
				},
			},
			VLInsert: &vmv1.VLInsert{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
				},
			},
			VLStorage: &vmv1.VLStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
				},
			},
		},
	}
}

func newVLSingle(name, namespace string, owner metav1.OwnerReference) *vmv1.VLSingle {
	return &vmv1.VLSingle{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.Now(),
			OwnerReferences:   []metav1.OwnerReference{owner},
		},
		Spec: vmv1.VLSingleSpec{},
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
	vlclusters        []*vmv1.VLCluster
	cr                *vmv1alpha1.VLDistributed
	predefinedObjects []runtime.Object
}

func beforeEach(o opts) *testData {
	zonesCount := 3
	zs := &zones{
		backends: make([]vlBackend, 0, zonesCount),
		vlagents: make([]*vmv1.VLAgent, 0, zonesCount),
	}
	namespace := "default"
	cr := &vmv1alpha1.VLDistributed{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VLDistributed",
			APIVersion: vmv1alpha1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vdc",
			Namespace: namespace,
		},
		Spec: vmv1alpha1.VLDistributedSpec{
			Zones: make([]vmv1alpha1.VLDistributedZone, zonesCount),
			VMAuth: vmv1alpha1.VLDistributedAuth{
				Name: "vmauth-proxy",
			},
		},
	}
	var predefinedObjects []runtime.Object
	var vlclusters []*vmv1.VLCluster
	owner := cr.AsOwner()
	for i := range cr.Spec.Zones {
		name := fmt.Sprintf("vlcluster-%d", i+1)
		vlCluster := newVLCluster(name, namespace, "v1.51.0", owner)
		vlAgent := newVLAgent(name, namespace, owner)
		zs.backends = append(zs.backends, vlBackend{obj: vlCluster})
		zs.vlagents = append(zs.vlagents, vlAgent)
		vlclusters = append(vlclusters, vlCluster)
		predefinedObjects = append(predefinedObjects, vlAgent, vlCluster)
		cr.Spec.Zones[i] = vmv1alpha1.VLDistributedZone{
			Name: name,
			VLCluster: vmv1alpha1.VLDistributedZoneCluster{
				Name: name,
				Spec: vlCluster.Spec,
			},
			VLAgent: vmv1alpha1.VLDistributedZoneAgent{
				Name: name,
			},
		}
	}

	predefinedObjects = append(predefinedObjects, cr)
	d := &testData{
		zones:             zs,
		vlclusters:        vlclusters,
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
				case *vmv1.VLCluster:
					v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
					v.Status.ObservedGeneration = 1
				case *vmv1beta1.VMAuth:
					v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				case *vmv1.VLAgent:
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
			for _, vlAgent := range d.zones.vlagents {
				for _, vlCluster := range d.vlclusters {
					vlAgent.Spec.RemoteWrite = append(vlAgent.Spec.RemoteWrite, vmv1.VLAgentRemoteWriteSpec{
						URL: vlCluster.GetRemoteWriteURL(),
					})
				}
			}
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			zs, err := getZones(ctx, rclient, d.cr)
			assert.NoError(t, err)

			// Verify urls order is preserved
			for i, vlCluster := range d.vlclusters {
				for _, vlAgent := range zs.vlagents {
					assert.Len(t, vlAgent.Spec.RemoteWrite, len(d.vlclusters))
					assert.Equal(t, vlCluster.GetRemoteWriteURL(), vlAgent.Spec.RemoteWrite[i].URL)
				}
			}
		},
	})

	// validate remote write urls is appended to vlagent in a valid order
	f(opts{
		prepare: func(d *testData) {
			for _, vlAgent := range d.zones.vlagents {
				vlCluster := d.vlclusters[2]
				vlAgent.Spec.RemoteWrite = []vmv1.VLAgentRemoteWriteSpec{
					{URL: vlCluster.GetRemoteWriteURL()},
				}
			}
		},
		validate: func(ctx context.Context, rclient client.Client, d *testData) {
			zs, err := getZones(ctx, rclient, d.cr)
			assert.NoError(t, err)

			// Verify urls order is preserved
			for _, vlAgent := range zs.vlagents {
				assert.Len(t, vlAgent.Spec.RemoteWrite, 3)
				vlCluster0 := d.vlclusters[0]
				vlCluster1 := d.vlclusters[1]
				vlCluster2 := d.vlclusters[2]
				assert.Equal(t, vlCluster2.GetRemoteWriteURL(), vlAgent.Spec.RemoteWrite[0].URL)
				assert.Equal(t, vlCluster0.GetRemoteWriteURL(), vlAgent.Spec.RemoteWrite[1].URL)
				assert.Equal(t, vlCluster1.GetRemoteWriteURL(), vlAgent.Spec.RemoteWrite[2].URL)
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
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.VMAuth.Spec.LogLevel = "ERROR"
		},
		preRun: func(c client.Client, d *testData) {
			clusters := []*vmv1.VLCluster{d.vlclusters[0]}
			lb := buildVMAuthLB(d.cr, newTestLBZones(d.zones.vlagents, clusters, nil))
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
			clusters := []*vmv1.VLCluster{d.vlclusters[0]}
			vmAuth := buildVMAuthLB(d.cr, newTestLBZones(d.zones.vlagents, clusters, nil))
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
		},
	})

	// should not update VMAuth if spec matches
	f(opts{
		prepare: func(d *testData) {
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.VMAuth.Spec = vmv1beta1.VMAuthSpec{
				LogLevel: "INFO",
			}
		},
		preRun: func(c client.Client, d *testData) {
			clusters := []*vmv1.VLCluster{d.vlclusters[0]}
			lb := buildVMAuthLB(d.cr, newTestLBZones(d.zones.vlagents, clusters, nil))
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
			clusters := []*vmv1.VLCluster{d.vlclusters[0]}
			vmAuth := buildVMAuthLB(d.cr, newTestLBZones(d.zones.vlagents, clusters, nil))
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
		},
	})

	// should create VMAuth if it does not exist
	f(opts{
		prepare: func(d *testData) {
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
			var vlClusterObjs, vlAgentObjs []vmv1beta1.NamespacedName
			for i := range d.zones.vlagents {
				vlAgentObjs = append(vlAgentObjs, vmv1beta1.NamespacedName{
					Name:      d.zones.vlagents[i].Name,
					Namespace: d.zones.vlagents[i].Namespace,
				})
			}
			for i := range d.vlclusters {
				vlClusterObjs = append(vlClusterObjs, vmv1beta1.NamespacedName{
					Name:      d.vlclusters[i].Name,
					Namespace: d.vlclusters[i].Namespace,
				})
			}
			// oldest generations are listed first in vmauth
			slices.Reverse(vlClusterObjs)
			targetRefs := []vmv1beta1.TargetRef{
				{
					Name:  "write",
					Paths: []string{"/insert/.+"},
					CRD: &vmv1beta1.CRDRef{
						Kind:    "VLAgent",
						Objects: vlAgentObjs,
					},
					URLMapCommon: vmv1beta1.URLMapCommon{
						LoadBalancingPolicy: ptr.To("least_loaded"),
					},
				},
				{
					Name:  "read",
					Paths: []string{"/flags", "/metrics", "/select/.+"},
					CRD: &vmv1beta1.CRDRef{
						Kind:    "VLCluster/vlselect",
						Objects: vlClusterObjs,
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

	// should create VMAuth with VLSingle read target
	f(opts{
		prepare: func(d *testData) {
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.BackendType = vmv1alpha1.VLDistributedBackendTypeVLSingle
			owner := d.cr.AsOwner()
			// Pre-create VLSingle objects with names matching z.VLSingleName(cr) = "{cr.Name}-{zone.Name}"
			for i := range d.cr.Spec.Zones {
				name := fmt.Sprintf("%s-%s", d.cr.Name, d.cr.Spec.Zones[i].Name)
				vlSingle := newVLSingle(name, d.cr.Namespace, owner)
				d.predefinedObjects = append(d.predefinedObjects, vlSingle)
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
			assert.Equal(t, "VLAgent", got.Spec.DefaultTargetRefs[0].CRD.Kind)
			assert.Equal(t, "read", got.Spec.DefaultTargetRefs[1].Name)
			assert.Equal(t, "VLSingle", got.Spec.DefaultTargetRefs[1].CRD.Kind)
			// sorted alphabetically then reversed: vlcluster-3, vlcluster-2, vlcluster-1
			assert.Equal(t, []vmv1beta1.NamespacedName{
				{Name: "test-vdc-vlcluster-3", Namespace: d.cr.Namespace},
				{Name: "test-vdc-vlcluster-2", Namespace: d.cr.Namespace},
				{Name: "test-vdc-vlcluster-1", Namespace: d.cr.Namespace},
			}, got.Spec.DefaultTargetRefs[1].CRD.Objects)
		},
	})

	// should adopt existing VMAuth if owner reference is missing
	f(opts{
		prepare: func(d *testData) {
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.VMAuth.Spec = vmv1beta1.VMAuthSpec{
				LogLevel: "INFO",
			}
		},
		preRun: func(c client.Client, d *testData) {
			clusters := []*vmv1.VLCluster{d.vlclusters[0]}
			lb := buildVMAuthLB(d.cr, newTestLBZones(d.zones.vlagents, clusters, nil))
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
			clusters := []*vmv1.VLCluster{d.vlclusters[0]}
			vmAuth := buildVMAuthLB(d.cr, newTestLBZones(d.zones.vlagents, clusters, nil))
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
					Paths: []string{"/insert/.+"},
					URLMapCommon: vmv1beta1.URLMapCommon{
						LoadBalancingPolicy: ptr.To("least_loaded"),
					},
				},
				{
					Name:  "read",
					Paths: []string{"/flags", "/metrics", "/select/.+"},
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
