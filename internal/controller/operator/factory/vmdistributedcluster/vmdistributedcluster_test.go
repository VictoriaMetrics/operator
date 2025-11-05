package vmdistributedcluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/VictoriaMetrics/operator/api/client/versioned/scheme"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
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
	tc.Actions = append(tc.Actions, action{Method: "Update", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})
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

type customErrorClient struct {
	client.Client
	customError error
}

func (tc *customErrorClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return tc.customError
}

func (tc *customErrorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return tc.customError
}

func (tc *customErrorClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return tc.customError
}

func (tc *customErrorClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return tc.customError
}

func (tc *customErrorClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return tc.customError
}

func (tc *customErrorClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return tc.customError
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

func newVMUser(name string, targetRefs []vmv1beta1.TargetRef) *vmv1beta1.VMUser {
	return &vmv1beta1.VMUser{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: vmv1beta1.VMUserSpec{
			TargetRefs: targetRefs,
		},
	}
}

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

func beforeEach() testData {
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
	vmuser1 := newVMUser("vmuser-1", []vmv1beta1.TargetRef{
		{
			CRD:              &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "vmcluster-1", Namespace: "default"},
			TargetPathSuffix: "/select/0/prometheus/api/v1",
		},
	})
	vmuser2 := newVMUser("vmuser-2", []vmv1beta1.TargetRef{
		{
			CRD:              &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "vmcluster-2", Namespace: "default"},
			TargetPathSuffix: "/select/0/prometheus/api/v1",
		},
	})
	vmcluster1 := newVMCluster("vmcluster-1", "v1.0.0")
	vmcluster2 := newVMCluster("vmcluster-2", "v1.0.0")

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
	rclient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		vmagent,
		vmuser1,
		vmuser2,
		vmcluster1,
		vmcluster2,
		cr,
	).Build()
	tc := &trackingClient{
		Client:  rclient,
		Actions: []action{},
		objects: make(map[client.ObjectKey]client.Object),
	}
	return testData{
		vmagent:        vmagent,
		vmusers:        []*vmv1beta1.VMUser{vmuser1, vmuser2},
		vmcluster1:     vmcluster1,
		vmcluster2:     vmcluster2,
		cr:             cr,
		trackingClient: tc,
	}
}

func TestCreateOrUpdate_ErrorHandling(t *testing.T) {
	t.Run("Paused CR should do nothing", func(t *testing.T) {
		data := beforeEach()
		data.cr.Spec.Paused = true
		rclient := data.trackingClient
		ctx := context.TODO()

		err := CreateOrUpdate(ctx, data.cr, rclient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.NoError(t, err) // No error as it's paused
		assert.Empty(t, rclient.Actions)
	})

	t.Run("Missing VMAgent should return error", func(t *testing.T) {
		data := beforeEach()
		data.cr.Spec.VMAgent.Name = "non-existent"
		rclient := data.trackingClient
		ctx := context.TODO()

		err := CreateOrUpdate(ctx, data.cr, rclient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch global vmagent")
	})

	t.Run("Missing VMUser should return error", func(t *testing.T) {
		data := beforeEach()
		data.cr.Spec.VMUsers = append(data.cr.Spec.VMUsers, corev1.LocalObjectReference{Name: "non-existent-vmuser"})
		rclient := data.trackingClient
		ctx := context.TODO()

		err := CreateOrUpdate(ctx, data.cr, rclient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch vmusers")
	})

	t.Run("Missing VMCluster should return error", func(t *testing.T) {
		data := beforeEach()
		data.cr.Spec.Zones[0].Ref.Name = "non-existent-vmcluster"
		rclient := data.trackingClient
		ctx := context.TODO()

		err := CreateOrUpdate(ctx, data.cr, rclient, vmclusterWaitReadyDeadline, httpTimeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch vmclusters")
	})

	t.Run("VMClusterRefOrSpec validation errors", func(t *testing.T) {
		data := beforeEach()
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
		assert.Contains(t, err.Error(), "either VMClusterRefOrSpec.Spec or VMClusterRefOrSpec.Ref must be set for zone at index 0")

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

	vmcluster1 := newVMCluster("ref-cluster-1", "v1.0.0")
	// vmcluster2 will be an in-memory representation for an inline cluster
	vmcluster2 := newVMCluster("inline-cluster-2", "v1.0.0")

	rclient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vmcluster1).Build()

	t.Run("Fetch existing and create in-memory inline clusters", func(t *testing.T) {
		zones := []vmv1alpha1.VMClusterRefOrSpec{
			{Ref: &corev1.LocalObjectReference{Name: "ref-cluster-1"}},
			{Name: "inline-cluster-2", Spec: &vmcluster2.Spec},
		}

		fetchedClusters, err := fetchVMClusters(ctx, rclient, namespace, zones)
		assert.NoError(t, err)
		assert.Len(t, fetchedClusters, 2)
		assert.Equal(t, vmcluster1.Name, fetchedClusters[0].Name)
		assert.Equal(t, vmcluster2.Name, fetchedClusters[1].Name)
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
		_, err := fetchVMClusters(ctx, rclient, namespace, zones)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "vmclusters.operator.victoriametrics.com \"non-existent-cluster\" not found")
	})

	t.Run("Error when inline spec is invalid (e.g., missing name)", func(t *testing.T) {
		zones := []vmv1alpha1.VMClusterRefOrSpec{
			{Spec: &vmv1beta1.VMClusterSpec{}}, // Missing name for inline spec is caught by validateVMClusterRefOrSpec
		}
		_, err := fetchVMClusters(ctx, rclient, namespace, zones)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMClusterRefOrSpec.Name must be set when Spec is provided")
	})
}

func TestWaitForVMClusterVMAgentMetrics(t *testing.T) {
	t.Run("VMAgent metrics return zero", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "vm_persistentqueue_bytes_pending 0")
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
				fmt.Fprintln(w, "vm_persistentqueue_bytes_pending 100")
			} else {
				fmt.Fprintln(w, "vm_persistentqueue_bytes_pending 0")
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
			fmt.Fprintln(w, "vm_persistentqueue_bytes_pending 0")
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
		assert.Contains(t, err.Error(), "VMClusterRefOrSpec at index 0 must specify either Ref or Spec")
	})

	t.Run("Error: Neither Ref nor Spec set", func(t *testing.T) {
		refOrSpec := vmv1alpha1.VMClusterRefOrSpec{}
		err := validateVMClusterRefOrSpec(0, refOrSpec)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMClusterRefOrSpec at index 0 must have either Ref or Spec set")
	})

	t.Run("Error: Spec set but Name missing", func(t *testing.T) {
		refOrSpec := vmv1alpha1.VMClusterRefOrSpec{
			Spec: &vmv1beta1.VMClusterSpec{},
		}
		err := validateVMClusterRefOrSpec(0, refOrSpec)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMClusterRefOrSpec.Name must be set when Spec is provided for index 0")
	})

	t.Run("Error: Ref set but Name missing in Ref", func(t *testing.T) {
		refOrSpec := vmv1alpha1.VMClusterRefOrSpec{
			Ref: &corev1.LocalObjectReference{Name: ""},
		}
		err := validateVMClusterRefOrSpec(0, refOrSpec)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMClusterRefOrSpec.Ref.Name must be set for reference at index 0")
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
		assert.Equal(t, int32(2), *merged.VMSelect.ReplicaCount)
		assert.Equal(t, "custom-sa", merged.ServiceAccountName)
		assert.Equal(t, "bar", merged.VMSelect.ExtraArgs["foo"])

		// Ensure un-overridden parts remain from baseSpec
		assert.NotNil(t, merged.VMInsert)
		assert.Equal(t, *baseSpec.VMInsert.ReplicaCount, *merged.VMInsert.ReplicaCount)
		assert.NotNil(t, merged.VMStorage)
		assert.Equal(t, *baseSpec.VMStorage.ReplicaCount, *merged.VMStorage.ReplicaCount)
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
		assert.Equal(t, *baseSpec.VMSelect.ReplicaCount, *merged.VMSelect.ReplicaCount)
		// VMInsert ReplicaCount should be overridden
		assert.NotNil(t, merged.VMInsert)
		assert.Equal(t, int32(3), *merged.VMInsert.ReplicaCount)
		assert.Equal(t, *baseSpec.VMStorage.ReplicaCount, *merged.VMStorage.ReplicaCount)
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
		assert.NotNil(t, merged.VMSelect.ExtraArgs)
		assert.Equal(t, "bar", merged.VMSelect.ExtraArgs["foo"])
		// Other fields should remain from baseSpec
		assert.Equal(t, baseSpec.ClusterVersion, merged.ClusterVersion)
		assert.Equal(t, *baseSpec.VMSelect.ReplicaCount, *merged.VMSelect.ReplicaCount)
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
		assert.Equal(t, "new_value", merged.VMSelect.ExtraArgs["new_arg"])
	})

	t.Run("Override nested map with existing key change", func(t *testing.T) {
		baseSpecWithExtraArgs := baseSpec
		baseSpecWithExtraArgs.VMSelect.ExtraArgs = map[string]string{"foo": "initial_value", "bar": "baz"}

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
		assert.Equal(t, "updated_value", merged.VMSelect.ExtraArgs["foo"])
		assert.Equal(t, "baz", merged.VMSelect.ExtraArgs["bar"]) // Unchanged key remains
	})
}

func TestGetReferencedVMCluster(t *testing.T) {
	tests := []struct {
		name               string
		namespace          string
		ref                *corev1.LocalObjectReference
		existingVMClusters []*vmv1beta1.VMCluster
		expectedErr        string
		expectedVMCluster  *vmv1beta1.VMCluster
	}{
		{
			name:      "Successfully get VMCluster",
			namespace: "default",
			ref:       &corev1.LocalObjectReference{Name: "my-vmcluster"},
			existingVMClusters: []*vmv1beta1.VMCluster{
				newVMCluster("my-vmcluster", "v1"),
			},
			expectedErr:       "",
			expectedVMCluster: newVMCluster("my-vmcluster", "v1"),
		},
		{
			name:      "VMCluster not found",
			namespace: "default",
			ref:       &corev1.LocalObjectReference{Name: "non-existent-vmcluster"},
			existingVMClusters: []*vmv1beta1.VMCluster{
				newVMCluster("my-vmcluster", "v1"),
			},
			expectedErr:       "referenced VMCluster default/non-existent-vmcluster not found: vmclusters.operator.victoriametrics.com \"non-existent-vmcluster\" not found",
			expectedVMCluster: nil,
		},
		{
			name:               "Client get error",
			namespace:          "default",
			ref:                &corev1.LocalObjectReference{Name: "error-vmcluster"},
			existingVMClusters: []*vmv1beta1.VMCluster{},
			expectedErr:        "failed to get referenced VMCluster default/error-vmcluster: some arbitrary error",
			expectedVMCluster:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td := beforeEach()
			ctx := context.Background()

			var rclient client.Client
			if tt.name == "Client get error" {
				rclient = &customErrorClient{
					Client:      td.trackingClient,
					customError: fmt.Errorf("some arbitrary error"),
				}
			} else {

				td.trackingClient.mu.Lock()
				td.trackingClient.objects = make(map[client.ObjectKey]client.Object)
				for _, obj := range tt.existingVMClusters {
					td.trackingClient.objects[client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}] = obj
				}
				td.trackingClient.mu.Unlock()

				rclient = td.trackingClient
			}

			vmCluster, err := getReferencedVMCluster(ctx, rclient, tt.namespace, tt.ref)

			if tt.expectedErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.expectedErr)
				}
				if err.Error() != tt.expectedErr {
					t.Errorf("expected error %q, got %q", tt.expectedErr, err.Error())
				}
			} else if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if !reflect.DeepEqual(vmCluster, tt.expectedVMCluster) {
				t.Errorf("expected VMCluster %+v, got %+v", tt.expectedVMCluster, vmCluster)
			}
		})
	}
}

func TestWaitForVMClusterReady(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)
	_ = vmv1alpha1.AddToScheme(scheme)

	const testDeadline = 2 * time.Second

	testCases := []struct {
		name                 string
		vmCluster            *vmv1beta1.VMCluster
		setupClient          func(t *testing.T, fakeClient client.Client) client.Client // Function to customize the client behavior
		initialVMClusterFunc func(vmc *vmv1beta1.VMCluster)                             // Function to set initial status or mutate before polling
		pollResults          []vmv1beta1.UpdateStatus                                   // Sequence of status results during polling
		expectedErrSubstring string
	}{
		{
			name:      "should return nil when VMCluster becomes ready within deadline",
			vmCluster: newVMCluster("ready-cluster", "v1.0.0"),
			setupClient: func(t *testing.T, fakeClient client.Client) client.Client {
				// Simulate status change over time.
				// The first Get will return non-operational, then operational.
				return &mockClientWithPollingResponse{
					Client:      fakeClient,
					statuses:    []vmv1beta1.UpdateStatus{vmv1beta1.UpdateStatusExpanding, vmv1beta1.UpdateStatusOperational},
					statusIndex: 0,
					t:           t,
				}
			},
			initialVMClusterFunc: func(vmc *vmv1beta1.VMCluster) {
				vmc.Status.UpdateStatus = vmv1beta1.UpdateStatusExpanding
			},
			expectedErrSubstring: "",
		},
		{
			name:      "should return error if VMCluster is not found",
			vmCluster: newVMCluster("non-existent-cluster", "v1.0.0"),
			setupClient: func(t *testing.T, fakeClient client.Client) client.Client {
				// The fake client will return IsNotFound if the object is not in initial objects.
				// For this test, we expect the function under test to return an error.
				return fakeClient
			},
			initialVMClusterFunc: nil, // Ensure no initial object is added to the fake client
			expectedErrSubstring: "VMCluster not found",
		},
		{
			name:      "should return error if VMCluster remains not ready until deadline",
			vmCluster: newVMCluster("stuck-cluster", "v1.0.0"),
			setupClient: func(t *testing.T, fakeClient client.Client) client.Client {
				// Always return pending status
				return &mockClientWithPollingResponse{
					Client:      fakeClient,
					statuses:    []vmv1beta1.UpdateStatus{vmv1beta1.UpdateStatusExpanding, vmv1beta1.UpdateStatusExpanding, vmv1beta1.UpdateStatusExpanding},
					statusIndex: 0,
					t:           t,
				}
			},
			initialVMClusterFunc: func(vmc *vmv1beta1.VMCluster) {
				vmc.Status.UpdateStatus = vmv1beta1.UpdateStatusExpanding
			},
			expectedErrSubstring: "failed to wait for VMCluster default/stuck-cluster to be ready: context deadline exceeded",
		},
		{
			name:      "should return error if Get fails unexpectedly",
			vmCluster: newVMCluster("get-error-cluster", "v1.0.0"),
			setupClient: func(t *testing.T, fakeClient client.Client) client.Client {
				return &customErrorClient{
					Client:      fakeClient,
					customError: fmt.Errorf("simulated get error"),
				}
			},
			initialVMClusterFunc: func(vmc *vmv1beta1.VMCluster) {
				// Still need to create the VMCluster in the fake client for the customErrorClient to wrap
				// However, Get will still return the custom error for this specific VMCluster
			},
			expectedErrSubstring: "failed to fetch VMCluster default/get-error-cluster",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Prepare the initial VMCluster object if needed
			initialVMCluster := tc.vmCluster.DeepCopy()
			if tc.initialVMClusterFunc != nil {
				tc.initialVMClusterFunc(initialVMCluster)
			}

			// Setup fake client with initial objects
			var initialObjects []client.Object
			if tc.initialVMClusterFunc != nil {
				initialObjects = append(initialObjects, initialVMCluster)
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(initialObjects...).Build()

			// Customize client behavior based on test case
			rclient := tc.setupClient(t, fakeClient)

			ctx, cancel := context.WithTimeout(context.Background(), testDeadline+500*time.Millisecond) // Give a little extra time for the poll to time out
			defer cancel()

			// Call the function under test
			err := waitForVMClusterReady(ctx, rclient, tc.vmCluster, testDeadline)

			// Assert errors
			if tc.expectedErrSubstring != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErrSubstring)
			} else {
				assert.NoError(t, err)
				// If no error, ensure the status is operational on the returned vmCluster object (which is passed by reference)
				assert.Equal(t, vmv1beta1.UpdateStatusOperational, tc.vmCluster.Status.UpdateStatus)
			}
		})
	}
}

// mockClientWithPollingResponse is a client.Client implementation that allows controlling
// the VMCluster's UpdateStatus during Get calls to simulate polling.
type mockClientWithPollingResponse struct {
	client.Client
	statuses    []vmv1beta1.UpdateStatus
	statusIndex int
	t           *testing.T
	mu          sync.Mutex
}

func (m *mockClientWithPollingResponse) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Call the underlying fake client's Get first
	err := m.Client.Get(ctx, key, obj, opts...)
	if err != nil {
		return err
	}

	// If it's a VMCluster and we have statuses to simulate
	if vmCluster, ok := obj.(*vmv1beta1.VMCluster); ok && len(m.statuses) > 0 {
		currentStatus := m.statuses[m.statusIndex]
		vmCluster.Status.UpdateStatus = currentStatus
		m.t.Logf("MockClient: Setting VMCluster %s/%s status to %s (index %d)", key.Namespace, key.Name, currentStatus, m.statusIndex)
		if m.statusIndex < len(m.statuses)-1 {
			m.statusIndex++
		}
	}
	return nil
}

func TestFindVMUserReadRuleForVMCluster(t *testing.T) {
	// Define a VMCluster for testing
	testVMCluster := newVMCluster("test-vmcluster", "v1")

	// Define various VMUser configurations
	vmUserWithMatchingRule := newVMUser("user-match", []vmv1beta1.TargetRef{
		{
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster/vmselect",
				Name:      testVMCluster.Name,
				Namespace: testVMCluster.Namespace,
			},
			TargetPathSuffix: "/select/0", // Matching suffix
		},
	})

	vmUserWithIncorrectSuffix := newVMUser("user-wrong-suffix", []vmv1beta1.TargetRef{
		{
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster/vmselect",
				Name:      testVMCluster.Name,
				Namespace: testVMCluster.Namespace,
			},
			TargetPathSuffix: "/insert/0", // Incorrect suffix
		},
	})

	vmUserWithIncorrectKind := newVMUser("user-wrong-kind", []vmv1beta1.TargetRef{
		{
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMAgent", // Incorrect Kind
				Name:      testVMCluster.Name,
				Namespace: testVMCluster.Namespace,
			},
			TargetPathSuffix: "/select/0",
		},
	})

	vmUserWithIncorrectName := newVMUser("user-wrong-name", []vmv1beta1.TargetRef{
		{
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster/vmselect",
				Name:      "another-cluster", // Incorrect Name
				Namespace: testVMCluster.Namespace,
			},
			TargetPathSuffix: "/select/0",
		},
	})

	vmUserWithNilCRD := newVMUser("user-nil-crd", []vmv1beta1.TargetRef{
		{
			CRD:              nil, // Nil CRD
			TargetPathSuffix: "/select/0",
		},
	})

	vmUserWithEmptyTargetRefs := newVMUser("user-empty-targetrefs", []vmv1beta1.TargetRef{})

	testCases := []struct {
		name                 string
		vmUserObjs           []*vmv1beta1.VMUser
		vmCluster            *vmv1beta1.VMCluster
		expectedTargetRef    *vmv1beta1.TargetRef
		expectedErrSubstring string
	}{
		{
			name:                 "should return error if no vmusers are provided",
			vmUserObjs:           []*vmv1beta1.VMUser{},
			vmCluster:            testVMCluster,
			expectedTargetRef:    nil,
			expectedErrSubstring: "no vmuser has target refs for vmcluster test-vmcluster",
		},
		{
			name:                 "should return error if no matching vmuser exists",
			vmUserObjs:           []*vmv1beta1.VMUser{vmUserWithIncorrectKind, vmUserWithIncorrectSuffix},
			vmCluster:            testVMCluster,
			expectedTargetRef:    nil,
			expectedErrSubstring: "no vmuser has target refs for vmcluster test-vmcluster",
		},
		{
			name:                 "should return error if targetpathsuffix does not match",
			vmUserObjs:           []*vmv1beta1.VMUser{vmUserWithIncorrectSuffix},
			vmCluster:            testVMCluster,
			expectedTargetRef:    nil,
			expectedErrSubstring: "no vmuser has target refs for vmcluster test-vmcluster",
		},
		{
			name:                 "should return matching TargetRef when found",
			vmUserObjs:           []*vmv1beta1.VMUser{vmUserWithMatchingRule},
			vmCluster:            testVMCluster,
			expectedTargetRef:    &vmUserWithMatchingRule.Spec.TargetRefs[0],
			expectedErrSubstring: "",
		},
		{
			name:                 "should pick the first matching rule among multiple vmusers",
			vmUserObjs:           []*vmv1beta1.VMUser{vmUserWithIncorrectName, vmUserWithMatchingRule, vmUserWithIncorrectSuffix},
			vmCluster:            testVMCluster,
			expectedTargetRef:    &vmUserWithMatchingRule.Spec.TargetRefs[0],
			expectedErrSubstring: "",
		},
		{
			name:                 "should return error if CRD is nil in targetref",
			vmUserObjs:           []*vmv1beta1.VMUser{vmUserWithNilCRD},
			vmCluster:            testVMCluster,
			expectedTargetRef:    nil,
			expectedErrSubstring: "no vmuser has target refs for vmcluster test-vmcluster",
		},
		{
			name:                 "should return error if target refs are empty",
			vmUserObjs:           []*vmv1beta1.VMUser{vmUserWithEmptyTargetRefs},
			vmCluster:            testVMCluster,
			expectedTargetRef:    nil,
			expectedErrSubstring: "no vmuser has target refs for vmcluster test-vmcluster",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := findVMUserReadRuleForVMCluster(tc.vmUserObjs, tc.vmCluster)

			if tc.expectedErrSubstring != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expectedErrSubstring) {
					t.Fatalf("Expected error containing \"%s\", but got: %v", tc.expectedErrSubstring, err)
				}
				if result != nil {
					t.Fatalf("Expected nil TargetRef on error, but got: %+v", result)
				}
			} else {
				if err != nil {
					t.Fatalf("Did not expect an error, but got: %v", err)
				}
				if result == nil {
					t.Fatal("Expected a TargetRef, but got nil")
				}
				if !reflect.DeepEqual(result, tc.expectedTargetRef) {
					t.Errorf("Expected TargetRef %+v, but got %+v", *tc.expectedTargetRef, *result)
				}
			}
		})
	}
}

func TestUpdateVMUserTargetRefs(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(scheme)
	_ = vmv1alpha1.AddToScheme(scheme)

	newTargetRef := func(kind, name, namespace, suffix string) *vmv1beta1.TargetRef {
		return &vmv1beta1.TargetRef{
			CRD: &vmv1beta1.CRDRef{
				Kind:      kind,
				Name:      name,
				Namespace: namespace,
			},
			TargetPathSuffix: suffix,
		}
	}

	testCases := []struct {
		name                 string
		initialVMUser        *vmv1beta1.VMUser
		targetRefToUpdate    *vmv1beta1.TargetRef
		status               bool              // true for add, false for remove
		expectedVMUser       *vmv1beta1.VMUser // Expected state of the VMUser after the operation
		expectedActions      []action
		rClient              client.Client // Specific client for error cases
		expectedErrSubstring string
	}{
		{
			name: `should add targetRef if status is true and not present in refs`,
			initialVMUser: newVMUser(`test-user-add`, []vmv1beta1.TargetRef{
				*newTargetRef(`VMCluster/vmselect`, `cluster1`, `default`, `/select/0`),
			}),
			targetRefToUpdate: newTargetRef(`VMCluster/vmselect`, `cluster2`, `default`, `/select/0`),
			status:            true,
			expectedVMUser: newVMUser(`test-user-add`, []vmv1beta1.TargetRef{
				*newTargetRef(`VMCluster/vmselect`, `cluster1`, `default`, `/select/0`),
				*newTargetRef(`VMCluster/vmselect`, `cluster2`, `default`, `/select/0`),
			}),
			expectedActions: []action{
				{Method: `Get`, ObjectKey: types.NamespacedName{Name: `test-user-add`, Namespace: `default`}},
				{Method: `Update`, ObjectKey: types.NamespacedName{Name: `test-user-add`, Namespace: `default`}, Object: newVMUser(`test-user-add`, []vmv1beta1.TargetRef{
					*newTargetRef(`VMCluster/vmselect`, `cluster1`, `default`, `/select/0`),
					*newTargetRef(`VMCluster/vmselect`, `cluster2`, `default`, `/select/0`),
				})},
				{Method: `Get`, ObjectKey: types.NamespacedName{Name: `test-user-add`, Namespace: `default`}},
			},
			expectedErrSubstring: ``,
		},
		{
			name: `should not add targetRef if status is true and already present`,
			initialVMUser: newVMUser(`test-user-present`, []vmv1beta1.TargetRef{
				*newTargetRef(`VMCluster/vmselect`, `cluster1`, `default`, `/select/0`),
				*newTargetRef(`VMCluster/vmselect`, `cluster2`, `default`, `/select/0`),
			}),
			targetRefToUpdate: newTargetRef(`VMCluster/vmselect`, `cluster2`, `default`, `/select/0`),
			status:            true,
			expectedVMUser: newVMUser(`test-user-present`, []vmv1beta1.TargetRef{
				*newTargetRef(`VMCluster/vmselect`, `cluster1`, `default`, `/select/0`),
				*newTargetRef(`VMCluster/vmselect`, `cluster2`, `default`, `/select/0`),
			}),
			expectedActions: []action{
				{Method: `Get`, ObjectKey: types.NamespacedName{Name: `test-user-present`, Namespace: `default`}},
				{Method: `Get`, ObjectKey: types.NamespacedName{Name: `test-user-present`, Namespace: `default`}},
			},
			expectedErrSubstring: ``,
		},
		{
			name: `should remove targetRef if status is false and present`,
			initialVMUser: newVMUser(`test-user-remove`, []vmv1beta1.TargetRef{
				*newTargetRef(`VMCluster/vmselect`, `cluster1`, `default`, `/select/0`),
				*newTargetRef(`VMCluster/vmselect`, `cluster2`, `default`, `/select/0`),
			}),
			targetRefToUpdate: newTargetRef(`VMCluster/vmselect`, `cluster2`, `default`, `/select/0`),
			status:            false,
			expectedVMUser: newVMUser(`test-user-remove`, []vmv1beta1.TargetRef{
				*newTargetRef(`VMCluster/vmselect`, `cluster1`, `default`, `/select/0`),
			}),
			expectedActions: []action{
				{Method: `Get`, ObjectKey: types.NamespacedName{Name: `test-user-remove`, Namespace: `default`}},
				{Method: `Update`, ObjectKey: types.NamespacedName{Name: `test-user-remove`, Namespace: `default`}, Object: newVMUser(`test-user-remove`, []vmv1beta1.TargetRef{
					*newTargetRef(`VMCluster/vmselect`, `cluster1`, `default`, `/select/0`),
				})},
				{Method: `Get`, ObjectKey: types.NamespacedName{Name: `test-user-remove`, Namespace: `default`}},
			},
			expectedErrSubstring: ``,
		},
		{
			name: `should not remove targetRef if status is false and not present`,
			initialVMUser: newVMUser(`test-user-no-remove`, []vmv1beta1.TargetRef{
				*newTargetRef(`VMCluster/vmselect`, `cluster1`, `default`, `/select/0`),
			}),
			targetRefToUpdate: newTargetRef(`VMCluster/vmselect`, `cluster2`, `default`, `/select/0`),
			status:            false,
			expectedVMUser: newVMUser(`test-user-no-remove`, []vmv1beta1.TargetRef{
				*newTargetRef(`VMCluster/vmselect`, `cluster1`, `default`, `/select/0`),
			}),
			expectedActions: []action{
				{Method: `Get`, ObjectKey: types.NamespacedName{Name: `test-user-no-remove`, Namespace: `default`}},
				{Method: `Get`, ObjectKey: types.NamespacedName{Name: `test-user-no-remove`, Namespace: `default`}},
			},
			expectedErrSubstring: ``,
		},
		{
			name:                 `should return error if Get fails`,
			initialVMUser:        newVMUser(`test-user-get-fail`, []vmv1beta1.TargetRef{}), // VMUser exists, but Get fails with customErrorClient
			targetRefToUpdate:    newTargetRef(`VMCluster/vmselect`, `cluster1`, `default`, `/select/0`),
			status:               true,
			expectedVMUser:       nil,
			rClient:              &customErrorClient{customError: fmt.Errorf(`simulated get error`)},
			expectedErrSubstring: `failed to fetch vmuser test-user-get-fail`,
			expectedActions:      []action{}, // No actions recorded by trackingClient if customErrorClient directly returns error
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var rclient client.Client
			var trClient *trackingClient

			initialObjects := []client.Object{}
			if tc.initialVMUser != nil {
				initialObjects = append(initialObjects, tc.initialVMUser.DeepCopy())
			}

			// Always create a fake client for tracking, initialized with initial objects
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(initialObjects...).Build()
			trClient = &trackingClient{
				Client:  fakeClient,
				objects: make(map[client.ObjectKey]client.Object),
			}
			for _, obj := range initialObjects {
				trClient.objects[client.ObjectKeyFromObject(obj)] = obj.DeepCopyObject().(client.Object)
			}

			if customErrClient, ok := tc.rClient.(*customErrorClient); ok {
				// If customErrorClient is provided, wrap the fake client in it
				// This allows customErrorClient to simulate errors on Get/Update calls,
				// while trackingClient still tracks other operations or the state before the error.
				customErrClient.Client = fakeClient
				rclient = customErrClient
			} else {
				// Otherwise, use the tracking client as the main client
				rclient = trClient
			}

			ctx := context.Background()
			err := updateVMUserTargetRefs(ctx, rclient, tc.initialVMUser, tc.targetRefToUpdate, tc.status)

			if tc.expectedErrSubstring != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErrSubstring)
				// If error is expected, the VMUser should not have been updated successfully.
				// For Get errors, actualVMUser might not be found.
				// For Update errors, the state should be the same as initialVMUser.
			} else {
				assert.NoError(t, err)
				// Verify the state of the VMUser in the client
				actualVMUser := &vmv1beta1.VMUser{}
				err = rclient.Get(ctx, types.NamespacedName{Name: tc.initialVMUser.Name, Namespace: tc.initialVMUser.Namespace}, actualVMUser)
				assert.NoError(t, err)
				assert.True(t, reflect.DeepEqual(tc.expectedVMUser.Spec.TargetRefs, actualVMUser.Spec.TargetRefs),
					fmt.Sprintf(`Expected TargetRefs: %+v, Got: %+v`, tc.expectedVMUser.Spec.TargetRefs, actualVMUser.Spec.TargetRefs))
			}
			compareExpectedActions(t, trClient.Actions, tc.expectedActions)

			// Special handling for customErrorClient cases where trackingClient might not record all actions
			// For "should return error if Get fails", trClient.Actions would be empty because Get itself is overridden.
			// if _, ok := rclient.(*customErrorClient); ok && strings.Contains(tc.expectedErrSubstring, "failed to fetch vmuser") {
			// 	assert.Empty(t, trClient.Actions, "No actions should be recorded if Get fails immediately for customErrorClient")
			// } else {
			// 	compareExpectedActions(t, trClient.Actions, tc.expectedActions)
			// }
		})

	}
}
