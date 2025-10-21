package vmdistributedcluster

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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
	httpTimeout                = 500 * time.Millisecond
)

// trackingClient records actions performed by the client
// so that their order and objects can be verified in tests
type action struct {
	Method    string
	ObjectKey client.ObjectKey
	Object    client.Object
}

type trackingClient struct {
	client.Client
	Actions []action
	mu      sync.Mutex
}

func (tc *trackingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.Actions = append(tc.Actions, action{Method: "Get", ObjectKey: key, Object: obj.DeepCopyObject().(client.Object)})
	return tc.Client.Get(ctx, key, obj, opts...)
}

func (tc *trackingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.Actions = append(tc.Actions, action{Method: "Update", Object: obj.DeepCopyObject().(client.Object)})
	return tc.Client.Update(ctx, obj, opts...)
}

func (tc *trackingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.Actions = append(tc.Actions, action{Method: "Create", Object: obj.DeepCopyObject().(client.Object)})
	return tc.Client.Create(ctx, obj, opts...)
}

func (tc *trackingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.Actions = append(tc.Actions, action{Method: "Delete", Object: obj.DeepCopyObject().(client.Object)})
	return tc.Client.Delete(ctx, obj, opts...)
}

func (tc *trackingClient) Status() client.StatusWriter {
	return tc.Client.Status()
}

// alwaysReadyClient is a client which always returns Ready update status.
type alwaysReadyClient struct {
	client.Client
}

func (c *alwaysReadyClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	err := c.Client.Get(ctx, key, obj, opts...)
	if err != nil {
		return err
	}
	if cluster, ok := obj.(*vmv1beta1.VMCluster); ok {
		cluster.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
	}
	return nil
}

// alwaysFailingUpdateClient is a client which always returns an error on Update
type alwaysFailingUpdateClient struct {
	client.Client
}

func (f *alwaysFailingUpdateClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return fmt.Errorf("simulated update error")
}

// Custom function to compare expected actions with actual actions
func compareExpectedActions(t *testing.T, expectedActions []action, actualActions []action) {
	for i, action := range actualActions {
		expectedObj := expectedActions[i].Object
		assert.Equal(t, expectedActions[i].Method, action.Method, fmt.Sprintf("Action %d method mismatch", i))
		assert.IsType(t, expectedObj, action.Object, fmt.Sprintf("Action %d object type mismatch", i))

		if action.Method == "Get" {
			assert.Equal(t, expectedActions[i].ObjectKey, action.ObjectKey, fmt.Sprintf("Action %d object key mismatch", i))
		} else if action.Method == "Update" {
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

// Helper functions to create new objects
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

func newVMDistributedCluster(name, namespace, clusterVersion, vmuserName string, vmclusterNames []string, vmClusterInfo []vmv1alpha1.VMClusterStatus) *vmv1alpha1.VMDistributedCluster {
	vmClustersRefs := make([]vmv1alpha1.VMClusterAgentPair, len(vmclusterNames))
	for i, name := range vmclusterNames {
		vmClustersRefs[i] = vmv1alpha1.VMClusterAgentPair{
			LocalObjectReference: corev1.LocalObjectReference{Name: name},
		}
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

type testData struct {
	vmuser         *vmv1beta1.VMUser
	vmcluster1     *vmv1beta1.VMCluster
	vmcluster2     *vmv1beta1.VMCluster
	cr             *vmv1alpha1.VMDistributedCluster
	trackingClient trackingClient
}

func beforeEach() testData {
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
		WithObjects(vmuser, vmcluster1, vmcluster2, cr).
		Build()

	return testData{
		vmuser:         vmuser,
		vmcluster1:     vmcluster1,
		vmcluster2:     vmcluster2,
		cr:             cr,
		trackingClient: trackingClient{Client: baseFake, Actions: []action{}},
	}
}

func TestCreateOrUpdate_DistributedCluster(t *testing.T) {
	ctx := context.Background()

	t.Run("Successful reconciliation", func(t *testing.T) {
		td := beforeEach()
		td.trackingClient.Client = &alwaysReadyClient{Client: td.trackingClient.Client}
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.NoError(t, err, "CreateOrUpdate should succeed when all resources are present")
		assert.Len(t, td.trackingClient.Actions, 17, "Should perform 17 actions")

		expectedActions := []action{
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Update", Object: newVMUser(td.vmuser.Name, td.vmuser.Namespace, []vmv1beta1.TargetRef{{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-2", Namespace: "default"}, TargetPathSuffix: "/select/1"}})},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Update", Object: newVMCluster(td.vmcluster1.Name, td.vmcluster1.Namespace, td.cr.Spec.ClusterVersion, td.vmcluster1.Generation, ptr.To(vmv1beta1.UpdateStatusOperational))},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Update", Object: newVMUser(td.vmuser.Name, td.vmuser.Namespace, []vmv1beta1.TargetRef{{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-2", Namespace: "default"}, TargetPathSuffix: "/select/1"}, {CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-1", Namespace: "default"}, TargetPathSuffix: "/select/1"}})},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Update", Object: newVMUser(td.vmuser.Name, td.vmuser.Namespace, []vmv1beta1.TargetRef{{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-1", Namespace: "default"}, TargetPathSuffix: "/select/1"}})},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Update", Object: newVMCluster(td.vmcluster2.Name, td.vmcluster2.Namespace, td.cr.Spec.ClusterVersion, td.vmcluster2.Generation, ptr.To(vmv1beta1.UpdateStatusOperational))},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Update", Object: newVMUser(td.vmuser.Name, td.vmuser.Namespace, []vmv1beta1.TargetRef{{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-1", Namespace: "default"}, TargetPathSuffix: "/select/1"}, {CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-2", Namespace: "default"}, TargetPathSuffix: "/select/1"}})},
		}
		compareExpectedActions(t, expectedActions, td.trackingClient.Actions)

		updatedVMCluster1 := &vmv1beta1.VMCluster{}
		err = td.trackingClient.Get(ctx, types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, updatedVMCluster1)
		assert.NoError(t, err)
		assert.Equal(t, td.cr.Spec.ClusterVersion, updatedVMCluster1.Spec.ClusterVersion, "VMCluster1 version should be updated")

		updatedVMCluster2 := &vmv1beta1.VMCluster{}
		err = td.trackingClient.Get(ctx, types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, updatedVMCluster2)
		assert.NoError(t, err)
		assert.Equal(t, td.cr.Spec.ClusterVersion, updatedVMCluster2.Spec.ClusterVersion, "VMCluster2 version should be updated")

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
		td.cr.Spec.VMClusters = []vmv1alpha1.VMClusterAgentPair{
			{
				LocalObjectReference: corev1.LocalObjectReference{Name: "missing-cluster"},
			},
		}
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err, "CreateOrUpdate should error if VMCluster is missing")
		assert.Len(t, td.trackingClient.Actions, 2, "Should perform two actions")
	})

	t.Run("VMUser not specified", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.VMUser.Name = ""
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err, "CreateOrUpdate should error if VMUser is not specified")
		assert.Contains(t, err.Error(), "global loadbalancing vmuser is not specified")
		assert.Len(t, td.trackingClient.Actions, 0, "Should perform no actions")
	})

	t.Run("VMUser not found", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.VMUser.Name = "non-existent-vmuser"
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err, "CreateOrUpdate should error if VMUser is not found")
		assert.Contains(t, err.Error(), "failed to get VMUser default/non-existent-vmuser")
		assert.Len(t, td.trackingClient.Actions, 1, "Should perform one action (Get VMUser)")
	})

	t.Run("VMClusters fetch error", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.VMClusters = []vmv1alpha1.VMClusterAgentPair{
			{
				LocalObjectReference: corev1.LocalObjectReference{Name: "vmcluster-1"},
			}, {
				LocalObjectReference: corev1.LocalObjectReference{Name: "non-existent-cluster"},
			},
		}
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err, "CreateOrUpdate should error if VMCluster fetch fails")
		assert.Contains(t, err.Error(), "failed to get VMCluster default/vmcluster-1")
		assert.Len(t, td.trackingClient.Actions, 2, "Should perform two actions")
	})

	t.Run("No matching rule found", func(t *testing.T) {
		td := beforeEach()
		td.vmuser.Spec.TargetRefs = []vmv1beta1.TargetRef{
			{
				CRD: &vmv1beta1.CRDRef{
					Kind:      "OtherKind",
					Name:      td.vmcluster1.Name,
					Namespace: td.vmcluster1.Namespace,
				},
				TargetPathSuffix: "/select/1",
			},
		}
		err := td.trackingClient.Update(ctx, td.vmuser)
		assert.NoError(t, err)
		td.trackingClient.Actions = []action{}

		err = CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
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
		td.cr.Status.VMClusterInfo[0].Generation = 2
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
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

		td.trackingClient.Client = &alwaysReadyClient{Client: td.trackingClient.Client}
		err = CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.NoError(t, err, "CreateOrUpdate should succeed when cluster version matches")
		assert.Len(t, td.trackingClient.Actions, 10, "Should perform ten actions")

		expectedActions := []action{
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Update", Object: newVMUser(td.vmuser.Name, td.vmuser.Namespace, []vmv1beta1.TargetRef{{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-1", Namespace: "default"}, TargetPathSuffix: "/select/1"}})},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Update", Object: newVMCluster(td.vmcluster2.Name, td.vmcluster2.Namespace, td.cr.Spec.ClusterVersion, td.vmcluster2.Generation, ptr.To(vmv1beta1.UpdateStatusOperational))},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Update", Object: newVMUser(td.vmuser.Name, td.vmuser.Namespace, []vmv1beta1.TargetRef{{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-1", Namespace: "default"}, TargetPathSuffix: "/select/1"}, {CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-2", Namespace: "default"}, TargetPathSuffix: "/select/1"}})},
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

		err = CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.NoError(t, err, "CreateOrUpdate should succeed when no update required")
		assert.Len(t, td.trackingClient.Actions, 3, "Should perform three actions")

		expectedActions := []action{
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmuser.Name, Namespace: td.vmuser.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster1.Namespace}, Object: &vmv1beta1.VMCluster{}},
		}
		compareExpectedActions(t, expectedActions, td.trackingClient.Actions)
	})

	t.Run("Paused - no actions performed", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.Paused = true

		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.NoError(t, err, "CreateOrUpdate should succeed without error when paused")
		assert.Len(t, td.trackingClient.Actions, 0, "Should perform no actions when paused")

		// Verify that cluster versions were not changed
		unchangedVMCluster1 := &vmv1beta1.VMCluster{}
		err = td.trackingClient.Get(ctx, types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, unchangedVMCluster1)
		assert.NoError(t, err)
		assert.Equal(t, "v1.0.0", unchangedVMCluster1.Spec.ClusterVersion, "VMCluster1 version should remain unchanged when paused")

		unchangedVMCluster2 := &vmv1beta1.VMCluster{}
		err = td.trackingClient.Get(ctx, types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.vmcluster2.Namespace}, unchangedVMCluster2)
		assert.NoError(t, err)
		assert.Equal(t, "v1.0.0", unchangedVMCluster2.Spec.ClusterVersion, "VMCluster2 version should remain unchanged when paused")
	})

	t.Run("Paused - ignores missing VMUser", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.Paused = true
		td.cr.Spec.VMUser.Name = "non-existent-vmuser"

		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.NoError(t, err, "CreateOrUpdate should succeed without error when paused, even with missing VMUser")
		assert.Len(t, td.trackingClient.Actions, 0, "Should perform no actions when paused")
	})

	t.Run("Paused - ignores missing VMClusters", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.Paused = true
		td.cr.Spec.VMClusters = []vmv1alpha1.VMClusterAgentPair{{LocalObjectReference: corev1.LocalObjectReference{Name: "missing-cluster"}}}

		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.NoError(t, err, "CreateOrUpdate should succeed without error when paused, even with missing VMClusters")
		assert.Len(t, td.trackingClient.Actions, 0, "Should perform no actions when paused")
	})

	t.Run("Paused - ignores version mismatch", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.Paused = true
		td.cr.Spec.ClusterVersion = "v2.0.0" // Different version than clusters

		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.NoError(t, err, "CreateOrUpdate should succeed without error when paused, even with version mismatch")
		assert.Len(t, td.trackingClient.Actions, 0, "Should perform no actions when paused")

		// Verify versions were not updated
		unchangedVMCluster1 := &vmv1beta1.VMCluster{}
		err = td.trackingClient.Get(ctx, types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.vmcluster1.Namespace}, unchangedVMCluster1)
		assert.NoError(t, err)
		assert.Equal(t, "v1.0.0", unchangedVMCluster1.Spec.ClusterVersion, "VMCluster1 version should remain unchanged when paused")
	})
}

func TestWaitForVMClusterReady(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	namespace := "default"
	name := "vmcluster-ready"

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

func TestWaitForVMClusterReadyWithDelay(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	namespace := "default"
	name := "vmcluster-delayed-ready"

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
		// Create a deep copy for this test to avoid shared state
		vmclusterCopy := vmcluster.DeepCopy()
		baseFake := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(vmclusterCopy).
			WithStatusSubresource(&vmv1beta1.VMCluster{}).
			Build()
		fakeClient := trackingClient{Client: baseFake, Actions: []action{}}

		go func() {
			time.Sleep(3 * time.Second)
			var obj vmv1beta1.VMCluster
			if err := fakeClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &obj); err != nil {
				t.Errorf("Failed to get VMCluster in goroutine: %v", err)
				return
			}
			obj.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
			if err := fakeClient.Status().Update(ctx, &obj); err != nil {
				t.Errorf("Failed to update VMCluster status in goroutine: %v", err)
			}
		}()

		// Use a separate copy for the main goroutine to avoid race conditions
		vmclusterForWait := vmclusterCopy.DeepCopy()
		err := waitForVMClusterReady(ctx, &fakeClient, vmclusterForWait, 5*time.Second)
		assert.NoError(t, err)

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

	targetRef := vmv1beta1.TargetRef{
		CRD: &vmv1beta1.CRDRef{
			Kind:      "VMCluster/vmselect",
			Name:      clusterName,
			Namespace: namespace,
		},
		TargetPathSuffix: "/select/test",
	}

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
		fakeClient := trackingClient{Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithRule, initialVMUser, vmClusterToManage).
			Build(), Actions: []action{}}

		err := setVMClusterStatusInVMUser(ctx, &fakeClient, crWithRule, vmClusterToManage, initialVMUser, true)
		assert.NoError(t, err)

		updatedVMUser := &vmv1beta1.VMUser{}
		err = fakeClient.Get(ctx, types.NamespacedName{Name: vmuserName, Namespace: namespace}, updatedVMUser)
		assert.NoError(t, err)
		assert.Len(t, updatedVMUser.Spec.TargetRefs, 1)
		assert.Equal(t, targetRef, updatedVMUser.Spec.TargetRefs[0])
	})

	t.Run("Disable cluster - remove target ref", func(t *testing.T) {
		initialVMUser := newVMUser(vmuserName, namespace, []vmv1beta1.TargetRef{targetRef})
		fakeClient := trackingClient{Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithRule, initialVMUser, vmClusterToManage).
			Build(), Actions: []action{}}

		err := setVMClusterStatusInVMUser(ctx, &fakeClient, crWithRule, vmClusterToManage, initialVMUser, false)
		assert.NoError(t, err)

		updatedVMUser := &vmv1beta1.VMUser{}
		err = fakeClient.Get(ctx, types.NamespacedName{Name: vmuserName, Namespace: namespace}, updatedVMUser)
		assert.NoError(t, err)
		assert.Len(t, updatedVMUser.Spec.TargetRefs, 0)
	})

	t.Run("No matching rule in VMDistributedCluster status", func(t *testing.T) {
		crWithoutRule := newVMDistributedCluster("dist-cluster-no-rule", namespace, "v1.0.0", vmuserName, []string{}, []vmv1alpha1.VMClusterStatus{})
		initialVMUser := newVMUser(vmuserName, namespace, []vmv1beta1.TargetRef{})
		fakeClient := trackingClient{Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithRule, initialVMUser, vmClusterToManage).
			Build(), Actions: []action{}}

		err := setVMClusterStatusInVMUser(ctx, &fakeClient, crWithoutRule, vmClusterToManage, initialVMUser, true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no matching rule found for vmcluster")
	})

	t.Run("Failure to fetch VMUser", func(t *testing.T) {
		initialVMUser := newVMUser("non-existent-user", namespace, []vmv1beta1.TargetRef{})
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
		failingClient := &alwaysFailingUpdateClient{Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(crWithRule, initialVMUser, vmClusterToManage).
			Build()}

		err := setVMClusterStatusInVMUser(ctx, failingClient, crWithRule, vmClusterToManage, initialVMUser, true)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "simulated update error")
	})
}

func TestGetGenerationsFromStatus(t *testing.T) {
	t.Run("Empty status", func(t *testing.T) {
		status := &vmv1alpha1.VMDistributedClusterStatus{}
		result := getGenerationsFromStatus(status)
		assert.Empty(t, result)
	})

	t.Run("Multiple clusters", func(t *testing.T) {
		status := &vmv1alpha1.VMDistributedClusterStatus{
			VMClusterInfo: []vmv1alpha1.VMClusterStatus{
				{VMClusterName: "cluster-1", Generation: 1},
				{VMClusterName: "cluster-2", Generation: 2},
				{VMClusterName: "cluster-3", Generation: 3},
			},
		}
		result := getGenerationsFromStatus(status)
		assert.Len(t, result, 3)
		assert.Equal(t, int64(1), result["cluster-1"])
		assert.Equal(t, int64(2), result["cluster-2"])
		assert.Equal(t, int64(3), result["cluster-3"])
	})

	t.Run("Duplicate cluster names", func(t *testing.T) {
		status := &vmv1alpha1.VMDistributedClusterStatus{
			VMClusterInfo: []vmv1alpha1.VMClusterStatus{
				{VMClusterName: "cluster-1", Generation: 1},
				{VMClusterName: "cluster-1", Generation: 2},
			},
		}
		result := getGenerationsFromStatus(status)
		assert.Len(t, result, 1)
		assert.Equal(t, int64(2), result["cluster-1"])
	})
}

func TestFetchVMClusters(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	t.Run("Successful fetch multiple clusters", func(t *testing.T) {
		cluster1 := newVMCluster("cluster-1", "default", "v1.0.0", 1, nil)
		cluster2 := newVMCluster("cluster-2", "default", "v1.0.0", 1, nil)
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster1, cluster2).
			Build()

		refs := []vmv1alpha1.VMClusterAgentPair{
			{LocalObjectReference: corev1.LocalObjectReference{Name: "cluster-1"}},
			{LocalObjectReference: corev1.LocalObjectReference{Name: "cluster-2"}},
		}

		result, err := fetchVMClusters(ctx, fakeClient, "default", refs)
		assert.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "cluster-1", result[0].VMCluster.Name)
		assert.Equal(t, "cluster-2", result[1].VMCluster.Name)
	})

	t.Run("Empty refs list", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		refs := []vmv1alpha1.VMClusterAgentPair{}

		result, err := fetchVMClusters(ctx, fakeClient, "default", refs)
		assert.NoError(t, err)
		assert.Len(t, result, 0)
	})

	t.Run("Cluster not found", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		refs := []vmv1alpha1.VMClusterAgentPair{
			{LocalObjectReference: corev1.LocalObjectReference{Name: "nonexistent"}},
		}

		result, err := fetchVMClusters(ctx, fakeClient, "default", refs)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to get VMCluster default/nonexistent")
	})
}

// mockVMAgent implements the VMAgentWithStatus interface for testing
type mockVMAgent struct {
	url      string
	replicas int32
}

func (m *mockVMAgent) AsURL() string {
	return m.url
}

func (m *mockVMAgent) GetMetricPath() string {
	return "/metrics"
}

func (m *mockVMAgent) GetReplicas() int32 {
	return m.replicas
}

func (m *mockVMAgent) GetNamespace() string {
	return "default"
}

func (m *mockVMAgent) GetName() string {
	return "test-vmagent"
}

func TestWaitForVMClusterVMAgentMetrics(t *testing.T) {
	ctx := context.Background()
	httpClient := &http.Client{}

	t.Run("Returns nil when VMAgent is nil", func(t *testing.T) {
		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, nil, 5*time.Second)
		assert.NoError(t, err)
	})

	t.Run("Returns error when VMAgent has zero replicas", func(t *testing.T) {
		vmAgent := &mockVMAgent{
			replicas: 0,
		}

		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 5*time.Second)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMAgent default/test-vmagent is not ready")
	})

	t.Run("Successfully waits for metrics to become zero", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount <= 2 {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, "vmagent_remotewrite_pending_data_bytes 1024\n")
			} else {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, "vmagent_remotewrite_pending_data_bytes 0\n")
			}
		}))
		defer server.Close()

		// Create a mock VMAgent that implements our interface
		vmAgent := &mockVMAgent{
			url:      server.URL,
			replicas: 1,
		}

		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 5*time.Second)

		assert.NoError(t, err)
		assert.GreaterOrEqual(t, callCount, 3, "Should have made at least 3 calls to the metrics endpoint")
	})

	t.Run("Times out when metrics never become zero", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "vmagent_remotewrite_pending_data_bytes 1024\n")
		}))
		defer server.Close()

		// Create a mock VMAgent that implements our interface
		vmAgent := &mockVMAgent{
			url:      server.URL,
			replicas: 1,
		}

		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 2*time.Second)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to wait for VMAgent metrics")
	})

	t.Run("Returns error when HTTP server returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Internal Server Error")
		}))
		defer server.Close()

		vmAgent := &mockVMAgent{
			url:      server.URL,
			replicas: 1,
		}

		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 2*time.Second)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to wait for VMAgent metrics")
	})

	t.Run("Returns error when metrics endpoint is unreachable", func(t *testing.T) {
		// Create a mock VMAgent with an invalid URL
		vmAgent := &mockVMAgent{
			url:      "http://invalid-host-that-does-not-exist.local:8080",
			replicas: 1,
		}

		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 2*time.Second)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to wait for VMAgent metrics")
	})

	t.Run("Returns error when metric is missing from response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "some_other_metric 123\nanother_metric 456\n")
		}))
		defer server.Close()

		vmAgent := &mockVMAgent{
			url:      server.URL,
			replicas: 1,
		}

		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 2*time.Second)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to wait for VMAgent metrics")
	})

	t.Run("Returns error when metric value is invalid", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "vmagent_remotewrite_pending_data_bytes invalid_value\n")
		}))
		defer server.Close()

		vmAgent := &mockVMAgent{
			url:      server.URL,
			replicas: 1,
		}

		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 2*time.Second)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to wait for VMAgent metrics")
	})

	t.Run("Successfully handles metrics with additional whitespace", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "vmagent_remotewrite_pending_data_bytes   0  \n")
		}))
		defer server.Close()

		vmAgent := &mockVMAgent{
			url:      server.URL,
			replicas: 1,
		}

		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 2*time.Second)
		assert.NoError(t, err)
	})

	t.Run("Successfully handles metrics with comments and empty lines", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			if callCount == 1 {
				fmt.Fprintf(w, "# HELP vmagent_remotewrite_pending_data_bytes Pending data bytes\n")
				fmt.Fprintf(w, "# TYPE vmagent_remotewrite_pending_data_bytes gauge\n")
				fmt.Fprintf(w, "\n")
				fmt.Fprintf(w, "vmagent_remotewrite_pending_data_bytes 1024\n")
				fmt.Fprintf(w, "\n")
				fmt.Fprintf(w, "# Some other metric\n")
			} else {
				fmt.Fprintf(w, "# HELP vmagent_remotewrite_pending_data_bytes Pending data bytes\n")
				fmt.Fprintf(w, "# TYPE vmagent_remotewrite_pending_data_bytes gauge\n")
				fmt.Fprintf(w, "\n")
				fmt.Fprintf(w, "vmagent_remotewrite_pending_data_bytes 0\n")
				fmt.Fprintf(w, "\n")
			}
		}))
		defer server.Close()

		vmAgent := &mockVMAgent{
			url:      server.URL,
			replicas: 1,
		}

		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 2*time.Second)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, callCount, 2, "Should have made at least 2 calls")
	})

	t.Run("Respects context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "vmagent_remotewrite_pending_data_bytes 1024\n")
		}))
		defer server.Close()

		vmAgent := &mockVMAgent{
			url:      server.URL,
			replicas: 1,
		}

		// Create a context that will be cancelled after 1 second
		ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()

		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 5*time.Second)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to wait for VMAgent metrics")
	})
}

func TestFetchVMAgentDiskBufferMetric(t *testing.T) {
	ctx := context.Background()
	httpClient := &http.Client{}

	t.Run("Successfully parses valid metric", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "vmagent_remotewrite_pending_data_bytes 1024\n")
		}))
		defer server.Close()

		value, err := fetchVMAgentDiskBufferMetric(ctx, httpClient, server.URL)
		assert.NoError(t, err)
		assert.Equal(t, int64(1024), value)
	})

	t.Run("Returns error when metric not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "some_other_metric 123\n")
		}))
		defer server.Close()

		value, err := fetchVMAgentDiskBufferMetric(ctx, httpClient, server.URL)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "metric vmagent_remotewrite_pending_data_bytes not found")
		assert.Equal(t, int64(0), value)
	})

	t.Run("Returns error when metric value is invalid", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "vmagent_remotewrite_pending_data_bytes invalid_value\n")
		}))
		defer server.Close()

		value, err := fetchVMAgentDiskBufferMetric(ctx, httpClient, server.URL)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "could not parse metric value")
		assert.Equal(t, int64(0), value)
	})

	t.Run("Returns error when HTTP request fails", func(t *testing.T) {
		value, err := fetchVMAgentDiskBufferMetric(ctx, httpClient, "http://invalid-host.local:8080")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch metrics from VMAgent")
		assert.Equal(t, int64(0), value)
	})

	t.Run("Successfully handles multiple metrics and finds the correct one", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "metric1 100\n")
			fmt.Fprintf(w, "vmagent_remotewrite_pending_data_bytes 2048\n")
			fmt.Fprintf(w, "metric2 300\n")
		}))
		defer server.Close()

		value, err := fetchVMAgentDiskBufferMetric(ctx, httpClient, server.URL)
		assert.NoError(t, err)
		assert.Equal(t, int64(2048), value)
	})

	t.Run("Successfully handles zero metric value", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "vmagent_remotewrite_pending_data_bytes 0\n")
		}))
		defer server.Close()

		value, err := fetchVMAgentDiskBufferMetric(ctx, httpClient, server.URL)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), value)
	})
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
	fakeClient := trackingClient{Client: baseFake, Actions: []action{}}

	err := changeVMClusterVersion(ctx, &fakeClient, vmcluster, "v1.1.0")
	assert.NoError(t, err)
	assert.Equal(t, "v1.1.0", vmcluster.Spec.ClusterVersion)
}

func TestFindVMUserReadRuleForVMCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)

	t.Run("TargetRef with different kinds", func(t *testing.T) {
		vmuser := &vmv1beta1.VMUser{
			Spec: vmv1beta1.VMUserSpec{
				TargetRefs: []vmv1beta1.TargetRef{
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
							Kind:      "VMCluster/vminsert",
							Name:      "cluster-1",
							Namespace: "default",
						},
						TargetPathSuffix: "/insert/1",
					},
					{
						CRD: &vmv1beta1.CRDRef{
							Kind:      "OtherResource",
							Name:      "other-1",
							Namespace: "default",
						},
						TargetPathSuffix: "/other/1",
					},
				},
			},
		}
		cluster := &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-1",
				Namespace: "default",
			},
		}

		result, err := findVMUserReadRuleForVMCluster(vmuser, cluster)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "VMCluster/vmselect", result.CRD.Kind)
		assert.Equal(t, "/select/1", result.TargetPathSuffix)
	})

	t.Run("TargetRef with different path suffixes", func(t *testing.T) {
		testCases := []struct {
			name         string
			targetSuffix string
			shouldMatch  bool
		}{
			{
				name:         "Valid select path",
				targetSuffix: "/select/1",
				shouldMatch:  true,
			},
			{
				name:         "Valid select path with multiple segments",
				targetSuffix: "/select/prometheus/1",
				shouldMatch:  true,
			},
			{
				name:         "Invalid insert path",
				targetSuffix: "/insert/1",
				shouldMatch:  false,
			},
			{
				name:         "Invalid path without select prefix",
				targetSuffix: "/api/v1/select",
				shouldMatch:  false,
			},
			{
				name:         "Empty path suffix",
				targetSuffix: "",
				shouldMatch:  false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				vmuser := &vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmuser-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMCluster/vmselect",
									Name:      "cluster-1",
									Namespace: "default",
								},
								TargetPathSuffix: tc.targetSuffix,
							},
						},
					},
				}
				cluster := &vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "default",
					},
				}

				result, err := findVMUserReadRuleForVMCluster(vmuser, cluster)
				if tc.shouldMatch {
					assert.NoError(t, err)
					assert.NotNil(t, result)
					assert.Equal(t, tc.targetSuffix, result.TargetPathSuffix)
				} else {
					assert.Error(t, err)
					assert.Nil(t, result)
					assert.Contains(t, err.Error(), "vmuser vmuser-1 has no target refs")
				}
			})
		}
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
			VMClusters: []vmv1alpha1.VMClusterAgentPair{},
			VMUser:     corev1.LocalObjectReference{Name: "fake"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cr).
		Build()

	err := client.Delete(ctx, cr)
	assert.NoError(t, err, "Delete should succeed")

	var deleted vmv1alpha1.VMDistributedCluster
	err = client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &deleted)
	assert.Error(t, err, "Get should return error after delete")
	assert.True(t, k8serrors.IsNotFound(err), "Error should be IsNotFound")
}
