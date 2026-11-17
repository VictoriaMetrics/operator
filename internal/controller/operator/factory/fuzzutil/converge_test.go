package fuzzutil

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// fatalRecorder captures Fatalf instead of failing the test.
type fatalRecorder struct {
	testing.TB
	msg string
}

func (r *fatalRecorder) Fatalf(format string, args ...any) {
	r.msg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// childReconciler creates a child ConfigMap once, or on every run if flap is set.
type childReconciler struct {
	client.Client
	flap bool
}

func (r childReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: req.Namespace}}
	if err := r.Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{}); err == nil && !r.flap {
		return reconcile.Result{}, nil
	}
	if err := r.Create(ctx, cm); err != nil && !k8serrors.IsAlreadyExists(err) {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func TestConverge(t *testing.T) {
	f := func(flap bool, wantErr string) {
		t.Helper()
		cr := &vmv1beta1.VMAlert{ObjectMeta: metav1.ObjectMeta{Name: fuzzName, Namespace: fuzzNamespace, UID: fuzzUID}}
		rec := &fatalRecorder{TB: t}
		done := make(chan struct{})
		go func() {
			defer close(done)
			converge(rec, cr, nil, func(c client.Client) reconcile.Reconciler { return childReconciler{c, flap} })
		}()
		<-done
		if (wantErr == "") != (rec.msg == "") || !strings.Contains(rec.msg, wantErr) {
			t.Fatalf("got failure %q, want it to contain %q", rec.msg, wantErr)
		}
	}

	// converged child is left alone
	f(false, "")

	// converged child is written again
	f(true, "wrote:\nCreate ConfigMap default/child")
}
