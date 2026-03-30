package finalize

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestOnClusterDelete(t *testing.T) {
	ctx := context.TODO()
	cr := &vmv1beta1.VMCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cluster",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
	}

	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentRoot)
	saMeta := metav1.ObjectMeta{
		Name:            b.GetServiceAccountName(),
		Namespace:       b.GetNamespace(),
		Finalizers:      []string{vmv1beta1.FinalizerName},
		OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
	}

	insertMeta := metav1.ObjectMeta{
		Name:            cr.PrefixedName(vmv1beta1.ClusterComponentInsert),
		Namespace:       cr.GetNamespace(),
		Finalizers:      []string{vmv1beta1.FinalizerName},
		OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		Labels:          build.NewChildBuilder(cr, vmv1beta1.ClusterComponentInsert).SelectorLabels(),
	}

	cl := k8stools.GetTestClientWithObjects([]runtime.Object{
		cr.DeepCopy(),
		&corev1.ServiceAccount{ObjectMeta: saMeta},
		&appsv1.Deployment{ObjectMeta: insertMeta},
	})

	err := OnClusterDelete(ctx, cl, cr)
	assert.NoError(t, err)

	// Check finalizer on CR
	var cluster vmv1beta1.VMCluster
	err = cl.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, &cluster)
	assert.NoError(t, err)
	assert.Empty(t, cluster.Finalizers)

	var sa corev1.ServiceAccount
	err = cl.Get(ctx, types.NamespacedName{Name: saMeta.Name, Namespace: saMeta.Namespace}, &sa)
	assert.NoError(t, err)
	assert.Empty(t, sa.Finalizers)

	var insertDep appsv1.Deployment
	err = cl.Get(ctx, types.NamespacedName{Name: insertMeta.Name, Namespace: insertMeta.Namespace}, &insertDep)
	assert.NoError(t, err)
	assert.Empty(t, insertDep.Finalizers)
}

func TestOnInsertDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMCluster
		shouldRemove      bool
		predefinedObjects []runtime.Object
		objMeta           metav1.ObjectMeta
	}
	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		err := OnInsertDelete(ctx, cl, o.cr, o.shouldRemove)
		assert.NoError(t, err)

		var dep appsv1.Deployment
		err = cl.Get(ctx, types.NamespacedName{Name: o.objMeta.Name, Namespace: o.objMeta.Namespace}, &dep)
		if o.shouldRemove {
			assert.Error(t, err) // deleted
		} else {
			assert.NoError(t, err)
			assert.Empty(t, dep.Finalizers) // removed finalizer
		}
	}

	cr := &vmv1beta1.VMCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cluster",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
	}
	objMeta := metav1.ObjectMeta{
		Name:            cr.PrefixedName(vmv1beta1.ClusterComponentInsert),
		Namespace:       cr.GetNamespace(),
		Finalizers:      []string{vmv1beta1.FinalizerName},
		OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		Labels:          build.NewChildBuilder(cr, vmv1beta1.ClusterComponentInsert).SelectorLabels(),
	}
	predefined := []runtime.Object{
		cr.DeepCopy(),
		&appsv1.Deployment{ObjectMeta: *objMeta.DeepCopy()},
		&policyv1.PodDisruptionBudget{ObjectMeta: *objMeta.DeepCopy()},
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: *objMeta.DeepCopy()},
	}
	for _, shouldRemove := range []bool{false, true} {
		t.Run(fmt.Sprintf("shouldRemove=%v", shouldRemove), func(t *testing.T) {
			f(opts{
				cr:                cr,
				shouldRemove:      shouldRemove,
				predefinedObjects: predefined,
				objMeta:           objMeta,
			})
		})
	}
}

func TestOnSelectDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMCluster
		shouldRemove      bool
		predefinedObjects []runtime.Object
		objMeta           metav1.ObjectMeta
	}
	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		err := OnSelectDelete(ctx, cl, o.cr, o.shouldRemove)
		assert.NoError(t, err)

		var dep appsv1.Deployment
		err = cl.Get(ctx, types.NamespacedName{Name: o.objMeta.Name, Namespace: o.objMeta.Namespace}, &dep)
		if o.shouldRemove {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			assert.Empty(t, dep.Finalizers)
		}
	}

	cr := &vmv1beta1.VMCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cluster",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
	}
	objMeta := metav1.ObjectMeta{
		Name:            cr.PrefixedName(vmv1beta1.ClusterComponentSelect),
		Namespace:       cr.GetNamespace(),
		Finalizers:      []string{vmv1beta1.FinalizerName},
		OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		Labels:          build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect).SelectorLabels(),
	}
	predefined := []runtime.Object{
		cr.DeepCopy(),
		&appsv1.Deployment{ObjectMeta: *objMeta.DeepCopy()},
		&appsv1.StatefulSet{ObjectMeta: *objMeta.DeepCopy()},
		&policyv1.PodDisruptionBudget{ObjectMeta: *objMeta.DeepCopy()},
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: *objMeta.DeepCopy()},
	}

	for _, shouldRemove := range []bool{false, true} {
		t.Run(fmt.Sprintf("shouldRemove=%v", shouldRemove), func(t *testing.T) {
			f(opts{
				cr:                cr,
				shouldRemove:      shouldRemove,
				predefinedObjects: predefined,
				objMeta:           objMeta,
			})
		})
	}
}

func TestOnStorageDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMCluster
		shouldRemove      bool
		predefinedObjects []runtime.Object
		objMeta           metav1.ObjectMeta
	}
	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		err := OnStorageDelete(ctx, cl, o.cr, o.shouldRemove)
		assert.NoError(t, err)

		var sts appsv1.StatefulSet
		err = cl.Get(ctx, types.NamespacedName{Name: o.objMeta.Name, Namespace: o.objMeta.Namespace}, &sts)
		if o.shouldRemove {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			assert.Empty(t, sts.Finalizers)
		}
	}

	cr := &vmv1beta1.VMCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cluster",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
	}
	objMeta := metav1.ObjectMeta{
		Name:            cr.PrefixedName(vmv1beta1.ClusterComponentStorage),
		Namespace:       cr.GetNamespace(),
		Finalizers:      []string{vmv1beta1.FinalizerName},
		OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		Labels:          build.NewChildBuilder(cr, vmv1beta1.ClusterComponentStorage).SelectorLabels(),
	}
	predefined := []runtime.Object{
		cr.DeepCopy(),
		&appsv1.StatefulSet{ObjectMeta: *objMeta.DeepCopy()},
		&policyv1.PodDisruptionBudget{ObjectMeta: *objMeta.DeepCopy()},
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: *objMeta.DeepCopy()},
	}

	for _, shouldRemove := range []bool{false, true} {
		t.Run(fmt.Sprintf("shouldRemove=%v", shouldRemove), func(t *testing.T) {
			f(opts{
				cr:                cr,
				shouldRemove:      shouldRemove,
				predefinedObjects: predefined,
				objMeta:           objMeta,
			})
		})
	}
}

func TestOnClusterLoadBalancerDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMCluster
		shouldRemove      bool
		predefinedObjects []runtime.Object
		objMeta           metav1.ObjectMeta
	}
	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		err := OnClusterLoadBalancerDelete(ctx, cl, o.cr, o.shouldRemove)
		assert.NoError(t, err)

		var dep appsv1.Deployment
		err = cl.Get(ctx, types.NamespacedName{Name: o.objMeta.Name, Namespace: o.objMeta.Namespace}, &dep)
		if o.shouldRemove {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			assert.Empty(t, dep.Finalizers)
		}
	}

	cr := &vmv1beta1.VMCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cluster",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
	}
	objMeta := metav1.ObjectMeta{
		Name:            cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
		Namespace:       cr.GetNamespace(),
		Finalizers:      []string{vmv1beta1.FinalizerName},
		OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		Labels:          build.NewChildBuilder(cr, vmv1beta1.ClusterComponentBalancer).SelectorLabels(),
	}

	predefined := []runtime.Object{
		cr.DeepCopy(),
		&appsv1.Deployment{ObjectMeta: *objMeta.DeepCopy()},
		&corev1.Secret{ObjectMeta: *objMeta.DeepCopy()},
		&policyv1.PodDisruptionBudget{ObjectMeta: *objMeta.DeepCopy()},
	}

	for _, shouldRemove := range []bool{false, true} {
		t.Run(fmt.Sprintf("shouldRemove=%v", shouldRemove), func(t *testing.T) {
			f(opts{
				cr:                cr,
				shouldRemove:      shouldRemove,
				predefinedObjects: predefined,
				objMeta:           objMeta,
			})
		})
	}
}

func TestChildCleaner(t *testing.T) {
	cc := NewChildCleaner()
	cc.KeepPDB("pdb-1")
	cc.KeepHPA("hpa-1")
	cc.KeepVPA("vpa-1")
	cc.KeepService("svc-1")
	cc.KeepScrape("scrape-1")

	assert.True(t, cc.pdbs.Has("pdb-1"))
	assert.True(t, cc.hpas.Has("hpa-1"))
	assert.True(t, cc.vpas.Has("vpa-1"))
	assert.True(t, cc.services.Has("svc-1"))
	assert.True(t, cc.scrapes.Has("scrape-1"))

	ctx := context.TODO()
	cr := &vmv1beta1.VMCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cluster",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
	}

	cl := k8stools.GetTestClientWithObjects([]runtime.Object{})
	err := cc.RemoveOrphaned(ctx, cl, cr)
	assert.NoError(t, err)
}
