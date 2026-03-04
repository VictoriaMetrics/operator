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

func TestServiceAccountReconcile(t *testing.T) {
	type opts struct {
		new, prev         *corev1.ServiceAccount
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getSA := func(fns ...func(sa *corev1.ServiceAccount)) *corev1.ServiceAccount {
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: "default",
			},
		}
		for _, fn := range fns {
			fn(sa)
		}
		return sa
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		synctest.Test(t, func(t *testing.T) {
			assert.NoError(t, ServiceAccount(ctx, cl, o.new, o.prev, nil))
			assert.Equal(t, o.actions, cl.Actions)
		})
	}

	nn := types.NamespacedName{Name: "test-sa", Namespace: "default"}

	// create
	f(opts{
		new: getSA(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ServiceAccount", Resource: nn},
			{Verb: "Create", Kind: "ServiceAccount", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getSA(),
		prev: getSA(),
		predefinedObjects: []runtime.Object{
			getSA(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ServiceAccount", Resource: nn},
		},
	})

	// update annotations
	f(opts{
		new: getSA(func(sa *corev1.ServiceAccount) {
			sa.Annotations = map[string]string{"new-key": "new-value"}
		}),
		prev: getSA(),
		predefinedObjects: []runtime.Object{
			getSA(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ServiceAccount", Resource: nn},
			{Verb: "Update", Kind: "ServiceAccount", Resource: nn},
		},
	})
}
