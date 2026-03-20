package operator

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestReconcileAndTrackStatus(t *testing.T) {
	type opts struct {
		object     *vmv1beta1.VMAlert
		cb         func() (ctrl.Result, error)
		wantStatus vmv1beta1.UpdateStatus
		wantResult ctrl.Result
		wantErr    bool
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects([]runtime.Object{o.object})
		result, err := reconcileAndTrackStatus(context.Background(), fclient, o.object, o.cb)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		assert.Equal(t, o.wantResult, result)

		got := &vmv1beta1.VMAlert{}
		assert.NoError(t, fclient.Get(context.Background(), types.NamespacedName{
			Name:      o.object.Name,
			Namespace: o.object.Namespace,
		}, got))
		assert.Equal(t, o.wantStatus, got.Status.UpdateStatus)
	}

	// paused object: callback not called, status set to paused
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmalert",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAlertSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					Paused: true,
				},
			},
		},
		cb: func() (ctrl.Result, error) {
			t.Fatal("callback must not be called when object is paused")
			return ctrl.Result{}, nil
		},
		wantStatus: vmv1beta1.UpdateStatusPaused,
	})

	// object created, status set to operational
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmalert",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAlertSpec{
				SelectAllByDefault: true,
			},
		},
		cb: func() (ctrl.Result, error) {
			return ctrl.Result{}, nil
		},
		wantStatus: vmv1beta1.UpdateStatusOperational,
	})

	// spec unchanged, callback succeeds: status set to operational
	unchangedSpec := vmv1beta1.VMAlertSpec{
		SelectAllByDefault: true,
	}
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmalert",
				Namespace: "default",
			},
			Spec: unchangedSpec,
			Status: vmv1beta1.VMAlertStatus{
				LastAppliedSpec: unchangedSpec.DeepCopy(),
			},
		},
		cb: func() (ctrl.Result, error) {
			return ctrl.Result{}, nil
		},
		wantStatus: vmv1beta1.UpdateStatusOperational,
	})

	// conflict: status set to expanding, no error returned
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmalert",
				Namespace: "default",
			},
		},
		cb: func() (ctrl.Result, error) {
			return ctrl.Result{}, k8serrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "test", fmt.Errorf("conflict"))
		},
		wantStatus: vmv1beta1.UpdateStatusExpanding,
		wantErr:    false,
	})

	// wait interrupted: status set to expanding, no error returned
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmalert",
				Namespace: "default",
			},
		},
		cb: func() (ctrl.Result, error) {
			return ctrl.Result{}, wait.ErrorInterrupted(fmt.Errorf("timeout"))
		},
		wantStatus: vmv1beta1.UpdateStatusExpanding,
		wantErr:    false,
	})

	// non-retryable error: status set to failed, error returned
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmalert",
				Namespace: "default",
			},
		},
		cb: func() (ctrl.Result, error) {
			return ctrl.Result{}, fmt.Errorf("some reconciliation error")
		},
		wantStatus: vmv1beta1.UpdateStatusFailed,
		wantErr:    true,
	})

	// propagate custom result
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmalert",
				Namespace: "default",
			},
		},
		cb: func() (ctrl.Result, error) {
			return ctrl.Result{Requeue: true}, nil
		},
		wantStatus: vmv1beta1.UpdateStatusOperational,
		wantResult: ctrl.Result{Requeue: true},
	})
}
