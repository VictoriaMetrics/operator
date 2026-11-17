package fuzzutil

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// noFaultBytes is the byte range [0, noFaultBytes) meaning no fault, so most calls succeed and reconciles get deep.
const noFaultBytes = 200

type fault string

const (
	faultConflict        fault = "conflict"
	faultTooManyRequests fault = "tooManyRequests"
	faultTimeout         fault = "timeout"
	faultAppliedTimeout  fault = "appliedTimeout" // real write runs, then fails, e.g. response lost after apply
	faultNotFound        fault = "notFound"
	faultStale           fault = "stale" // last real read served again, e.g. lagging informer cache
)

var (
	errInjected = errors.New("injected fault")

	writeFaults = []fault{faultConflict, faultTooManyRequests, faultTimeout, faultAppliedTimeout}
	readFaults  = []fault{faultNotFound, faultStale}
)

// faultPicker consumes one fuzz byte per API call to pick its fault; exhausted data means no fault.
type faultPicker struct {
	mu    sync.Mutex
	data  []byte
	calls int

	// cr kind never gets read faults, otherwise reconcile mostly stops at the CR get.
	cr schema.GroupVersionKind
	// snapshots keeps last real reads served by stale faults.
	snapshots map[string]runtime.Object
	// trace lists fired faults, e.g. "#3 update apps/v1, Kind=Deployment default/vmalert-fuzz: conflict".
	trace []string
}

func newFaultPicker(data []byte, cr schema.GroupVersionKind) *faultPicker {
	return &faultPicker{data: data, cr: cr, snapshots: map[string]runtime.Object{}}
}

func (p *faultPicker) exhausted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.data) == 0
}

// wrap returns c injecting faults into reads, writes and status writes.
func (p *faultPicker) wrap(c client.WithWatch) client.WithWatch {
	return interceptor.NewClient(c, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			return p.read(c.Scheme(), "get", key, obj, func() error { return c.Get(ctx, key, obj, opts...) })
		},
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			lo := (&client.ListOptions{}).ApplyOptions(opts)
			key := client.ObjectKey{Namespace: lo.Namespace, Name: fmt.Sprintf("%v|%v", lo.LabelSelector, lo.FieldSelector)}
			return p.read(c.Scheme(), "list", key, list, func() error { return c.List(ctx, list, opts...) })
		},
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			return p.write(c.Scheme(), "create", obj, func() error { return c.Create(ctx, obj, opts...) })
		},
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			return p.write(c.Scheme(), "update", obj, func() error { return c.Update(ctx, obj, opts...) })
		},
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			return p.write(c.Scheme(), "patch", obj, func() error { return c.Patch(ctx, obj, patch, opts...) })
		},
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			return p.write(c.Scheme(), "delete", obj, func() error { return c.Delete(ctx, obj, opts...) })
		},
		DeleteAllOf: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteAllOfOption) error {
			return p.write(c.Scheme(), "deleteAllOf", obj, func() error { return c.DeleteAllOf(ctx, obj, opts...) })
		},
		SubResourceUpdate: func(ctx context.Context, c client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			return p.write(c.Scheme(), sub+"/update", obj, func() error { return c.SubResource(sub).Update(ctx, obj, opts...) })
		},
		SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			return p.write(c.Scheme(), sub+"/patch", obj, func() error { return c.SubResource(sub).Patch(ctx, obj, patch, opts...) })
		},
	})
}

// pick consumes the next byte and maps it to one of faults; "" means no fault.
func (p *faultPicker) pick(s *runtime.Scheme, obj runtime.Object, faults []fault) (fault, schema.GroupVersionKind) {
	p.calls++
	gvk, _ := apiutil.GVKForObject(obj, s)
	if meta.IsListType(obj) {
		gvk.Kind = strings.TrimSuffix(gvk.Kind, "List")
	}
	if len(p.data) == 0 {
		return "", gvk
	}

	b := p.data[0]
	p.data = p.data[1:]
	if b < noFaultBytes {
		return "", gvk
	}
	return faults[int(b-noFaultBytes)%len(faults)], gvk
}

func (p *faultPicker) fired(verb string, gvk schema.GroupVersionKind, key client.ObjectKey, f fault) {
	p.trace = append(p.trace, fmt.Sprintf("#%d %s %s %s: %s", p.calls, verb, gvk, key, f))
}

// read injects a read fault into obj, else runs do and records its result.
func (p *faultPicker) read(s *runtime.Scheme, verb string, key client.ObjectKey, obj runtime.Object, do func() error) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// type in key keeps typed, unstructured and metadata reads apart
	snap := fmt.Sprintf("%s|%T|%s", verb, obj, key)
	f, gvk := p.pick(s, obj, readFaults)
	switch {
	case gvk == p.cr:
	case f == faultNotFound:
		p.fired(verb, gvk, key, f)
		// lists never return NotFound, an empty result models a missing object
		if meta.IsListType(obj) {
			return meta.SetList(obj, nil)
		}
		return k8serrors.NewNotFound(schema.GroupResource{Group: gvk.Group, Resource: strings.ToLower(gvk.Kind)}, key.Name)
	case f == faultStale:
		old, ok := p.snapshots[snap]
		if !ok {
			break
		}
		p.fired(verb, gvk, key, f)
		reflect.ValueOf(obj).Elem().Set(reflect.ValueOf(old.DeepCopyObject()).Elem())
		return nil
	}

	if err := do(); err != nil {
		return err
	}
	p.snapshots[snap] = obj.DeepCopyObject()
	return nil
}

// write injects a write fault around do, the real call.
func (p *faultPicker) write(s *runtime.Scheme, verb string, obj client.Object, do func() error) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	f, gvk := p.pick(s, obj, writeFaults)
	if f == "" {
		return do()
	}
	p.fired(verb, gvk, client.ObjectKeyFromObject(obj), f)
	if f == faultAppliedTimeout {
		if err := do(); err != nil {
			return err
		}
	}

	switch f {
	case faultConflict:
		gr := schema.GroupResource{Group: gvk.Group, Resource: strings.ToLower(gvk.Kind)}
		return k8serrors.NewConflict(gr, obj.GetName(), errInjected)
	case faultTooManyRequests:
		return k8serrors.NewTooManyRequests(errInjected.Error(), 1)
	}
	return fmt.Errorf("%w: %w", errInjected, context.DeadlineExceeded)
}
