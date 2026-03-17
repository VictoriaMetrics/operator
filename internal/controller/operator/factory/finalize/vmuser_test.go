package finalize

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestOnVMUserDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMUser
		predefinedObjects []runtime.Object
		verify            func(client client.Client)
	}

	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		err := OnVMUserDelete(ctx, cl, o.cr)
		assert.NoError(t, err)

		if o.verify != nil {
			o.verify(cl)
		}
	}

	cr := &vmv1beta1.VMUser{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMUser",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-vmuser",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
	}

	f(opts{
		cr: cr,
		predefinedObjects: []runtime.Object{
			cr,
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
		},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
			var vmuser vmv1beta1.VMUser
			err := cl.Get(ctx, nsnCR, &vmuser)
			assert.NoError(t, err)
			assert.Empty(t, vmuser.Finalizers)

			nsnSecret := types.NamespacedName{Name: cr.PrefixedName(), Namespace: cr.Namespace}
			var sec corev1.Secret
			err = cl.Get(ctx, nsnSecret, &sec)
			assert.NoError(t, err)
			assert.Empty(t, sec.Finalizers)
		},
	})
}
