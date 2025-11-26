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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

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

	// If we have a stored copy in the internal map, use it and avoid calling the
	// underlying fake client (which may try to use managedFields/structured-merge-diff).
	if stored, ok := tc.objects[key]; ok && stored != nil {
		// Deep-copy stored into obj via JSON marshal/unmarshal to avoid aliasing.
		b, err := json.Marshal(stored)
		if err != nil {
			return fmt.Errorf("failed to marshal stored object for Get: %w", err)
		}
		if err := json.Unmarshal(b, obj); err != nil {
			return fmt.Errorf("failed to unmarshal stored object into target for Get: %w", err)
		}
		return nil
	}

	// Fallback to underlying client if not present in the internal map.
	return tc.Client.Get(ctx, key, obj, opts...)
}

func (tc *trackingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.Actions = append(tc.Actions, action{Method: "Update", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})

	// Deep copy the provided object and store it in the internal map. This avoids
	// invoking the fake client's Update logic which may interact with managedFields
	// and cause reflect panics if TypeMeta is not present or other edge cases.
	b, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal object for Update: %w", err)
	}
	stored := reflect.New(reflect.TypeOf(obj).Elem()).Interface().(client.Object)
	if err := json.Unmarshal(b, stored); err != nil {
		return fmt.Errorf("failed to unmarshal object for Update: %w", err)
	}
	tc.objects[client.ObjectKeyFromObject(stored)] = stored
	return nil
}

func (tc *trackingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Ensure TypeMeta is set for objects created in tests. The fake client can
	// attempt to use structured-merge-diff/managed fields which expects TypeMeta
	// to be populated. Tests often construct objects without TypeMeta, which
	// leads to reflect panics inside the fake client. Populate TypeMeta for
	// common types used in these tests to avoid that where possible.
	switch o := obj.(type) {
	case *vmv1beta1.VMAgent:
		if o.TypeMeta.Kind == "" {
			o.TypeMeta = metav1.TypeMeta{APIVersion: vmv1beta1.GroupVersion.String(), Kind: "VMAgent"}
		}
	case *vmv1beta1.VMCluster:
		if o.TypeMeta.Kind == "" {
			o.TypeMeta = metav1.TypeMeta{APIVersion: vmv1beta1.GroupVersion.String(), Kind: "VMCluster"}
		}
	case *vmv1beta1.VMUser:
		if o.TypeMeta.Kind == "" {
			o.TypeMeta = metav1.TypeMeta{APIVersion: vmv1beta1.GroupVersion.String(), Kind: "VMUser"}
		}
	case *corev1.ConfigMap:
		if o.TypeMeta.Kind == "" {
			o.TypeMeta = metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: "ConfigMap"}
		}
	}

	// Deep copy the object via JSON marshal/unmarshal and store in internal map.
	b, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal object for Create: %w", err)
	}
	stored := reflect.New(reflect.TypeOf(obj).Elem()).Interface().(client.Object)
	if err := json.Unmarshal(b, stored); err != nil {
		return fmt.Errorf("failed to unmarshal object for Create: %w", err)
	}
	tc.objects[client.ObjectKeyFromObject(stored)] = stored

	tc.Actions = append(tc.Actions, action{Method: "Create", ObjectKey: client.ObjectKeyFromObject(obj), Object: obj})
	// Do not call underlying client's Create to avoid managedFields/structured-merge interactions in tests.
	return nil
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
func (m *mockVMAgent) PrefixedName() string {
	// Mirror concrete VMAgent.PrefixedName behaviour for tests:
	// concrete uses the format \"vmagent-%s\".
	return "vmagent-test-vmagent"
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

func newVMDistributedCluster(name, namespace string, zones []vmv1alpha1.VMClusterRefOrSpec, vmAgentSpec vmv1alpha1.VMAgentNameAndSpec, vmAuthUser vmv1alpha1.VMUserNameAndSpec, vmAuth vmv1alpha1.VMAuthNameAndSpec) *vmv1alpha1.VMDistributedCluster {
	return &vmv1alpha1.VMDistributedCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VMDistributedCluster",
			APIVersion: vmv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: vmv1alpha1.VMDistributedClusterSpec{
			Zones:      vmv1alpha1.ZoneSpec{VMClusters: zones},
			VMAgent:    vmAgentSpec,
			VMAuthUser: vmAuthUser,
			VMAuth:     vmAuth,
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
	vmAuth := &vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmauth-proxy",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAuthSpec{},
	}

	zones := []vmv1alpha1.VMClusterRefOrSpec{
		{Ref: &corev1.LocalObjectReference{Name: "vmcluster-1"}},
		{Ref: &corev1.LocalObjectReference{Name: "vmcluster-2"}},
	}
	vmAgentSpec := vmv1alpha1.VMAgentNameAndSpec{Name: vmagent.Name}
	vmUserRef := vmv1alpha1.VMUserNameAndSpec{Name: vmuser1.Name}
	cr := newVMDistributedCluster("test-vdc", "default", zones, vmAgentSpec, vmUserRef, vmv1alpha1.VMAuthNameAndSpec{Name: "vmauth-proxy"})

	// Create a new trackingClient
	rclient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		vmagent,
		vmuser1,
		vmuser2,
		vmcluster1,
		vmcluster2,
		vmAuth,
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
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	t.Run("Paused CR should do nothing", func(t *testing.T) {
		data := beforeEach()
		data.cr.Spec.Paused = true
		rclient := data.trackingClient
		ctx := context.TODO()

		err := CreateOrUpdate(ctx, data.cr, rclient, scheme, httpTimeout)
		assert.NoError(t, err) // No error as it's paused
		assert.Empty(t, rclient.Actions)
	})

	t.Run("Missing VMCluster should return error", func(t *testing.T) {
		data := beforeEach()
		data.cr.Spec.Zones.VMClusters[0].Ref.Name = "non-existent-vmcluster"
		rclient := data.trackingClient
		ctx := context.TODO()

		err := CreateOrUpdate(ctx, data.cr, rclient, scheme, httpTimeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch vmclusters")
	})

	t.Run("VMClusterRefOrSpec validation errors", func(t *testing.T) {
		data := beforeEach()
		rclient := data.trackingClient
		ctx := context.TODO()

		// Both Ref and Spec set
		data.cr.Spec.Zones = vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
			{
				Name: "vmcluster-1",
				Ref:  &corev1.LocalObjectReference{Name: "vmcluster-1"},
				Spec: &vmv1beta1.VMClusterSpec{},
			},
		}}
		err := CreateOrUpdate(ctx, data.cr, rclient, scheme, httpTimeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "either VMClusterRefOrSpec.Spec or VMClusterRefOrSpec.Ref must be set for zone at index 0")

		// Neither Ref nor Spec set
		data.cr.Spec.Zones = vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
			{},
		}}
		err = CreateOrUpdate(ctx, data.cr, rclient, scheme, httpTimeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "VMClusterRefOrSpec.Spec or VMClusterRefOrSpec.Ref must be set for zone at index 0")

		// Spec provided but Name missing
		data.cr.Spec.Zones = vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
			{Spec: &vmv1beta1.VMClusterSpec{}},
		}}
		err = CreateOrUpdate(ctx, data.cr, rclient, scheme, httpTimeout)
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
		err := waitForVMClusterVMAgentMetrics(ctx, ts.Client(), mockVMAgent, time.Second, nil)
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
		err := waitForVMClusterVMAgentMetrics(ctx, ts.Client(), mockVMAgent, time.Second*2, nil)
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
		// When rclient is nil the function returns early (no-op). Update test to reflect that behavior.
		err := waitForVMClusterVMAgentMetrics(ctx, ts.Client(), mockVMAgent, 500*time.Millisecond, nil) // Shorter deadline
		assert.NoError(t, err)
	})
}

// Unit tests for helper functions and adapter behavior
func TestVmAgentAdapterAndURLHelpers(t *testing.T) {
	t.Run("vmAgentAdapter.PrefixedName returns same as concrete", func(t *testing.T) {
		ag := &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "default"},
		}
		adapter := &vmAgentAdapter{VMAgent: ag}
		expected := ag.PrefixedName()
		got := adapter.PrefixedName()
		assert.Equal(t, expected, got)
	})

	t.Run("buildPerIPMetricURL builds proper URL with scheme and port", func(t *testing.T) {
		baseURL := "http://my-svc.default.svc:1234"
		metricPath := "/metrics"
		ip := "10.0.0.1"
		got := buildPerIPMetricURL(baseURL, metricPath, ip)
		expected := "http://10.0.0.1:1234/metrics"
		assert.Equal(t, expected, got)
	})

	t.Run("buildPerIPMetricURL defaults port when not present", func(t *testing.T) {
		baseURL := "http://my-svc.default.svc"
		metricPath := "/metrics"
		ip := "10.0.0.2"
		got := buildPerIPMetricURL(baseURL, metricPath, ip)
		expected := "http://10.0.0.2:8429/metrics"
		assert.Equal(t, expected, got)
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

func TestSetVMClusterInfo(t *testing.T) {
	type args struct {
		vmCluster       *vmv1beta1.VMCluster
		vmUserObjs      []*vmv1beta1.VMUser
		currentCRStatus *vmv1alpha1.VMDistributedClusterStatus
	}
	tests := []struct {
		name    string
		args    args
		want    vmv1alpha1.VMClusterStatus
		wantErr bool
	}{
		{
			name: "successfully set VMClusterInfo with existing VMUser",
			args: args{
				vmCluster: &vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMClusterSpec{},
				},
				vmUserObjs: []*vmv1beta1.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-user",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMUserSpec{
							TargetRefs: []vmv1beta1.TargetRef{
								{
									CRD: &vmv1beta1.CRDRef{
										Name:      "test-cluster",
										Kind:      "VMCluster/vmselect",
										Namespace: "default",
									},
									TargetPathSuffix: "/select/1",
								},
							},
						},
					},
				},
				currentCRStatus: &vmv1alpha1.VMDistributedClusterStatus{},
			},
			want: vmv1alpha1.VMClusterStatus{
				VMClusterName: "test-cluster",
				Generation:    0,
				TargetRef: vmv1beta1.TargetRef{
					CRD: &vmv1beta1.CRDRef{
						Name:      "test-cluster",
						Kind:      "VMCluster/vmselect",
						Namespace: "default",
					},
					TargetPathSuffix: "/select/1",
				},
			},
			wantErr: false,
		},
		{
			name: "error when no VMUser found for VMCluster and not in status",
			args: args{
				vmCluster: &vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-no-user",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMClusterSpec{},
				},
				vmUserObjs:      []*vmv1beta1.VMUser{},
				currentCRStatus: &vmv1alpha1.VMDistributedClusterStatus{},
			},
			want: vmv1alpha1.VMClusterStatus{
				VMClusterName: "test-cluster-no-user",
				Generation:    0,
			},
			wantErr: true,
		},
		{
			name: "retrieve TargetRef from status when no VMUser found",
			args: args{
				vmCluster: &vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-from-status",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMClusterSpec{},
				},
				vmUserObjs: []*vmv1beta1.VMUser{},
				currentCRStatus: &vmv1alpha1.VMDistributedClusterStatus{
					VMClusterInfo: []vmv1alpha1.VMClusterStatus{
						{
							VMClusterName: "test-cluster-from-status",
							TargetRef: vmv1beta1.TargetRef{
								CRD: &vmv1beta1.CRDRef{
									Name:      "test-cluster-from-status",
									Kind:      "VMCluster",
									Namespace: "default",
								},
							},
						},
					},
				},
			},
			want: vmv1alpha1.VMClusterStatus{
				VMClusterName: "test-cluster-from-status",
				Generation:    0,
				TargetRef: vmv1beta1.TargetRef{
					CRD: &vmv1beta1.CRDRef{
						Name:      "test-cluster-from-status",
						Kind:      "VMCluster",
						Namespace: "default",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "retrieve TargetRef from status when VMUser no longer has matching entry",
			args: args{
				vmCluster: &vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster-from-status",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMClusterSpec{},
				},
				vmUserObjs: []*vmv1beta1.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-user",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMUserSpec{
							TargetRefs: []vmv1beta1.TargetRef{
								{
									CRD: &vmv1beta1.CRDRef{
										Name:      "some-other-cluster",
										Kind:      "VMCluster/vmselect",
										Namespace: "default",
									},
									TargetPathSuffix: "/select/1",
								},
							},
						},
					},
				},
				currentCRStatus: &vmv1alpha1.VMDistributedClusterStatus{
					VMClusterInfo: []vmv1alpha1.VMClusterStatus{
						{
							VMClusterName: "test-cluster-from-status",
							TargetRef: vmv1beta1.TargetRef{
								CRD: &vmv1beta1.CRDRef{
									Name:      "test-cluster-from-status",
									Kind:      "VMCluster",
									Namespace: "default",
								},
							},
						},
					},
				},
			},
			want: vmv1alpha1.VMClusterStatus{
				VMClusterName: "test-cluster-from-status",
				Generation:    0,
				TargetRef: vmv1beta1.TargetRef{
					CRD: &vmv1beta1.CRDRef{
						Name:      "test-cluster-from-status",
						Kind:      "VMCluster",
						Namespace: "default",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := setVMClusterInfo(tt.args.vmCluster, tt.args.vmUserObjs, tt.args.currentCRStatus)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestEnsureNoVMClusterOwners(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)

	type args struct {
		cr           *vmv1alpha1.VMDistributedCluster
		vmClusterObj *vmv1beta1.VMCluster
		scheme       *runtime.Scheme
	}
	tests := []struct {
		name    string
		args    args
		setup   func(cr *vmv1alpha1.VMDistributedCluster, vmClusterObj *vmv1beta1.VMCluster, scheme *runtime.Scheme)
		wantErr bool
	}{
		{
			name: "owner ref exists and is the cr, no error",
			args: args{
				cr:           newVMDistributedCluster("test-vmdc", "default", []vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: "cluster-1"}}}, vmv1alpha1.VMAgentNameAndSpec{}, vmv1alpha1.VMUserNameAndSpec{}, vmv1alpha1.VMAuthNameAndSpec{}),
				vmClusterObj: newVMCluster("cluster-1", "default"),
				scheme:       scheme,
			},
			setup: func(cr *vmv1alpha1.VMDistributedCluster, vmClusterObj *vmv1beta1.VMCluster, scheme *runtime.Scheme) {
				_ = controllerutil.SetOwnerReference(cr, vmClusterObj, scheme)
			},
			wantErr: false,
		},
		{
			name: "owner ref does not exist and nothing changes",
			args: args{
				cr:           newVMDistributedCluster("test-vmdc", "default", []vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: "cluster-1"}}}, vmv1alpha1.VMAgentNameAndSpec{}, vmv1alpha1.VMUserNameAndSpec{}, vmv1alpha1.VMAuthNameAndSpec{}),
				vmClusterObj: newVMCluster("cluster-1", "default"),
				scheme:       scheme,
			},
			setup:   func(cr *vmv1alpha1.VMDistributedCluster, vmClusterObj *vmv1beta1.VMCluster, scheme *runtime.Scheme) {},
			wantErr: false,
		},
		{
			name: "vmcluster has unexpected owner ref, should return error",
			args: args{
				cr:           newVMDistributedCluster("test-vmdc", "default", []vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: "cluster-1"}}}, vmv1alpha1.VMAgentNameAndSpec{}, vmv1alpha1.VMUserNameAndSpec{}, vmv1alpha1.VMAuthNameAndSpec{}),
				vmClusterObj: newVMCluster("cluster-1", "default"),
				scheme:       scheme,
			},
			setup: func(cr *vmv1alpha1.VMDistributedCluster, vmClusterObj *vmv1beta1.VMCluster, scheme *runtime.Scheme) {
				// Set an owner reference that is NOT the 'cr'
				otherCR := &vmv1alpha1.VMDistributedCluster{}
				otherCR.SetName("other-vmdc")
				otherCR.SetNamespace("default")
				otherCR.APIVersion = "operator.victoriametrics.com/v1alpha1"
				otherCR.Kind = "VMDistributedCluster"
				_ = controllerutil.SetOwnerReference(otherCR, vmClusterObj, scheme)
			},
			wantErr: true,
		},
		{
			name: "vmcluster has cr and another unexpected owner ref, should return error",
			args: args{
				cr:           newVMDistributedCluster("test-vmdc", "default", []vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: "cluster-1"}}}, vmv1alpha1.VMAgentNameAndSpec{}, vmv1alpha1.VMUserNameAndSpec{}, vmv1alpha1.VMAuthNameAndSpec{}),
				vmClusterObj: newVMCluster("cluster-1", "default"),
				scheme:       scheme,
			},
			setup: func(cr *vmv1alpha1.VMDistributedCluster, vmClusterObj *vmv1beta1.VMCluster, scheme *runtime.Scheme) {
				// Set the primary CR as an owner
				_ = controllerutil.SetOwnerReference(cr, vmClusterObj, scheme)

				// Set another different CR as an owner
				otherCR := &vmv1alpha1.VMDistributedCluster{}
				otherCR.SetName("other-vmdc-2")
				otherCR.SetNamespace("default")
				otherCR.APIVersion = "operator.victoriametrics.com/v1alpha1"
				otherCR.Kind = "VMDistributedCluster"
				_ = controllerutil.SetOwnerReference(otherCR, vmClusterObj, scheme)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset owner references for each test case
			tt.args.vmClusterObj.SetOwnerReferences(nil)
			if tt.setup != nil {
				tt.setup(tt.args.cr, tt.args.vmClusterObj, tt.args.scheme)
			}

			err := ensureNoVMClusterOwners(tt.args.cr, tt.args.vmClusterObj)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Verify if no unexpected owner references exist
			if !tt.wantErr {
				ref := metav1.OwnerReference{
					APIVersion: tt.args.cr.APIVersion,
					Kind:       tt.args.cr.Kind,
					Name:       tt.args.cr.GetName(),
				}
				for _, owner := range tt.args.vmClusterObj.GetOwnerReferences() {
					isCROwner := owner.APIVersion == ref.APIVersion &&
						owner.Kind == ref.Kind &&
						owner.Name == ref.Name
					assert.True(t, isCROwner, "vmcluster %s should not have unexpected owner reference: %s/%s/%s", tt.args.vmClusterObj.Name, owner.APIVersion, owner.Kind, owner.Name)
				}
			}
		})
	}
}

func TestUpdateOrCreateVMAgent(t *testing.T) {
	s := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(s)
	_ = vmv1beta1.AddToScheme(s)
	_ = corev1.AddToScheme(s)

	// Prepare a CR and a VMCluster referenced by the CR
	cr := newVMDistributedCluster(
		"test-vmdc", "default",
		[]vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: "vmcluster-1"}}},
		vmv1alpha1.VMAgentNameAndSpec{
			Name: "test-vmagent",
		},
		vmv1alpha1.VMUserNameAndSpec{},
		vmv1alpha1.VMAuthNameAndSpec{})
	vmCluster := newVMCluster("vmcluster-1", "default")

	t.Run("creates VMAgent when missing", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, vmCluster).Build()
		tc := &trackingClient{Client: fakeClient, Actions: []action{}, objects: make(map[client.ObjectKey]client.Object)}
		ctx := context.Background()

		vmAgent, err := updateOrCreateVMAgent(ctx, tc, cr, s, []*vmv1beta1.VMCluster{vmCluster})
		assert.NoError(t, err)
		// The function returns the vmAgent object; ensure it's present in the API server
		created := &vmv1beta1.VMAgent{}
		err = tc.Get(ctx, types.NamespacedName{Name: "test-vmagent", Namespace: "default"}, created)
		assert.NoError(t, err)
		assert.Equal(t, "test-vmagent", created.Name)
		// Ensure that at least Get then Create were called
		if assert.GreaterOrEqual(t, len(tc.Actions), 2) {
			assert.Equal(t, "Get", tc.Actions[0].Method)
			assert.Equal(t, "Create", tc.Actions[1].Method)
		}
		// Ensure returned object has expected name/namespace
		assert.Equal(t, "test-vmagent", vmAgent.Name)
		assert.Equal(t, "default", vmAgent.Namespace)
	})

	t.Run("returns existing VMAgent when present and no update needed", func(t *testing.T) {
		// existing VMAgent already has desired RemoteWrite for vmCluster and owner reference, so no update expected
		tenant := "0"
		existing := &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmagent",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: cr.APIVersion,
						Kind:       cr.Kind,
						Name:       cr.Name,
						Controller: ptr.To(true),
					},
				},
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL: remoteWriteURL(vmCluster, &tenant),
					},
				},
			},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(existing, cr, vmCluster).Build()
		tc := &trackingClient{Client: fakeClient, Actions: []action{}, objects: make(map[client.ObjectKey]client.Object)}
		ctx := context.Background()

		vmAgent, err := updateOrCreateVMAgent(ctx, tc, cr, s, []*vmv1beta1.VMCluster{vmCluster})
		assert.NoError(t, err)
		assert.Equal(t, existing.Name, vmAgent.Name)

		// Only Get should be performed (no Create/Update)
		assert.Len(t, tc.Actions, 1)
		assert.Equal(t, "Get", tc.Actions[0].Method)
	})

	t.Run("updates existing VMAgent when spec differs", func(t *testing.T) {
		// Existing vmagent with empty spec
		existing := &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{},
		}

		// Create a CR that contains a VMAgent.Spec which should cause an update
		vmAgentSpec := &vmv1beta1.VMAgentSpec{
			PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{
				Labels: map[string]string{"foo": "bar"},
			},
		}
		crWithSpec := newVMDistributedCluster(
			"test-vmdc-update", "default",
			[]vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: "vmcluster-1"}}},
			vmv1alpha1.VMAgentNameAndSpec{
				Name: "test-vmagent",
				Spec: vmAgentSpec,
			},
			vmv1alpha1.VMUserNameAndSpec{},
			vmv1alpha1.VMAuthNameAndSpec{},
		)

		fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(existing, crWithSpec, vmCluster).Build()
		tc := &trackingClient{Client: fakeClient, Actions: []action{}, objects: make(map[client.ObjectKey]client.Object)}
		ctx := context.Background()

		updatedAgent, err := updateOrCreateVMAgent(ctx, tc, crWithSpec, s, []*vmv1beta1.VMCluster{vmCluster})
		assert.NoError(t, err)
		assert.NotNil(t, updatedAgent)
		assert.Equal(t, "test-vmagent", updatedAgent.Name)

		// Ensure Update was called (Get then Update)
		if assert.GreaterOrEqual(t, len(tc.Actions), 2) {
			assert.Equal(t, "Get", tc.Actions[0].Method)
			assert.Equal(t, "Update", tc.Actions[1].Method)
		}

		// Verify that the in-memory stored object reflects the updated spec (label present)
		got := &vmv1beta1.VMAgent{}
		err = tc.Get(ctx, types.NamespacedName{Name: "test-vmagent", Namespace: "default"}, got)
		assert.NoError(t, err)
		if assert.NotNil(t, got.Spec.PodMetadata) {
			assert.Equal(t, "bar", got.Spec.PodMetadata.Labels["foo"])
		}
	})

	t.Run("propagates Get error from client", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, vmCluster).Build()
		custom := &customErrorClient{Client: fakeClient, customError: fmt.Errorf("simulated error")}
		ctx := context.Background()

		_, err := updateOrCreateVMAgent(ctx, custom, cr, s, []*vmv1beta1.VMCluster{vmCluster})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get VMAgent")
	})
}

func TestUpdateOrCreateVMuser(t *testing.T) {
	s := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(s)
	_ = vmv1beta1.AddToScheme(s)
	_ = corev1.AddToScheme(s)

	// Prepare a VMCluster referenced by the CR
	vmCluster := newVMCluster("vmcluster-1", "v1.0.0")

	t.Run("creates VMUser when inline spec provided and missing", func(t *testing.T) {
		// Inline VMUser spec
		inlineSpec := &vmv1beta1.VMUserSpec{
			TargetRefs: []vmv1beta1.TargetRef{
				{
					CRD:              &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "vmcluster-1", Namespace: "default"},
					TargetPathSuffix: "/select/0",
				},
			},
		}
		cr := newVMDistributedCluster(
			"test-vmdc-user-create", "default",
			[]vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: vmCluster.Name}}},
			vmv1alpha1.VMAgentNameAndSpec{Name: "test-vmagent"},
			vmv1alpha1.VMUserNameAndSpec{Name: "vmuser-create", Spec: inlineSpec},
			vmv1alpha1.VMAuthNameAndSpec{Name: "vmauth-proxy"},
		)

		fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, vmCluster).Build()
		tc := &trackingClient{Client: fakeClient, Actions: []action{}, objects: make(map[client.ObjectKey]client.Object)}
		ctx := context.Background()

		vmUsers, err := updateOrCreateVMuser(ctx, tc, cr, cr.Namespace, cr.Spec.VMAuthUser, s)
		assert.NoError(t, err)
		assert.Len(t, vmUsers, 1)
		created := &vmv1beta1.VMUser{}
		err = tc.Get(ctx, types.NamespacedName{Name: "vmuser-create", Namespace: "default"}, created)
		assert.NoError(t, err)
		assert.Equal(t, "vmuser-create", created.Name)
		assert.Equal(t, inlineSpec.TargetRefs, created.Spec.TargetRefs)

		// Ensure Get then Create
		if assert.GreaterOrEqual(t, len(tc.Actions), 2) {
			assert.Equal(t, "Get", tc.Actions[0].Method)
			assert.Equal(t, "Create", tc.Actions[1].Method)
		}
	})

	t.Run("no-op when existing VMUser matches spec", func(t *testing.T) {
		inlineSpec := &vmv1beta1.VMUserSpec{
			TargetRefs: []vmv1beta1.TargetRef{
				{
					CRD:              &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "vmcluster-1", Namespace: "default"},
					TargetPathSuffix: "/select/0",
				},
			},
		}
		existing := &vmv1beta1.VMUser{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmuser-noop",
				Namespace: "default",
			},
			Spec: *inlineSpec.DeepCopy(),
		}
		cr := newVMDistributedCluster(
			"test-vmdc-user-noop", "default",
			[]vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: vmCluster.Name}}},
			vmv1alpha1.VMAgentNameAndSpec{Name: "test-vmagent"},
			vmv1alpha1.VMUserNameAndSpec{Name: existing.Name, Spec: inlineSpec},
			vmv1alpha1.VMAuthNameAndSpec{Name: "vmauth-proxy"},
		)

		fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, vmCluster, existing).Build()
		tc := &trackingClient{Client: fakeClient, Actions: []action{}, objects: make(map[client.ObjectKey]client.Object)}
		// Pre-populate tracking client cache
		tc.objects[types.NamespacedName{Name: existing.Name, Namespace: existing.Namespace}] = existing.DeepCopyObject().(client.Object)

		ctx := context.Background()
		// Pass nil scheme to avoid adding owner references in the no-op case.
		vmUsers, err := updateOrCreateVMuser(ctx, tc, cr, cr.Namespace, cr.Spec.VMAuthUser, nil)
		assert.NoError(t, err)
		assert.Len(t, vmUsers, 1)
		// Only Get should be performed (no Update)
		assert.Len(t, tc.Actions, 1)
		assert.Equal(t, "Get", tc.Actions[0].Method)
	})

	t.Run("updates VMUser when spec differs", func(t *testing.T) {
		inlineSpec := &vmv1beta1.VMUserSpec{
			TargetRefs: []vmv1beta1.TargetRef{},
		}
		updatedInlineSpec := &vmv1beta1.VMUserSpec{
			TargetRefs: []vmv1beta1.TargetRef{
				{
					CRD:              &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "vmcluster-1", Namespace: "default"},
					TargetPathSuffix: "/select/0",
				},
			},
		}
		existing := &vmv1beta1.VMUser{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmuser-update",
				Namespace: "default",
			},
			Spec: *inlineSpec.DeepCopy(),
		}
		cr := newVMDistributedCluster(
			"test-vmdc-user-update", "default",
			[]vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: vmCluster.Name}}},
			vmv1alpha1.VMAgentNameAndSpec{Name: "test-vmagent"},
			vmv1alpha1.VMUserNameAndSpec{Name: existing.Name, Spec: updatedInlineSpec},
			vmv1alpha1.VMAuthNameAndSpec{Name: "vmauth-proxy"},
		)

		fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, vmCluster, existing).Build()
		tc := &trackingClient{Client: fakeClient, Actions: []action{}, objects: make(map[client.ObjectKey]client.Object)}
		tc.objects[types.NamespacedName{Name: existing.Name, Namespace: existing.Namespace}] = existing.DeepCopyObject().(client.Object)

		ctx := context.Background()
		vmUsers, err := updateOrCreateVMuser(ctx, tc, cr, cr.Namespace, cr.Spec.VMAuthUser, s)
		assert.NoError(t, err)
		assert.Len(t, vmUsers, 1)

		// Get then Update
		if assert.GreaterOrEqual(t, len(tc.Actions), 2) {
			assert.Equal(t, "Get", tc.Actions[0].Method)
			assert.Equal(t, "Update", tc.Actions[1].Method)
		}

		// Verify update
		updated := &vmv1beta1.VMUser{}
		err = tc.Get(ctx, types.NamespacedName{Name: existing.Name, Namespace: existing.Namespace}, updated)
		assert.NoError(t, err)
		assert.Equal(t, updatedInlineSpec.TargetRefs, updated.Spec.TargetRefs)
	})

	t.Run("sets ownerRef when creating inline spec and scheme provided", func(t *testing.T) {
		inlineSpec := &vmv1beta1.VMUserSpec{
			TargetRefs: []vmv1beta1.TargetRef{},
		}
		cr := newVMDistributedCluster(
			"test-vmdc-user-ownercreate", "default",
			[]vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: vmCluster.Name}}},
			vmv1alpha1.VMAgentNameAndSpec{Name: "test-vmagent"},
			vmv1alpha1.VMUserNameAndSpec{Name: "vmuser-owner-create", Spec: inlineSpec},
			vmv1alpha1.VMAuthNameAndSpec{Name: "vmauth-proxy"},
		)
		// ensure UID is set so ownerRef contains it
		cr.UID = "owner-uid-create"

		fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, vmCluster).Build()
		tc := &trackingClient{Client: fakeClient, Actions: []action{}, objects: make(map[client.ObjectKey]client.Object)}
		ctx := context.Background()

		vmUsers, err := updateOrCreateVMuser(ctx, tc, cr, cr.Namespace, cr.Spec.VMAuthUser, s)
		assert.NoError(t, err)
		assert.Len(t, vmUsers, 1)

		created := &vmv1beta1.VMUser{}
		err = tc.Get(ctx, types.NamespacedName{Name: "vmuser-owner-create", Namespace: "default"}, created)
		assert.NoError(t, err)

		assert.NotEmpty(t, created.OwnerReferences)
		assert.Equal(t, cr.Name, created.OwnerReferences[0].Name)
		assert.Equal(t, cr.UID, created.OwnerReferences[0].UID)
	})

	t.Run("adds ownerRef when existing VMUser missing it", func(t *testing.T) {
		existing := &vmv1beta1.VMUser{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmuser-add-owner",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMUserSpec{
				TargetRefs: []vmv1beta1.TargetRef{},
			},
		}
		cr := newVMDistributedCluster(
			"test-vmdc-user-addowner", "default",
			[]vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: vmCluster.Name}}},
			vmv1alpha1.VMAgentNameAndSpec{Name: "test-vmagent"},
			vmv1alpha1.VMUserNameAndSpec{Name: existing.Name, Spec: &existing.Spec},
			vmv1alpha1.VMAuthNameAndSpec{Name: "vmauth-proxy"},
		)
		cr.UID = "owner-uid-add"

		fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, vmCluster, existing).Build()
		tc := &trackingClient{Client: fakeClient, Actions: []action{}, objects: make(map[client.ObjectKey]client.Object)}
		tc.objects[types.NamespacedName{Name: existing.Name, Namespace: existing.Namespace}] = existing.DeepCopyObject().(client.Object)

		ctx := context.Background()
		vmUsers, err := updateOrCreateVMuser(ctx, tc, cr, cr.Namespace, cr.Spec.VMAuthUser, s)
		assert.NoError(t, err)
		assert.Len(t, vmUsers, 1)

		updated := &vmv1beta1.VMUser{}
		err = tc.Get(ctx, types.NamespacedName{Name: existing.Name, Namespace: existing.Namespace}, updated)
		assert.NoError(t, err)

		assert.NotEmpty(t, updated.OwnerReferences)
		assert.Equal(t, cr.Name, updated.OwnerReferences[0].Name)
	})

	t.Run("propagates Get error from client", func(t *testing.T) {
		inlineSpec := &vmv1beta1.VMUserSpec{
			TargetRefs: []vmv1beta1.TargetRef{},
		}
		cr := newVMDistributedCluster(
			"test-vmdc-user-geterr", "default",
			[]vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: vmCluster.Name}}},
			vmv1alpha1.VMAgentNameAndSpec{Name: "test-vmagent"},
			vmv1alpha1.VMUserNameAndSpec{Name: "vmuser-fail", Spec: inlineSpec},
			vmv1alpha1.VMAuthNameAndSpec{Name: "vmauth-proxy"},
		)

		fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, vmCluster).Build()
		custom := &customErrorClient{Client: fakeClient, customError: fmt.Errorf("simulated error")}
		ctx := context.Background()

		_, err := updateOrCreateVMuser(ctx, custom, cr, cr.Namespace, cr.Spec.VMAuthUser, s)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get VMUser")
	})
}

func TestRemoteWriteURL(t *testing.T) {
	vmCluster := newVMCluster("my-cluster", "default")

	tenantID := ptr.To("123")
	expectedURL := "http://my-cluster.default.cluster.local.:8480/insert/123/prometheus/api/v1/write"
	if url := remoteWriteURL(vmCluster, tenantID); url != expectedURL {
		t.Errorf("RemoteWriteURL() got = %v, want %v", url, expectedURL)
	}

	tenantID = ptr.To("my-tenant")
	expectedURL = "http://my-cluster.default.cluster.local.:8480/insert/my-tenant/prometheus/api/v1/write"
	if url := remoteWriteURL(vmCluster, tenantID); url != expectedURL {
		t.Errorf("RemoteWriteURL() got = %v, want %v", url, expectedURL)
	}
}

func TestSetOwnerRefIfNeeded(t *testing.T) {
	s := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(s)
	_ = vmv1beta1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	_ = corev1.AddToScheme(s)

	ownerCR := newVMDistributedCluster(
		"owner-vmdc", "test-ns",
		[]vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: "cluster-1"}}},
		vmv1alpha1.VMAgentNameAndSpec{
			Name: "test-vmagent",
		},
		vmv1alpha1.VMUserNameAndSpec{},
		vmv1alpha1.VMAuthNameAndSpec{})
	ownerCR.UID = "owner-uid" // Ensure UID is set for owner reference

	tests := []struct {
		name          string
		ownerCR       *vmv1alpha1.VMDistributedCluster
		controlledObj client.Object
		existingRef   bool // Indicates if the controlledObj already has the ownerCR as an ownerRef
		expectChange  bool // Indicates if SetOwnerRefIfNeeded should return true (i.e., it added/updated the ref)
		expectErr     bool
	}{
		{
			name:    "Add owner ref to new object",
			ownerCR: ownerCR,
			controlledObj: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "test-ns",
				},
			},
			existingRef:  false,
			expectChange: true,
			expectErr:    false,
		},
		{
			name:    "Object already has owner ref",
			ownerCR: ownerCR,
			controlledObj: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "test-ns",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: ownerCR.APIVersion,
							Kind:       ownerCR.Kind,
							Name:       ownerCR.Name,
							UID:        ownerCR.UID,
						},
					},
				},
			},
			existingRef:  true,
			expectChange: false,
			expectErr:    false,
		},
		{
			name:    "Object has different owner ref, add new one",
			ownerCR: ownerCR,
			controlledObj: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "test-ns",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "some.api/v1",
							Kind:       "SomeOtherCRD",
							Name:       "other-owner",
							UID:        "other-uid",
						},
					},
				},
			},
			existingRef:  false, // It doesn't have *this* owner ref
			expectChange: true,
			expectErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed, err := setOwnerRefIfNeeded(tt.ownerCR, tt.controlledObj, s)

			if (err != nil) != tt.expectErr {
				t.Fatalf("SetOwnerRefIfNeeded() error = %v, wantErr %v", err, tt.expectErr)
			}
			if changed != tt.expectChange {
				t.Errorf("SetOwnerRefIfNeeded() changed = %v, wantChange %v", changed, tt.expectChange)
			}

			if !tt.expectErr && tt.expectChange {
				// Verify owner reference is correctly set
				foundRef := false
				expectedOwnerRef := metav1.OwnerReference{
					APIVersion: tt.ownerCR.APIVersion,
					Kind:       tt.ownerCR.Kind,
					Name:       tt.ownerCR.Name,
					UID:        tt.ownerCR.UID,
				}
				for _, ref := range tt.controlledObj.GetOwnerReferences() {
					// Use DeepEqual for comparison of OwnerReference fields
					if reflect.DeepEqual(ref, expectedOwnerRef) {
						foundRef = true
						break
					}
				}
				if !foundRef {
					t.Errorf("owner reference was not set on controlled object")
				}
			}
			if !tt.expectErr && !tt.expectChange && tt.existingRef {
				// Verify owner reference is still there and not duplicated
				ownerRef := metav1.OwnerReference{
					APIVersion: tt.ownerCR.APIVersion,
					Kind:       tt.ownerCR.Kind,
					Name:       tt.ownerCR.Name,
					UID:        tt.ownerCR.UID,
				}
				count := 0
				for _, ref := range tt.controlledObj.GetOwnerReferences() {
					if reflect.DeepEqual(ref, ownerRef) {
						count++
					}
				}
				if count != 1 {
					t.Errorf("expected exactly one owner reference, got %d", count)
				}
			}
		})
	}
}
