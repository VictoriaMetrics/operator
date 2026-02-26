package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestDaemonSetReconcile(t *testing.T) {
	type opts struct {
		new, prev         *appsv1.DaemonSet
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
		wantErr           bool
	}
	getDaemonSet := func(fns ...func(d *appsv1.DaemonSet)) *appsv1.DaemonSet {
		d := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-ds",
				Namespace:  "default",
				Finalizers: []string{vmv1beta1.FinalizerName},
			},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": "test"},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image:tag",
							},
						},
					},
				},
			},
		}
		for _, fn := range fns {
			fn(d)
		}
		return d
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		if o.wantErr {
			assert.Error(t, DaemonSet(ctx, cl, o.new, o.prev, nil))
		} else {
			assert.NoError(t, DaemonSet(ctx, cl, o.new, o.prev, nil))
		}
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-ds", Namespace: "default"}

	// create daemonset
	f(opts{
		new: getDaemonSet(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "DaemonSet", Resource: nn},
			{Verb: "Create", Kind: "DaemonSet", Resource: nn},
			{Verb: "Get", Kind: "DaemonSet", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getDaemonSet(),
		prev: getDaemonSet(),
		predefinedObjects: []runtime.Object{
			getDaemonSet(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "DaemonSet", Resource: nn},
			{Verb: "Get", Kind: "DaemonSet", Resource: nn},
		},
	})

	// update spec
	f(opts{
		new: getDaemonSet(func(d *appsv1.DaemonSet) {
			d.Spec.Template.Annotations = map[string]string{"new-annotation": "value"}
		}),
		prev: getDaemonSet(),
		predefinedObjects: []runtime.Object{
			getDaemonSet(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "DaemonSet", Resource: nn},
			{Verb: "Update", Kind: "DaemonSet", Resource: nn},
			{Verb: "Get", Kind: "DaemonSet", Resource: nn},
		},
	})

	// no update on status change
	f(opts{
		new:  getDaemonSet(),
		prev: getDaemonSet(),
		predefinedObjects: []runtime.Object{
			getDaemonSet(func(d *appsv1.DaemonSet) {
				d.Status.NumberReady = 1
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "DaemonSet", Resource: nn},
			{Verb: "Get", Kind: "DaemonSet", Resource: nn},
		},
	})

	// object marked for deletion
	f(opts{
		new: getDaemonSet(),
		predefinedObjects: []runtime.Object{
			getDaemonSet(func(d *appsv1.DaemonSet) {
				d.DeletionTimestamp = ptr.To(metav1.Now())
			}),
		},
		actions: []k8stools.ClientAction{
			// TODO: this looks weird
			{Verb: "Get", Kind: "DaemonSet", Resource: nn},
			{Verb: "Patch", Kind: "DaemonSet", Resource: nn},
			{Verb: "Get", Kind: "DaemonSet", Resource: nn},
			{Verb: "Create", Kind: "DaemonSet", Resource: nn},
			{Verb: "Get", Kind: "DaemonSet", Resource: nn},
		},
	})
}
