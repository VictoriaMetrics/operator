package manager

import (
	"context"
	"reflect"
	"sync"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DryRunClient wraps client.Client and sends mutating operations with server-side dry-run, so nothing is persisted.
type DryRunClient struct {
	client client.Client
	log    logr.Logger

	mu sync.Mutex
	// deleted holds objects deleted with dry-run, they still exist at the API server.
	deleted map[deletedKey]struct{}
}

type deletedKey struct {
	gvk schema.GroupVersionKind
	nsn types.NamespacedName
}

// This ensures DryRunClient implements all methods of client.Client otherwise it would not compile.
var _ client.Client = (*DryRunClient)(nil)

// kindOf returns obj kind, typed objects have no GVK set, so it falls back to the Go type name.
func kindOf(obj client.Object) string {
	if k := obj.GetObjectKind().GroupVersionKind().Kind; k != "" {
		return k
	}
	typ := reflect.TypeOf(obj)
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ.Name()
}

// NewDryRunClient returns a DryRunClient.
func NewDryRunClient(c client.Client, log logr.Logger) client.Client {
	return &DryRunClient{
		client:  c,
		log:     log,
		deleted: make(map[deletedKey]struct{}),
	}
}

// keyOf returns obj key for tracking dry-run deletions.
func (d *DryRunClient) keyOf(obj client.Object) (deletedKey, bool) {
	gvk, err := d.client.GroupVersionKindFor(obj)
	if err != nil {
		return deletedKey{}, false
	}
	return deletedKey{gvk: gvk, nsn: client.ObjectKeyFromObject(obj)}, true
}

func (d *DryRunClient) wasDeleted(obj client.Object) bool {
	key, ok := d.keyOf(obj)
	if !ok {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok = d.deleted[key]
	return ok
}

func (d *DryRunClient) markDeleted(obj client.Object) {
	key, ok := d.keyOf(obj)
	if !ok {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleted[key] = struct{}{}
}

func (d *DryRunClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return d.client.Get(ctx, key, obj, opts...)
}

func (d *DryRunClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return d.client.List(ctx, list, opts...)
}

func (d *DryRunClient) Scheme() *runtime.Scheme {
	return d.client.Scheme()
}

func (d *DryRunClient) RESTMapper() meta.RESTMapper {
	return d.client.RESTMapper()
}

func (d *DryRunClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return d.client.GroupVersionKindFor(obj)
}

func (d *DryRunClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return d.client.IsObjectNamespaced(obj)
}

func (d *DryRunClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	d.log.Info("dry-run: Create", "kind", kindOf(obj), "name", obj.GetName(), "namespace", obj.GetNamespace())
	err := d.client.Create(ctx, obj, append(opts, client.DryRunAll)...)
	// a recreated object, e.g. a Service with changed type, still exists after its dry-run deletion,
	// so the API server cannot validate its creation. Report it as created instead of a false error.
	if k8serrors.IsAlreadyExists(err) && d.wasDeleted(obj) {
		d.log.Info("dry-run: Create skipped, object was deleted earlier in this dry-run, its spec is not validated by the API server",
			"kind", kindOf(obj), "name", obj.GetName(), "namespace", obj.GetNamespace())
		return nil
	}
	return err
}

func (d *DryRunClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	d.log.Info("dry-run: Delete", "kind", kindOf(obj), "name", obj.GetName(), "namespace", obj.GetNamespace())
	if err := d.client.Delete(ctx, obj, append(opts, client.DryRunAll)...); err != nil {
		return err
	}
	d.markDeleted(obj)
	return nil
}

func (d *DryRunClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	d.log.Info("dry-run: Update", "kind", kindOf(obj), "name", obj.GetName(), "namespace", obj.GetNamespace())
	return d.client.Update(ctx, obj, append(opts, client.DryRunAll)...)
}

func (d *DryRunClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	d.log.Info("dry-run: Patch", "kind", kindOf(obj), "name", obj.GetName(), "namespace", obj.GetNamespace())
	return d.client.Patch(ctx, obj, patch, append(opts, client.DryRunAll)...)
}

func (d *DryRunClient) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
	d.log.Info("dry-run: Apply")
	return d.client.Apply(ctx, obj, append(opts, client.DryRunAll)...)
}

func (d *DryRunClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	d.log.Info("dry-run: DeleteAllOf", "kind", kindOf(obj))
	return d.client.DeleteAllOf(ctx, obj, append(opts, client.DryRunAll)...)
}

func (d *DryRunClient) SubResource(subResource string) client.SubResourceClient {
	return &dryRunSubResourceClient{
		SubResourceClient: d.client.SubResource(subResource),
		log:               d.log,
		subResource:       subResource,
	}
}

type dryRunSubResourceClient struct {
	client.SubResourceClient
	log         logr.Logger
	subResource string
}

func (d *dryRunSubResourceClient) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	d.log.Info("dry-run: SubResource Create", "subResource", d.subResource, "kind", kindOf(obj), "name", obj.GetName(), "namespace", obj.GetNamespace())
	return d.SubResourceClient.Create(ctx, obj, subResource, append(opts, client.DryRunAll)...)
}

func (d *dryRunSubResourceClient) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	d.log.Info("dry-run: SubResource Update", "subResource", d.subResource, "kind", kindOf(obj), "name", obj.GetName(), "namespace", obj.GetNamespace())
	return d.SubResourceClient.Update(ctx, obj, append(opts, client.DryRunAll)...)
}

func (d *dryRunSubResourceClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	d.log.Info("dry-run: SubResource Patch", "subResource", d.subResource, "kind", kindOf(obj), "name", obj.GetName(), "namespace", obj.GetNamespace())
	return d.SubResourceClient.Patch(ctx, obj, patch, append(opts, client.DryRunAll)...)
}

func (d *dryRunSubResourceClient) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
	d.log.Info("dry-run: SubResource Apply", "subResource", d.subResource)
	return d.SubResourceClient.Apply(ctx, obj, append(opts, client.DryRunAll)...)
}

func (d *DryRunClient) Status() client.SubResourceWriter {
	return d.SubResource("status")
}
