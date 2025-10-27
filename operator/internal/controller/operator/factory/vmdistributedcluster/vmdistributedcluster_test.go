package vmdistributedcluster

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"strings"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"k8s.io/apimachinery/pkg/api/meta"
)

// newScheme creates a new scheme with all the necessary types registered.
// This is a helper function to avoid duplicating scheme setup in each test.
func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = vmv1beta1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	return s
}

func TestGetReferencedVMCluster(t *testing.T) {
	s := newScheme()
	testVMCluster := newVMCluster("test-vmcluster", "default", "v1.0.0")

	tests := []struct {
		name        string
		clientObjs  []client.Object
		namespace   string
		ref         *corev1.LocalObjectReference
		expectedErr error
		expectedVM  *vmv1beta1.VMCluster
	}{
		{
			name:       "successfully retrieves VMCluster",
			clientObjs: []client.Object{testVMCluster},
			namespace:  "default",
			ref:        &corev1.LocalObjectReference{Name: "test-vmcluster"},
			expectedVM: testVMCluster,
		},
		{
			name:        "VMCluster not found",
			clientObjs:  []client.Object{},
			namespace:   "default",
			ref:         &corev1.LocalObjectReference{Name: "non-existent"},
			expectedErr: k8serrors.NewNotFound(schema.GroupResource{Group: "operator.victoriametrics.com", Resource: "vmclusters"}, "non-existent"),
		},
		{
			name:        "generic client error",
			clientObjs:  []client.Object{testVMCluster},
			namespace:   "default",
			ref:         &corev1.LocalObjectReference{Name: "test-vmcluster"},
			expectedErr: errors.New("simulated error"), // This will be caught by the tracking client
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			var c client.Client
			if tt.name == "generic client error" {
				c = &mockClient{
					mockGet: func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						return tt.expectedErr
					},
					// Other methods can be nil or default implementations
				}
			} else {
				c = fake.NewClientBuilder().WithScheme(s).WithObjects(tt.clientObjs...).Build()
			}

			vmCluster, err := getReferencedVMCluster(ctx, c, tt.namespace, tt.ref)

			if tt.expectedErr != nil {
				assert.Error(t, err)
				if k8serrors.IsNotFound(tt.expectedErr) {
					assert.True(t, k8serrors.IsNotFound(errors.Unwrap(err)))
				} else {
					assert.Contains(t, err.Error(), tt.expectedErr.Error())
				}
				assert.Nil(t, vmCluster)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedVM.Name, vmCluster.Name)
				assert.Equal(t, tt.expectedVM.Namespace, vmCluster.Namespace)
			}
		})
	}
}

func TestMergeMapsRecursive(t *testing.T) {
	tests := []struct {
		name        string
		base        map[string]interface{}
		override    map[string]interface{}
		expected    map[string]interface{}
		modifiedExp bool
	}{
		{
			name:        "merge two simple maps",
			base:        map[string]interface{}{"a": 1, "b": "hello"},
			override:    map[string]interface{}{"c": 3},
			expected:    map[string]interface{}{"a": 1, "b": "hello", "c": 3},
			modifiedExp: true,
		},
		{
			name:        "override a scalar value",
			base:        map[string]interface{}{"a": 1, "b": "hello"},
			override:    map[string]interface{}{"b": "world"},
			expected:    map[string]interface{}{"a": 1, "b": "world"},
			modifiedExp: true,
		},
		{
			name:        "recursively merge nested maps",
			base:        map[string]interface{}{"a": 1, "nested": map[string]interface{}{"x": 10, "y": 20}},
			override:    map[string]interface{}{"nested": map[string]interface{}{"y": 25, "z": 30}},
			expected:    map[string]interface{}{"a": 1, "nested": map[string]interface{}{"x": 10, "y": 25, "z": 30}},
			modifiedExp: true,
		},
		{
			name:        "handle nil override values (delete key)",
			base:        map[string]interface{}{"a": 1, "b": "hello"},
			override:    map[string]interface{}{"b": nil},
			expected:    map[string]interface{}{"a": 1},
			modifiedExp: true,
		},
		{
			name:        "handle a nil base value (add key)",
			base:        map[string]interface{}{"a": 1},
			override:    map[string]interface{}{"b": "new"},
			expected:    map[string]interface{}{"a": 1, "b": "new"},
			modifiedExp: true,
		},
		{
			name:        "merge with an empty override map",
			base:        map[string]interface{}{"a": 1, "b": "hello"},
			override:    map[string]interface{}{},
			expected:    map[string]interface{}{"a": 1, "b": "hello"},
			modifiedExp: false,
		},
		{
			name:        "merge with an empty base map",
			base:        map[string]interface{}{},
			override:    map[string]interface{}{"a": 1, "b": "hello"},
			expected:    map[string]interface{}{"a": 1, "b": "hello"},
			modifiedExp: true,
		},
		{
			name:        "override a nested map with a scalar",
			base:        map[string]interface{}{"a": map[string]interface{}{"x": 10}},
			override:    map[string]interface{}{"a": 100},
			expected:    map[string]interface{}{"a": 100},
			modifiedExp: true,
		},
		{
			name:        "override a scalar with a nested map",
			base:        map[string]interface{}{"a": 100},
			override:    map[string]interface{}{"a": map[string]interface{}{"x": 10}},
			expected:    map[string]interface{}{"a": map[string]interface{}{"x": 10}},
			modifiedExp: true,
		},
		{
			name:        "no modification",
			base:        map[string]interface{}{"a": 1},
			override:    map[string]interface{}{"a": 1},
			expected:    map[string]interface{}{"a": 1},
			modifiedExp: false,
		},
		{
			name:        "deep equality no modification",
			base:        map[string]interface{}{"a": map[string]interface{}{"b": 2}},
			override:    map[string]interface{}{"a": map[string]interface{}{"b": 2}},
			expected:    map[string]interface{}{"a": map[string]interface{}{"b": 2}},
			modifiedExp: false,
		},
		{
			name:        "nil override of non-existent key",
			base:        map[string]interface{}{"a": 1},
			override:    map[string]interface{}{"nonexistent": nil},
			expected:    map[string]interface{}{"a": 1},
			modifiedExp: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualBase := make(map[string]interface{})
			for k, v := range tt.base {
				actualBase[k] = v // Deep copy for mutable map
			}

			modified := mergeMapsRecursive(actualBase, tt.override)
			assert.Equal(t, tt.expected, actualBase)
			assert.Equal(t, tt.modifiedExp, modified)
		})
	}
}

type mockClient struct {
	mockGet func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error
	mockList func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
	// Add other mock methods as needed
}

func (m *mockClient) Get(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
	if m.mockGet != nil {
		return m.mockGet(ctx, key, obj, opts...)
	}
	return nil
}

func (m *mockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if m.mockList != nil {
		return m.mockList(ctx, list, opts...)
	}
	return nil
}

func (m *mockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error { return nil }
func (m *mockClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error { return nil }
func (m *mockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error { return nil }
func (m *mockClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error { return nil }
func (m *mockClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error { return nil }
func (m *mockClient) Status() client.StatusWriter { return &mockStatusWriter{} }
func (m *mockClient) Scheme() *runtime.Scheme { return newScheme() } // Use newScheme here
func (m *mockClient) RESTMapper() meta.RESTMapper { panic("not implemented") } // Only if needed

type mockStatusWriter struct{}

func (m *mockStatusWriter) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error { return nil }
func (m *mockStatusWriter) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error { return nil }
func (m *mockStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error { return nil }
func (m *mockStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error { return nil }

func TestFetchVMUsers(t *testing.T) {
	s := newScheme()
	testVMUser1 := newVMUser("test-user-1", "default", nil)
	testVMUser2 := newVMUser("test-user-2", "default", nil)

	tests := []struct {
		name        string
		clientObjs  []client.Object
		namespace   string
		refs        []corev1.LocalObjectReference
		expectedErr string
		expectedVMUsers []*vmv1beta1.VMUser
	}{
		{
			name:       "successfully retrieves multiple VMUsers",
			clientObjs: []client.Object{testVMUser1, testVMUser2},
			namespace:  "default",
			refs: []corev1.LocalObjectReference{
				{Name: "test-user-1"},
				{Name: "test-user-2"},
			},
			expectedVMUsers: []*vmv1beta1.VMUser{testVMUser1, testVMUser2},
		},
		{
			name:        "vmuser name not specified",
			clientObjs:  []client.Object{testVMUser1},
			namespace:   "default",
			refs:        []corev1.LocalObjectReference{{Name: ""}},
			expectedErr: "vmuser name is not specified",
		},
		{
			name:        "referenced vmuser not found",
			clientObjs:  []client.Object{testVMUser1},
			namespace:   "default",
			refs:        []corev1.LocalObjectReference{{Name: "non-existent-user"}},
			expectedErr: "failed to get VMUser default/non-existent-user: vmusers.operator.victoriametrics.com \"non-existent-user\" not found",
		},
		{
			name:        "generic client error when fetching vmuser",
			clientObjs:  []client.Object{testVMUser1},
			namespace:   "default",
			refs:        []corev1.LocalObjectReference{{Name: "test-user-1"}},
			expectedErr: "simulated error", // This will be caught by the tracking client
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			var c client.Client
			if tt.name == "generic client error when fetching vmuser" {
				c = &mockClient{
					mockGet: func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						return errors.New("simulated error")
					},
				}
			} else {
				c = fake.NewClientBuilder().WithScheme(s).WithObjects(tt.clientObjs...).Build()
			}

			vmUsers, err := fetchVMUsers(ctx, c, tt.namespace, tt.refs)

			if tt.expectedErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr)
				assert.Nil(t, vmUsers)
			} else {
				assert.NoError(t, err)
				assert.Len(t, vmUsers, len(tt.expectedVMUsers))
				for i := range vmUsers {
					assert.Equal(t, tt.expectedVMUsers[i].Name, vmUsers[i].Name)
					assert.Equal(t, tt.expectedVMUsers[i].Namespace, vmUsers[i].Namespace)
				}
			}
		})
	}
}

func TestFetchVMAgent(t *testing.T) {
	s := newScheme()
	testVMAgent := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vmagent",
			Namespace: "default",
		},
	}

	tests := []struct {
		name        string
		clientObjs  []client.Object
		namespace   string
		ref         corev1.LocalObjectReference
		expectedErr string
		expectedVMAgent *vmv1beta1.VMAgent
	}{
		{
			name:       "successfully retrieves VMAgent",
			clientObjs: []client.Object{testVMAgent},
			namespace:  "default",
			ref:        corev1.LocalObjectReference{Name: "test-vmagent"},
			expectedVMAgent: testVMAgent,
		},
		{
			name:        "vmagent name not specified",
			clientObjs:  []client.Object{testVMAgent},
			namespace:   "default",
			ref:         corev1.LocalObjectReference{Name: ""},
			expectedErr: "global vmagent name is not specified",
		},
		{
			name:        "referenced vmagent not found",
			clientObjs:  []client.Object{},
			namespace:   "default",
			ref:         corev1.LocalObjectReference{Name: "non-existent-vmagent"},
			expectedErr: "failed to get global VMAgent default/non-existent-vmagent: vmagents.operator.victoriametrics.com \"non-existent-vmagent\" not found",
		},
		{
			name:        "generic client error when fetching vmagent",
			clientObjs:  []client.Object{testVMAgent},
			namespace:   "default",
			ref:         corev1.LocalObjectReference{Name: "test-vmagent"},
			expectedErr: "simulated error", // This will be caught by the tracking client
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			var c client.Client
			if tt.name == "generic client error when fetching vmagent" {
				c = &mockClient{
					mockGet: func(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
						return errors.New("simulated error")
					},
				}
			} else {
				c = fake.NewClientBuilder().WithScheme(s).WithObjects(tt.clientObjs...).Build()
			}

			vmAgent, err := fetchVMAgent(ctx, c, tt.namespace, tt.ref)

			if tt.expectedErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr)
				assert.Nil(t, vmAgent)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedVMAgent.Name, vmAgent.Name)
				assert.Equal(t, tt.expectedVMAgent.Namespace, vmAgent.Namespace)
			}
		})
	}
}

func TestFindVMUserReadRuleForVMCluster(t *testing.T) {
	tests := []struct {
		name            string
		vmUserObjs      []*vmv1beta1.VMUser
		vmCluster       *vmv1beta1.VMCluster
		expectedTargetRef *vmv1beta1.TargetRef
		expectedErr     string
	}{
		{
			name: "matching rule found",
			vmUserObjs: []*vmv1beta1.VMUser{
				newVMUser("user1", "default", []vmv1beta1.TargetRef{
					{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "test-cluster", Namespace: "default"}, TargetPathSuffix: "/select/0/prometheus/api/v1/read"},
				}),
			},
			vmCluster:       newVMCluster("test-cluster", "default", "v1.0.0"),
			expectedTargetRef: &vmv1beta1.TargetRef{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "test-cluster", Namespace: "default"}, TargetPathSuffix: "/select/0/prometheus/api/v1/read"},
			expectedErr:     "",
		},
		{
			name: "no matching rule found",
			vmUserObjs: []*vmv1beta1.VMUser{
				newVMUser("user1", "default", []vmv1beta1.TargetRef{
					{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "other-cluster", Namespace: "default"}, TargetPathSuffix: "/select/0/prometheus/api/v1/read"},
				}),
			},
			vmCluster:       newVMCluster("test-cluster", "default", "v1.0.0"),
			expectedTargetRef: nil,
			expectedErr:     "no vmuser has target refs for vmcluster test-cluster",
		},
		{
			name: "multiple vmusers, one with matching rule",
			vmUserObjs: []*vmv1beta1.VMUser{
				newVMUser("user1", "default", []vmv1beta1.TargetRef{
					{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "other-cluster", Namespace: "default"}, TargetPathSuffix: "/select/0/prometheus/api/v1/read"},
				}),
				newVMUser("user2", "default", []vmv1beta1.TargetRef{
					{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "test-cluster", Namespace: "default"}, TargetPathSuffix: "/select/0/prometheus/api/v1/read"},
				}),
			},
			vmCluster:       newVMCluster("test-cluster", "default", "v1.0.0"),
			expectedTargetRef: &vmv1beta1.TargetRef{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "test-cluster", Namespace: "default"}, TargetPathSuffix: "/select/0/prometheus/api/v1/read"},
			expectedErr:     "",
		},
		{
			name: "crd kind mismatch",
			vmUserObjs: []*vmv1beta1.VMUser{
				newVMUser("user1", "default", []vmv1beta1.TargetRef{
					{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vminsert", Name: "test-cluster", Namespace: "default"}, TargetPathSuffix: "/select/0/prometheus/api/v1/read"},
				}),
			},
			vmCluster:       newVMCluster("test-cluster", "default", "v1.0.0"),
			expectedTargetRef: nil,
			expectedErr:     "no vmuser has target refs for vmcluster test-cluster",
		},
		{
			name: "target path suffix mismatch",
			vmUserObjs: []*vmv1beta1.VMUser{
				newVMUser("user1", "default", []vmv1beta1.TargetRef{
					{CRD: &vmv1beta1.CRDRef{Kind: "VMCluster/vmselect", Name: "test-cluster", Namespace: "default"}, TargetPathSuffix: "/insert/0/prometheus/api/v1/write"},
				}),
			},
			vmCluster:       newVMCluster("test-cluster", "default", "v1.0.0"),
			expectedTargetRef: nil,
			expectedErr:     "no vmuser has target refs for vmcluster test-cluster",
		},
		{
			name: "empty vmuser objects slice",
			vmUserObjs:      []*vmv1beta1.VMUser{},
			vmCluster:       newVMCluster("test-cluster", "default", "v1.0.0"),
			expectedTargetRef: nil,
			expectedErr:     "no vmuser has target refs for vmcluster test-cluster",
		},
		{
			name: "vmuser with empty target refs",
			vmUserObjs: []*vmv1beta1.VMUser{
				newVMUser("user1", "default", []vmv1beta1.TargetRef{}),
			},
			vmCluster:       newVMCluster("test-cluster", "default", "v1.0.0"),
			expectedTargetRef: nil,
			expectedErr:     "no vmuser has target refs for vmcluster test-cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tref, err := findVMUserReadRuleForVMCluster(tt.vmUserObjs, tt.vmCluster)

			if tt.expectedErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErr)
				assert.Nil(t, tref)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedTargetRef, tref)
			}
		})
	}
}
