package manager

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DryRunClient wraps client.Client and sends mutating operations with server-side dry-run, so nothing is persisted.
type DryRunClient struct {
	client.Client
	log logr.Logger
}

// NewDryRunClient returns a DryRunClient.
func NewDryRunClient(c client.Client, log logr.Logger) client.Client {
	return &DryRunClient{
		Client: c,
		log:    log,
	}
}

func (d *DryRunClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	d.log.Info("dry-run: Create", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName(), "namespace", obj.GetNamespace())
	return d.Client.Create(ctx, obj, append(opts, client.DryRunAll)...)
}

func (d *DryRunClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	d.log.Info("dry-run: Delete", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName(), "namespace", obj.GetNamespace())
	return d.Client.Delete(ctx, obj, append(opts, client.DryRunAll)...)
}

func (d *DryRunClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	d.log.Info("dry-run: Update", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName(), "namespace", obj.GetNamespace())
	return d.Client.Update(ctx, obj, append(opts, client.DryRunAll)...)
}

func (d *DryRunClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	d.log.Info("dry-run: Patch", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName(), "namespace", obj.GetNamespace())
	return d.Client.Patch(ctx, obj, patch, append(opts, client.DryRunAll)...)
}

func (d *DryRunClient) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
	d.log.Info("dry-run: Apply")
	return d.Client.Apply(ctx, obj, append(opts, client.DryRunAll)...)
}

func (d *DryRunClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	d.log.Info("dry-run: DeleteAllOf", "kind", obj.GetObjectKind().GroupVersionKind().Kind)
	return d.Client.DeleteAllOf(ctx, obj, append(opts, client.DryRunAll)...)
}

func (d *DryRunClient) SubResource(subResource string) client.SubResourceClient {
	return &dryRunSubResourceClient{
		SubResourceClient: d.Client.SubResource(subResource),
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
	d.log.Info("dry-run: SubResource Create", "subResource", d.subResource, "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName(), "namespace", obj.GetNamespace())
	return d.SubResourceClient.Create(ctx, obj, subResource, append(opts, client.DryRunAll)...)
}

func (d *dryRunSubResourceClient) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	d.log.Info("dry-run: SubResource Update", "subResource", d.subResource, "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName(), "namespace", obj.GetNamespace())
	return d.SubResourceClient.Update(ctx, obj, append(opts, client.DryRunAll)...)
}

func (d *dryRunSubResourceClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	d.log.Info("dry-run: SubResource Patch", "subResource", d.subResource, "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName(), "namespace", obj.GetNamespace())
	return d.SubResourceClient.Patch(ctx, obj, patch, append(opts, client.DryRunAll)...)
}

func (d *dryRunSubResourceClient) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
	d.log.Info("dry-run: SubResource Apply", "subResource", d.subResource)
	return d.SubResourceClient.Apply(ctx, obj, append(opts, client.DryRunAll)...)
}

func (d *DryRunClient) Status() client.SubResourceWriter {
	return d.SubResource("status")
}
