package vmdistributed

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const (
	vmclusterWaitReadyDeadline = time.Minute
	httpTimeout                = time.Second * 5
)

func newVMCluster(name, version string) *vmv1beta1.VMCluster {
	return &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"tenant": "default"},
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
				UpdateStatus: vmv1beta1.UpdateStatusOperational,
			},
		},
	}
}

// newVMDistributed constructs a VMDistributed for tests.
func newVMDistributed(name string, zones []vmv1alpha1.VMDistributedZone, vmAgentSpec vmv1alpha1.VMDistributedAgent, extras ...any) *vmv1alpha1.VMDistributed {
	var vmAuth vmv1alpha1.VMDistributedAuth

	// Parse extras to find VMAuth (ignore any legacy VMUser parameters).
	for _, e := range extras {
		switch v := e.(type) {
		case vmv1alpha1.VMDistributedAuth:
			vmAuth = v
		case *vmv1alpha1.VMDistributedAuth:
			if v != nil {
				vmAuth = *v
			}
		default:
			// unknown extra param — ignore
		}
	}

	return &vmv1alpha1.VMDistributed{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VMDistributed",
			APIVersion: vmv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: vmv1alpha1.VMDistributedSpec{
			Zones:   zones,
			VMAgent: vmAgentSpec,
			VMAuth:  vmAuth,
		},
	}
}

type opts struct {
	prepare func(*testData)
	verify  func(context.Context, *k8stools.TestClientWithStatsTrack, *testData)
}

type testData struct {
	vmagent           *vmv1beta1.VMAgent
	vmclusters        []*vmv1beta1.VMCluster
	cr                *vmv1alpha1.VMDistributed
	predefinedObjects []runtime.Object
}

func beforeEach(o opts) *testData {
	vmagent := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vmagent", Namespace: "default"},
		Spec: vmv1beta1.VMAgentSpec{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{ReplicaCount: ptr.To(int32(1))},
		},
		Status: vmv1beta1.VMAgentStatus{Replicas: 1},
	}
	vmclusters := []*vmv1beta1.VMCluster{
		newVMCluster("vmcluster-1", "v1.0.0"),
		newVMCluster("vmcluster-2", "v1.0.0"),
	}
	var zones []vmv1alpha1.VMDistributedZone
	for i, cluster := range vmclusters {
		zones = append(zones, vmv1alpha1.VMDistributedZone{
			Name: fmt.Sprintf("vmcluster-%d", i+1),
			VMCluster: &vmv1alpha1.VMDistributedCluster{
				Ref: &corev1.LocalObjectReference{Name: cluster.Name},
			},
		})
	}
	vmAgentSpec := vmv1alpha1.VMDistributedAgent{Name: vmagent.Name}
	cr := newVMDistributed("test-vdc", zones, vmAgentSpec, vmv1alpha1.VMDistributedAuth{Name: "vmauth-proxy"})
	d := &testData{
		vmagent:    vmagent,
		vmclusters: vmclusters,
		cr:         cr,
		predefinedObjects: []runtime.Object{
			vmagent, vmclusters[0], vmclusters[1], cr,
		},
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
			assert.NoError(t, CreateOrUpdate(ctx, d.cr, rclient, httpTimeout))
			assert.Empty(t, rclient.TotalCallsCount(nil))
		},
	})

	// missing VMCluster should return error
	f(opts{
		prepare: func(d *testData) {
			d.cr.Spec.Zones[0].VMCluster.Ref.Name = "non-existent-vmcluster"
		},
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			err := CreateOrUpdate(ctx, d.cr, rclient, httpTimeout)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "failed to fetch vmclusters")
		},
	})

	// check existing remote write urls order
	f(opts{
		prepare: func(d *testData) {
			for _, cluster := range d.vmclusters {
				d.vmagent.Spec.RemoteWrite = append(d.vmagent.Spec.RemoteWrite, vmv1beta1.VMAgentRemoteWriteSpec{
					URL: remoteWriteURL(cluster),
				})
			}
		},
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			_, err := updateOrCreateVMAgent(ctx, rclient, d.cr, d.vmclusters)
			assert.NoError(t, err)

			// Fetch the resulting vmagent
			var got vmv1beta1.VMAgent
			assert.NoError(t, rclient.Get(ctx, client.ObjectKey{Name: d.vmagent.Name, Namespace: d.vmagent.Namespace}, &got))

			// Verify urls order is preserved
			assert.Len(t, got.Spec.RemoteWrite, len(d.vmclusters))
			for i := range d.vmclusters {
				assert.Equal(t, remoteWriteURL(d.vmclusters[i]), got.Spec.RemoteWrite[i].URL)
			}
		},
	})

	// verify remote write urls is appended to vmagent in a valid order
	f(opts{
		prepare: func(d *testData) {
			d.vmagent.Spec.RemoteWrite = []vmv1beta1.VMAgentRemoteWriteSpec{
				{URL: remoteWriteURL(d.vmclusters[0])},
			}
		},
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			_, err := updateOrCreateVMAgent(ctx, rclient, d.cr, d.vmclusters)
			assert.NoError(t, err)

			// Fetch the resulting vmagent
			got := &vmv1beta1.VMAgent{}
			assert.NoError(t, rclient.Get(ctx, client.ObjectKey{Name: d.vmagent.Name, Namespace: d.vmagent.Namespace}, got))

			// Verify urls order is preserved
			assert.Len(t, got.Spec.RemoteWrite, 2)
			for i := range d.vmclusters {
				assert.Equal(t, remoteWriteURL(d.vmclusters[i]), got.Spec.RemoteWrite[i].URL)
			}
		},
	})

	// should do nothing if VMAuth name is empty
	f(opts{
		prepare: func(d *testData) {
			d.cr.Spec.VMAuth.Name = ""
		},
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			assert.NoError(t, createOrUpdateVMAuthLB(ctx, rclient, d.cr, d.vmclusters))
			assert.Empty(t, rclient.TotalCallsCount(nil))
		},
	})

	// should update VMAuth if spec changes
	f(opts{
		prepare: func(d *testData) {
			d.vmclusters[0].Spec.VMSelect.Port = "8481"
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.predefinedObjects = append(d.predefinedObjects, &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmauth-lb",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{
					LogLevel: "ERROR",
				},
			})
		},
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			d.cr.Spec.VMAuth.Spec = &vmv1beta1.VMAuthSpec{
				LogLevel: "INFO",
			}
			clusters := []*vmv1beta1.VMCluster{d.vmclusters[0]}
			assert.NoError(t, createOrUpdateVMAuthLB(ctx, rclient, d.cr, clusters))

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
			d.vmclusters[0].Spec.VMSelect.Port = "8481"
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.VMAuth.Spec = &vmv1beta1.VMAuthSpec{
				LogLevel: "INFO",
			}
			lb := &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:            d.cr.Spec.VMAuth.Name,
					Namespace:       d.cr.Namespace,
					OwnerReferences: []metav1.OwnerReference{d.cr.AsOwner()},
				},
				Spec: *d.cr.Spec.VMAuth.Spec,
			}
			lb.Spec.UnauthorizedUserAccessSpec = &vmv1beta1.VMAuthUnauthorizedUserAccessSpec{
				TargetRefs: getVMClusterTargetRefs([]*vmv1beta1.VMCluster{d.vmclusters[0]}),
			}
			d.predefinedObjects = append(d.predefinedObjects, lb)
		},
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			clusters := []*vmv1beta1.VMCluster{d.vmclusters[0]}
			assert.NoError(t, createOrUpdateVMAuthLB(ctx, rclient, d.cr, clusters))

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
			for i := range d.vmclusters {
				d.vmclusters[i].Spec.VMSelect.Port = "8481"
			}
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.VMAuth.Spec = &vmv1beta1.VMAuthSpec{
				LogLevel: "INFO",
			}
		},
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			assert.NoError(t, createOrUpdateVMAuthLB(ctx, rclient, d.cr, d.vmclusters))
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
			assert.Len(t, createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs, 2)

			firstTargetRef := createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs[0]
			assert.Equal(t, ptr.To(0), firstTargetRef.DropSrcPathPrefixParts)
			assert.Equal(t, ptr.To("first_available"), firstTargetRef.LoadBalancingPolicy)
			assert.Equal(t, []int{500, 502, 503}, firstTargetRef.RetryStatusCodes)
			assert.Equal(t, []string{"/select/.+", "/admin/tenants"}, firstTargetRef.Paths)
			assert.NotNil(t, firstTargetRef.CRD)
			assert.Equal(t, "VMCluster/vmselect", firstTargetRef.CRD.Kind)
			assert.Equal(t, d.vmclusters[0].Name, firstTargetRef.CRD.Name)
			assert.Equal(t, d.vmclusters[0].Namespace, firstTargetRef.CRD.Namespace)

			secondTargetRef := createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs[1]
			assert.Equal(t, ptr.To(0), secondTargetRef.DropSrcPathPrefixParts)
			assert.Equal(t, ptr.To("first_available"), secondTargetRef.LoadBalancingPolicy)
			assert.Equal(t, []int{500, 502, 503}, secondTargetRef.RetryStatusCodes)
			assert.Equal(t, []string{"/select/.+", "/admin/tenants"}, secondTargetRef.Paths)
			assert.NotNil(t, secondTargetRef.CRD)
			assert.Equal(t, "VMCluster/vmselect", secondTargetRef.CRD.Kind)
			assert.Equal(t, d.vmclusters[1].Name, secondTargetRef.CRD.Name)
			assert.Equal(t, d.vmclusters[1].Namespace, secondTargetRef.CRD.Namespace)

			// Verify OwnerReference
			assert.NotEmpty(t, createdVMAuth.OwnerReferences)
			assert.Equal(t, d.cr.Name, createdVMAuth.OwnerReferences[0].Name)
		},
	})

	// should adopt existing VMAuth if owner reference is missing
	f(opts{
		prepare: func(d *testData) {
			d.vmclusters[0].Spec.VMSelect.Port = "8481"
			d.cr.Spec.VMAuth.Name = "vmauth-lb"
			d.cr.Spec.VMAuth.Spec = &vmv1beta1.VMAuthSpec{
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
			})
		},
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			clusters := []*vmv1beta1.VMCluster{d.vmclusters[0]}
			assert.NoError(t, createOrUpdateVMAuthLB(ctx, rclient, d.cr, clusters))
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
			for i := range d.vmclusters {
				d.vmclusters[i].Spec.VMSelect.Port = "8481"
			}

			// Enable load balancer for vmcluster[0]
			d.vmclusters[0].Spec.RequestsLoadBalancer.Enabled = true
			d.vmclusters[0].Spec.RequestsLoadBalancer.Spec.Port = "8427"
		},
		verify: func(ctx context.Context, rclient *k8stools.TestClientWithStatsTrack, d *testData) {
			assert.NoError(t, createOrUpdateVMAuthLB(ctx, rclient, d.cr, d.vmclusters))
			createdItem := rclient.CreateCalls.First(&vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmauth-lb",
					Namespace: "default",
				},
			})

			assert.NotNil(t, createdItem)
			createdVMAuth := createdItem.(*vmv1beta1.VMAuth)
			assert.NotNil(t, createdVMAuth.Spec.UnauthorizedUserAccessSpec)
			assert.Len(t, createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs, 2)
			firstTargetRef := createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs[0]
			assert.Equal(t, ptr.To(0), firstTargetRef.DropSrcPathPrefixParts)
			assert.Equal(t, ptr.To("first_available"), firstTargetRef.LoadBalancingPolicy)
			assert.Equal(t, []int{500, 502, 503}, firstTargetRef.RetryStatusCodes)
			assert.Equal(t, []string{"/select/.+", "/admin/tenants"}, firstTargetRef.Paths)
			assert.NotNil(t, firstTargetRef.CRD)
			assert.Equal(t, "VMCluster/vmselect", firstTargetRef.CRD.Kind)
			assert.Equal(t, d.vmclusters[0].Name, firstTargetRef.CRD.Name)
			assert.Equal(t, d.vmclusters[0].Namespace, firstTargetRef.CRD.Namespace)

			secondTargetRef := createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs[1]
			assert.Equal(t, ptr.To(0), secondTargetRef.DropSrcPathPrefixParts)
			assert.Equal(t, ptr.To("first_available"), secondTargetRef.LoadBalancingPolicy)
			assert.Equal(t, []int{500, 502, 503}, secondTargetRef.RetryStatusCodes)
			assert.Equal(t, []string{"/select/.+", "/admin/tenants"}, secondTargetRef.Paths)
			assert.NotNil(t, secondTargetRef.CRD)
			assert.Equal(t, "VMCluster/vmselect", secondTargetRef.CRD.Kind)
			assert.Equal(t, d.vmclusters[1].Name, secondTargetRef.CRD.Name)
			assert.Equal(t, d.vmclusters[1].Namespace, secondTargetRef.CRD.Namespace)
		},
	})
}
