package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestConfigMapReconcile(t *testing.T) {
	type opts struct {
		new               *corev1.ConfigMap
		prevMeta          *metav1.ObjectMeta
		predefinedObjects []runtime.Object
		validate          func(*k8stools.TestClientWithStatsTrack, *corev1.ConfigMap)
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		rclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		_, err := ConfigMap(ctx, rclient, o.new, o.prevMeta)
		assert.NoError(t, err)
		var got corev1.ConfigMap
		nsn := types.NamespacedName{
			Name:      o.new.Name,
			Namespace: o.new.Namespace,
		}
		assert.NoError(t, rclient.Get(ctx, nsn, &got))
		if o.validate != nil {
			o.validate(rclient, &got)
		}
	}

	// create configmap
	f(opts{
		new: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Data: map[string]string{
				"data": "test",
			},
		},
		validate: func(rclient *k8stools.TestClientWithStatsTrack, c *corev1.ConfigMap) {
			assert.Equal(t, 1, rclient.CreateCalls.Count(c))
			assert.Equal(t, 0, rclient.UpdateCalls.Count(c))
			assert.Equal(t, "test", c.Data["data"])
		},
	})

	// no update needed
	f(opts{
		new: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Data: map[string]string{
				"data": "test",
			},
		},
		prevMeta: &metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Data: map[string]string{
					"data": "test",
				},
			},
		},
		validate: func(rclient *k8stools.TestClientWithStatsTrack, c *corev1.ConfigMap) {
			assert.Equal(t, 0, rclient.CreateCalls.Count(c))
			assert.Equal(t, 0, rclient.UpdateCalls.Count(c))
			assert.Equal(t, "test", c.Data["data"])
		},
	})

	// annotations changed
	f(opts{
		new: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
				Annotations: map[string]string{
					"key": "value",
				},
			},
			Data: map[string]string{
				"data": "test",
			},
		},
		prevMeta: &metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Data: map[string]string{
					"data": "test",
				},
			},
		},
		validate: func(rclient *k8stools.TestClientWithStatsTrack, c *corev1.ConfigMap) {
			assert.Equal(t, 0, rclient.CreateCalls.Count(c))
			assert.Equal(t, 1, rclient.UpdateCalls.Count(c))
			assert.Equal(t, "value", c.Annotations["key"])
		},
	})

	// data updated
	f(opts{
		new: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Data: map[string]string{
				"data": "after",
			},
		},
		prevMeta: &metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Data: map[string]string{
					"data": "before",
				},
			},
		},
		validate: func(rclient *k8stools.TestClientWithStatsTrack, c *corev1.ConfigMap) {
			assert.Equal(t, 0, rclient.CreateCalls.Count(c))
			assert.Equal(t, 1, rclient.UpdateCalls.Count(c))
			assert.Equal(t, "after", c.Data["data"])
		},
	})

	// np update with 3-rd party annotations
	f(opts{
		new: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
				Annotations: map[string]string{
					"key": "value",
				},
			},
			Data: map[string]string{
				"data": "test",
			},
		},
		prevMeta: &metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Annotations: map[string]string{
				"key": "value",
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					Annotations: map[string]string{
						"key":      "value",
						"external": "value",
					},
				},
				Data: map[string]string{
					"data": "test",
				},
			},
		},
		validate: func(rclient *k8stools.TestClientWithStatsTrack, c *corev1.ConfigMap) {
			assert.Equal(t, 0, rclient.CreateCalls.Count(c))
			assert.Equal(t, 0, rclient.UpdateCalls.Count(c))
			assert.Equal(t, "test", c.Data["data"])
		},
	})
}
