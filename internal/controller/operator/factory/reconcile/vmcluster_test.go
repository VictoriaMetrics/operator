package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestVMClusterReconcile(t *testing.T) {
	type opts struct {
		new, prev         *vmv1beta1.VMCluster
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getVMCluster := func(fns ...func(v *vmv1beta1.VMCluster)) *vmv1beta1.VMCluster {
		v := &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmcluster",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod: "1d",
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
		assert.NoError(t, VMCluster(ctx, cl, o.new, o.prev, nil))
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-vmcluster", Namespace: "default"}

	// create
	f(opts{
		new: getVMCluster(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMCluster", Resource: nn},
			{Verb: "Create", Kind: "VMCluster", Resource: nn},
			{Verb: "Get", Kind: "VMCluster", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getVMCluster(),
		prev: getVMCluster(),
		predefinedObjects: []runtime.Object{
			getVMCluster(func(v *vmv1beta1.VMCluster) {
				v.Finalizers = []string{vmv1beta1.FinalizerName}
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Status.ObservedGeneration = v.Generation
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMCluster", Resource: nn},
			{Verb: "Get", Kind: "VMCluster", Resource: nn},
		},
	})

	// update spec
	f(opts{
		new: getVMCluster(func(v *vmv1beta1.VMCluster) {
			v.Spec.RetentionPeriod = "2d"
		}),
		prev: getVMCluster(),
		predefinedObjects: []runtime.Object{
			getVMCluster(func(v *vmv1beta1.VMCluster) {
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Status.ObservedGeneration = v.Generation
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMCluster", Resource: nn},
			{Verb: "Update", Kind: "VMCluster", Resource: nn},
			{Verb: "Get", Kind: "VMCluster", Resource: nn},
		},
	})
}
