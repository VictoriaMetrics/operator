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

func TestSecretReconcile(t *testing.T) {
	type opts struct {
		new               *corev1.Secret
		prevMeta          *metav1.ObjectMeta
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getSecret := func(fns ...func(s *corev1.Secret)) *corev1.Secret {
		s := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				"key": []byte("value"),
			},
		}
		for _, fn := range fns {
			fn(s)
		}
		return s
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		assert.NoError(t, Secret(ctx, cl, o.new, o.prevMeta, nil))
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-secret", Namespace: "default"}

	// create
	f(opts{
		new: getSecret(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Create", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new: getSecret(),
		prevMeta: &getSecret(func(s *corev1.Secret) {
			s.Finalizers = []string{vmv1beta1.FinalizerName}
		}).ObjectMeta,
		predefinedObjects: []runtime.Object{
			getSecret(func(s *corev1.Secret) {
				s.Finalizers = []string{vmv1beta1.FinalizerName}
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
		},
	})

	// update data
	f(opts{
		new: getSecret(func(s *corev1.Secret) {
			s.Data["key"] = []byte("new-value")
		}),
		predefinedObjects: []runtime.Object{
			getSecret(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Update", Resource: nn},
		},
	})
}
