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

func TestVMServiceScrapeForCRD(t *testing.T) {
	type opts struct {
		new, prev         *vmv1beta1.VMServiceScrape
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getVMServiceScrape := func(fns ...func(v *vmv1beta1.VMServiceScrape)) *vmv1beta1.VMServiceScrape {
		v := &vmv1beta1.VMServiceScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-vmservicescrape",
				Namespace:  "default",
				Finalizers: []string{vmv1beta1.FinalizerName},
			},
			Spec: vmv1beta1.VMServiceScrapeSpec{
				Endpoints: []vmv1beta1.Endpoint{
					{Port: "web"},
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
		cl := k8stools.GetTestClientWithActionsAndObjects(o.predefinedObjects)
		assert.NoError(t, VMServiceScrapeForCRD(ctx, cl, o.new, o.prev, nil))
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-vmservicescrape", Namespace: "default"}

	// create
	f(opts{
		new: getVMServiceScrape(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMServiceScrape", Resource: nn},
			{Verb: "Create", Kind: "VMServiceScrape", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getVMServiceScrape(),
		prev: getVMServiceScrape(),
		predefinedObjects: []runtime.Object{
			getVMServiceScrape(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMServiceScrape", Resource: nn},
		},
	})

	// no update on status change
	f(opts{
		new:  getVMServiceScrape(),
		prev: getVMServiceScrape(),
		predefinedObjects: []runtime.Object{
			getVMServiceScrape(func(v *vmv1beta1.VMServiceScrape) {
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusFailed
				v.Status.Reason = "some error"
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMServiceScrape", Resource: nn},
		},
	})

	// update spec
	f(opts{
		new: getVMServiceScrape(func(v *vmv1beta1.VMServiceScrape) {
			v.Spec.Endpoints[0].Port = "metrics"
		}),
		prev: getVMServiceScrape(),
		predefinedObjects: []runtime.Object{
			getVMServiceScrape(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMServiceScrape", Resource: nn},
			{Verb: "Update", Kind: "VMServiceScrape", Resource: nn},
		},
	})
}

func TestVMPodScrapeForCRD(t *testing.T) {
	type opts struct {
		new, prev         *vmv1beta1.VMPodScrape
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getVMPodScrape := func(fns ...func(v *vmv1beta1.VMPodScrape)) *vmv1beta1.VMPodScrape {
		v := &vmv1beta1.VMPodScrape{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-vmpodscrape",
				Namespace:  "default",
				Finalizers: []string{vmv1beta1.FinalizerName},
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
		cl := k8stools.GetTestClientWithActionsAndObjects(o.predefinedObjects)
		assert.NoError(t, VMPodScrape(ctx, cl, o.new, o.prev, nil))
		assert.Equal(t, o.actions, cl.Actions)
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
		predefinedObjects: []runtime.Object{
			getVMPodScrape(),
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
		predefinedObjects: []runtime.Object{
			getVMPodScrape(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "VMPodScrape", Resource: nn},
			{Verb: "Update", Kind: "VMPodScrape", Resource: nn},
		},
	})
}
