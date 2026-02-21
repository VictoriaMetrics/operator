package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestPersistentVolumeClaimReconcile(t *testing.T) {
	type opts struct {
		new, prev         *corev1.PersistentVolumeClaim
		predefinedObjects []runtime.Object
		actions           []k8stools.ClientAction
	}
	getPVC := func(fns ...func(p *corev1.PersistentVolumeClaim)) *corev1.PersistentVolumeClaim {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc",
				Namespace: "default",
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("10Gi"),
					},
				},
			},
		}
		for _, fn := range fns {
			fn(pvc)
		}
		return pvc
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		assert.NoError(t, PersistentVolumeClaim(ctx, cl, o.new, o.prev, nil))
		assert.Equal(t, o.actions, cl.Actions)
	}

	nn := types.NamespacedName{Name: "test-pvc", Namespace: "default"}

	// create
	f(opts{
		new: getPVC(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: nn},
			{Verb: "Create", Kind: "PersistentVolumeClaim", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getPVC(),
		prev: getPVC(),
		predefinedObjects: []runtime.Object{
			getPVC(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: nn},
		},
	})

	// update annotations
	f(opts{
		new: getPVC(func(p *corev1.PersistentVolumeClaim) {
			p.Annotations = map[string]string{"new-annotation": "value"}
		}),
		prev: getPVC(),
		predefinedObjects: []runtime.Object{
			getPVC(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: nn},
			{Verb: "Update", Kind: "PersistentVolumeClaim", Resource: nn},
		},
	})

	// remove annotations
	f(opts{
		new: getPVC(),
		prev: getPVC(func(p *corev1.PersistentVolumeClaim) {
			p.Annotations = map[string]string{"new-annotation": "value"}
		}),
		predefinedObjects: []runtime.Object{
			getPVC(func(p *corev1.PersistentVolumeClaim) {
				p.Annotations = map[string]string{"new-annotation": "value"}
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: nn},
			{Verb: "Update", Kind: "PersistentVolumeClaim", Resource: nn},
		},
	})
}
