package vmdistributedcluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/VictoriaMetrics/operator/api/client/versioned/scheme"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	vmclusterWaitReadyDeadline = time.Minute
	httpTimeout                = time.Second * 5
)

// action represents a client action
type action struct {
	Method    string
	ObjectKey client.ObjectKey
	Object    client.Object
}

var _ client.Client = (*trackingClient)(nil)

type trackingClient struct {
	client.Client // The embedded fake client
	Actions       []action
	objects       map[client.ObjectKey]client.Object // Store created/updated objects
	mu            sync.Mutex
}

type trackingStatusWriter struct {
	client.StatusWriter
	*trackingClient
}

func (tc *trackingClient) Status() client.StatusWriter {
	return &trackingStatusWriter{tc.Client.Status(), tc}
}

func (tc *trackingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.Actions = append(tc.Actions, action{Method: "Get", ObjectKey: key, Object: obj})

	// Try to get from our internal map first
	if storedObj, found := tc.objects[key]; found {
		// Use scheme.Scheme.Convert to safely copy the stored object into the provided empty shell
		if err := scheme.Scheme.Convert(storedObj, obj, nil); err != nil {
			return err
		}
		return nil
	}

	// Fallback to the underlying fake client
	return tc.Client.Get(ctx, key, obj, opts...)
}

func (tc *trackingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.Actions = append(tc.Actions, action{Method: "update", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})
	// Update the object in our internal map as well
	objCopy := obj.DeepCopyObject().(client.Object)
	tc.objects[client.ObjectKeyFromObject(objCopy)] = objCopy
	return tc.Client.Update(ctx, obj, opts...)
}

func (tc *trackingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.Actions = append(tc.Actions, action{Method: "Create", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})

	// Store a deep copy of the created object in our internal map
	// This makes it available for subsequent Get calls
	objCopy := obj.DeepCopyObject().(client.Object)
	tc.objects[client.ObjectKeyFromObject(objCopy)] = objCopy

	return tc.Client.Create(ctx, obj, opts...)
}

func (tc *trackingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.Actions = append(tc.Actions, action{Method: "Delete", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})
	return tc.Client.Delete(ctx, obj, opts...)
}

var _ client.StatusClient = (*trackingClient)(nil)

type alwaysFailingUpdateClient struct {
	client.Client
}

func (f *alwaysFailingUpdateClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return fmt.Errorf("simulated update failure")
}

func (tsw *trackingStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	tsw.mu.Lock()
	defer tsw.mu.Unlock()
	tsw.Actions = append(tsw.Actions, action{Method: "StatusCreate", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})
	return tsw.StatusWriter.Create(ctx, obj, subResource, opts...)
}

func (tsw *trackingStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	tsw.mu.Lock()
	defer tsw.mu.Unlock()
	tsw.Actions = append(tsw.Actions, action{Method: "StatusUpdate", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})
	return tsw.StatusWriter.Update(ctx, obj, opts...)
}

func (tsw *trackingStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	tsw.mu.Lock()
	defer tsw.mu.Unlock()
	tsw.Actions = append(tsw.Actions, action{Method: "StatusPatch", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})
	return tsw.StatusWriter.Patch(ctx, obj, patch, opts...)
}

var _ client.SubResourceWriter = (*trackingStatusWriter)(nil)

func compareExpectedActions(t *testing.T, expected []action, actual []action) {
	assert.Len(t, actual, len(expected), "number of actions should match")
	for i := range expected {
		assert.Equal(t, expected[i].Method, actual[i].Method, "method should match for action %d", i)
		assert.Equal(t, expected[i].ObjectKey, actual[i].ObjectKey, "object key should match for action %d", i)
		// Optionally compare object content more deeply if needed, but type/key is often enough
	}
}

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

func newVMCluster(name, namespace, version string, replicas int32) *vmv1beta1.VMCluster {
	return &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"tenant": "default"},
		},
		Spec: vmv1beta1.VMClusterSpec{
			ClusterVersion: version,
			VMSelect: &vmv1beta1.VMSelect{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(replicas),
				},
			},
			VMInsert: &vmv1beta1.VMInsert{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(replicas),
				},
			},
			VMStorage: &vmv1beta1.VMStorage{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(replicas),
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

func newVMDistributedCluster(name, namespace string, zones []vmv1alpha1.VMClusterRefOrSpec, vmAgentRef corev1.LocalObjectReference, vmUserRefs []corev1.LocalObjectReference) *vmv1alpha1.VMDistributedCluster {
	return &vmv1alpha1.VMDistributedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vmv1alpha1.VMDistributedClusterSpec{
			Zones:   zones,
			VMAgent: vmAgentRef,
			VMUsers: vmUserRefs,
		},
	}
}

type testData struct {
	vmagent        *vmv1beta1.VMAgent
	vmusers        []*vmv1beta1.VMUser
	vmcluster1     *vmv1beta1.VMCluster
	vmcluster2     *vmv1beta1.VMCluster
	cr             *vmv1alpha1.VMDistributedCluster
	trackingClient *trackingClient
}

func beforeEach(t *testing.T, initialObjs ...runtime.Object) testData {
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	vmagent := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vmagent", Namespace: "default"},
		Spec: vmv1beta1.VMAgentSpec{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{ReplicaCount: ptr.To(int32(1))},
		},
		Status: vmv1beta1.VMAgentStatus{Replicas: 1},
	}
	vmuser1 := newVMUser("vmuser-1", "default", []vmv1beta1.TargetRef{
		{
			CRD:              &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "vmcluster-1", Namespace: "default"},
			TargetPathSuffix: "/select/0/prometheus/api/v1",
		},
	})
	vmuser2 := newVMUser("vmuser-2", "default", []vmv1beta1.TargetRef{
		{
			CRD:              &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "vmcluster-2", Namespace: "default"},
			TargetPathSuffix: "/select/0/prometheus/api/v1",
		},
	})
	vmcluster1 := newVMCluster("vmcluster-1", "default", "v1.0.0", 1)
	vmcluster2 := newVMCluster("vmcluster-2", "default", "v1.0.0", 1)

	zones := []vmv1alpha1.VMClusterRefOrSpec{
		{Ref: &corev1.LocalObjectReference{Name: "vmcluster-1"}},
		{Ref: &corev1.LocalObjectReference{Name: "vmcluster-2"}},
	}
	vmAgentRef := corev1.LocalObjectReference{Name: vmagent.Name}
	vmUserRefs := []corev1.LocalObjectReference{
		{Name: vmuser1.Name},
		{Name: vmuser2.Name},
	}
	cr := newVMDistributedCluster("test-vdc", "default", zones, vmAgentRef, vmUserRefs)

	// Create a new trackingClient
	tc := &trackingClient{
		Client:  fake.NewClientBuilder().WithScheme(scheme).WithObjects(vmagent, vmuser1, vmuser2, vmcluster1, vmcluster2).WithRuntimeObjects(initialObjs...).Build(),
		Actions: []action{},
		objects: make(map[client.ObjectKey]client.Object),
	}

	// Populate the trackingClient's internal objects map with initial objects
	for _, obj := range initialObjs {
		if co, ok := obj.(client.Object); ok {
			tc.objects[client.ObjectKeyFromObject(co)] = co.DeepCopyObject().(client.Object)
		}
	}

	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		vmagent,
		vmuser1,
		vmuser2,
		vmcluster1,
		vmcluster2,
		cr,
	)

	var clientObjs []client.Object
	for _, obj := range initialObjs {
		clientObjs = append(clientObjs, obj.(client.Object))
	}
	rclient := builder.WithObjects(clientObjs...).Build()

	trackingClient := &trackingClient{Client: rclient}

	return testData{
		vmagent:        vmagent,
		vmusers:        []*vmv1beta1.VMUser{vmuser1, vmuser2},
		vmcluster1:     vmcluster1,
		vmcluster2:     vmcluster2,
		cr:             cr,
		trackingClient: trackingClient,
	}
}

func TestCreateOrUpdate_ErrorHandling(t *testing.T) {
	t.Run("Paused CR should do nothing", func(t *testing.T) {
		data := beforeEach(t)
		data.cr.Spec.Paused = true
		rclient := data.trackingClient
		ctx := context.TODO()

		err := CreateOrUpdate(ctx, data.cr, rclient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.NoError(t, err) // No error as it's paused
		assert.Empty(t, rclient.Actions)
	})

	t.Run("Missing VMAgent should return error", func(t *testing.T) {
		data := beforeEach(t)
		data.cr.Spec.VMAgent.Name = "non-existent"
		rclient := data.trackingClient
		ctx := context.TODO()

		err := CreateOrUpdate(ctx, data.cr, rclient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch global vmagent")
	})

	t.Run("Missing VMUser should return error", func(t *testing.T) {
		data := beforeEach(t)
		data.cr.Spec.VMUsers = append(data.cr.Spec.VMUsers, corev1.LocalObjectReference{Name: "non-existent-vmuser"})
		rclient := data.trackingClient
		ctx := context.TODO()

		err := CreateOrUpdate(ctx, data.cr, rclient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch vmusers")
	})

	t.Run("Missing VMCluster should return error", func(t *testing.T) {
		data := beforeEach(t)
		data.cr.Spec.Zones[0].Ref.Name = "non-existent-vmcluster"
		rclient := data.trackingClient
		ctx := context.TODO()

		err := CreateOrUpdate(ctx, data.cr, rclient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch vmclusters")
	})

	t.Run("VMClusterRefOrSpec validation errors", func(t *testing.T) {
		data := beforeEach(t)
		rclient := data.trackingClient
		ctx := context.TODO()

		// Both Ref and Spec set
		data.cr.Spec.Zones = []vmv1alpha1.VMClusterRefOrSpec{
			{
				Name: "vmcluster-1",
				Ref:  &corev1.LocalObjectReference{Name: "vmcluster-1"},
				Spec: &vmv1beta1.VMClusterSpec{},
			},
		}
		err := CreateOrUpdate(ctx, data.cr, rclient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Either VMClusterRefOrSpec.Spec or VMClusterRefOrSpec.Ref must be set for zone at index 0")

		// Neither Ref nor Spec set
		data.cr.Spec.Zones = []vmv1alpha1.VMClusterRefOrSpec{
			{},
		}
		err = CreateOrUpdate(ctx, data.cr, rclient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMClusterRefOrSpec.Spec or VMClusterRefOrSpec.Ref must be set for zone at index 0")

		// Spec provided but Name missing
		data.cr.Spec.Zones = []vmv1alpha1.VMClusterRefOrSpec{
			{Spec: &vmv1beta1.VMClusterSpec{}},
		}
		err = CreateOrUpdate(ctx, data.cr, rclient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMClusterRefOrSpec.Name must be set when Spec is provided for zone at index 0")
	})
}

func TestGetGenerationsFromStatus(t *testing.T) {
	status := &vmv1alpha1.VMDistributedClusterStatus{
		VMClusterInfo: []vmv1alpha1.VMClusterStatus{
			{VMClusterName: "cluster1", Generation: 1},
			{VMClusterName: "cluster2", Generation: 5},
		},
	}
	generations := getGenerationsFromStatus(status)
	assert.Equal(t, map[string]int64{"cluster1": 1, "cluster2": 5}, generations)

	emptyStatus := &vmv1alpha1.VMDistributedClusterStatus{}
	emptyGenerations := getGenerationsFromStatus(emptyStatus)
	assert.Empty(t, emptyGenerations)
}

func TestFetchVMClusters(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	ctx := context.TODO()
	namespace := "default"
	crName := "test-vdc"

	vmcluster1 := newVMCluster("ref-cluster-1", namespace, "v1.0.0", 1)
	// vmcluster2 will be an in-memory representation for an inline cluster
	vmcluster2 := newVMCluster("inline-cluster-2", namespace, "v1.0.0", 1)

	rclient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vmcluster1).Build()

	t.Run("Fetch existing and create in-memory inline clusters", func(t *testing.T) {
		zones := []vmv1alpha1.VMClusterRefOrSpec{
			{Ref: &corev1.LocalObjectReference{Name: "ref-cluster-1"}},
			{Name: "inline-cluster-2", Spec: &vmcluster2.Spec},
		}

		fetchedClusters, err := fetchVMClusters(ctx, rclient, crName, namespace, zones)
		assert.NoError(t, err)
		assert.Len(t, fetchedClusters, 2)
		assert.Equal(t, vmcluster1.Name, fetchedClusters[0].Name)
		// Inline cluster gets CR name prefix, and it's an in-memory object, not created in fake client yet.
		assert.Equal(t, fmt.Sprintf("%s-%d", crName, 1), fetchedClusters[1].Name)
		assert.Equal(t, vmcluster2.Spec, fetchedClusters[1].Spec)

		// Verify inline cluster was NOT actually created in the fake client
		createdInline := &vmv1beta1.VMCluster{}
		err = rclient.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-%d", crName, 1), Namespace: namespace}, createdInline)
		assert.True(t, k8serrors.IsNotFound(err))
	})

	t.Run("Error when referenced cluster not found", func(t *testing.T) {
		zones := []vmv1alpha1.VMClusterRefOrSpec{
			{Ref: &corev1.LocalObjectReference{Name: "non-existent-cluster"}},
		}
		_, err := fetchVMClusters(ctx, rclient, crName, namespace, zones)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "vmclusters.operator.victoriametrics.com \"non-existent-cluster\" not found")
	})

	t.Run("Error when inline spec is invalid (e.g., missing name)", func(t *testing.T) {
		zones := []vmv1alpha1.VMClusterRefOrSpec{
			{Spec: &vmv1beta1.VMClusterSpec{}}, // Missing name for inline spec is caught by validateVMClusterRefOrSpec
		}
		_, err := fetchVMClusters(ctx, rclient, crName, namespace, zones)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMClusterRefOrSpec.Name must be set when Spec is provided")
	})
}

func TestWaitForVMClusterVMAgentMetrics(t *testing.T) {
	t.Run("VMAgent metrics return zero", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "vmagent_remotewrite_pending_data_bytes 0")
		}))
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		mockVMAgent := &mockVMAgent{url: ts.URL, replicas: 1}
		err := waitForVMClusterVMAgentMetrics(ctx, ts.Client(), mockVMAgent, time.Second)
		assert.NoError(t, err)
	})

	t.Run("VMAgent metrics return non-zero then zero", func(t *testing.T) {
		callCount := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				fmt.Fprintln(w, "vmagent_remotewrite_pending_data_bytes 100")
			} else {
				fmt.Fprintln(w, "vmagent_remotewrite_pending_data_bytes 0")
			}
		}))
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		mockVMAgent := &mockVMAgent{url: ts.URL, replicas: 1}
		err := waitForVMClusterVMAgentMetrics(ctx, ts.Client(), mockVMAgent, time.Second*2)
		assert.NoError(t, err)
		assert.True(t, callCount > 1) // Ensure it polled multiple times
	})

	t.Run("VMAgent metrics timeout", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second) // Simulate a long response
			fmt.Fprintln(w, "vmagent_remotewrite_pending_data_bytes 0")
		}))
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		mockVMAgent := &mockVMAgent{url: ts.URL, replicas: 1}
		err := waitForVMClusterVMAgentMetrics(ctx, ts.Client(), mockVMAgent, 500*time.Millisecond) // Shorter deadline
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to wait for VMAgent metrics")
	})
}

func TestValidateVMClusterRefOrSpec(t *testing.T) {
	t.Run("Valid Ref", func(t *testing.T) {
		refOrSpec := vmv1alpha1.VMClusterRefOrSpec{
			Ref: &corev1.LocalObjectReference{Name: "cluster-name"},
		}
		err := validateVMClusterRefOrSpec(0, refOrSpec)
		assert.NoError(t, err)
	})

	t.Run("Valid Spec", func(t *testing.T) {
		refOrSpec := vmv1alpha1.VMClusterRefOrSpec{
			Name: "new-cluster",
			Spec: &vmv1beta1.VMClusterSpec{},
		}
		err := validateVMClusterRefOrSpec(0, refOrSpec)
		assert.NoError(t, err)
	})

	t.Run("Error: Both Ref and Spec set", func(t *testing.T) {
		refOrSpec := vmv1alpha1.VMClusterRefOrSpec{
			Ref:  &corev1.LocalObjectReference{Name: "cluster-name"},
			Name: "new-cluster",
			Spec: &vmv1beta1.VMClusterSpec{},
		}
		err := validateVMClusterRefOrSpec(0, refOrSpec)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must have either Ref or Spec set")
	})

	t.Run("Error: Neither Ref nor Spec set", func(t *testing.T) {
		refOrSpec := vmv1alpha1.VMClusterRefOrSpec{}
		err := validateVMClusterRefOrSpec(0, refOrSpec)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must have either Ref or Spec set")
	})

	t.Run("Error: Spec set but Name missing", func(t *testing.T) {
		refOrSpec := vmv1alpha1.VMClusterRefOrSpec{
			Spec: &vmv1beta1.VMClusterSpec{},
		}
		err := validateVMClusterRefOrSpec(0, refOrSpec)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Name must be set when Spec is provided")
	})

	t.Run("Error: Ref set but Name missing in Ref", func(t *testing.T) {
		refOrSpec := vmv1alpha1.VMClusterRefOrSpec{
			Ref: &corev1.LocalObjectReference{Name: ""},
		}
		err := validateVMClusterRefOrSpec(0, refOrSpec)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Ref.Name must be set for reference")
	})

	t.Run("Error: Spec and OverrideSpec set", func(t *testing.T) {
		overrideSpecVMClusterSpec := vmv1beta1.VMClusterSpec{}
		overrideSpecJSON, err := json.Marshal(overrideSpecVMClusterSpec)
		assert.NoError(t, err)
		overrideSpec := &apiextensionsv1.JSON{Raw: overrideSpecJSON}

		refOrSpec := vmv1alpha1.VMClusterRefOrSpec{
			Name:         "new-cluster",
			Spec:         &vmv1beta1.VMClusterSpec{},
			OverrideSpec: overrideSpec,
		}
		err = validateVMClusterRefOrSpec(0, refOrSpec)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot have both Spec and OverrideSpec set")
	})
}

func TestApplyOverrideSpec(t *testing.T) {
	baseSpec := vmv1beta1.VMClusterSpec{
		ClusterVersion: "v1.0.0",
		VMSelect: &vmv1beta1.VMSelect{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptr.To(int32(1)),
				ExtraArgs:    map[string]string{},
			},
		},
		VMInsert: &vmv1beta1.VMInsert{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptr.To(int32(1)),
				ExtraArgs:    map[string]string{},
			},
		},
		VMStorage: &vmv1beta1.VMStorage{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptr.To(int32(1)),
				ExtraArgs:    map[string]string{},
			},
		},
		ServiceAccountName: "default-sa",
	}

	t.Run("Full override", func(t *testing.T) {
		overrideSpecVMClusterSpec := vmv1beta1.VMClusterSpec{
			ClusterVersion: "v1.1.0",
			VMSelect: &vmv1beta1.VMSelect{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(2)),
					ExtraArgs:    map[string]string{"foo": "bar"},
				},
			},
			ServiceAccountName: "custom-sa",
		}
		overrideSpecJSON, err := json.Marshal(overrideSpecVMClusterSpec)
		assert.NoError(t, err)
		overrideSpec := &apiextensionsv1.JSON{Raw: overrideSpecJSON}

		merged, modified, err := ApplyOverrideSpec(baseSpec, overrideSpec)
		assert.NoError(t, err)
		assert.True(t, modified)
		assert.Equal(t, "v1.1.0", merged.ClusterVersion)
		assert.Equal(t, int32(2), *merged.VMSelect.CommonApplicationDeploymentParams.ReplicaCount)
		assert.Equal(t, "custom-sa", merged.ServiceAccountName)
		assert.Equal(t, "bar", merged.VMSelect.CommonApplicationDeploymentParams.ExtraArgs["foo"])

		// Ensure un-overridden parts remain from baseSpec
		assert.NotNil(t, merged.VMInsert)
		assert.Equal(t, *baseSpec.VMInsert.CommonApplicationDeploymentParams.ReplicaCount, *merged.VMInsert.CommonApplicationDeploymentParams.ReplicaCount)
		assert.NotNil(t, merged.VMStorage)
		assert.Equal(t, *baseSpec.VMStorage.CommonApplicationDeploymentParams.ReplicaCount, *merged.VMStorage.CommonApplicationDeploymentParams.ReplicaCount)
	})

	t.Run("Partial override (nil fields ignored)", func(t *testing.T) {
		overrideSpecVMClusterSpec := vmv1beta1.VMClusterSpec{
			ClusterVersion: "v1.2.0",
			VMSelect:       nil, // Should not override base VMSelect
			VMInsert: &vmv1beta1.VMInsert{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(3)),
				},
			},
		}
		overrideSpecJSON, err := json.Marshal(overrideSpecVMClusterSpec)
		assert.NoError(t, err)
		overrideSpec := &apiextensionsv1.JSON{Raw: overrideSpecJSON}

		merged, modified, err := ApplyOverrideSpec(baseSpec, overrideSpec)
		assert.NoError(t, err)
		assert.True(t, modified)
		assert.Equal(t, "v1.2.0", merged.ClusterVersion)
		// VMSelect should be from baseSpec
		assert.NotNil(t, merged.VMSelect)
		assert.Equal(t, *baseSpec.VMSelect.CommonApplicationDeploymentParams.ReplicaCount, *merged.VMSelect.CommonApplicationDeploymentParams.ReplicaCount)
		// VMInsert ReplicaCount should be overridden
		assert.NotNil(t, merged.VMInsert)
		assert.Equal(t, int32(3), *merged.VMInsert.CommonApplicationDeploymentParams.ReplicaCount)
		assert.Equal(t, *baseSpec.VMStorage.CommonApplicationDeploymentParams.ReplicaCount, *merged.VMStorage.CommonApplicationDeploymentParams.ReplicaCount)
	})

	t.Run("Empty override spec", func(t *testing.T) {
		overrideSpec := &apiextensionsv1.JSON{Raw: []byte{}}

		merged, modified, err := ApplyOverrideSpec(baseSpec, overrideSpec)
		assert.NoError(t, err)
		assert.False(t, modified) // No actual changes
		assert.Equal(t, baseSpec, merged)
	})

	t.Run("Override with new fields (should be added)", func(t *testing.T) {
		overrideSpecVMClusterSpec := vmv1beta1.VMClusterSpec{
			VMSelect: &vmv1beta1.VMSelect{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ExtraArgs: map[string]string{"foo": "bar"},
				},
			},
		}
		overrideSpecJSON, err := json.Marshal(overrideSpecVMClusterSpec)
		assert.NoError(t, err)
		overrideSpec := &apiextensionsv1.JSON{Raw: overrideSpecJSON}

		merged, modified, err := ApplyOverrideSpec(baseSpec, overrideSpec)
		assert.NoError(t, err)
		assert.True(t, modified)
		assert.NotNil(t, merged.VMSelect.CommonApplicationDeploymentParams.ExtraArgs)
		assert.Equal(t, "bar", merged.VMSelect.CommonApplicationDeploymentParams.ExtraArgs["foo"])
		// Other fields should remain from baseSpec
		assert.Equal(t, baseSpec.ClusterVersion, merged.ClusterVersion)
		assert.Equal(t, *baseSpec.VMSelect.CommonApplicationDeploymentParams.ReplicaCount, *merged.VMSelect.CommonApplicationDeploymentParams.ReplicaCount)
	})

	t.Run("Override nested map with new key", func(t *testing.T) {
		overrideSpecVMClusterSpec := vmv1beta1.VMClusterSpec{
			VMSelect: &vmv1beta1.VMSelect{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ExtraArgs: map[string]string{"new_arg": "new_value"},
				},
			},
		}
		overrideSpecJSON, err := json.Marshal(overrideSpecVMClusterSpec)
		assert.NoError(t, err)
		overrideSpec := &apiextensionsv1.JSON{Raw: overrideSpecJSON}

		merged, modified, err := ApplyOverrideSpec(baseSpec, overrideSpec)
		assert.NoError(t, err)
		assert.True(t, modified)
		assert.Equal(t, "new_value", merged.VMSelect.CommonApplicationDeploymentParams.ExtraArgs["new_arg"])
	})

	t.Run("Override nested map with existing key change", func(t *testing.T) {
		baseSpecWithExtraArgs := baseSpec
		baseSpecWithExtraArgs.VMSelect.CommonApplicationDeploymentParams.ExtraArgs = map[string]string{"foo": "initial_value", "bar": "baz"}

		overrideSpecVMClusterSpec := vmv1beta1.VMClusterSpec{
			VMSelect: &vmv1beta1.VMSelect{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ExtraArgs: map[string]string{"foo": "updated_value"},
				},
			},
		}
		overrideSpecJSON, err := json.Marshal(overrideSpecVMClusterSpec)
		assert.NoError(t, err)
		overrideSpec := &apiextensionsv1.JSON{Raw: overrideSpecJSON}

		merged, modified, err := ApplyOverrideSpec(baseSpecWithExtraArgs, overrideSpec)
		assert.NoError(t, err)
		assert.True(t, modified)
		assert.Equal(t, "updated_value", merged.VMSelect.CommonApplicationDeploymentParams.ExtraArgs["foo"])
		assert.Equal(t, "baz", merged.VMSelect.CommonApplicationDeploymentParams.ExtraArgs["bar"]) // Unchanged key remains
	})
}
