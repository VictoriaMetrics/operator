package fuzzutil

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const (
	// fuzzUID is the CR UID, the fake client assigns none.
	fuzzUID = types.UID("fuzz-uid")

	// maxFaultyRounds bounds reconciles under faults, as many as a controller requeues in a burst.
	maxFaultyRounds = 10
)

// childLists returns every list kind of s, so any object a controller creates is compared.
// Events are skipped, they log attempts, not state.
func childLists(s *runtime.Scheme) []client.ObjectList {
	var lists []client.ObjectList
	for gvk := range s.AllKnownTypes() {
		if gvk.Version == runtime.APIVersionInternal || gvk.Kind == "EventList" || !strings.HasSuffix(gvk.Kind, "List") {
			continue
		}
		obj, err := s.New(gvk)
		if err != nil || !meta.IsListType(obj) {
			continue
		}
		if l, ok := obj.(client.ObjectList); ok {
			lists = append(lists, l)
		}
	}
	return lists
}

// Reconciler is a CR controller from the operator package.
type Reconciler interface {
	reconcile.Reconciler
	Init(name string, rclient client.Client, l logr.Logger, sc *runtime.Scheme, cf *config.BaseOperatorConf)
}

// step is a user action on the CR, e.g. spec update, followed by reconciles.
type step struct {
	name string
	do   func(context.Context, client.Client) error
}

// Converge fuzzes a reconciler under injected API faults through CR create, spec update and delete:
// after each step, once faults stop, clean reconciles must succeed and leave the CR and its children
// as a fault-free run does.
// Input is the CR (as in CreateOrUpdate), optional updated CR, and fault bytes for faultPicker.
func Converge[T client.Object](f *testing.F, seeds []string, newCR func() T, newReconciler func() Reconciler) {
	for i, seed := range seeds {
		f.Add([]byte(seed), []byte{}, []byte{}, true)
		f.Add([]byte(seed), []byte(seeds[(i+1)%len(seeds)]), []byte{200, 201, 202, 203, 204, 205}, false)
	}
	// empty input fills foreign finalizers, so delete leaves the CR with deletionTimestamp
	f.Add([]byte{}, []byte{}, []byte{}, true)
	f.Fuzz(func(t *testing.T, data, update, faults []byte, optionalAPIs bool) {
		cr := decodeCR(data, newCR)
		cr.SetUID(fuzzUID)
		cr.SetGeneration(1)
		cr.SetResourceVersion("")
		cr.SetDeletionTimestamp(nil)
		cr.SetManagedFields(nil)

		var updated client.Object
		if len(update) > 0 {
			updated = decodeCR(update, newCR)
		}
		setOptionalAPIs(t, optionalAPIs)

		kind := reflect.TypeOf(cr).Elem().Name()
		build := func(c client.Client) reconcile.Reconciler {
			r := newReconciler()
			r.Init(kind, c, logr.Discard(), c.Scheme(), config.MustGetBaseConfig())
			return r
		}
		synctest.Test(t, func(t *testing.T) {
			converge(t, cr, updated, faults, build)
		})
	})
}

// steps lists actions to converge after: create (cr already exists), spec update if any, delete.
func steps(cr, updated client.Object) []step {
	s := []step{{name: "create", do: func(context.Context, client.Client) error { return nil }}}
	if updated != nil {
		s = append(s, step{name: "update", do: func(ctx context.Context, c client.Client) error { return updateSpec(ctx, c, updated) }})
	}
	return append(s, step{name: "delete", do: func(ctx context.Context, c client.Client) error {
		return c.Delete(ctx, cr.DeepCopyObject().(client.Object))
	}})
}

func converge(t testing.TB, cr, updated client.Object, faults []byte, build func(client.Client) reconcile.Reconciler) {
	ctx := t.Context()
	req := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cr)}
	steps := steps(cr, updated)

	// reference state after each step and fault-free reconciles, inputs it rejects are out of scope
	clean := newClient(cr)
	var wants []map[string]map[string]any
	for _, s := range steps {
		if err := s.do(ctx, clean); err != nil {
			return
		}
		for range 2 {
			if _, err := build(clean).Reconcile(ctx, req); err != nil {
				return
			}
		}
		wants = append(wants, snapshot(t, clean, cr))
	}

	// same steps, faulty rounds after each until fault bytes run out, errors are expected
	raw := newClient(cr)
	gvk, err := apiutil.GVKForObject(cr, raw.Scheme())
	if err != nil {
		t.Fatalf("cannot get CR kind: %s", err)
	}
	picker := newFaultPicker(faults, gvk)
	r := build(picker.wrap(raw.(client.WithWatch)))
	for i, s := range steps {
		if err := s.do(ctx, raw); err != nil {
			t.Fatalf("%s failed: %s\nfaults:\n%s", s.name, err, strings.Join(picker.trace, "\n"))
		}
		for j := 0; j < maxFaultyRounds && !picker.exhausted(); j++ {
			_, _ = r.Reconcile(ctx, req)
		}

		trace := strings.Join(picker.trace, "\n")
		for range 2 {
			if _, err := build(raw).Reconcile(ctx, req); err != nil {
				t.Fatalf("clean reconcile after %s with faults failed: %s\nfaults:\n%s", s.name, err, trace)
			}
		}
		if diff := cmp.Diff(wants[i], snapshot(t, raw, cr)); diff != "" {
			t.Fatalf("state after %s with faults differs from fault-free run (-want +got):\n%s\nfaults:\n%s", s.name, diff, trace)
		}
	}
}

// updateSpec replaces the stored CR spec with updated one, as a user edit does.
func updateSpec(ctx context.Context, c client.Client, updated client.Object) error {
	cur := updated.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, client.ObjectKeyFromObject(updated), cur); err != nil {
		return err
	}
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cur)
	if err != nil {
		return err
	}
	src, err := runtime.DefaultUnstructuredConverter.ToUnstructured(updated)
	if err != nil {
		return err
	}
	u["spec"] = src["spec"]
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u, cur); err != nil {
		return err
	}
	cur.SetGeneration(cur.GetGeneration() + 1)
	return c.Update(ctx, cur)
}

// newClient returns a fake client holding cr and referenced assets, with ready pods for StatefulSets.
func newClient(cr client.Object) client.Client {
	fns := k8stools.GetInterceptorsWithObjects(nil)
	create, update := fns.Create, fns.Update
	fns.Create = func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
		if err := create(ctx, cl, obj, opts...); err != nil {
			return err
		}
		return stsPods(ctx, cl, obj)
	}
	fns.Update = func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
		if err := update(ctx, cl, obj, opts...); err != nil {
			return err
		}
		return stsPods(ctx, cl, obj)
	}
	c := k8stools.GetTestClientWithObjectsAndInterceptors(append(RefObjects(), cr.DeepCopyObject()), fns)
	build.AddDefaults(c.Scheme())
	return c
}

// stsPods creates missing ready pods of a StatefulSet, as its controller does; OnDelete rolling update needs them.
func stsPods(ctx context.Context, cl client.WithWatch, obj client.Object) error {
	sts, ok := obj.(*appsv1.StatefulSet)
	if !ok || sts.Spec.Selector == nil {
		return nil
	}
	for i := range ptr.Deref(sts.Spec.Replicas, 1) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fmt.Sprintf("%s-%d", sts.Name, i),
				Namespace:       sts.Namespace,
				Labels:          map[string]string{appsv1.ControllerRevisionHashLabelKey: sts.Status.UpdateRevision},
				OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "StatefulSet", Name: sts.Name, UID: sts.UID, Controller: ptr.To(true)}},
			},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			},
		}
		maps.Copy(pod.Labels, sts.Spec.Selector.MatchLabels)
		if err := cl.Create(ctx, pod); err != nil && !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}

// snapshot returns normalized CR and children keyed by "kind ns/name", checking children ownerRefs.
func snapshot(t testing.TB, c client.Client, cr client.Object) map[string]map[string]any {
	ctx := t.Context()
	got := map[string]map[string]any{}

	// CR is gone after delete once finalizers are removed
	owner := cr.DeepCopyObject().(client.Object)
	switch err := c.Get(ctx, client.ObjectKeyFromObject(cr), owner); {
	case err == nil:
		got["CR"] = normalize(t, owner, true)
	case !k8serrors.IsNotFound(err):
		t.Fatalf("cannot get CR: %s", err)
	}

	for _, list := range childLists(c.Scheme()) {
		if err := c.List(ctx, list); err != nil {
			t.Fatalf("cannot list %T: %s", list, err)
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			t.Fatalf("cannot extract %T: %s", list, err)
		}
		for _, item := range items {
			obj := item.(client.Object)
			checkOwners(t, obj, cr)
			got[fmt.Sprintf("%T %s/%s", obj, obj.GetNamespace(), obj.GetName())] = normalize(t, obj, false)
		}
	}
	return got
}

// checkOwners fails when obj references the CR kind by name with a wrong UID.
func checkOwners(t testing.TB, obj, owner client.Object) {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Name == owner.GetName() && ref.UID != owner.GetUID() {
			t.Fatalf("%T %s/%s ownerRef %s/%s has UID %q, want %q", obj, obj.GetNamespace(), obj.GetName(), ref.Kind, ref.Name, ref.UID, owner.GetUID())
		}
	}
}

// normalize drops server-assigned fields; children status belongs to their own controllers.
// Timestamps stay, synctest freezes the clock.
func normalize(t testing.TB, obj client.Object, keepStatus bool) map[string]any {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		t.Fatalf("cannot convert %T: %s", obj, err)
	}
	if md, ok := u["metadata"].(map[string]any); ok {
		delete(md, "resourceVersion")
		delete(md, "managedFields")
		delete(md, "generation")

		// synctest clock advances between runs, so only presence of deletionTimestamp matters
		if _, ok := md["deletionTimestamp"]; ok {
			md["deletionTimestamp"] = true
		}
	}
	if !keepStatus {
		delete(u, "status")
	}
	return u
}
