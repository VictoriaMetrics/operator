package vmdistributed

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

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
		Status: vmv1beta1.VMAgentStatus{
			StatusMetadata: vmv1beta1.StatusMetadata{
				UpdateStatus:       vmv1beta1.UpdateStatusOperational,
				ObservedGeneration: 1,
			},
			Replicas: 1,
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
		Status: vmv1beta1.VMClusterStatus{
			StatusMetadata: vmv1beta1.StatusMetadata{
				UpdateStatus:       vmv1beta1.UpdateStatusOperational,
				ObservedGeneration: 1,
			},
		},
	}
}

type opts struct {
	prepare func(*testData)
	verify  func(context.Context, *k8stools.TestClientWithStatsTrack, *testData)
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

func TestCreateOrUpdate(t *testing.T) {
	f := func(o opts) {
		t.Helper()
		d := beforeEach(o)
		rclient := k8stools.GetTestClientWithObjects(d.predefinedObjects)
		ctx := context.Background()
		o.verify(ctx, rclient, d)
	}

	// paused CR should do nothing
	f(opts{
		prepare: func(d *testData) {
			d.cr.Spec.Paused = true
		},
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			assert.NoError(t, CreateOrUpdate(ctx, d.cr, rclient))
			assert.Empty(t, rclient.TotalCallsCount(nil))
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
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
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

	// verify remote write urls is appended to vmagent in a valid order
	f(opts{
		prepare: func(d *testData) {
			for _, vmAgent := range d.zones.vmagents {
				vmCluster := d.zones.vmclusters[2]
				vmAgent.Spec.RemoteWrite = []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: vmCluster.GetRemoteWriteURL()},
				}
			}
		},
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
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
			d.cr.Spec.VMAuth.Name = ""
		},
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			vmAuth := buildVMAuthLB(d.cr, d.zones.vmagents, d.zones.vmclusters)
			vmAuth.Status.StatusMetadata = vmv1beta1.StatusMetadata{
				UpdateStatus: vmv1beta1.UpdateStatusOperational,
			}
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
			assert.Equal(t, 3, rclient.TotalCallsCount(nil))
			// Verify the VMAuth was created with the default name (cr.Name)
			createdItem := rclient.CreateCalls.First(&vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      d.cr.Name,
					Namespace: d.cr.Namespace,
				},
			})
			assert.NotNil(t, createdItem, "VMAuth should be created with default name")
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
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			d.cr.Spec.VMAuth.Spec = vmv1beta1.VMAuthSpec{
				LogLevel: "INFO",
			}
			clusters := []*vmv1beta1.VMCluster{d.zones.vmclusters[0]}
			vmAuth := buildVMAuthLB(d.cr, d.zones.vmagents, clusters)
			vmAuth.Status.StatusMetadata = vmv1beta1.StatusMetadata{
				UpdateStatus:       vmv1beta1.UpdateStatusOperational,
				ObservedGeneration: 1,
			}
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))

			// Check for update call
			item := rclient.UpdateCalls.First(&vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmauth-lb",
					Namespace: "default",
				},
			})
			assert.NotNil(t, item, "VMAuth should be updated")
			updatedVMAuth := item.(*vmv1beta1.VMAuth)
			assert.Equal(t, "INFO", updatedVMAuth.Spec.LogLevel)
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
					Finalizers:      []string{vmv1beta1.FinalizerName},
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
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			clusters := []*vmv1beta1.VMCluster{d.zones.vmclusters[0]}
			vmAuth := buildVMAuthLB(d.cr, d.zones.vmagents, clusters)
			vmAuth.Status.StatusMetadata = vmv1beta1.StatusMetadata{
				UpdateStatus:       vmv1beta1.UpdateStatusOperational,
				ObservedGeneration: 1,
			}
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))

			// Should contain no updates
			assert.Nil(t, rclient.UpdateCalls.First(&vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmauth-lb",
					Namespace: "default",
				},
			}))
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
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			vmAuth := buildVMAuthLB(d.cr, d.zones.vmagents, d.zones.vmclusters)
			vmAuth.Status.StatusMetadata = vmv1beta1.StatusMetadata{
				UpdateStatus: vmv1beta1.UpdateStatusOperational,
			}
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
			createdItem := rclient.CreateCalls.First(&vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmauth-lb",
					Namespace: "default",
				},
			})
			assert.NotNil(t, createdItem)
			createdVMAuth := createdItem.(*vmv1beta1.VMAuth)
			assert.Equal(t, "vmauth-lb", createdVMAuth.Name)
			assert.Equal(t, "default", createdVMAuth.Namespace)
			assert.Equal(t, "INFO", createdVMAuth.Spec.LogLevel)
			assert.NotNil(t, createdVMAuth.Spec.UnauthorizedUserAccessSpec)
			assert.Len(t, createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs, 6)

			for i := range createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs {
				targetRef := &createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs[i]
				assert.Equal(t, ptr.To("first_available"), targetRef.LoadBalancingPolicy)
				assert.Equal(t, []int{500, 502, 503}, targetRef.RetryStatusCodes)
				assert.NotNil(t, targetRef.CRD)
				if i < len(d.zones.vmagents) {
					assert.Equal(t, d.zones.vmagents[i].Name, targetRef.CRD.Name)
					assert.Equal(t, d.zones.vmagents[i].Namespace, targetRef.CRD.Namespace)
					assert.Equal(t, []string{"/insert/.+"}, targetRef.Paths)
					assert.Equal(t, "VMAgent", targetRef.CRD.Kind)
				} else {
					idx := i - len(d.zones.vmagents)
					assert.Equal(t, d.zones.vmclusters[idx].Name, targetRef.CRD.Name)
					assert.Equal(t, d.zones.vmclusters[idx].Namespace, targetRef.CRD.Namespace)
					assert.Equal(t, []string{"/select/.+", "/admin/tenants"}, targetRef.Paths)
					assert.Equal(t, "VMCluster/vmselect", targetRef.CRD.Kind)
				}
			}

			// Verify OwnerReference
			assert.NotEmpty(t, createdVMAuth.OwnerReferences)
			assert.Equal(t, d.cr.Name, createdVMAuth.OwnerReferences[0].Name)
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
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			clusters := []*vmv1beta1.VMCluster{d.zones.vmclusters[0]}
			vmAuth := buildVMAuthLB(d.cr, d.zones.vmagents, clusters)
			vmAuth.Status.StatusMetadata = vmv1beta1.StatusMetadata{
				UpdateStatus:       vmv1beta1.UpdateStatusOperational,
				ObservedGeneration: 1,
			}
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
			updatedVMAuth := rclient.UpdateCalls.First(&vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmauth-lb",
					Namespace: "default",
				},
			})
			assert.NotNil(t, updatedVMAuth)
			ownerReferences := updatedVMAuth.GetOwnerReferences()
			assert.NotEmpty(t, ownerReferences)
			assert.Equal(t, d.cr.Name, ownerReferences[0].Name)
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
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			vmAuth := buildVMAuthLB(d.cr, d.zones.vmagents, d.zones.vmclusters)
			vmAuth.Status.StatusMetadata = vmv1beta1.StatusMetadata{
				UpdateStatus: vmv1beta1.UpdateStatusOperational,
			}
			owner := d.cr.AsOwner()
			assert.NoError(t, reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner))
			createdItem := rclient.CreateCalls.First(&vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmauth-lb",
					Namespace: "default",
				},
			})

			assert.NotNil(t, createdItem)
			createdVMAuth := createdItem.(*vmv1beta1.VMAuth)
			assert.NotNil(t, createdVMAuth.Spec.UnauthorizedUserAccessSpec)
			assert.Len(t, createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs, 6)

			for i := range createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs {
				targetRef := &createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs[i]
				assert.Equal(t, ptr.To("first_available"), targetRef.LoadBalancingPolicy)
				assert.Equal(t, []int{500, 502, 503}, targetRef.RetryStatusCodes)
				assert.NotNil(t, targetRef.CRD)
				if i < len(d.zones.vmagents) {
					assert.Equal(t, d.zones.vmagents[i].Name, targetRef.CRD.Name)
					assert.Equal(t, d.zones.vmagents[i].Namespace, targetRef.CRD.Namespace)
					assert.Equal(t, []string{"/insert/.+"}, targetRef.Paths)
					assert.Equal(t, "VMAgent", targetRef.CRD.Kind)
				} else {
					idx := i - len(d.zones.vmagents)
					assert.Equal(t, d.zones.vmclusters[idx].Name, targetRef.CRD.Name)
					assert.Equal(t, d.zones.vmclusters[idx].Namespace, targetRef.CRD.Namespace)
					assert.Equal(t, []string{"/select/.+", "/admin/tenants"}, targetRef.Paths)
					assert.Equal(t, "VMCluster/vmselect", targetRef.CRD.Kind)
				}
			}
		},
	})
}
