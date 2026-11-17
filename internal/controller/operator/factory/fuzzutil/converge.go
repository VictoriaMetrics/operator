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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

// fuzzUID is the CR UID, the fake client assigns none.
const fuzzUID = types.UID("fuzz-uid")

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

// Converge fuzzes a reconciler through CR create, spec update and delete:
// after each step, once reconciled, another reconcile must not write and children must reference the CR UID.
// Input is the CR (as in CreateOrUpdate) and optional updated CR.
func Converge[T client.Object](f *testing.F, seeds []string, newCR func() T, newReconciler func() Reconciler) {
	for i, seed := range seeds {
		f.Add([]byte(seed), []byte{}, true)
		f.Add([]byte(seed), []byte(seeds[(i+1)%len(seeds)]), false)
	}
	// empty input fills foreign finalizers, so delete leaves the CR with deletionTimestamp
	f.Add([]byte{}, []byte{}, true)
	f.Fuzz(func(t *testing.T, data, update []byte, optionalAPIs bool) {
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
			converge(t, cr, updated, build)
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

func converge(t testing.TB, cr, updated client.Object, build func(client.Client) reconcile.Reconciler) {
	ctx := t.Context()
	req := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cr)}

	// inputs rejected by a step or the first reconcile are out of scope
	c := newClient(cr)
	for _, s := range steps(cr, updated) {
		if err := s.do(ctx, c); err != nil {
			return
		}
		if _, err := build(c).Reconcile(ctx, req); err != nil {
			return
		}

		// converged state must not be rewritten, e.g. by defaulting that never settles
		rc, actions := recordWrites(c)
		if _, err := build(rc).Reconcile(ctx, req); err != nil {
			return
		}
		noWrites(t, *actions, "reconcile after "+s.name)
		checkChildren(t, c, cr)
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

// checkChildren fails when any object references the CR by name with a wrong UID.
func checkChildren(t testing.TB, c client.Client, cr client.Object) {
	ctx := t.Context()
	for _, list := range childLists(c.Scheme()) {
		if err := c.List(ctx, list); err != nil {
			t.Fatalf("cannot list %T: %s", list, err)
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			t.Fatalf("cannot extract %T: %s", list, err)
		}
		for _, item := range items {
			checkOwners(t, item.(client.Object), cr)
		}
	}
}

// checkOwners fails when obj references the CR kind by name with a wrong UID.
func checkOwners(t testing.TB, obj, owner client.Object) {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Name == owner.GetName() && ref.UID != owner.GetUID() {
			t.Fatalf("%T %s/%s ownerRef %s/%s has UID %q, want %q", obj, obj.GetNamespace(), obj.GetName(), ref.Kind, ref.Name, ref.UID, owner.GetUID())
		}
	}
}
