package reconcile

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

type clientWithActions struct {
	client.Client
	actions []string
}

func getTestClient(o client.Object, predefinedObjects []runtime.Object) *clientWithActions {
	var cwa clientWithActions
	cwa.Client = k8stools.GetTestClientWithObjectsAndInterceptors(predefinedObjects, interceptor.Funcs{
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if o.GetName() == obj.GetName() && o.GetNamespace() == obj.GetNamespace() {
				cwa.actions = append(cwa.actions, "Create")
			}
			return cl.Create(ctx, obj, opts...)
		},
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if o.GetName() == key.Name && o.GetNamespace() == key.Namespace {
				cwa.actions = append(cwa.actions, "Get")
			}
			return cl.Get(ctx, key, obj, opts...)
		},
		Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if o.GetName() == obj.GetName() && o.GetNamespace() == obj.GetNamespace() {
				cwa.actions = append(cwa.actions, "Update")
			}
			return cl.Update(ctx, obj, opts...)
		},
	})
	return &cwa
}
