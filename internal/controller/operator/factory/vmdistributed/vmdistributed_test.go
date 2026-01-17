package VMDistributed

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
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
		if o.Kind == "" {
			o.TypeMeta = metav1.TypeMeta{APIVersion: vmv1beta1.GroupVersion.String(), Kind: "VMAgent"}
		}
	case *vmv1beta1.VMCluster:
		if o.Kind == "" {
			o.TypeMeta = metav1.TypeMeta{APIVersion: vmv1beta1.GroupVersion.String(), Kind: "VMCluster"}
		}
	case *corev1.ConfigMap:
		if o.Kind == "" {
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
func newVMDistributed(name string, zones []vmv1alpha1.VMClusterRefOrSpec, vmAgentSpec vmv1alpha1.VMAgentNameAndSpec, extras ...interface{}) *vmv1alpha1.VMDistributed {
	var vmAuth vmv1alpha1.VMAuthNameAndSpec

	// Parse extras to find VMAuth (ignore any legacy VMUser parameters).
	for _, e := range extras {
		switch v := e.(type) {
		case vmv1alpha1.VMAuthNameAndSpec:
			vmAuth = v
		case *vmv1alpha1.VMAuthNameAndSpec:
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
			Zones:   vmv1alpha1.ZoneSpec{VMClusters: zones},
			VMAgent: vmAgentSpec,
			VMAuth:  vmAuth,
		},
	}
}

type testData struct {
	vmagent        *vmv1beta1.VMAgent
	vmcluster1     *vmv1beta1.VMCluster
	vmcluster2     *vmv1beta1.VMCluster
	cr             *vmv1alpha1.VMDistributed
	trackingClient *trackingClient
	scheme         *runtime.Scheme
}

func beforeEach() testData {
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)

	vmagent := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vmagent", Namespace: "default"},
		Spec: vmv1beta1.VMAgentSpec{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{ReplicaCount: ptr.To(int32(1))},
		},
		Status: vmv1beta1.VMAgentStatus{Replicas: 1},
	}
	vmcluster1 := newVMCluster("vmcluster-1", "v1.0.0")
	vmcluster2 := newVMCluster("vmcluster-2", "v1.0.0") // keep original helper semantics

	zones := []vmv1alpha1.VMClusterRefOrSpec{
		{Ref: &corev1.LocalObjectReference{Name: "vmcluster-1"}},
		{Ref: &corev1.LocalObjectReference{Name: "vmcluster-2"}},
	}
	vmAgentSpec := vmv1alpha1.VMAgentNameAndSpec{Name: vmagent.Name}
	cr := newVMDistributed("test-vdc", zones, vmAgentSpec, vmv1alpha1.VMAuthNameAndSpec{Name: "vmauth-proxy"})

	// Create a new trackingClient
	rclient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		vmagent,
		vmcluster1,
		vmcluster2,
		cr,
	).Build()
	build.AddDefaults(rclient.Scheme())
	tc := &trackingClient{
		Client:  rclient,
		Actions: []action{},
		objects: make(map[client.ObjectKey]client.Object),
	}
	return testData{
		vmagent:        vmagent,
		vmcluster1:     vmcluster1,
		vmcluster2:     vmcluster2,
		cr:             cr,
		trackingClient: tc,
		scheme:         scheme,
	}
}

func TestCreateOrUpdate_ErrorHandling(t *testing.T) {
	t.Run("Paused CR should do nothing", func(t *testing.T) {
		data := beforeEach()
		data.cr.Spec.Paused = true
		rclient := data.trackingClient
		ctx := context.TODO()

		err := CreateOrUpdate(ctx, data.cr, rclient, httpTimeout)
		assert.NoError(t, err) // No error as it's paused
		assert.Empty(t, rclient.Actions)
	})

	t.Run("Missing VMCluster should return error", func(t *testing.T) {
		data := beforeEach()
		data.cr.Spec.Zones.VMClusters[0].Ref.Name = "non-existent-vmcluster"
		rclient := data.trackingClient
		ctx := context.TODO()

		err := CreateOrUpdate(ctx, data.cr, rclient, httpTimeout)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch vmclusters")
	})
}
