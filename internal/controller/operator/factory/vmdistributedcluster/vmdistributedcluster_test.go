package vmdistributedcluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

const (
	vmclusterWaitReadyDeadline = 500 * time.Millisecond
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

func TestFindVMUserReadRuleForVMCluster(t *testing.T) {
	clusterName := "vmcluster-1"
	clusterNamespace := "test-ns"
	vmCluster := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: clusterNamespace,
		},
	}

	// Case 1: Matching TargetRef with correct Kind, Name, Namespace, and TargetPathSuffix
	vmUser := &vmv1beta1.VMUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: "user1",
		},
		Spec: vmv1beta1.VMUserSpec{
			TargetRefs: []vmv1beta1.TargetRef{
				{
					CRD: &vmv1beta1.CRDRef{
						Kind:      "VMCluster",
						Name:      clusterName,
						Namespace: clusterNamespace,
					},
					TargetPathSuffix: "/select/api",
				},
			},
		},
	}
	ref, err := findVMUserReadRuleForVMCluster(vmUser, vmCluster)
	assert.NoError(t, err)
	assert.NotNil(t, ref)
	assert.Equal(t, "/select/api", ref.TargetPathSuffix)

	// Case 2: No matching TargetRef (wrong Kind)
	vmUserWrongKind := &vmv1beta1.VMUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: "user2",
		},
		Spec: vmv1beta1.VMUserSpec{
			TargetRefs: []vmv1beta1.TargetRef{
				{
					CRD: &vmv1beta1.CRDRef{
						Kind:      "OtherKind",
						Name:      clusterName,
						Namespace: clusterNamespace,
					},
					TargetPathSuffix: "/select/api",
				},
			},
		},
	}
	ref, err = findVMUserReadRuleForVMCluster(vmUserWrongKind, vmCluster)
	assert.Error(t, err)
	assert.Nil(t, ref)

	// Case 3: No matching TargetRef (wrong Name)
	vmUserWrongName := &vmv1beta1.VMUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: "user3",
		},
		Spec: vmv1beta1.VMUserSpec{
			TargetRefs: []vmv1beta1.TargetRef{
				{
					CRD: &vmv1beta1.CRDRef{
						Kind:      "VMCluster",
						Name:      "other-cluster",
						Namespace: clusterNamespace,
					},
					TargetPathSuffix: "/select/api",
				},
			},
		},
	}
	ref, err = findVMUserReadRuleForVMCluster(vmUserWrongName, vmCluster)
	assert.Error(t, err)
	assert.Nil(t, ref)

	// Case 4: No matching TargetRef (wrong Namespace)
	vmUserWrongNS := &vmv1beta1.VMUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: "user4",
		},
		Spec: vmv1beta1.VMUserSpec{
			TargetRefs: []vmv1beta1.TargetRef{
				{
					CRD: &vmv1beta1.CRDRef{
						Kind:      "VMCluster",
						Name:      clusterName,
						Namespace: "other-ns",
					},
					TargetPathSuffix: "/select/api",
				},
			},
		},
	}
	ref, err = findVMUserReadRuleForVMCluster(vmUserWrongNS, vmCluster)
	assert.Error(t, err)
	assert.Nil(t, ref)

	// Case 5: No matching TargetRef (wrong TargetPathSuffix)
	vmUserWrongSuffix := &vmv1beta1.VMUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: "user5",
		},
		Spec: vmv1beta1.VMUserSpec{
			TargetRefs: []vmv1beta1.TargetRef{
				{
					CRD: &vmv1beta1.CRDRef{
						Kind:      "VMCluster",
						Name:      clusterName,
						Namespace: clusterNamespace,
					},
					TargetPathSuffix: "/insert/api",
				},
			},
		},
	}
	ref, err = findVMUserReadRuleForVMCluster(vmUserWrongSuffix, vmCluster)
	assert.Error(t, err)
	assert.Nil(t, ref)
}

func (tc *trackingClient) Status() client.StatusWriter {
	return tc.Client.Status()
}

// Helper to create a basic VMUser object
// Helper to create a basic VMUser object
func newVMUser(name, namespace string, targetRefs []vmv1beta1.TargetRef) *vmv1beta1.VMUser {
	return &vmv1beta1.VMUser{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vmv1beta1.VMUserSpec{
			TargetRefs: targetRefs,
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
			VMSelect: &vmv1beta1.VMSelect{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
	}
}

type testData struct {
	vmuser         *vmv1beta1.VMUser
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

	vmuserName := "test-vmuser"
	vmuser := newVMUser(vmuserName, "default", []vmv1beta1.TargetRef{
		{
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster",
				Name:      "cluster-1",
				Namespace: "default",
			},
			TargetPathSuffix: "/select/1",
		},
		{
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster",
				Name:      "cluster-2",
				Namespace: "default",
			},
			TargetPathSuffix: "/select/1",
		},
	})

	vmcluster1Name := "cluster-1"
	vmcluster1 := newVMCluster(vmcluster1Name, "default", "v1.0.0", 1)

	vmcluster2Name := "cluster-2"
	vmcluster2 := newVMCluster(vmcluster2Name, "default", "v1.0.0", 1)

	cr := newVMDistributedCluster("distributed-1", "default", "v1.1.0", vmuserName, []string{vmcluster1Name, vmcluster2Name}, []vmv1alpha1.VMClusterStatus{
		{VMClusterName: "cluster-1", Generation: 1, TargetRef: vmv1beta1.TargetRef{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster", Name: "cluster-1", Namespace: "default"}, TargetPathSuffix: "/select/1"}},
		{VMClusterName: "cluster-2", Generation: 1, TargetRef: vmv1beta1.TargetRef{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster", Name: "cluster-2", Namespace: "default"}, TargetPathSuffix: "/select/1"}},
	})

	baseFake := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(vmuser, vmcluster1, vmcluster2, cr). // Add cr to objects for fetching later
		Build()

	return testData{
		vmuser:         vmuser,
		vmcluster1:     vmcluster1,
		vmcluster2:     vmcluster2,
		cr:             cr,
		trackingClient: trackingClient{baseFake, []action{}},
	}
}

// Helper to create a basic VMDistributedCluster object
func newVMDistributedCluster(name, namespace, clusterVersion, vmuserName string, vmclusterNames []string, vmClusterInfo []vmv1alpha1.VMClusterStatus) *vmv1alpha1.VMDistributedCluster {
	vmClustersRefs := make([]corev1.LocalObjectReference, len(vmclusterNames))
	for i, name := range vmclusterNames {
		vmClustersRefs[i] = corev1.LocalObjectReference{Name: name}
	}
	return &vmv1alpha1.VMDistributedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vmv1alpha1.VMDistributedClusterSpec{
			ClusterVersion: clusterVersion,
			VMUser:         corev1.LocalObjectReference{Name: vmuserName},
			VMClusters:     vmClustersRefs,
		},
		Status: vmv1alpha1.VMDistributedClusterStatus{
			VMClusterInfo: vmClusterInfo,
		},
	}
}

type alwaysReadyClient struct {
	client.Client
}

func (c *alwaysReadyClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	err := c.Client.Get(ctx, key, obj, opts...)
	if err != nil {
		return err
	}
	// Patch status to always be ready
	if cluster, ok := obj.(*vmv1beta1.VMCluster); ok {
		cluster.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
	}
	return nil
}

func TestCreateOrUpdate_DistributedCluster(t *testing.T) {
	ctx := context.Background()

	t.Run("Successful reconciliation", func(t *testing.T) {
		td := beforeEach()
		// Patch: use alwaysReadyClient for VMCluster readiness
		td.trackingClient.Client = &alwaysReadyClient{Client: td.trackingClient.Client}
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline)
		assert.NoError(t, err, "CreateOrUpdate should succeed when all resources are present")
		assert.Len(t, td.trackingClient.Actions, 17, "Should perform seventeen actions")
	})

	// t.Run("VMAuth is unmanaged", func(t *testing.T) {
	// 	td := beforeEach()

	// 	// Make VMAuth unmanaged by setting SelectAllByDefault to false
	// 	td.vmuser.Spec.SelectAllByDefault = false
	// 	err := td.trackingClient.Update(ctx, td.vmuser)
	// 	assert.NoError(t, err)
	// 	td.trackingClient.Actions = []action{}

	// 	err = CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline)
	// 	assert.Error(t, err, "CreateOrUpdate should error if VMAuth is unmanaged")
	// 	assert.Len(t, td.trackingClient.Actions, 1, "Should perform one action")
	// })

	t.Run("VMCluster missing", func(t *testing.T) {
		td := beforeEach()

		td.cr.Spec.VMClusters = []corev1.LocalObjectReference{
			{Name: "missing-cluster"},
		}
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline)
		assert.Error(t, err, "CreateOrUpdate should error if VMCluster is missing")
		assert.Len(t, td.trackingClient.Actions, 2, "Should perform two actions")
	})

	t.Run("Generations change triggers status update", func(t *testing.T) {
		td := beforeEach()
		td.cr.Status.VMClusterInfo[0].Generation = 2 // update generation
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline)
		assert.Error(t, err, "CreateOrUpdate should error if generations change detected")
		assert.Len(t, td.trackingClient.Actions, 3, "Should perform three actions")
	})

	t.Run("Partial cluster version update", func(t *testing.T) {
		td := beforeEach()
		td.vmcluster1.Spec.ClusterVersion = "v1.1.0"
		err := td.trackingClient.Update(ctx, td.vmcluster1)
		assert.NoError(t, err)
		td.trackingClient.Actions = []action{}

		// Patch: use alwaysReadyClient for VMCluster readiness
		td.trackingClient.Client = &alwaysReadyClient{Client: td.trackingClient.Client}
		err = CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline)
		assert.NoError(t, err, "CreateOrUpdate should succeed when cluster version matches")
		assert.Len(t, td.trackingClient.Actions, 10, "Should perform ten actions")
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

		err = CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline)
		assert.NoError(t, err, "CreateOrUpdate should succeed when no update required")
		assert.Len(t, td.trackingClient.Actions, 3, "Should perform three actions")
	})
}

func TestWaitForVMClusterReady(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	namespace := "default"
	name := "vmcluster-ready"

	// Ready cluster
	readyCluster := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: vmv1beta1.VMClusterStatus{
			StatusMetadata: vmv1beta1.StatusMetadata{
				UpdateStatus: vmv1beta1.UpdateStatusOperational,
			},
		},
	}

	// Not ready cluster
	notReadyCluster := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "vmcluster-not-ready",
		},
		Status: vmv1beta1.VMClusterStatus{
			StatusMetadata: vmv1beta1.StatusMetadata{
				UpdateStatus: vmv1beta1.UpdateStatusExpanding,
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(readyCluster, notReadyCluster).
		Build()

	t.Run("returns nil when cluster is ready", func(t *testing.T) {
		arc := &alwaysReadyClient{Client: client}
		err := waitForVMClusterReady(ctx, arc, readyCluster, 500*time.Millisecond)
		assert.NoError(t, err)
	})

	t.Run("returns error when cluster is not ready", func(t *testing.T) {
		err := waitForVMClusterReady(ctx, client, notReadyCluster, vmclusterWaitReadyDeadline)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to wait for VMCluster")
	})

	t.Run("returns error when cluster is missing", func(t *testing.T) {
		missing := &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "missing-cluster",
			},
		}
		err := waitForVMClusterReady(ctx, client, missing, vmclusterWaitReadyDeadline)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMCluster not found")
	})
}

func TestVMDistributedClusterDelete(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)

	namespace := "default"
	name := "delete-unit-test"
	cr := &vmv1alpha1.VMDistributedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: vmv1alpha1.VMDistributedClusterSpec{
			VMClusters: []corev1.LocalObjectReference{},
			VMUser:     corev1.LocalObjectReference{Name: "fake"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cr).
		Build()

	// Delete the resource
	err := client.Delete(ctx, cr)
	assert.NoError(t, err, "Delete should succeed")

	// Try to get the resource
	var deleted vmv1alpha1.VMDistributedCluster
	err = client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &deleted)
	assert.Error(t, err, "Get should return error after delete")
	assert.True(t, k8serrors.IsNotFound(err), "Error should be IsNotFound")
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
func TestFetchVMUser(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	vmuser := newVMUser("test-vmuser", "default", []vmv1beta1.TargetRef{})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(vmuser).
		Build()

	auth, err := fetchVMUser(ctx, fakeClient, vmuser.Namespace, corev1.LocalObjectReference{Name: vmuser.Name})
	assert.NoError(t, err)
	assert.Equal(t, "test-vmuser", auth.Name)
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

func TestSetVMClusterStatusInVMUser(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)

	vmuserName := "test-vmuser"
	clusterName := "cluster-to-manage"
	namespace := "default"

	// Define the target ref that will be added/removed
	targetRef := vmv1beta1.TargetRef{
		CRD: &vmv1beta1.CRDRef{
			Kind:      "VMCluster",
			Name:      clusterName,
			Namespace: namespace,
		},
		TargetPathSuffix: "/select/test",
	}

	// VMDistributedCluster with the targetRef in its status
	crWithRule := newVMDistributedCluster("dist-cluster-1", namespace, "v1.0.0", vmuserName, []string{clusterName}, []vmv1alpha1.VMClusterStatus{
		{
			VMClusterName: clusterName,
			Generation:    1,
			TargetRef:     targetRef,
		},
	})

	vmClusterToManage := newVMCluster(clusterName, namespace, "v1.0.0", 1)

	t.Run("Enable cluster - add target ref", func(t *testing.T) {
		initialVMUser := newVMUser(vmuserName, namespace, []vmv1beta1.TargetRef{})
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithRule, initialVMUser, vmClusterToManage).
			Build()

		err := setVMClusterStatusInVMUser(ctx, fakeClient, crWithRule, vmClusterToManage, initialVMUser, true)
		assert.NoError(t, err)

		// Verify VMUser's TargetRefs are updated
		updatedVMUser := &vmv1beta1.VMUser{}
		err = fakeClient.Get(ctx, types.NamespacedName{Name: vmuserName, Namespace: namespace}, updatedVMUser)
		assert.NoError(t, err)
		assert.Len(t, updatedVMUser.Spec.TargetRefs, 1)
		assert.Equal(t, targetRef, updatedVMUser.Spec.TargetRefs[0])
	})

	t.Run("Disable cluster - remove target ref", func(t *testing.T) {
		initialVMUser := newVMUser(vmuserName, namespace, []vmv1beta1.TargetRef{targetRef})
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithRule, initialVMUser, vmClusterToManage).
			Build()

		err := setVMClusterStatusInVMUser(ctx, fakeClient, crWithRule, vmClusterToManage, initialVMUser, false)
		assert.NoError(t, err)

		// Verify VMUser's TargetRefs are updated
		updatedVMUser := &vmv1beta1.VMUser{}
		err = fakeClient.Get(ctx, types.NamespacedName{Name: vmuserName, Namespace: namespace}, updatedVMUser)
		assert.NoError(t, err)
		assert.Len(t, updatedVMUser.Spec.TargetRefs, 0)
	})

	t.Run("No matching rule in VMDistributedCluster status", func(t *testing.T) {
		crWithoutRule := newVMDistributedCluster("dist-cluster-no-rule", namespace, "v1.0.0", vmuserName, []string{}, []vmv1alpha1.VMClusterStatus{})
		initialVMUser := newVMUser(vmuserName, namespace, []vmv1beta1.TargetRef{})
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithoutRule, initialVMUser, vmClusterToManage).
			Build()

		err := setVMClusterStatusInVMUser(ctx, fakeClient, crWithoutRule, vmClusterToManage, initialVMUser, true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no matching rule found for vmcluster")
	})

	t.Run("Failure to fetch VMUser", func(t *testing.T) {
		initialVMUser := newVMUser("non-existent-user", namespace, []vmv1beta1.TargetRef{}) // Use a non-existent user
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithRule, vmClusterToManage). // Don't add initialVMUser
			Build()

		err := setVMClusterStatusInVMUser(ctx, fakeClient, crWithRule, vmClusterToManage, initialVMUser, true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch vmuser")
	})

	t.Run("Failure to update VMUser", func(t *testing.T) {
		initialVMUser := newVMUser(vmuserName, namespace, []vmv1beta1.TargetRef{})
		// Use a client that will fail on update
		failingClient := &failingUpdateClient{Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithRule, initialVMUser, vmClusterToManage).
			Build()}

		err := setVMClusterStatusInVMUser(ctx, failingClient, crWithRule, vmClusterToManage, initialVMUser, true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "simulated update error")
	})
}

// Helper client that always returns an error on Update calls
type failingUpdateClient struct {
	client.Client
}

func (f *failingUpdateClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return fmt.Errorf("simulated update error")
}
