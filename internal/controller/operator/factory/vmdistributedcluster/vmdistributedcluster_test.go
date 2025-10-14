package vmdistributedcluster

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
	vmauth     *vmv1beta1.VMAuth
	vmcluster1 *vmv1beta1.VMCluster
	vmcluster2 *vmv1beta1.VMCluster
	cr         *vmv1alpha1.VMDistributedCluster
	fakeClient client.Client
}

func beforeEach() testData {
	// Initialize test environment
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)

	vmauth := newVMAuth("test-vmauth", "default")
	vmcluster1 := newVMCluster("cluster-1", "default", "v1.0.0", 1)
	vmcluster2 := newVMCluster("cluster-2", "default", "v1.0.0", 1)
	cr := &vmv1alpha1.VMDistributedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "distributed-1",
			Namespace: "default",
		},
		Spec: vmv1alpha1.VMDistributedClusterSpec{
			ClusterVersion: "v1.1.0",
			VMAuth:         vmauth,
			VMClusters: []vmv1beta1.VMCluster{
				*vmcluster1,
				*vmcluster2,
			},
		},
		Status: vmv1alpha1.VMDistributedClusterStatus{
			VMClusterGenerations: []vmv1alpha1.VMClusterGenerationPair{
				{VMClusterName: "cluster-1", Generation: 1},
				{VMClusterName: "cluster-2", Generation: 1},
			},
		},
	}

	return testData{
		vmauth:     vmauth,
		vmcluster1: vmcluster1,
		vmcluster2: vmcluster2,
		cr:         cr,
		fakeClient: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(vmauth, vmcluster1, vmcluster2).
			Build(),
	}

}

func TestCreateOrUpdate_DistributedCluster(t *testing.T) {
	ctx := context.Background()

	t.Run("Successful reconciliation", func(t *testing.T) {
		td := beforeEach()
		err := CreateOrUpdate(ctx, td.cr, td.fakeClient)
		assert.NoError(t, err, "CreateOrUpdate should succeed when all resources are present")
	})

	t.Run("VMAuth is unmanaged", func(t *testing.T) {
		td := beforeEach()

		// Make VMAuth unmanaged by setting SelectAllByDefault to false
		td.vmauth.Spec.SelectAllByDefault = false
		err := td.fakeClient.Update(ctx, td.vmauth)
		assert.NoError(t, err)
		err = CreateOrUpdate(ctx, td.cr, td.fakeClient)
		assert.Error(t, err, "CreateOrUpdate should error if VMAuth is unmanaged")
	})

	t.Run("VMCluster missing", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.VMClusters = []vmv1beta1.VMCluster{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-cluster",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMClusterSpec{
					ClusterVersion: "v1.0.0",
				},
			},
		}
		err := CreateOrUpdate(ctx, td.cr, td.fakeClient)
		assert.Error(t, err, "CreateOrUpdate should error if VMCluster is missing")
	})

	t.Run("Generations change triggers status update", func(t *testing.T) {
		td := beforeEach()
		td.cr.Status.VMClusterGenerations[0].Generation = 2 // update generation
		err := CreateOrUpdate(ctx, td.cr, td.fakeClient)
		assert.Error(t, err, "CreateOrUpdate should error if generations change detected")
	})

	t.Run("Cluster version update", func(t *testing.T) {
		td := beforeEach()
		td.vmcluster1.Spec.ClusterVersion = "v1.1.0"
		err := td.fakeClient.Update(ctx, td.vmcluster1)
		assert.NoError(t, err)
		err = CreateOrUpdate(ctx, td.cr, td.fakeClient)
		assert.NoError(t, err, "CreateOrUpdate should succeed when cluster version matches")
	})

	t.Run("No update required", func(t *testing.T) {
		td := beforeEach()
		td.vmcluster1.Spec.ClusterVersion = "v1.1.0"
		td.vmcluster2.Spec.ClusterVersion = "v1.1.0"
		err := td.fakeClient.Update(ctx, td.vmcluster1)
		assert.NoError(t, err)
		err = CreateOrUpdate(ctx, td.cr, td.fakeClient)
		assert.NoError(t, err, "CreateOrUpdate should succeed when no update required")
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

	specs := []vmv1beta1.VMCluster{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-1",
				Namespace: "default",
			},
		},
	}
	clusters, err := fetchVMClusters(ctx, fakeClient, specs)
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

	auth, err := fetchVMAuth(ctx, fakeClient, vmauth)
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
