package finalize

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestOnVTSingleDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1.VTSingle
		predefinedObjects []runtime.Object
		verify            func(client client.Client)
	}

	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		err := OnVTSingleDelete(ctx, cl, o.cr)
		assert.NoError(t, err)

		if o.verify != nil {
			o.verify(cl)
		}
	}

	cr := &vmv1.VTSingle{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1",
			Kind:       "VTSingle",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-vtsingle",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
		Spec: vmv1.VTSingleSpec{
			ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
				EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
					Name: "custom-service",
				},
			},
		},
	}

	f(opts{
		cr: cr,
		predefinedObjects: []runtime.Object{
			cr,
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.GetServiceAccountName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:       cr.PrefixedName(),
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "custom-service",
					Namespace:  cr.GetNamespace(),
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
		},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
			var vtsingle vmv1.VTSingle
			err := cl.Get(ctx, nsnCR, &vtsingle)
			assert.NoError(t, err)
			assert.Empty(t, vtsingle.Finalizers)

			nsnPrefixed := types.NamespacedName{Name: cr.PrefixedName(), Namespace: cr.Namespace}

			var dep appsv1.Deployment
			err = cl.Get(ctx, nsnPrefixed, &dep)
			assert.NoError(t, err)
			assert.Empty(t, dep.Finalizers)

			var svc corev1.Service
			err = cl.Get(ctx, nsnPrefixed, &svc)
			assert.NoError(t, err)
			assert.Empty(t, svc.Finalizers)

			nsnSA := types.NamespacedName{Name: cr.GetServiceAccountName(), Namespace: cr.Namespace}
			var sa corev1.ServiceAccount
			err = cl.Get(ctx, nsnSA, &sa)
			assert.NoError(t, err)
			assert.Empty(t, sa.Finalizers)

			var pvc corev1.PersistentVolumeClaim
			err = cl.Get(ctx, nsnPrefixed, &pvc)
			assert.NoError(t, err)
			assert.Empty(t, pvc.Finalizers)

			nsnCustomSvc := types.NamespacedName{Name: "custom-service", Namespace: cr.Namespace}
			var customSvc corev1.Service
			err = cl.Get(ctx, nsnCustomSvc, &customSvc)
			assert.NoError(t, err)
			assert.Empty(t, customSvc.Finalizers)
		},
	})
}
