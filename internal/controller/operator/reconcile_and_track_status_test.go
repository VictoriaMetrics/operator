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
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestReconcileAndTrackStatus(t *testing.T) {
	nsn := types.NamespacedName{Name: "test-vmalert", Namespace: "default"}

	type opts struct {
		object     *vmv1beta1.VMAlert
		cb         func(client.Client) (ctrl.Result, error)
		wantStatus vmv1beta1.UpdateStatus
		wantResult ctrl.Result
		wantErr    bool
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects([]runtime.Object{o.object})
		result, err := reconcileAndTrackStatus(context.Background(), fclient, o.object, func() (ctrl.Result, error) {
			return o.cb(fclient)
		})
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		assert.Equal(t, o.wantResult, result)

		got := &vmv1beta1.VMAlert{}
		assert.NoError(t, fclient.Get(context.Background(), nsn, got))
		assert.Equal(t, o.wantStatus, got.Status.UpdateStatus)
	}

	noop := func(_ client.Client) (ctrl.Result, error) { return ctrl.Result{}, nil }

	// object created (no prior status): callback succeeds → operational
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vmalert", Namespace: "default"},
			Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
		},
		cb:         noop,
		wantStatus: vmv1beta1.UpdateStatusOperational,
	})

	// spec unchanged: callback succeeds → operational
	unchangedSpec := vmv1beta1.VMAlertSpec{SelectAllByDefault: true}
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vmalert", Namespace: "default"},
			Spec:       unchangedSpec,
			Status: vmv1beta1.VMAlertStatus{
				LastAppliedSpec: unchangedSpec.DeepCopy(),
			},
		},
		cb:         noop,
		wantStatus: vmv1beta1.UpdateStatusOperational,
	})

	// retryable conflict error, operational → expanding
	opSpec := vmv1beta1.VMAlertSpec{SelectAllByDefault: true}
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vmalert", Namespace: "default"},
			Spec:       opSpec,
			Status: vmv1beta1.VMAlertStatus{
				StatusMetadata:  vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational},
				LastAppliedSpec: opSpec.DeepCopy(),
			},
		},
		cb: func(_ client.Client) (ctrl.Result, error) {
			return ctrl.Result{}, k8serrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "test", fmt.Errorf("conflict"))
		},
		wantStatus: vmv1beta1.UpdateStatusExpanding,
	})

	// retryable wait interrupted, operational → expanding
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vmalert", Namespace: "default"},
			Spec:       opSpec,
			Status: vmv1beta1.VMAlertStatus{
				StatusMetadata:  vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational},
				LastAppliedSpec: opSpec.DeepCopy(),
			},
		},
		cb: func(_ client.Client) (ctrl.Result, error) {
			return ctrl.Result{}, wait.ErrorInterrupted(fmt.Errorf("timeout"))
		},
		wantStatus: vmv1beta1.UpdateStatusExpanding,
	})

	// operational → expanding → operational
	prevSpec := vmv1beta1.VMAlertSpec{SelectAllByDefault: true}
	newSpec := vmv1beta1.VMAlertSpec{SelectAllByDefault: false}
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vmalert", Namespace: "default"},
			Spec:       newSpec,
			Status: vmv1beta1.VMAlertStatus{
				StatusMetadata:  vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational},
				LastAppliedSpec: prevSpec.DeepCopy(),
			},
		},
		cb: func(c client.Client) (ctrl.Result, error) {
			got := &vmv1beta1.VMAlert{}
			assert.NoError(t, c.Get(context.Background(), nsn, got))
			assert.Equal(t, vmv1beta1.UpdateStatusExpanding, got.Status.UpdateStatus)
			return ctrl.Result{}, nil
		},
		wantStatus: vmv1beta1.UpdateStatusOperational,
	})

	pausedSpec := vmv1beta1.VMAlertSpec{
		SelectAllByDefault: true,
		CommonAppsParams:   vmv1beta1.CommonAppsParams{Paused: true},
	}
	// object created as paused: callback not called, status set to paused
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vmalert", Namespace: "default"},
			Spec:       pausedSpec,
		},
		cb: func(_ client.Client) (ctrl.Result, error) {
			t.Fatal("callback must not be called when object is paused")
			return ctrl.Result{}, nil
		},
		wantStatus: vmv1beta1.UpdateStatusPaused,
	})

	// operational → paused
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vmalert", Namespace: "default"},
			Spec:       pausedSpec,
			Status: vmv1beta1.VMAlertStatus{
				StatusMetadata:  vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusOperational},
				LastAppliedSpec: pausedSpec.DeepCopy(),
			},
		},
		cb: func(_ client.Client) (ctrl.Result, error) {
			t.Fatal("callback must not be called when object is paused")
			return ctrl.Result{}, nil
		},
		wantStatus: vmv1beta1.UpdateStatusPaused,
	})

	// paused → operational
	unpausedSpec := vmv1beta1.VMAlertSpec{SelectAllByDefault: true}
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vmalert", Namespace: "default"},
			Spec:       unpausedSpec,
			Status: vmv1beta1.VMAlertStatus{
				StatusMetadata:  vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusPaused},
				LastAppliedSpec: pausedSpec.DeepCopy(),
			},
		},
		cb:         noop,
		wantStatus: vmv1beta1.UpdateStatusOperational,
	})

	// expanding → operational
	expandingSpec := vmv1beta1.VMAlertSpec{SelectAllByDefault: true}
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vmalert", Namespace: "default"},
			Spec:       expandingSpec,
			Status: vmv1beta1.VMAlertStatus{
				StatusMetadata:  vmv1beta1.StatusMetadata{UpdateStatus: vmv1beta1.UpdateStatusExpanding},
				LastAppliedSpec: expandingSpec.DeepCopy(),
			},
		},
		cb:         noop,
		wantStatus: vmv1beta1.UpdateStatusOperational,
	})

	// non-retryable error: status set to failed, error returned
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vmalert", Namespace: "default"},
		},
		cb: func(_ client.Client) (ctrl.Result, error) {
			return ctrl.Result{}, fmt.Errorf("some reconciliation error")
		},
		wantStatus: vmv1beta1.UpdateStatusFailed,
		wantErr:    true,
	})

	// propagate custom result on success
	f(opts{
		object: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vmalert", Namespace: "default"},
		},
		cb: func(_ client.Client) (ctrl.Result, error) {
			return ctrl.Result{Requeue: true}, nil
		},
		wantStatus: vmv1beta1.UpdateStatusOperational,
		wantResult: ctrl.Result{Requeue: true},
	})
}
