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
	"github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

const (
	vmclusterWaitReadyDeadline = 500 * time.Millisecond
)

type action struct {
	Method    string
	ObjectKey client.ObjectKey
	Object    client.Object
}

type trackingClient struct {
	client.Client
	Actions []action
}

func (tc *trackingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	tc.Actions = append(tc.Actions, action{Method: "Get", ObjectKey: key, Object: obj.DeepCopyObject().(client.Object)})
	return tc.Client.Get(ctx, key, obj, opts...)
}

func (tc *trackingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	tc.Actions = append(tc.Actions, action{Method: "Update", Object: obj.DeepCopyObject().(client.Object)})
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
func newVMCluster(name, namespace, version string, generation int64, status *vmv1beta1.UpdateStatus) *vmv1beta1.VMCluster {
	vmCluster := &vmv1beta1.VMCluster{
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
	if status != nil {
		vmCluster.Status = vmv1beta1.VMClusterStatus{
			StatusMetadata: vmv1beta1.StatusMetadata{
				UpdateStatus: *status,
			},
		}
	}
	return vmCluster
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
				Kind:      "VMCluster/vmselect",
				Name:      "cluster-1",
				Namespace: "default",
			},
			TargetPathSuffix: "/select/1",
		},
		{
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster/vmselect",
				Name:      "cluster-2",
				Namespace: "default",
			},
			TargetPathSuffix: "/select/1",
		},
	})

	vmcluster1Name := "cluster-1"
	vmcluster1 := newVMCluster(vmcluster1Name, "default", "v1.0.0", 1, nil)

	vmcluster2Name := "cluster-2"
	vmcluster2 := newVMCluster(vmcluster2Name, "default", "v1.0.0", 1, nil)

	cr := newVMDistributedCluster("distributed-1", "default", "v1.1.0", vmuserName, []string{vmcluster1Name, vmcluster2Name}, []vmv1alpha1.VMClusterStatus{
		{VMClusterName: "cluster-1", Generation: 1, TargetRef: vmv1beta1.TargetRef{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-1", Namespace: "default"}, TargetPathSuffix: "/select/1"}},
		{VMClusterName: "cluster-2", Generation: 1, TargetRef: vmv1beta1.TargetRef{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-2", Namespace: "default"}, TargetPathSuffix: "/select/1"}},
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

// Helper client that always returns an error on Update calls
type alwaysFailingUpdateClient struct {
	client.Client
}

func (f *alwaysFailingUpdateClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return fmt.Errorf("simulated update error")
}

func compareExpectedActions(t *testing.T, expectedActions []action, actualActions []action) {
	for i, action := range actualActions {
		expectedObj := expectedActions[i].Object
		assert.Equal(t, expectedActions[i].Method, action.Method, fmt.Sprintf("Action %d method mismatch", i))
		assert.IsType(t, expectedObj, action.Object, fmt.Sprintf("Action %d object type mismatch", i))

		if action.Method == "Get" {
			assert.Equal(t, expectedActions[i].ObjectKey, action.ObjectKey, fmt.Sprintf("Action %d object key mismatch", i))
		} else if action.Method == "Update" {
			// Custom validation logic for VMUser update
			if vmUser, ok := action.Object.(*vmv1beta1.VMUser); ok {
				assert.Equal(t, expectedObj.(*vmv1beta1.VMUser).Spec.TargetRefs, vmUser.Spec.TargetRefs, fmt.Sprintf("Action %d object target refs mismatch", i))
			} else if vmCluster, ok := action.Object.(*vmv1beta1.VMCluster); ok {
				assert.Equal(t, expectedObj.(*vmv1beta1.VMCluster).Spec, vmCluster.Spec, fmt.Sprintf("Action %d object Specs mismatch", i))
				assert.Equal(t, expectedObj.(*vmv1beta1.VMCluster).Status, vmCluster.Status, fmt.Sprintf("Action %d object Status mismatch", i))
			} else {
				assert.Equal(t, expectedObj, action.Object, fmt.Sprintf("Action %d object mismatch", i))
			}

		}
	}
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
						Kind:      "VMCluster/vmselect",
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
						Kind:      "VMCluster/vminsert",
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
						Kind:      "VMCluster/vmselect",
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
						Kind:      "VMCluster/vmselect",
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
						Kind:      "VMCluster/vmselect",
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

func TestCreateOrUpdate_DistributedCluster(t *testing.T) {
	ctx := context.Background()

	t.Run("Successful reconciliation", func(t *testing.T) {
		td := beforeEach()
		// Patch: use alwaysReadyClient for VMCluster readiness
		td.trackingClient.Client = &alwaysReadyClient{Client: td.trackingClient.Client}
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline)
		assert.NoError(t, err, "CreateOrUpdate should succeed when all resources are present")
		assert.Len(t, td.trackingClient.Actions, 17, "Should perform 17 actions")

		// Verify actions in order
		expectedActions := []action{
			// 0. Fetch vmuser
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			// 1. fetch vmclusters
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, Object: &vmv1beta1.VMCluster{}},
			// 3. fetch vmuser before updating targetrefs
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			// 4. Update vmuser to remove cluster-1 from targetrefs
			{Method: "Update", Object: newVMUser(td.vmuser.Name, td.vmuser.Namespace, []vmv1beta1.TargetRef{{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-2", Namespace: "default"}, TargetPathSuffix: "/select/1"}})},
			// 5. Fetch fresh copy of vmcluster1
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
			// 6. Update vmcluster1 to new version, set it operational immediately
			{Method: "Update", Object: newVMCluster(td.vmcluster1.Name, td.vmcluster1.Namespace, td.cr.Spec.ClusterVersion, td.vmcluster1.Generation, ptr.To(v1beta1.UpdateStatusOperational))},
			// 7. Fetching vmcluster1 status again to check if it is operational
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
			// 8. Fetch fresh vmuser
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			// 9. Switch vmcluster1 back on
			{Method: "Update", Object: newVMUser(td.vmuser.Name, td.vmuser.Namespace, []vmv1beta1.TargetRef{{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-2", Namespace: "default"}, TargetPathSuffix: "/select/1"}, {CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-1", Namespace: "default"}, TargetPathSuffix: "/select/1"}})},
			// 10. Get fresh vmuser
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			// 11. Remove cluster2 from vmuser before update
			{Method: "Update", Object: newVMUser(td.vmuser.Name, td.vmuser.Namespace, []vmv1beta1.TargetRef{{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-1", Namespace: "default"}, TargetPathSuffix: "/select/1"}})},
			// 12. Get fresh vmcluster2
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, Object: &vmv1beta1.VMCluster{}},
			// 13. Update vmcluster2 version (and set status to operational)
			{Method: "Update", Object: newVMCluster(td.vmcluster2.Name, td.vmcluster2.Namespace, td.cr.Spec.ClusterVersion, td.vmcluster2.Generation, ptr.To(v1beta1.UpdateStatusOperational))},
			// 14. Get fresh vmcluster2 to check status
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, Object: &vmv1beta1.VMCluster{}},
			// 15. Get fresh vmuser
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			// 16. Turn back vmcluster2
			{Method: "Update", Object: newVMUser(td.vmuser.Name, td.vmuser.Namespace, []vmv1beta1.TargetRef{{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-1", Namespace: "default"}, TargetPathSuffix: "/select/1"}, {CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-2", Namespace: "default"}, TargetPathSuffix: "/select/1"}})}, // setVMClusterStatusInVMUser (enable cluster2) - Update VMUser
		}
		compareExpectedActions(t, expectedActions, td.trackingClient.Actions)

		// Verify that VMCluster1 is updated correctly
		updatedVMCluster1 := &vmv1beta1.VMCluster{}
		err = td.trackingClient.Get(ctx, types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, updatedVMCluster1)
		assert.NoError(t, err)
		assert.Equal(t, td.cr.Spec.ClusterVersion, updatedVMCluster1.Spec.ClusterVersion, "VMCluster1 version should be updated")

		// Verify that VMCluster2 is updated correctly
		updatedVMCluster2 := &vmv1beta1.VMCluster{}
		err = td.trackingClient.Get(ctx, types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, updatedVMCluster2)
		assert.NoError(t, err)
		assert.Equal(t, td.cr.Spec.ClusterVersion, updatedVMCluster2.Spec.ClusterVersion, "VMCluster2 version should be updated")

		// Check that VMUser has targetrefs pointing to both VMClusters
		updatedVMUser := &vmv1beta1.VMUser{}
		err = td.trackingClient.Get(ctx, types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, updatedVMUser)
		assert.NoError(t, err)
		expectedTargetRefs := make([]vmv1beta1.TargetRef, len(td.cr.Status.VMClusterInfo))
		for i, info := range td.cr.Status.VMClusterInfo {
			expectedTargetRefs[i] = info.TargetRef
		}
		assert.ElementsMatch(t, expectedTargetRefs, updatedVMUser.Spec.TargetRefs, "VMUser TargetRefs should match VMDistributedCluster status")
	})

	t.Run("VMCluster missing", func(t *testing.T) {
		td := beforeEach()

		td.cr.Spec.VMClusters = []corev1.LocalObjectReference{
			{Name: "missing-cluster"},
		}
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline)
		assert.Error(t, err, "CreateOrUpdate should error if VMCluster is missing")
		assert.Len(t, td.trackingClient.Actions, 2, "Should perform two actions")
	})

	t.Run("VMUser not specified", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.VMUser.Name = "" // Simulate missing VMUser reference
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline)
		assert.Error(t, err, "CreateOrUpdate should error if VMUser is not specified")
		assert.Contains(t, err.Error(), "global loadbalancing vmuser is not specified")
		assert.Len(t, td.trackingClient.Actions, 0, "Should perform no actions")
	})

	t.Run("VMUser not found", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.VMUser.Name = "non-existent-vmuser" // Simulate non-existent VMUser
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline)
		assert.Error(t, err, "CreateOrUpdate should error if VMUser is not found")
		assert.Contains(t, err.Error(), "failed to get VMUser default/non-existent-vmuser")
		assert.Len(t, td.trackingClient.Actions, 1, "Should perform one action (Get VMUser)")
	})

	t.Run("VMClusters fetch error", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.VMClusters = []corev1.LocalObjectReference{
			{Name: "vmcluster-1"},
			{Name: "non-existent-cluster"}, // Simulate one missing VMCluster
		}
		// Since alwaysReadyClient is used, we need to ensure the fake client itself returns not found for "non-existent-cluster"
		// The beforeEach already sets up the client with existing clusters, so removing the non-existent one from there
		// or using a new fake client without the non-existent cluster would achieve this.
		// For simplicity, let's just make sure "non-existent-cluster" is not in the initial fake client objects.
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline)
		assert.Error(t, err, "CreateOrUpdate should error if VMCluster fetch fails")
		assert.Contains(t, err.Error(), "failed to get VMCluster default/vmcluster-1")
		assert.Len(t, td.trackingClient.Actions, 2, "Should perform two actions")
	})

	t.Run("findVMUserReadRuleForVMCluster error", func(t *testing.T) {
		td := beforeEach()
		// Modify vmuser so findVMUserReadRuleForVMCluster returns an error
		td.vmuser.Spec.TargetRefs = []vmv1beta1.TargetRef{
			{
				CRD: &vmv1beta1.CRDRef{
					Kind:      "OtherKind", // This will cause a mismatch in findVMUserReadRuleForVMCluster
					Name:      td.vmcluster1.Name,
					Namespace: td.vmcluster1.Namespace,
				},
				TargetPathSuffix: "/select/1",
			},
		}
		err := td.trackingClient.Update(ctx, td.vmuser)
		assert.NoError(t, err)
		td.trackingClient.Actions = []action{} // Clear actions after setup update

		err = CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline)
		assert.Error(t, err, "CreateOrUpdate should error if findVMUserReadRuleForVMCluster fails")
		assert.Contains(t, err.Error(), "failed to find the rule for vmcluster cluster-1")
		assert.Len(t, td.trackingClient.Actions, 3, "Should perform three actions")

		expectedActions := []action{
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: "cluster-1", Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: "cluster-2", Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
		}
		compareExpectedActions(t, expectedActions, td.trackingClient.Actions)
	})

	t.Run("Generations change triggers status update", func(t *testing.T) {
		td := beforeEach()
		td.cr.Status.VMClusterInfo[0].Generation = 2 // update generation
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline)
		assert.Error(t, err, "CreateOrUpdate should error if generations change detected")
		assert.Len(t, td.trackingClient.Actions, 3, "Should perform three actions")

		expectedActions := []action{
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
		}
		compareExpectedActions(t, expectedActions, td.trackingClient.Actions)
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

		expectedActions := []action{
			// 0. Fetch vmuser
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			// 1. fetch vmclusters
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, Object: &vmv1beta1.VMCluster{}},
			// 3. fetch vmuser before updating targetrefs
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			// 4. Remove cluster2 from vmuser before update
			{Method: "Update", Object: newVMUser(td.vmuser.Name, td.vmuser.Namespace, []vmv1beta1.TargetRef{{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-1", Namespace: "default"}, TargetPathSuffix: "/select/1"}})},
			// 5. Get fresh vmcluster2
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, Object: &vmv1beta1.VMCluster{}},
			// 6. Update vmcluster2 version (and set status to operational)
			{Method: "Update", Object: newVMCluster(td.vmcluster2.Name, td.vmcluster2.Namespace, td.cr.Spec.ClusterVersion, td.vmcluster2.Generation, ptr.To(v1beta1.UpdateStatusOperational))},
			// 7. Get fresh vmcluster2 to check status
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, Object: &vmv1beta1.VMCluster{}},
			// 8. Get fresh vmuser
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			// 9. Turn back vmcluster2
			{Method: "Update", Object: newVMUser(td.vmuser.Name, td.vmuser.Namespace, []vmv1beta1.TargetRef{{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-1", Namespace: "default"}, TargetPathSuffix: "/select/1"}, {CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-2", Namespace: "default"}, TargetPathSuffix: "/select/1"}})}, // setVMClusterStatusInVMUser (enable cluster2) - Update VMUser
		}
		compareExpectedActions(t, expectedActions, td.trackingClient.Actions)
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

		expectedActions := []action{
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
		}
		compareExpectedActions(t, expectedActions, td.trackingClient.Actions)
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

func TestChangeVMClusterVersion(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	vmcluster := newVMCluster("cluster-1", "default", "v1.0.0", 1, nil)
	baseFake := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(vmcluster).
		Build()
	fakeClient := trackingClient{baseFake, []action{}}

	err := changeVMClusterVersion(ctx, &fakeClient, vmcluster, "v1.1.0")
	assert.NoError(t, err)
	assert.Equal(t, "v1.1.0", vmcluster.Spec.ClusterVersion)
}

func TestWaitForVMClusterReadyWithDelay(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	namespace := "default"
	name := "vmcluster-delayed-ready"

	// Initial state: VMCluster is expanding
	vmcluster := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: vmv1beta1.VMClusterStatus{
			StatusMetadata: vmv1beta1.StatusMetadata{
				UpdateStatus: vmv1beta1.UpdateStatusExpanding,
			},
		},
	}

	t.Run("returns nil when cluster becomes ready after delay", func(t *testing.T) {
		baseFake := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(vmcluster).
			WithStatusSubresource(&vmv1beta1.VMCluster{}).
			Build()
		fakeClient := trackingClient{baseFake, []action{}}

		// Goroutine to update status to Operational after a delay
		go func() {
			time.Sleep(3 * time.Second) // Simulate delay
			var obj vmv1beta1.VMCluster
			if err := fakeClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &obj); err != nil {
				t.Errorf("Failed to get VMCluster in goroutine: %v", err)
				return
			}
			vmcluster.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
			if err := fakeClient.Status().Update(ctx, vmcluster); err != nil {
				t.Errorf("Failed to update VMCluster status in goroutine: %v", err)
			}
		}()

		// Set a deadline longer than the simulated delay
		err := waitForVMClusterReady(ctx, &fakeClient, vmcluster, 5*time.Second)
		assert.NoError(t, err)

		// Verify the cluster is indeed operational
		var updatedVMCluster vmv1beta1.VMCluster
		err = fakeClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &updatedVMCluster)
		assert.NoError(t, err)
		assert.Equal(t, vmv1beta1.UpdateStatusOperational, updatedVMCluster.Status.UpdateStatus)
	})
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
			Kind:      "VMCluster/vmselect",
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

	vmClusterToManage := newVMCluster(clusterName, namespace, "v1.0.0", 1, nil)

	t.Run("Enable cluster - add target ref", func(t *testing.T) {
		initialVMUser := newVMUser(vmuserName, namespace, []vmv1beta1.TargetRef{})
		fakeClient := trackingClient{fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithRule, initialVMUser, vmClusterToManage).
			Build(), []action{}}

		err := setVMClusterStatusInVMUser(ctx, &fakeClient, crWithRule, vmClusterToManage, initialVMUser, true)
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
		fakeClient := trackingClient{fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithRule, initialVMUser, vmClusterToManage).
			Build(), []action{}}

		err := setVMClusterStatusInVMUser(ctx, &fakeClient, crWithRule, vmClusterToManage, initialVMUser, false)
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
		fakeClient := trackingClient{fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithRule, initialVMUser, vmClusterToManage).
			Build(), []action{}}

		err := setVMClusterStatusInVMUser(ctx, &fakeClient, crWithoutRule, vmClusterToManage, initialVMUser, true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no matching rule found for vmcluster")
	})

	t.Run("Failure to fetch VMUser", func(t *testing.T) {
		initialVMUser := newVMUser("non-existent-user", namespace, []vmv1beta1.TargetRef{}) // Use a non-existent user
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithRule, vmClusterToManage).
			Build()

		err := setVMClusterStatusInVMUser(ctx, fakeClient, crWithRule, vmClusterToManage, initialVMUser, true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch vmuser")
	})

	t.Run("Failure to update VMUser", func(t *testing.T) {
		initialVMUser := newVMUser(vmuserName, namespace, []vmv1beta1.TargetRef{})
		// Use a client that will fail on update
		failingClient := &alwaysFailingUpdateClient{Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithRule, initialVMUser, vmClusterToManage).
			Build()}

		err := setVMClusterStatusInVMUser(ctx, failingClient, crWithRule, vmClusterToManage, initialVMUser, true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "simulated update error")
	})
}
