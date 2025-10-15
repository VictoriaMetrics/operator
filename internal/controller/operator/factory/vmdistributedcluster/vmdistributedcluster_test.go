package vmdistributedcluster

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type action struct {
	Method string
	Object client.Object
}

type trackingClient struct {
	client.Client
	Actions []action
}

func (tc *trackingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if obj != nil && obj.DeepCopyObject() != nil {
		tc.Actions = append(tc.Actions, action{Method: "Get", Object: obj.DeepCopyObject().(client.Object)})
	}
	return tc.Client.Get(ctx, key, obj, opts...)
}

func (tc *trackingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if obj != nil && obj.DeepCopyObject() != nil {
		tc.Actions = append(tc.Actions, action{Method: "Update", Object: obj.DeepCopyObject().(client.Object)})
	}
	return tc.Client.Update(ctx, obj, opts...)
}

func (tc *trackingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if obj != nil && obj.DeepCopyObject() != nil {
		tc.Actions = append(tc.Actions, action{Method: "Create", Object: obj.DeepCopyObject().(client.Object)})
	}
	return tc.Client.Create(ctx, obj, opts...)
}

func (tc *trackingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if obj != nil && obj.DeepCopyObject() != nil {
		tc.Actions = append(tc.Actions, action{Method: "Delete", Object: obj.DeepCopyObject().(client.Object)})
	}
	return tc.Client.Delete(ctx, obj, opts...)
}

func (tc *trackingClient) Status() client.StatusWriter {
	return tc.Client.Status()
}

// Helper to create a basic VMAuth object
func newVMAuth(name, namespace string) *vmv1beta1.VMAuth {
	return &vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vmv1beta1.VMAuthSpec{
			SelectAllByDefault: true,
		},
	}
}

// Helper to create a basic VMCluster object
func newVMCluster(name, namespace, version string, generation int64) *vmv1beta1.VMCluster {
	return &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: generation,
		},
		Spec: vmv1beta1.VMClusterSpec{
			ClusterVersion: version,
		},
	}
}

type testData struct {
	vmauth         *vmv1beta1.VMAuth
	vmcluster1     *vmv1beta1.VMCluster
	vmcluster2     *vmv1beta1.VMCluster
	cr             *vmv1alpha1.VMDistributedCluster
	trackingClient trackingClient
}

func beforeEach() testData {
	// Initialize test environment
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)

	vmauthName := "test-vmauth"
	vmauth := newVMAuth(vmauthName, "default")

	vmcluster1Name := "cluster-1"
	vmcluster1 := newVMCluster(vmcluster1Name, "default", "v1.0.0", 1)

	vmcluster2Name := "cluster-2"
	vmcluster2 := newVMCluster(vmcluster2Name, "default", "v1.0.0", 1)

	cr := &vmv1alpha1.VMDistributedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "distributed-1",
			Namespace: "default",
		},
		Spec: vmv1alpha1.VMDistributedClusterSpec{
			ClusterVersion: "v1.1.0",
			VMAuth:         corev1.LocalObjectReference{Name: vmauthName},
			VMClusters: []corev1.LocalObjectReference{
				{Name: vmcluster1Name},
				{Name: vmcluster2Name},
			},
		},
		Status: vmv1alpha1.VMDistributedClusterStatus{
			VMClusterGenerations: []vmv1alpha1.VMClusterGenerationPair{
				{VMClusterName: "cluster-1", Generation: 1},
				{VMClusterName: "cluster-2", Generation: 1},
			},
		},
	}

	baseFake := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(vmauth, vmcluster1, vmcluster2).
		Build()

	return testData{
		vmauth:         vmauth,
		vmcluster1:     vmcluster1,
		vmcluster2:     vmcluster2,
		cr:             cr,
		trackingClient: trackingClient{baseFake, []action{}},
	}

}

func TestCreateOrUpdate_DistributedCluster(t *testing.T) {
	ctx := context.Background()

	t.Run("Successful reconciliation", func(t *testing.T) {
		td := beforeEach()
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient)
		assert.NoError(t, err, "CreateOrUpdate should succeed when all resources are present")
		assert.Len(t, td.trackingClient.Actions, 5, "Should perform five actions")
	})

	t.Run("VMAuth is unmanaged", func(t *testing.T) {
		td := beforeEach()

		// Make VMAuth unmanaged by setting SelectAllByDefault to false
		td.vmauth.Spec.SelectAllByDefault = false
		err := td.trackingClient.Update(ctx, td.vmauth)
		assert.NoError(t, err)
		td.trackingClient.Actions = []action{}

		err = CreateOrUpdate(ctx, td.cr, &td.trackingClient)
		assert.Error(t, err, "CreateOrUpdate should error if VMAuth is unmanaged")
		assert.Len(t, td.trackingClient.Actions, 1, "Should perform one action")
	})

	t.Run("VMCluster missing", func(t *testing.T) {
		td := beforeEach()

		td.cr.Spec.VMClusters = []corev1.LocalObjectReference{
			{Name: "missing-cluster"},
		}
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient)
		assert.Error(t, err, "CreateOrUpdate should error if VMCluster is missing")
		assert.Len(t, td.trackingClient.Actions, 2, "Should perform two actions")
	})

	t.Run("Generations change triggers status update", func(t *testing.T) {
		td := beforeEach()
		td.cr.Status.VMClusterGenerations[0].Generation = 2 // update generation
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient)
		assert.Error(t, err, "CreateOrUpdate should error if generations change detected")
		assert.Len(t, td.trackingClient.Actions, 3, "Should perform three actions")
	})

	t.Run("Partial cluster version update", func(t *testing.T) {
		td := beforeEach()
		td.vmcluster1.Spec.ClusterVersion = "v1.1.0"
		err := td.trackingClient.Update(ctx, td.vmcluster1)
		assert.NoError(t, err)
		td.trackingClient.Actions = []action{}

		err = CreateOrUpdate(ctx, td.cr, &td.trackingClient)
		assert.NoError(t, err, "CreateOrUpdate should succeed when cluster version matches")
		assert.Len(t, td.trackingClient.Actions, 4, "Should perform four actions")
	})

	t.Run("No update required", func(t *testing.T) {
		td := beforeEach()
		td.vmcluster1.Spec.ClusterVersion = "v1.1.0"
		td.vmcluster2.Spec.ClusterVersion = "v1.1.0"
		err := td.trackingClient.Update(ctx, td.vmcluster1)
		assert.NoError(t, err)
		err = td.trackingClient.Update(ctx, td.vmcluster2)
		assert.NoError(t, err)
		td.trackingClient.Actions = []action{}

		err = CreateOrUpdate(ctx, td.cr, &td.trackingClient)
		assert.NoError(t, err, "CreateOrUpdate should succeed when no update required")
		assert.Len(t, td.trackingClient.Actions, 3, "Should perform three actions")
	})
}

func TestFetchVMClusters(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	vmcluster := newVMCluster("cluster-1", "default", "v1.0.0", 1)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(vmcluster).
		Build()
	clusters, err := fetchVMClusters(ctx, fakeClient, vmcluster.Namespace, []corev1.LocalObjectReference{
		{
			Name: vmcluster.Name,
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(clusters))
	assert.Equal(t, "cluster-1", clusters[0].Name)
}
func TestFetchVMAuth(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	vmauth := newVMAuth("test-vmauth", "default")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(vmauth).
		Build()

	auth, err := fetchVMAuth(ctx, fakeClient, vmauth.Namespace, corev1.LocalObjectReference{Name: vmauth.Name})
	assert.NoError(t, err)
	assert.Equal(t, "test-vmauth", auth.Name)
}

func TestChangeVMClusterVersion(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	vmcluster := newVMCluster("cluster-1", "default", "v1.0.0", 1)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(vmcluster).
		Build()

	err := changeVMClusterVersion(ctx, fakeClient, vmcluster, "v1.1.0")
	assert.NoError(t, err)
	assert.Equal(t, "v1.1.0", vmcluster.Spec.ClusterVersion)
}
