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

// stickyReconciler marks the CR failed on a write error and never clears the mark.
type stickyReconciler struct{ client.Client }

func (r stickyReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: req.Namespace}}
	err := r.Create(ctx, cm)
	if err == nil || k8serrors.IsAlreadyExists(err) {
		return reconcile.Result{}, nil
	}

	var cr vmv1beta1.VMAlert
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		return reconcile.Result{}, err
	}
	cr.Annotations = map[string]string{"failed": "true"}
	return reconcile.Result{}, r.Update(ctx, &cr)
}

func TestConverge(t *testing.T) {
	f := func(faults []byte, wantErr string) {
		t.Helper()
		cr := &vmv1beta1.VMAlert{ObjectMeta: metav1.ObjectMeta{Name: fuzzName, Namespace: fuzzNamespace, UID: fuzzUID}}
		rec := &fatalRecorder{TB: t}
		done := make(chan struct{})
		go func() {
			defer close(done)
			converge(rec, cr, nil, faults, func(c client.Client) reconcile.Reconciler { return stickyReconciler{c} })
		}()
		<-done
		if (wantErr == "") != (rec.msg == "") || !strings.Contains(rec.msg, wantErr) {
			t.Fatalf("got failure %q, want it to contain %q", rec.msg, wantErr)
		}
	}

	// no faults
	f(nil, "")

	// conflict on child create leaves the failed mark
	f([]byte{200}, "state after create with faults differs")
}
