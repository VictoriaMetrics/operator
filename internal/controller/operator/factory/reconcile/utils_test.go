package reconcile

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

type clientAction struct {
	Verb     string
	Resource types.NamespacedName
}

type clientWithActions struct {
	client.Client
	actions []clientAction
}

func getTestClient(predefinedObjects []runtime.Object) *clientWithActions {
	var cwa clientWithActions

	cwa.Client = k8stools.GetTestClientWithObjectsAndInterceptors(predefinedObjects, interceptor.Funcs{
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			cwa.actions = append(cwa.actions, clientAction{Verb: "Create", Resource: client.ObjectKeyFromObject(obj)})
			return cl.Create(ctx, obj, opts...)
		},
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			cwa.actions = append(cwa.actions, clientAction{Verb: "Get", Resource: key})
			return cl.Get(ctx, key, obj, opts...)
		},
		Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			cwa.actions = append(cwa.actions, clientAction{Verb: "Update", Resource: client.ObjectKeyFromObject(obj)})
			return cl.Update(ctx, obj, opts...)
		},
	})
	return &cwa
}
