package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestPDBReconcile(t *testing.T) {
	type opts struct {
		new, prev         *policyv1.PodDisruptionBudget
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getPDB := func(fns ...func(p *policyv1.PodDisruptionBudget)) *policyv1.PodDisruptionBudget {
		minAvailable := intstr.FromInt(1)
		pdb := &policyv1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pdb",
				Namespace: "default",
			},
			Spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: &minAvailable,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test",
					},
				},
			},
		}
		for _, fn := range fns {
			fn(pdb)
		}
		return pdb
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		assert.NoError(t, PDB(ctx, cl, o.new, o.prev, nil))
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-pdb", Namespace: "default"}

	// create
	f(opts{
		new: getPDB(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "PodDisruptionBudget", Resource: nn},
			{Verb: "Create", Kind: "PodDisruptionBudget", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getPDB(),
		prev: getPDB(),
		predefinedObjects: []runtime.Object{
			getPDB(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "PodDisruptionBudget", Resource: nn},
		},
	})

	// no update on status change
	f(opts{
		new:  getPDB(),
		prev: getPDB(),
		predefinedObjects: []runtime.Object{
			getPDB(func(p *policyv1.PodDisruptionBudget) {
				p.Status.DisruptionsAllowed = 1
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "PodDisruptionBudget", Resource: nn},
		},
	})

	// update spec
	f(opts{
		new: getPDB(func(p *policyv1.PodDisruptionBudget) {
			p.Spec.MinAvailable = nil
			p.Spec.MaxUnavailable = ptr.To(intstr.FromInt(1))
		}),
		prev: getPDB(),
		predefinedObjects: []runtime.Object{
			getPDB(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "PodDisruptionBudget", Resource: nn},
			{Verb: "Update", Kind: "PodDisruptionBudget", Resource: nn},
		},
	})
}
