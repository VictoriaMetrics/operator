package finalize

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestOnMCPDelete(t *testing.T) {
	ctx := context.TODO()
	f := func(cr client.Object, prefixedName string, onDelete func(client.Client) error) {
		t.Helper()
		objMeta := func(name string) metav1.ObjectMeta {
			return metav1.ObjectMeta{
				Name:       name,
				Namespace:  "default",
				Finalizers: []string{vmv1beta1.FinalizerName},
			}
		}
		children := []client.Object{
			&appsv1.Deployment{ObjectMeta: objMeta(prefixedName)},
			&corev1.Service{ObjectMeta: objMeta(prefixedName)},
			&corev1.Service{ObjectMeta: objMeta("custom-service")},
			&corev1.ServiceAccount{ObjectMeta: objMeta(prefixedName)},
			&policyv1.PodDisruptionBudget{ObjectMeta: objMeta(prefixedName)},
		}
		predefined := []runtime.Object{cr}
		for _, o := range children {
			predefined = append(predefined, o)
		}
		cl := k8stools.GetTestClientWithObjects(predefined)
		require.NoError(t, onDelete(cl))

		for _, o := range append(children, cr) {
			require.NoError(t, cl.Get(ctx, types.NamespacedName{Namespace: o.GetNamespace(), Name: o.GetName()}, o))
			assert.Empty(t, o.GetFinalizers(), "finalizers of %T %s must be removed", o, o.GetName())
		}
	}
	serviceSpec := &vmv1beta1.AdditionalServiceSpec{
		EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "custom-service"},
	}
	crMeta := metav1.ObjectMeta{
		Name:       "test",
		Namespace:  "default",
		Finalizers: []string{vmv1beta1.FinalizerName},
	}

	vm := &vmv1.VMMCP{
		TypeMeta:   metav1.TypeMeta{APIVersion: "operator.victoriametrics.com/v1", Kind: "VMMCP"},
		ObjectMeta: crMeta,
	}
	vm.Spec.ServiceSpec = serviceSpec
	f(vm, "vmmcp-test", func(cl client.Client) error {
		return OnVMMCPDelete(ctx, cl, vm)
	})
}
