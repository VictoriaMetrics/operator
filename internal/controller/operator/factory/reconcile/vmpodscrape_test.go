package reconcile

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestVMPodScrape(t *testing.T) {
	type opts struct {
		new, prev *vmv1beta1.VMPodScrape
		preRun    func(c *k8stools.ClientWithActions)
		actions   []k8stools.ClientAction
	}
	getVMPodScrape := func(fns ...func(v *vmv1beta1.VMPodScrape)) *vmv1beta1.VMPodScrape {
		v := &vmv1beta1.VMPodScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmpodscrape",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMPodScrapeSpec{
				PodMetricsEndpoints: []vmv1beta1.PodMetricsEndpoint{
					{Port: ptr.To("web")},
				},
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test",
					},
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
		cl := k8stools.GetTestClientWithActionsAndObjects(nil)
		if o.preRun != nil {
			o.preRun(cl)
			cl.Actions = nil
		}
		synctest.Test(t, func(t *testing.T) {
			assert.NoError(t, VMPodScrape(ctx, cl, o.new, o.prev, nil))
			assert.Equal(t, o.actions, cl.Actions)
		})
	}

	nn := types.NamespacedName{Name: "test-vmpodscrape", Namespace: "default"}

	// create
	f(opts{
		new: getVMPodScrape(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMPodScrape", Resource: nn},
			{Verb: "Create", Kind: "VMPodScrape", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getVMPodScrape(),
		prev: getVMPodScrape(),
		preRun: func(c *k8stools.ClientWithActions) {
			assert.NoError(t, c.Create(context.Background(), getVMPodScrape()))
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMPodScrape", Resource: nn},
		},
	})

	// no update on status change
	f(opts{
		new:  getVMPodScrape(),
		prev: getVMPodScrape(),
		preRun: func(c *k8stools.ClientWithActions) {
			assert.NoError(t, c.Create(context.Background(), getVMPodScrape(func(v *vmv1beta1.VMPodScrape) {
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
			})))
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMPodScrape", Resource: nn},
		},
	})

	// update spec
	f(opts{
		new: getVMPodScrape(func(v *vmv1beta1.VMPodScrape) {
			v.Spec.PodMetricsEndpoints[0].Port = ptr.To("metrics")
		}),
		prev: getVMPodScrape(),
		preRun: func(c *k8stools.ClientWithActions) {
			assert.NoError(t, c.Create(context.Background(), getVMPodScrape()))
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMPodScrape", Resource: nn},
			{Verb: "Update", Kind: "VMPodScrape", Resource: nn},
		},
	})
}
