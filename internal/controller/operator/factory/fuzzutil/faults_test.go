package fuzzutil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFaultPicker(t *testing.T) {
	ctx := context.TODO()
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "a"}}
	key := client.ObjectKeyFromObject(cm)
	base := fake.NewClientBuilder().WithObjects(cm.DeepCopy()).Build()
	podKind := corev1.SchemeGroupVersion.WithKind("Pod")

	// 0: no fault, 200: notFound, 201: stale, 200: conflict, 203: appliedTimeout, then data exhausted
	p := newFaultPicker([]byte{0, 200, 201, 200, 203}, podKind)
	c := p.wrap(base)
	var got corev1.ConfigMap
	require.NoError(t, c.Get(ctx, key, &got))
	assert.True(t, k8serrors.IsNotFound(c.Get(ctx, key, &corev1.ConfigMap{})))

	cm.Labels = map[string]string{"v": "1"}
	require.NoError(t, base.Update(ctx, cm))
	require.NoError(t, c.Get(ctx, key, &got))
	assert.Empty(t, got.Labels, "stale must serve the first read")

	assert.True(t, k8serrors.IsConflict(c.Update(ctx, cm)))
	cm.Labels["v"] = "2"
	assert.ErrorIs(t, c.Update(ctx, cm), context.DeadlineExceeded)
	require.NoError(t, base.Get(ctx, key, &got))
	assert.Equal(t, "2", got.Labels["v"], "appliedTimeout must apply the write")

	require.NoError(t, c.Update(ctx, cm))
	assert.True(t, p.exhausted())
	assert.Len(t, p.trace, 4)

	// CR kind never gets read faults
	p = newFaultPicker([]byte{200}, podKind)
	err := p.wrap(base).Get(ctx, key, &corev1.Pod{})
	assert.True(t, k8serrors.IsNotFound(err), "real read runs")
	assert.Empty(t, p.trace)
}
