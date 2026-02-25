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

func TestVMAgentReconcile(t *testing.T) {
	type opts struct {
		new, prev         *vmv1beta1.VMAgent
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
		validate          func(*vmv1beta1.VMAgent)
	}
	getVMAgent := func(fns ...func(v *vmv1beta1.VMAgent)) *vmv1beta1.VMAgent {
		v := &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
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
		assert.NoError(t, VMAgent(ctx, cl, o.new, o.prev, nil))
		assert.Equal(t, o.actions, cl.Actions)
		if o.validate != nil {
			var got vmv1beta1.VMAgent
			assert.NoError(t, cl.Get(ctx, types.NamespacedName{Name: o.new.Name, Namespace: o.new.Namespace}, &got))
			o.validate(&got)
		}
	}

	nn := types.NamespacedName{Name: "test-vmagent", Namespace: "default"}

	// create
	f(opts{
		new: getVMAgent(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Resource: nn},
			{Verb: "Create", Resource: nn},
			{Verb: "Get", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getVMAgent(),
		prev: getVMAgent(),
		predefinedObjects: []runtime.Object{
			getVMAgent(func(v *vmv1beta1.VMAgent) {
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

	// update spec
	f(opts{
		new: getVMAgent(func(v *vmv1beta1.VMAgent) {
			v.Spec.ReplicaCount = ptr.To(int32(2))
		}),
		prev: getVMAgent(),
		predefinedObjects: []runtime.Object{
			getVMAgent(func(v *vmv1beta1.VMAgent) {
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

	// update annotations
	f(opts{
		new: getVMAgent(func(v *vmv1beta1.VMAgent) {
			v.Annotations = map[string]string{"new-annotation": "value"}
		}),
		prev: getVMAgent(),
		predefinedObjects: []runtime.Object{
			getVMAgent(func(v *vmv1beta1.VMAgent) {
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

	// remove annotations
	f(opts{
		new: getVMAgent(),
		prev: getVMAgent(func(v *vmv1beta1.VMAgent) {
			v.Annotations = map[string]string{"new-annotation": "value"}
		}),
		predefinedObjects: []runtime.Object{
			getVMAgent(func(v *vmv1beta1.VMAgent) {
				v.Annotations = map[string]string{"new-annotation": "value"}
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
