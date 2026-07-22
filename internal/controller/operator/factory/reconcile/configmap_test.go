package reconcile

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestChangedKeysWithSizes(t *testing.T) {
	// no changes
	assert.Empty(t, changedKeysWithSizes(map[string]string{"a": "1"}, map[string]string{"a": "1"}, "data"))

	// nil vs empty map
	assert.Empty(t, changedKeysWithSizes(nil, map[string]string{}, "data"))

	// added, changed and removed keys
	desired := map[string]string{
		"added.yaml":   "abc",
		"changed.yaml": "123456",
		"same.yaml":    "same",
	}
	current := map[string]string{
		"changed.yaml": "12",
		"removed.yaml": "wxyz",
		"same.yaml":    "same",
	}
	expected := []string{
		"data['added.yaml'](current=absent,desired=3B)",
		"data['changed.yaml'](current=2B,desired=6B)",
		"data['removed.yaml'](current=4B,desired=absent)",
	}
	assert.Equal(t, expected, changedKeysWithSizes(desired, current, "data"))

	// binary data
	expected = []string{
		"binaryData['blob'](current=2B,desired=5B)",
	}
	assert.Equal(t, expected, changedKeysWithSizes(
		map[string][]byte{"blob": []byte("12345")},
		map[string][]byte{"blob": []byte("12")},
		"binaryData",
	))
}

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
		synctest.Test(t, func(t *testing.T) {
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
		})
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
			{Verb: "Get", Kind: "ConfigMap", Resource: nn},
			{Verb: "Create", Kind: "ConfigMap", Resource: nn},
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
					Name:      nn.Name,
					Namespace: nn.Namespace,
				},
				Data: map[string]string{
					"data": "test",
				},
			},
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ConfigMap", Resource: nn},
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
			{Verb: "Get", Kind: "ConfigMap", Resource: nn},
			{Verb: "Update", Kind: "ConfigMap", Resource: nn},
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
			{Verb: "Get", Kind: "ConfigMap", Resource: nn},
			{Verb: "Update", Kind: "ConfigMap", Resource: nn},
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
			{Verb: "Get", Kind: "ConfigMap", Resource: nn},
			{Verb: "Update", Kind: "ConfigMap", Resource: nn},
		},
		validate: func(c *corev1.ConfigMap) {
			assert.Equal(t, []byte("after"), c.BinaryData["data"])
		},
	})

	// data key added
	f(opts{
		new: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
			Data: map[string]string{
				"rule-a.yaml": "groups: []",
				"rule-b.yaml": "groups: []",
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
					"rule-a.yaml": "groups: []",
				},
			},
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ConfigMap", Resource: nn},
			{Verb: "Update", Kind: "ConfigMap", Resource: nn},
		},
		validate: func(c *corev1.ConfigMap) {
			assert.Contains(t, c.Data, "rule-b.yaml")
		},
	})

	// data key removed
	f(opts{
		new: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
			Data: map[string]string{
				"rule-a.yaml": "groups: []",
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
					"rule-a.yaml": "groups: []",
					"rule-b.yaml": "groups: []",
				},
			},
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ConfigMap", Resource: nn},
			{Verb: "Update", Kind: "ConfigMap", Resource: nn},
		},
		validate: func(c *corev1.ConfigMap) {
			_, exists := c.Data["rule-b.yaml"]
			assert.False(t, exists, "rule-b.yaml should have been removed from configmap")
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
				},
				Data: map[string]string{
					"data": "test",
				},
			},
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ConfigMap", Resource: nn},
		},
		validate: func(c *corev1.ConfigMap) {
			assert.Equal(t, "test", c.Data["data"])
		},
	})
}
