package vmdistributedcluster

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	vmclusterWaitReadyDeadline = 500 * time.Millisecond
	httpTimeout                = 500 * time.Millisecond
)

// action stores information about a client action for tracking.
type action struct {
	Method    string
	ObjectKey types.NamespacedName
	Object    client.Object
}

// trackingClient wraps a client.Client and records all actions.
type trackingClient struct {
	client.Client
	Actions []action
	mu      sync.Mutex
}

// trackingStatusWriter wraps a trackingClient and records status updates.
type trackingStatusWriter struct {
	*trackingClient
}

func (tc *trackingClient) Status() client.StatusWriter {
	return &trackingStatusWriter{tc}
}

func (tc *trackingClient) Get(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
	tc.mu.Lock()
	tc.Actions = append(tc.Actions, action{Method: "Get", ObjectKey: key, Object: obj})
	tc.mu.Unlock()
	return tc.Client.Get(ctx, key, obj)
}

func (tc *trackingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	tc.mu.Lock()
	tc.Actions = append(tc.Actions, action{Method: "Update", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})
	tc.mu.Unlock()
	return tc.Client.Update(ctx, obj, opts...)
}

func (tc *trackingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	tc.mu.Lock()
	tc.Actions = append(tc.Actions, action{Method: "Create", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})
	tc.mu.Unlock()
	return tc.Client.Create(ctx, obj, opts...)
}

func (tc *trackingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	tc.mu.Lock()
	tc.Actions = append(tc.Actions, action{Method: "Delete", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})
	tc.mu.Unlock()
	return tc.Client.Delete(ctx, obj, opts...)
}

// alwaysReadyClient is a client which always returns Ready update status.
type alwaysReadyClient struct {
	client.Client
}

func (c *alwaysReadyClient) Get(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
	err := c.Client.Get(ctx, key, obj)
	if err != nil {
		return err
	}
	if v, ok := obj.(*vmv1beta1.VMCluster); ok {
		v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
	}
	return nil
}

// Status returns a StatusWriter that always succeeds for updates.
func (c *alwaysReadyClient) Status() client.StatusWriter {
	return &alwaysReadyStatusWriter{c}
}

// alwaysReadyStatusWriter is a StatusWriter that always succeeds updates.
type alwaysReadyStatusWriter struct {
	*alwaysReadyClient
}

func (arsw *alwaysReadyStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return arsw.Client.Status().Create(ctx, obj, subResource, opts...)
}

func (arsw *alwaysReadyStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	// Simulate a successful status update
	return arsw.Client.Status().Update(ctx, obj, opts...)
}

func (arsw *alwaysReadyStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	// Simulate a successful status patch
	return arsw.Client.Status().Patch(ctx, obj, patch, opts...)
}

// Ensure alwaysReadyStatusWriter implements client.SubResourceWriter
var _ client.SubResourceWriter = &alwaysReadyStatusWriter{}

// alwaysFailingUpdateClient is a client which always returns an error on Update
type alwaysFailingUpdateClient struct {
	client.Client
}

func (f *alwaysFailingUpdateClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return fmt.Errorf("simulated update error")
}

func (tsw *trackingStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	tsw.mu.Lock()
	tsw.Actions = append(tsw.Actions, action{Method: "CreateStatus", ObjectKey: client.ObjectKeyFromObject(obj), Object: subResource})
	tsw.mu.Unlock()
	return tsw.Client.Status().Create(ctx, obj, subResource, opts...)
}

func (tsw *trackingStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	tsw.mu.Lock()
	tsw.Actions = append(tsw.Actions, action{Method: "UpdateStatus", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})
	tsw.mu.Unlock()
	return tsw.Client.Status().Update(ctx, obj, opts...)
}

func (tsw *trackingStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	tsw.mu.Lock()
	tsw.Actions = append(tsw.Actions, action{Method: "PatchStatus", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})
	tsw.mu.Unlock()
	return tsw.Client.Status().Patch(ctx, obj, patch, opts...)
}

// Ensure trackingStatusWriter implements client.StatusWriter
var _ client.StatusWriter = &trackingStatusWriter{}

func compareExpectedActions(t *testing.T, expected, actual []action) {
	assert.Equal(t, len(expected), len(actual), "number of actions mismatch")
	for i := range expected {
		assert.Equal(t, expected[i].Method, actual[i].Method, "Method mismatch at index %d", i)
		assert.Equal(t, expected[i].ObjectKey, actual[i].ObjectKey, "ObjectKey mismatch at index %d", i)
		// Compare only types for objects, as content can vary.
		assert.IsType(t, expected[i].Object, actual[i].Object, "Object type mismatch at index %d", i)
	}
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

func newVMDistributedCluster(name, namespace, clusterVersion, vmagentName string, vmuserNames []string, zoneNames []string, vmClusterInfo []vmv1alpha1.VMClusterStatus) *vmv1alpha1.VMDistributedCluster {
	zonesRefs := make([]vmv1alpha1.VMClusterRefOrSpec, len(zoneNames))
	for i, name := range zoneNames {
		zonesRefs[i] = vmv1alpha1.VMClusterRefOrSpec{
			Ref: &corev1.LocalObjectReference{Name: name},
		}
	}

	vmUsersRefs := make([]corev1.LocalObjectReference, len(vmuserNames))
	for i, name := range vmuserNames {
		vmUsersRefs[i] = corev1.LocalObjectReference{Name: name}
	}
	return &vmv1alpha1.VMDistributedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vmv1alpha1.VMDistributedClusterSpec{
			ClusterVersion: clusterVersion,
			VMAgent:        corev1.LocalObjectReference{Name: vmagentName},
			VMUsers:        vmUsersRefs,
			Zones:          zonesRefs,
		},
		Status: vmv1alpha1.VMDistributedClusterStatus{
			VMClusterInfo: vmClusterInfo,
		},
	}
}

type testData struct {
	vmagent        *vmv1beta1.VMAgent
	vmusers        []*vmv1beta1.VMUser
	vmcluster1     *vmv1beta1.VMCluster
	vmcluster2     *vmv1beta1.VMCluster
	cr             *vmv1alpha1.VMDistributedCluster
	trackingClient trackingClient
}

func beforeEach() testData {
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)

	vmagentName := "test-vmagent"
	vmagent := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vmagentName,
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			// Basic VMAgent spec
		},
	}

	vmuserNames := []string{"test-vmuser-1", "test-vmuser-2"}
	vmusers := make([]*vmv1beta1.VMUser, len(vmuserNames))
	for i, vmuserName := range vmuserNames {
		vmusers[i] = newVMUser(vmuserName, "default", []vmv1beta1.TargetRef{
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
	}

	vmcluster1Name := "cluster-1"
	vmcluster1 := newVMCluster(vmcluster1Name, "default", "v1.0.0", 1, nil)

	vmcluster2Name := "cluster-2"
	vmcluster2 := newVMCluster(vmcluster2Name, "default", "v1.0.0", 1, nil)

	cr := newVMDistributedCluster("distributed-1", "default", "v1.1.0", vmagentName, vmuserNames, []string{vmcluster1Name, vmcluster2Name}, []vmv1alpha1.VMClusterStatus{
		{VMClusterName: vmcluster1Name, Generation: 1, TargetRef: vmv1beta1.TargetRef{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: vmcluster1Name, Namespace: "default"}, TargetPathSuffix: "/select/1"}},
		{VMClusterName: vmcluster2Name, Generation: 1, TargetRef: vmv1beta1.TargetRef{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: vmcluster2Name, Namespace: "default"}, TargetPathSuffix: "/select/1"}},
	})

	objects := []client.Object{vmagent, vmcluster1, vmcluster2, cr}
	for _, vmuser := range vmusers {
		objects = append(objects, vmuser)
	}

	baseFake := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()

	return testData{
		vmagent:        vmagent,
		vmusers:        vmusers,
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
		// Replace client with a mock that always reports operational status for VMClusters
		// This avoids waiting for actual cluster deployment in unit tests
		td.trackingClient.Client = &alwaysReadyClient{Client: td.trackingClient.Client}

		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.NoError(t, err, "CreateOrUpdate should succeed when all resources are present")

		// Verify the CR has the new structure
		assert.Equal(t, "test-vmagent", td.cr.Spec.VMAgent.Name, "Should have VMAgent reference")
		assert.Len(t, td.cr.Spec.VMUsers, 2, "Should have 2 VMUsers")
		assert.Len(t, td.cr.Spec.Zones, 2, "Should have 2 Zones")
	})
}

func TestCreateOrUpdate_ErrorHandling(t *testing.T) {
	ctx := context.Background()

	t.Run("VMCluster missing", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.Zones = []vmv1alpha1.VMClusterRefOrSpec{
			{
				Ref: &corev1.LocalObjectReference{Name: "missing-cluster"},
			},
		}
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err, "CreateOrUpdate should error if VMCluster is missing")
		assert.Contains(t, err.Error(), "failed to fetch vmclusters: referenced VMCluster default/missing-cluster not found: vmclusters.operator.victoriametrics.com \"missing-cluster\" not found")

		expectedActions := []action{
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.cr.Spec.VMAgent.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMAgent{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[0].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[1].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: "missing-cluster", Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMCluster{}},
		}
		compareExpectedActions(t, expectedActions, td.trackingClient.Actions)
	})

	t.Run("VMAgent not specified", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.VMAgent.Name = ""
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err, "CreateOrUpdate should error if VMAgent is not specified")
		assert.Contains(t, err.Error(), "failed to fetch global vmagent: global vmagent name is not specified")
		compareExpectedActions(t, []action{}, td.trackingClient.Actions)
	})

	t.Run("VMAgent not found", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.VMAgent.Name = "non-existent-vmagent"
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err, "CreateOrUpdate should error if VMAgent is not found")
		assert.Contains(t, err.Error(), "failed to get global VMAgent default/non-existent-vmagent")

		expectedActions := []action{
			{Method: "Get", ObjectKey: types.NamespacedName{Name: "non-existent-vmagent", Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMAgent{}},
		}
		compareExpectedActions(t, expectedActions, td.trackingClient.Actions)
	})

	t.Run("Zones fetch error", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.Zones = []vmv1alpha1.VMClusterRefOrSpec{
			{
				Ref: &corev1.LocalObjectReference{Name: "vmcluster-1"},
			},
		}
		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err, "CreateOrUpdate should error if VMCluster is not found")
		assert.Contains(t, err.Error(), "failed to fetch vmclusters: referenced VMCluster default/vmcluster-1 not found: vmclusters.operator.victoriametrics.com \"vmcluster-1\" not found")

		expectedActions := []action{
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.cr.Spec.VMAgent.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMAgent{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[0].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[1].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: "vmcluster-1", Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMCluster{}},
		}
		compareExpectedActions(t, expectedActions, td.trackingClient.Actions)
	})

	t.Run("No matching rule found", func(t *testing.T) {
		td := beforeEach()

		// Create VMUsers, both with no target refs for cluster-1
		vmuserNoRules1 := newVMUser(td.vmusers[0].Name, td.vmusers[0].Namespace, []vmv1beta1.TargetRef{}) // No rules for test-vmuser-1
		vmuserNoRules2 := newVMUser(td.vmusers[1].Name, td.vmusers[1].Namespace, []vmv1beta1.TargetRef{}) // No rules for test-vmuser-2

		// Create a new trackingClient with the modified VMUsers
		scheme := runtime.NewScheme()
		_ = vmv1alpha1.AddToScheme(scheme)
		_ = vmv1beta1.AddToScheme(scheme)

		baseFake := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(td.vmagent, td.vmcluster1, td.vmcluster2, td.cr, vmuserNoRules1, vmuserNoRules2).
			Build()

		// Update td.cr's VMUsers to reference these specific VMUsers for this test
		td.cr.Spec.VMUsers = []corev1.LocalObjectReference{{Name: vmuserNoRules1.Name}, {Name: vmuserNoRules2.Name}}

		testTrackingClient := trackingClient{Client: baseFake, Actions: []action{}}

		err := CreateOrUpdate(ctx, td.cr, &testTrackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err, "CreateOrUpdate should error if findVMUserReadRuleForVMCluster fails")
		assert.Contains(t, err.Error(), "failed to find the rule for vmcluster cluster-1")

		expectedActions := []action{
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.cr.Spec.VMAgent.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMAgent{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: vmuserNoRules1.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: vmuserNoRules2.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMCluster{}},
		}
		compareExpectedActions(t, expectedActions, testTrackingClient.Actions)
	})

	t.Run("Generations change triggers status update", func(t *testing.T) {
		td := beforeEach()

		// Create a new trackingClient for this test to ensure status updates are handled
		scheme := runtime.NewScheme()
		_ = vmv1alpha1.AddToScheme(scheme)
		_ = vmv1beta1.AddToScheme(scheme)

		objects := []client.Object{td.vmagent, td.vmcluster1, td.vmcluster2, td.cr}
		for _, vmuser := range td.vmusers {
			objects = append(objects, vmuser)
		}

		baseFake := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objects...).
			WithStatusSubresource(&vmv1alpha1.VMDistributedCluster{}). // Ensure status subresource is enabled for VMDistributedCluster
			Build()

		testTrackingClient := trackingClient{Client: baseFake, Actions: []action{}}

		td.cr.Status.VMClusterInfo[0].Generation = 2 // Simulate a generation change

		err := CreateOrUpdate(ctx, td.cr, &testTrackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err, "CreateOrUpdate should error if generations change detected")
		assert.Contains(t, err.Error(), "unexpected generations change detected")
		// Expected actions: Get VMAgent (1), Get all VMUsers (2), Get all VMClusters (2) + Update status (1)
		// Total: 1 + 2 + 2 + 1 = 6 actions
		expectedActions := []action{
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.cr.Spec.VMAgent.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMAgent{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[0].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[1].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMCluster{}},
			// Expect a status update action
			{Method: "UpdateStatus", ObjectKey: types.NamespacedName{Name: td.cr.Name, Namespace: td.cr.Namespace}, Object: &vmv1alpha1.VMDistributedCluster{}},
		}
		compareExpectedActions(t, expectedActions, testTrackingClient.Actions)
	})
}

func TestCreateOrUpdate_PartialUpdate(t *testing.T) {
	ctx := context.Background()
	t.Run("Partial cluster version update", func(t *testing.T) {
		td := beforeEach()
		td.vmcluster1.Spec.ClusterVersion = "v1.1.0"
		err := td.trackingClient.Update(ctx, td.vmcluster1)
		assert.NoError(t, err)
		td.trackingClient.Actions = []action{}

		td.trackingClient.Client = &alwaysReadyClient{Client: td.trackingClient.Client}
		err = CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.NoError(t, err, "CreateOrUpdate should succeed when cluster version matches")
		// Expected actions: Get VMAgent (1), Get all VMUsers (2), Get all VMClusters (2)
		// Disable VMCluster (Get 2 VMUsers, Update 2 VMUsers) = 4
		// Change VMCluster version (Get 1 VMCluster, Update 1 VMCluster) = 2
		// Wait for VMCluster ready (Get 1 VMCluster) = 1
		// Enable VMCluster (Get 2 VMUsers) = 2
		// Total: 1 + 2 + 2 + 4 + 2 + 1 + 2 = 14 actions
		assert.Len(t, td.trackingClient.Actions, 14, "Should perform fourteen actions")

		expectedDisabledVMUserRef := []vmv1beta1.TargetRef{{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "cluster-1", Namespace: "default"}, TargetPathSuffix: "/select/1"}}

		expectedActions := []action{
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.cr.Spec.VMAgent.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMAgent{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[0].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[1].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[0].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Update", ObjectKey: types.NamespacedName{Name: td.vmusers[0].Name, Namespace: td.cr.Namespace}, Object: newVMUser(td.vmusers[0].Name, td.vmusers[0].Namespace, expectedDisabledVMUserRef)},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[1].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Update", ObjectKey: types.NamespacedName{Name: td.vmusers[1].Name, Namespace: td.cr.Namespace}, Object: newVMUser(td.vmusers[1].Name, td.vmusers[1].Namespace, expectedDisabledVMUserRef)},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Update", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.cr.Namespace}, Object: newVMCluster(td.vmcluster2.Name, td.vmcluster2.Namespace, td.cr.Spec.ClusterVersion, td.vmcluster2.Generation, ptr.To(vmv1beta1.UpdateStatusOperational))},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[0].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[1].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
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
		// Expected actions: Get VMAgent (1), Get all VMUsers (2), Get all VMClusters (2) = 5 actions
		assert.Len(t, td.trackingClient.Actions, 5, "Should perform five actions")

		expectedActions := []action{
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.cr.Spec.VMAgent.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMAgent{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[0].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmusers[1].Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMUser{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMCluster{}},
			{Method: "Get", ObjectKey: types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.cr.Namespace}, Object: &vmv1beta1.VMCluster{}},
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
		err = td.trackingClient.Get(ctx, types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.cr.Namespace}, unchangedVMCluster1)
		assert.NoError(t, err)
		assert.Equal(t, "v1.0.0", unchangedVMCluster1.Spec.ClusterVersion, "VMCluster1 version should remain unchanged when paused")

		unchangedVMCluster2 := &vmv1beta1.VMCluster{}
		err = td.trackingClient.Get(ctx, types.NamespacedName{Name: td.vmcluster2.Name, Namespace: td.cr.Namespace}, unchangedVMCluster2)
		assert.NoError(t, err)
		assert.Equal(t, "v1.0.0", unchangedVMCluster2.Spec.ClusterVersion, "VMCluster2 version should remain unchanged when paused")
	})

	t.Run("Paused - ignores missing VMUser", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.Paused = true
		td.cr.Spec.VMAgent.Name = "non-existent-vmuser"

		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.NoError(t, err, "CreateOrUpdate should succeed without error when paused, even with missing VMUser")
		assert.Len(t, td.trackingClient.Actions, 0, "Should perform no actions when paused")
	})

	t.Run("Paused - ignores missing Zones", func(t *testing.T) {
		td := beforeEach()
		td.cr.Spec.Paused = true
		td.cr.Spec.Zones = []vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: "missing-cluster"}}}

		err := CreateOrUpdate(ctx, td.cr, &td.trackingClient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.NoError(t, err, "CreateOrUpdate should succeed without error when paused, even with missing Zones")
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
		err = td.trackingClient.Get(ctx, types.NamespacedName{Name: td.vmcluster1.Name, Namespace: td.cr.Namespace}, unchangedVMCluster1)
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
		assert.NoError(t, fakeClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &updatedVMCluster))
		assert.Equal(t, vmv1beta1.UpdateStatusOperational, updatedVMCluster.Status.UpdateStatus)
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
	crName := "test-vmdistributedcluster"

	t.Run("Successful fetch multiple clusters", func(t *testing.T) {
		cluster1 := newVMCluster("cluster-1", "default", "v1.0.0", 1, nil)
		cluster2 := newVMCluster("cluster-2", "default", "v1.0.0", 1, nil)
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cluster1, cluster2).
			Build()

		refs := []vmv1alpha1.VMClusterRefOrSpec{
			{Ref: &corev1.LocalObjectReference{Name: "cluster-1"}},
			{Ref: &corev1.LocalObjectReference{Name: "cluster-2"}},
		}

		result, err := fetchVMClusters(ctx, fakeClient, crName, "default", refs)
		assert.NoError(t, err)
		assert.Len(t, result, 2)
		assert.Equal(t, "cluster-1", result[0].Name)
		assert.Equal(t, "cluster-2", result[1].Name)
	})

	t.Run("Empty refs list", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		refs := []vmv1alpha1.VMClusterRefOrSpec{}

		result, err := fetchVMClusters(ctx, fakeClient, crName, "default", refs)
		assert.NoError(t, err)
		assert.Len(t, result, 0)
	})

	t.Run("Cluster not found", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		refs := []vmv1alpha1.VMClusterRefOrSpec{
			{Ref: &corev1.LocalObjectReference{Name: "nonexistent"}},
		}

		_, err := fetchVMClusters(ctx, fakeClient, crName, "default", refs)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "referenced VMCluster default/nonexistent not found: vmclusters.operator.victoriametrics.com \"nonexistent\" not found")
	})

	t.Run("Successfully creates inline cluster with provided name", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		refs := []vmv1alpha1.VMClusterRefOrSpec{
			{Name: "my-inline-cluster", Spec: &vmv1beta1.VMClusterSpec{
				ClusterVersion: "v1.0.0",
				VMSelect: &vmv1beta1.VMSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			}},
		}

		result, err := fetchVMClusters(ctx, fakeClient, crName, "default", refs)
		assert.NoError(t, err)
		assert.Len(t, result, 1)
		expectedName := fmt.Sprintf("%s-%s", crName, "my-inline-cluster")
		assert.Equal(t, expectedName, result[0].Name)
	})

	t.Run("Error if Name is empty when Spec is set", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		refs := []vmv1alpha1.VMClusterRefOrSpec{
			{Spec: &vmv1beta1.VMClusterSpec{
				ClusterVersion: "v1.0.0",
				VMSelect: &vmv1beta1.VMSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			}},
		}

		_, err := fetchVMClusters(ctx, fakeClient, crName, "default", refs)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMClusterRefOrSpec.Name must be set when Spec is provided for index 0")
	})

	t.Run("Update existing inline cluster spec", func(t *testing.T) {
		// Initial cluster creation
		initialSpec := vmv1beta1.VMClusterSpec{
			ClusterVersion: "v1.0.0",
			VMSelect: &vmv1beta1.VMSelect{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		}
		inlineClusterName := "existing-inline-cluster"
		staticClusterName := fmt.Sprintf("%s-%s", crName, inlineClusterName)
		createdCluster := newVMCluster(staticClusterName, "default", "v1.0.0", 1, nil)
		createdCluster.Spec = initialSpec

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(createdCluster).Build()

		// Updated spec
		updatedSpec := vmv1beta1.VMClusterSpec{
			ClusterVersion: "v1.1.0", // Changed version
			VMSelect: &vmv1beta1.VMSelect{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(2)), // Changed replica count
				},
			},
		}

		refs := []vmv1alpha1.VMClusterRefOrSpec{
			{Name: inlineClusterName, Spec: &updatedSpec},
		}

		result, err := fetchVMClusters(ctx, fakeClient, crName, "default", refs)
		assert.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, staticClusterName, result[0].Name)
		assert.Equal(t, updatedSpec.ClusterVersion, result[0].Spec.ClusterVersion)
		assert.Equal(t, *updatedSpec.VMSelect.ReplicaCount, *result[0].Spec.VMSelect.ReplicaCount)
	})

	t.Run("Error if neither Ref nor Spec is set", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		refs := []vmv1alpha1.VMClusterRefOrSpec{{}}

		_, err := fetchVMClusters(ctx, fakeClient, crName, "default", refs)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMClusterRefOrSpec at index 0 must have either Ref or Spec set")
	})

	t.Run("Error if Ref.Name is empty", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		refs := []vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: ""}}}

		_, err := fetchVMClusters(ctx, fakeClient, crName, "default", refs)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMClusterRefOrSpec.Ref.Name must be set for reference at index 0")
	})

	t.Run("Error if both Ref and Spec are set", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		refs := []vmv1alpha1.VMClusterRefOrSpec{{
			Ref:  &corev1.LocalObjectReference{Name: "test-ref"},
			Spec: &vmv1beta1.VMClusterSpec{},
		}}

		_, err := fetchVMClusters(ctx, fakeClient, crName, "default", refs)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMClusterRefOrSpec at index 0 must have either Ref or Spec set, got: {Name: Ref:&LocalObjectReference{Name:test-ref,} Spec:")
	})
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

		vmAgent := &mockVMAgent{
			url:      server.URL,
			replicas: 1,
		}

		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 5*time.Second)

		assert.NoError(t, err)
		assert.True(t, callCount > 2, "Expected multiple calls to metrics endpoint")
	})

	t.Run("Returns error on http request failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate a server error
			w.WriteHeader(http.StatusInternalServerError)
		}))
		server.Close() // Close the server immediately to force connection error
		vmAgent := &mockVMAgent{
			url:      server.URL,
			replicas: 1,
		}
		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 5*time.Second)
		assert.Error(
			t, err)
		assert.Contains(t, err.Error(), "failed to fetch metrics from VMAgent")
	})

	t.Run("Returns error on invalid metric value", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "vmagent_remotewrite_pending_data_bytes invalid\n")
		}))
		defer server.Close()

		vmAgent := &mockVMAgent{
			url:      server.URL,
			replicas: 1,
		}

		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 5*time.Second)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "could not parse metric value")
	})

	t.Run("Returns error on metric not found", func(t *testing.T) {
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

		err := waitForVMClusterVMAgentMetrics(ctx, httpClient, vmAgent, 5*time.Second)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "metric vmagent_remotewrite_pending_data_bytes not found")
	})
}
