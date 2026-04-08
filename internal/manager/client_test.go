package manager

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDryRunClient(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	dryClient := NewDryRunClient(fakeClient, logr.Discard())

	f := func(ctx context.Context, obj client.Object, action string) {
		t.Helper()
		var err error
		switch action {
		case "create":
			err = dryClient.Create(ctx, obj)
		case "update":
			err = dryClient.Update(ctx, obj)
		case "delete":
			err = dryClient.Delete(ctx, obj)
		case "patch":
			err = dryClient.Patch(ctx, obj, client.MergeFrom(obj.DeepCopyObject().(client.Object)))
		case "deleteAllOf":
			err = dryClient.DeleteAllOf(ctx, obj)
		case "statusCreate":
			err = dryClient.Status().Create(ctx, obj, obj.DeepCopyObject().(client.Object))
		case "statusUpdate":
			err = dryClient.Status().Update(ctx, obj)
		case "statusPatch":
			err = dryClient.Status().Patch(ctx, obj, client.MergeFrom(obj.DeepCopyObject().(client.Object)))
		case "subResourceCreate":
			err = dryClient.SubResource("scale").Create(ctx, obj, obj.DeepCopyObject().(client.Object))
		case "subResourceUpdate":
			err = dryClient.SubResource("scale").Update(ctx, obj)
		case "subResourcePatch":
			err = dryClient.SubResource("scale").Patch(ctx, obj, client.MergeFrom(obj.DeepCopyObject().(client.Object)))
		}
		assert.NoError(t, err)

		var fetchObj corev1.Pod
		err = fakeClient.Get(ctx, client.ObjectKeyFromObject(obj), &fetchObj)
		if action == "create" {
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "not found")
		}
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}
	ctx := context.TODO()

	f(ctx, pod, "create")
	f(ctx, pod, "update")
	f(ctx, pod, "patch")
	f(ctx, pod, "delete")
	f(ctx, pod, "deleteAllOf")

	f(ctx, pod, "statusCreate")
	f(ctx, pod, "statusUpdate")
	f(ctx, pod, "statusPatch")

	f(ctx, pod, "subResourceCreate")
	f(ctx, pod, "subResourceUpdate")
	f(ctx, pod, "subResourcePatch")
}
