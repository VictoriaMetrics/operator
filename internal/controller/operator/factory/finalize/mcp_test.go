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

func TestOnVMMCPDelete(t *testing.T) {
	ctx := context.TODO()
	cr := &vmv1.VMMCP{
		TypeMeta: metav1.TypeMeta{APIVersion: "operator.victoriametrics.com/v1", Kind: "VMMCP"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: "v1", Kind: "ConfigMap", Name: "owner", UID: "owner-uid"},
			},
		},
	}
	cr.Spec.ServiceSpec = &vmv1beta1.AdditionalServiceSpec{
		EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "custom-service"},
	}
	objMeta := func(name string) metav1.ObjectMeta {
		return metav1.ObjectMeta{
			Name:            name,
			Namespace:       "default",
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		}
	}
	objs := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta("vmmcp-test")},
		&corev1.Service{ObjectMeta: objMeta("vmmcp-test")},
		&corev1.Service{ObjectMeta: objMeta("custom-service")},
		&corev1.ServiceAccount{ObjectMeta: objMeta("vmmcp-test")},
		&policyv1.PodDisruptionBudget{ObjectMeta: objMeta("vmmcp-test")},
		cr,
	}
	predefined := make([]runtime.Object, 0, len(objs))
	ownerRefs := make([][]metav1.OwnerReference, 0, len(objs))
	for _, o := range objs {
		predefined = append(predefined, o.DeepCopyObject())
		ownerRefs = append(ownerRefs, o.GetOwnerReferences())
	}
	cl := k8stools.GetTestClientWithObjects(predefined)
	require.NoError(t, OnVMMCPDelete(ctx, cl, cr))

	for i, o := range objs {
		require.NoError(t, cl.Get(ctx, types.NamespacedName{Namespace: o.GetNamespace(), Name: o.GetName()}, o))
		assert.Empty(t, o.GetFinalizers(), "finalizers of %T %s must be removed", o, o.GetName())
		assert.Equal(t, ownerRefs[i], o.GetOwnerReferences(), "owner references of %T %s must be retained", o, o.GetName())
	}
}

func TestOnVMMCPDeleteExternalServiceAccount(t *testing.T) {
	ctx := context.TODO()
	cr := &vmv1.VMMCP{
		TypeMeta: metav1.TypeMeta{APIVersion: "operator.victoriametrics.com/v1", Kind: "VMMCP"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
	}
	cr.Spec.ServiceAccountName = "external"
	// the VMMCP doesn't own the ServiceAccount set in spec.serviceAccountName, e.g. one shared with another VMMCP
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
		Name:       "external",
		Namespace:  "default",
		Finalizers: []string{vmv1beta1.FinalizerName},
	}}
	cl := k8stools.GetTestClientWithObjects([]runtime.Object{cr.DeepCopy(), sa.DeepCopy()})
	require.NoError(t, OnVMMCPDelete(ctx, cl, cr))

	require.NoError(t, cl.Get(ctx, types.NamespacedName{Namespace: "default", Name: "external"}, sa))
	assert.Equal(t, []string{vmv1beta1.FinalizerName}, sa.GetFinalizers(), "finalizers of a ServiceAccount, which the VMMCP doesn't own, must be kept")
}
