package reconcile

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestWriteAggregatedStatus(t *testing.T) {
	f := func(conditions []vmv1beta1.Condition, expectedStatus vmv1beta1.UpdateStatus, expectedReasonContains string) {
		t.Helper()
		stm := &vmv1beta1.StatusMetadata{Conditions: conditions}
		writeAggregatedStatus(stm, vmv1beta1.ConditionDomainTypeAppliedSuffix)
		assert.Equal(t, expectedStatus, stm.UpdateStatus)
		if expectedReasonContains == "" {
			assert.Empty(t, stm.Reason)
		} else {
			assert.Contains(t, stm.Reason, expectedReasonContains)
		}
	}

	// no parent ever applied this child object
	f(nil, vmv1beta1.UpdateStatusIgnored, "")

	// single parent, applied successfully
	f([]vmv1beta1.Condition{
		{Type: "vmagent1.ns.vmagent" + vmv1beta1.ConditionDomainTypeAppliedSuffix, Status: "True"},
	}, vmv1beta1.UpdateStatusOperational, "")

	// single parent, failed
	f([]vmv1beta1.Condition{
		{Type: "vmagent1.ns.vmagent" + vmv1beta1.ConditionDomainTypeAppliedSuffix, Status: "False", Message: "boom"},
	}, vmv1beta1.UpdateStatusFailed, "boom")

	// applied on one parent, failed on another: must not be globally Failed
	f([]vmv1beta1.Condition{
		{Type: "vmagent1.ns.vmagent" + vmv1beta1.ConditionDomainTypeAppliedSuffix, Status: "True"},
		{Type: "vmagent2.ns.vmagent" + vmv1beta1.ConditionDomainTypeAppliedSuffix, Status: "False", Message: "arbitraryFSAccessThroughSMs is not allowed"},
	}, vmv1beta1.UpdateStatusOperational, "arbitraryFSAccessThroughSMs is not allowed")

	// failed on all parents that reference it
	f([]vmv1beta1.Condition{
		{Type: "vmagent1.ns.vmagent" + vmv1beta1.ConditionDomainTypeAppliedSuffix, Status: "False", Message: "err1"},
		{Type: "vmagent2.ns.vmagent" + vmv1beta1.ConditionDomainTypeAppliedSuffix, Status: "False", Message: "err2"},
	}, vmv1beta1.UpdateStatusFailed, "err1")

	// conditions with a different suffix are ignored
	f([]vmv1beta1.Condition{
		{Type: "vmagent1.ns.vmagent.SomeOtherSuffix", Status: "False", Message: "unrelated"},
	}, vmv1beta1.UpdateStatusIgnored, "")
}

func TestStatusForChildObjects_ReleasesDroppedChildren(t *testing.T) {
	ctx := context.Background()
	parent := "test-releases-dropped.ns.vmalert"

	newRule := func(name string) *vmv1beta1.VMRule {
		return &vmv1beta1.VMRule{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		}
	}
	ruleA := newRule("rule-a")
	ruleB := newRule("rule-b")
	rclient := k8stools.GetTestClientWithObjects([]runtime.Object{ruleA, ruleB})

	// first cycle: parent selects both rules
	require.NoError(t, StatusForChildObjects(ctx, rclient, parent, []*vmv1beta1.VMRule{ruleA, ruleB}))

	var got vmv1beta1.VMRule
	require.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "rule-a"}, &got))
	assert.Equal(t, vmv1beta1.UpdateStatusOperational, got.Status.UpdateStatus)
	require.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "rule-b"}, &got))
	assert.Equal(t, vmv1beta1.UpdateStatusOperational, got.Status.UpdateStatus)

	// second cycle: parent no longer selects rule-b
	require.NoError(t, StatusForChildObjects(ctx, rclient, parent, []*vmv1beta1.VMRule{ruleA}))

	require.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "rule-a"}, &got))
	assert.Equal(t, vmv1beta1.UpdateStatusOperational, got.Status.UpdateStatus, "still-selected child must be unaffected")

	require.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "rule-b"}, &got))
	assert.Equal(t, vmv1beta1.UpdateStatusIgnored, got.Status.UpdateStatus, "dropped child must be released, not left stale")
	assert.Empty(t, got.Status.Conditions)
}

func TestStatusForChildObject_FastPathDoesNotReleaseSiblings(t *testing.T) {
	ctx := context.Background()
	parent := "test-fastpath.ns.vmalert"

	newRule := func(name string) *vmv1beta1.VMRule {
		return &vmv1beta1.VMRule{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		}
	}
	ruleA := newRule("rule-a")
	ruleB := newRule("rule-b")
	rclient := k8stools.GetTestClientWithObjects([]runtime.Object{ruleA, ruleB})

	require.NoError(t, StatusForChildObjects(ctx, rclient, parent, []*vmv1beta1.VMRule{ruleA, ruleB}))

	// fast-path update of a single object must not be treated as the full selection
	require.NoError(t, StatusForChildObject(ctx, rclient, parent, ruleA))

	var got vmv1beta1.VMRule
	require.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "rule-b"}, &got))
	assert.Equal(t, vmv1beta1.UpdateStatusOperational, got.Status.UpdateStatus, "fast path must not release siblings not passed to it")
}

func TestStatusForChildObjects_ReleaseIsStatelessAcrossCalls(t *testing.T) {
	ctx := context.Background()
	parent := "test-stateless.ns.vmalert"

	newRule := func(name string) *vmv1beta1.VMRule {
		return &vmv1beta1.VMRule{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"}}
	}
	ruleA := newRule("rule-a")
	ruleB := newRule("rule-b")
	rclient := k8stools.GetTestClientWithObjects([]runtime.Object{ruleA, ruleB})

	require.NoError(t, StatusForChildObjects(ctx, rclient, parent, []*vmv1beta1.VMRule{ruleA, ruleB}))

	var got vmv1beta1.VMRule
	require.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "rule-b"}, &got))
	require.NotEmpty(t, got.Status.Conditions, "precondition: rule-b must carry the parent's condition before it's dropped")

	// simulate a fresh process (no shared memory with the call above) selecting only rule-a
	require.NoError(t, StatusForChildObjects(ctx, rclient, parent, []*vmv1beta1.VMRule{ruleA}))

	require.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "rule-b"}, &got))
	assert.Equal(t, vmv1beta1.UpdateStatusIgnored, got.Status.UpdateStatus, "drop must be detected purely from persisted cluster state")
}

func TestStatusForChildObjects_FailedReleaseIsRetriedNextCall(t *testing.T) {
	ctx := context.Background()
	parent := "test-retry.ns.vmalert"

	newRule := func(name string) *vmv1beta1.VMRule {
		return &vmv1beta1.VMRule{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"}}
	}
	ruleA := newRule("rule-a")
	ruleB := newRule("rule-b")

	var failReleaseUpdates bool
	fns := interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, cl client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			if failReleaseUpdates {
				if r, ok := obj.(*vmv1beta1.VMRule); ok && r.Name == "rule-b" {
					return errors.New("injected transient failure")
				}
			}
			return cl.SubResource(subResourceName).Update(ctx, obj, opts...)
		},
	}
	rclient := k8stools.GetTestClientWithObjectsAndInterceptors([]runtime.Object{ruleA, ruleB}, fns)

	require.NoError(t, StatusForChildObjects(ctx, rclient, parent, []*vmv1beta1.VMRule{ruleA, ruleB}))

	// rule-b drops out of selection, but its release fails transiently; the error must
	// surface so the caller's reconcile gets requeued instead of silently dropping it
	failReleaseUpdates = true
	require.Error(t, StatusForChildObjects(ctx, rclient, parent, []*vmv1beta1.VMRule{ruleA}))

	var got vmv1beta1.VMRule
	require.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "rule-b"}, &got))
	assert.NotEmpty(t, got.Status.Conditions, "failed release must not silently drop the stale condition")
	assert.Equal(t, vmv1beta1.UpdateStatusOperational, got.Status.UpdateStatus, "status must remain as-is until the release actually succeeds")

	// transient failure clears; the retry must now succeed
	failReleaseUpdates = false
	require.NoError(t, StatusForChildObjects(ctx, rclient, parent, []*vmv1beta1.VMRule{ruleA}))

	require.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "rule-b"}, &got))
	assert.Equal(t, vmv1beta1.UpdateStatusIgnored, got.Status.UpdateStatus, "retried release must eventually succeed")
	assert.Empty(t, got.Status.Conditions)
}

func TestStatusForChildObjects_FallsBackWithoutIndexedClient(t *testing.T) {
	ctx := context.Background()
	parent := "test-no-index.ns.vmalert"

	scheme := runtime.NewScheme()
	require.NoError(t, vmv1beta1.AddToScheme(scheme))

	newRule := func(name string) *vmv1beta1.VMRule {
		return &vmv1beta1.VMRule{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"}}
	}
	ruleA := newRule("rule-a")
	ruleB := newRule("rule-b")
	rclient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&vmv1beta1.VMRule{}).
		WithObjects(ruleA, ruleB).
		Build()

	require.NoError(t, StatusForChildObjects(ctx, rclient, parent, []*vmv1beta1.VMRule{ruleA, ruleB}))
	require.NoError(t, StatusForChildObjects(ctx, rclient, parent, []*vmv1beta1.VMRule{ruleA}))

	var got vmv1beta1.VMRule
	require.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: "ns", Name: "rule-b"}, &got))
	assert.Equal(t, vmv1beta1.UpdateStatusIgnored, got.Status.UpdateStatus)
	assert.Empty(t, got.Status.Conditions)
}
