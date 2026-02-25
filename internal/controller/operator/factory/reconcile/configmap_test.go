package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestConfigMapReconcile(t *testing.T) {
	type opts struct {
		new               *corev1.ConfigMap
		prevMeta          *metav1.ObjectMeta
		owner             *metav1.OwnerReference
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
		validate          func(*corev1.ConfigMap)
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		_, err := ConfigMap(ctx, cl, o.new, o.prevMeta, o.owner)
		assert.NoError(t, err)
		assert.Equal(t, o.actions, cl.Actions)
		if o.validate != nil {
			var got corev1.ConfigMap
			nsn := types.NamespacedName{
				Name:      o.new.Name,
				Namespace: o.new.Namespace,
			}
			assert.NoError(t, cl.Get(ctx, nsn, &got))
			o.validate(&got)
		}
	}

	nn := types.NamespacedName{Name: "test", Namespace: "default"}

	// create configmap
	f(opts{
		new: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
			Data: map[string]string{
				"data": "test",
			},
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Create", Resource: nn},
		},
		validate: func(c *corev1.ConfigMap) {
			assert.Equal(t, "test", c.Data["data"])
		},
	})

	// no update needed
	f(opts{
		new: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
			Data: map[string]string{
				"data": "test",
			},
		},
		prevMeta: &metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       nn.Name,
					Namespace:  nn.Namespace,
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
				Data: map[string]string{
					"data": "test",
				},
			},
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
		},
		validate: func(c *corev1.ConfigMap) {
			assert.Equal(t, "test", c.Data["data"])
		},
	})

	// annotations changed
	f(opts{
		new: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
				Annotations: map[string]string{
					"key": "value",
				},
			},
			Data: map[string]string{
				"data": "test",
			},
		},
		prevMeta: &metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nn.Name,
					Namespace: nn.Namespace,
				},
				Data: map[string]string{
					"data": "test",
				},
			},
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Update", Resource: nn},
		},
		validate: func(c *corev1.ConfigMap) {
			assert.Equal(t, "value", c.Annotations["key"])
		},
	})

	// data updated
	f(opts{
		new: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
			Data: map[string]string{
				"data": "after",
			},
		},
		prevMeta: &metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nn.Name,
					Namespace: nn.Namespace,
				},
				Data: map[string]string{
					"data": "before",
				},
			},
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Update", Resource: nn},
		},
		validate: func(c *corev1.ConfigMap) {
			assert.Equal(t, "after", c.Data["data"])
		},
	})

	// binary data updated
	f(opts{
		new: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
			BinaryData: map[string][]byte{
				"data": []byte("after"),
			},
		},
		prevMeta: &metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nn.Name,
					Namespace: nn.Namespace,
				},
				BinaryData: map[string][]byte{
					"data": []byte("before"),
				},
			},
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Update", Resource: nn},
		},
		validate: func(c *corev1.ConfigMap) {
			assert.Equal(t, []byte("after"), c.BinaryData["data"])
		},
	})

	// no update with 3-rd party annotations
	f(opts{
		new: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
				Annotations: map[string]string{
					"key": "value",
				},
			},
			Data: map[string]string{
				"data": "test",
			},
		},
		prevMeta: &metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
			Annotations: map[string]string{
				"key": "value",
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nn.Name,
					Namespace: nn.Namespace,
					Annotations: map[string]string{
						"key":      "value",
						"external": "value",
					},
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
				Data: map[string]string{
					"data": "test",
				},
			},
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
		},
		validate: func(c *corev1.ConfigMap) {
			assert.Equal(t, "test", c.Data["data"])
		},
	})
}
