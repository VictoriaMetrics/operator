package reconcile

import (
	"context"
	"testing"
	"testing/synctest"

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
		wantErr           bool
	}
	getVMAgent := func(fns ...func(v *vmv1beta1.VMAgent)) *vmv1beta1.VMAgent {
		v := &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
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
		synctest.Test(t, func(t *testing.T) {
			err := VMAgent(ctx, cl, o.new, o.prev, nil)
			if o.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, o.actions, cl.Actions)
			if o.validate != nil {
				var got vmv1beta1.VMAgent
				assert.NoError(t, cl.Get(ctx, types.NamespacedName{Name: o.new.Name, Namespace: o.new.Namespace}, &got))
				o.validate(&got)
			}
		})
	}

	nn := types.NamespacedName{Name: "test-vmagent", Namespace: "default"}

	// create
	f(opts{
		new: getVMAgent(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
			{Verb: "Create", Kind: "VMAgent", Resource: nn},
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getVMAgent(),
		prev: getVMAgent(),
		predefinedObjects: []runtime.Object{
			getVMAgent(func(v *vmv1beta1.VMAgent) {
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Generation = 1
				v.Status.ObservedGeneration = 1
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
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
				v.Generation = 1
				v.Status.ObservedGeneration = 1
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
			{Verb: "Update", Kind: "VMAgent", Resource: nn},
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
		},
	})

	// no update on status change
	f(opts{
		new:  getVMAgent(),
		prev: getVMAgent(),
		predefinedObjects: []runtime.Object{
			getVMAgent(func(v *vmv1beta1.VMAgent) {
				v.Status.Replicas = 1
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
		},
	})

	// configmaps added
	f(opts{
		new: getVMAgent(func(v *vmv1beta1.VMAgent) {
			v.Spec.ConfigMaps = []string{"cm1"}
		}),
		prev: getVMAgent(),
		predefinedObjects: []runtime.Object{
			getVMAgent(func(v *vmv1beta1.VMAgent) {
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Generation = 1
				v.Status.ObservedGeneration = 1
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
			{Verb: "Update", Kind: "VMAgent", Resource: nn},
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
		},
		validate: func(v *vmv1beta1.VMAgent) {
			assert.Equal(t, []string{"cm1"}, v.Spec.ConfigMaps)
		},
	})

	// configmaps changed
	f(opts{
		new: getVMAgent(func(v *vmv1beta1.VMAgent) {
			v.Spec.ConfigMaps = []string{"cm2"}
		}),
		prev: getVMAgent(func(v *vmv1beta1.VMAgent) {
			v.Spec.ConfigMaps = []string{"cm1"}
		}),
		predefinedObjects: []runtime.Object{
			getVMAgent(func(v *vmv1beta1.VMAgent) {
				v.Spec.ConfigMaps = []string{"cm1"}
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Generation = 1
				v.Status.ObservedGeneration = 1
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
			{Verb: "Update", Kind: "VMAgent", Resource: nn},
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
		},
		validate: func(v *vmv1beta1.VMAgent) {
			assert.Equal(t, []string{"cm2"}, v.Spec.ConfigMaps)
		},
	})

	// configmaps removed
	f(opts{
		new: getVMAgent(),
		prev: getVMAgent(func(v *vmv1beta1.VMAgent) {
			v.Spec.ConfigMaps = []string{"cm1"}
		}),
		predefinedObjects: []runtime.Object{
			getVMAgent(func(v *vmv1beta1.VMAgent) {
				v.Spec.ConfigMaps = []string{"cm1"}
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Generation = 1
				v.Status.ObservedGeneration = 1
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
			{Verb: "Update", Kind: "VMAgent", Resource: nn},
			{Verb: "Get", Kind: "VMAgent", Resource: nn},
		},
		validate: func(v *vmv1beta1.VMAgent) {
			assert.Empty(t, v.Spec.ConfigMaps)
		},
	})
}
