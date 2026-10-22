package manager

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// isDryRun reports whether server-side dry-run option is set.
func isDryRun[O any](opts []O) bool {
	for _, o := range opts {
		if any(o) == any(client.DryRunAll) {
			return true
		}
	}
	return false
}

func TestDryRunClient(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	// calls records mutating verbs (subresource ones as <subresource>/<verb>) sent to the underlying client and whether they carried DryRun.
	calls := map[string]bool{}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, _ client.Object, opts ...client.CreateOption) error {
			calls["create"] = isDryRun(opts)
			return nil
		},
		Update: func(_ context.Context, _ client.WithWatch, _ client.Object, opts ...client.UpdateOption) error {
			calls["update"] = isDryRun(opts)
			return nil
		},
		Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, opts ...client.PatchOption) error {
			calls["patch"] = isDryRun(opts)
			return nil
		},
		Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, opts ...client.DeleteOption) error {
			calls["delete"] = isDryRun(opts)
			return nil
		},
		DeleteAllOf: func(_ context.Context, _ client.WithWatch, _ client.Object, opts ...client.DeleteAllOfOption) error {
			calls["deleteAllOf"] = isDryRun(opts)
			return nil
		},
		Apply: func(_ context.Context, _ client.WithWatch, _ runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
			calls["apply"] = isDryRun(opts)
			return nil
		},
		SubResourceApply: func(_ context.Context, _ client.Client, sub string, _ runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
			calls[sub+"/apply"] = isDryRun(opts)
			return nil
		},
		SubResourceCreate: func(_ context.Context, _ client.Client, sub string, _ client.Object, _ client.Object, opts ...client.SubResourceCreateOption) error {
			calls[sub+"/create"] = isDryRun(opts)
			return nil
		},
		SubResourceUpdate: func(_ context.Context, _ client.Client, sub string, _ client.Object, opts ...client.SubResourceUpdateOption) error {
			calls[sub+"/update"] = isDryRun(opts)
			return nil
		},
		SubResourcePatch: func(_ context.Context, _ client.Client, sub string, _ client.Object, _ client.Patch, opts ...client.SubResourcePatchOption) error {
			calls[sub+"/patch"] = isDryRun(opts)
			return nil
		},
	}).Build()
	dryClient := NewDryRunClient(fakeClient, logr.Discard())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}
	ctx := context.TODO()
	patch := client.MergeFrom(pod.DeepCopy())
	applyCfg := client.ApplyConfigurationFromUnstructured(&unstructured.Unstructured{Object: map[string]any{"kind": "Pod", "apiVersion": "v1"}})

	assert.NoError(t, dryClient.Create(ctx, pod))
	assert.NoError(t, dryClient.Update(ctx, pod))
	assert.NoError(t, dryClient.Patch(ctx, pod, patch))
	assert.NoError(t, dryClient.Delete(ctx, pod))
	assert.NoError(t, dryClient.DeleteAllOf(ctx, pod))
	assert.NoError(t, dryClient.SubResource("scale").Create(ctx, pod, pod.DeepCopy()))
	assert.NoError(t, dryClient.SubResource("scale").Update(ctx, pod))
	assert.NoError(t, dryClient.SubResource("scale").Patch(ctx, pod, patch))
	assert.NoError(t, dryClient.Apply(ctx, applyCfg))
	assert.NoError(t, dryClient.SubResource("scale").Apply(ctx, applyCfg))
	assert.NoError(t, dryClient.Status().Apply(ctx, applyCfg))
	assert.NoError(t, dryClient.Status().Create(ctx, pod, pod.DeepCopy()))
	assert.NoError(t, dryClient.Status().Update(ctx, pod))
	assert.NoError(t, dryClient.Status().Patch(ctx, pod, patch))

	assert.Len(t, calls, 14)
	for verb, dry := range calls {
		assert.True(t, dry, "%s must be sent with server-side dry-run", verb)
	}
}
