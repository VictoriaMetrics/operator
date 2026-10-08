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

func newTestVMEstimator() *vmv1.VMEstimator {
	return &vmv1.VMEstimator{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1",
			Kind:       "VMEstimator",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
	}
}

// vmEstimatorChildMeta returns metadata of object created by operator for the given component
func vmEstimatorChildMeta(cr *vmv1.VMEstimator, name string, kind vmv1beta1.ClusterComponent) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:            name,
		Namespace:       cr.Namespace,
		Labels:          cr.FinalLabels(kind),
		Finalizers:      []string{vmv1beta1.FinalizerName},
		OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
	}
}

func TestOnVMEstimatorDelete(t *testing.T) {
	ctx := context.TODO()
	cr := newTestVMEstimator()
	storage := vmv1beta1.ClusterComponentStorage
	sel := vmv1beta1.ClusterComponentSelect
	root := vmv1beta1.ClusterComponentRoot

	predefinedObjects := []runtime.Object{
		cr,
		&corev1.ServiceAccount{ObjectMeta: vmEstimatorChildMeta(cr, cr.GetServiceAccountName(), root)},
		&corev1.ConfigMap{ObjectMeta: vmEstimatorChildMeta(cr, cr.PrefixedName(root), root)},
		&appsv1.StatefulSet{ObjectMeta: vmEstimatorChildMeta(cr, cr.PrefixedName(storage), storage)},
		&policyv1.PodDisruptionBudget{ObjectMeta: vmEstimatorChildMeta(cr, cr.PrefixedName(storage), storage)},
		&vmv1beta1.VMServiceScrape{ObjectMeta: vmEstimatorChildMeta(cr, cr.PrefixedName(storage), storage)},
		&corev1.Service{ObjectMeta: vmEstimatorChildMeta(cr, cr.PrefixedName(storage), storage)},
		&corev1.Service{ObjectMeta: vmEstimatorChildMeta(cr, cr.PrefixedInsertName(), storage)},
		&appsv1.Deployment{ObjectMeta: vmEstimatorChildMeta(cr, cr.PrefixedName(sel), sel)},
		&corev1.Service{ObjectMeta: vmEstimatorChildMeta(cr, cr.PrefixedName(sel), sel)},
	}
	// object of another application must not be changed
	foreignSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name:       "foreign",
		Namespace:  cr.Namespace,
		Labels:     map[string]string{"app.kubernetes.io/instance": cr.Name},
		Finalizers: []string{vmv1beta1.FinalizerName},
	}}
	cl := k8stools.GetTestClientWithObjects(append(predefinedObjects, foreignSvc))
	require.NoError(t, OnVMEstimatorDelete(ctx, cl, cr))

	var gotForeignSvc corev1.Service
	require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: foreignSvc.Name, Namespace: foreignSvc.Namespace}, &gotForeignSvc))
	assert.Equal(t, []string{vmv1beta1.FinalizerName}, gotForeignSvc.Finalizers)

	// finalizers must be removed from CR and all its child objects,
	// objects are removed by kubernetes garbage collector
	for _, obj := range predefinedObjects {
		o := obj.(client.Object)
		got := o.DeepCopyObject().(client.Object)
		require.NoError(t, cl.Get(ctx, types.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()}, got))
		assert.Empty(t, got.GetFinalizers(), "%T %s", got, got.GetName())
	}
}
