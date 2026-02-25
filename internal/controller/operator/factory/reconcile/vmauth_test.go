package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestVMAuthReconcile(t *testing.T) {
	type opts struct {
		new, prev         *vmv1beta1.VMAuth
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getVMAuth := func(fns ...func(v *vmv1beta1.VMAuth)) *vmv1beta1.VMAuth {
		v := &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmauth",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAuthSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		}
		for _, fn := range fns {
			fn(v)
		}
		return v
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActionsAndObjects(o.predefinedObjects)
		assert.NoError(t, VMAuth(ctx, cl, o.new, o.prev, nil))
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-vmauth", Namespace: "default"}

	// create
	f(opts{
		new: getVMAuth(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Create", Resource: nn},
			{Verb: "Get", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getVMAuth(),
		prev: getVMAuth(),
		predefinedObjects: []runtime.Object{
			getVMAuth(func(v *vmv1beta1.VMAuth) {
				v.Finalizers = []string{vmv1beta1.FinalizerName}
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Status.ObservedGeneration = v.Generation
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Get", Resource: nn},
		},
	})

	// no update on status change
	f(opts{
		new:  getVMAuth(),
		prev: getVMAuth(),
		predefinedObjects: []runtime.Object{
			getVMAuth(func(v *vmv1beta1.VMAuth) {
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Status.Reason = "some error"
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMAuth", Resource: nn},
			{Verb: "Get", Kind: "VMAuth", Resource: nn},
		},
	})

	// update spec
	f(opts{
		new: getVMAuth(func(v *vmv1beta1.VMAuth) {
			v.Spec.ReplicaCount = ptr.To(int32(2))
		}),
		prev: getVMAuth(),
		predefinedObjects: []runtime.Object{
			getVMAuth(func(v *vmv1beta1.VMAuth) {
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Status.ObservedGeneration = v.Generation
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Update", Resource: nn},
			{Verb: "Get", Resource: nn},
		},
	})
}
