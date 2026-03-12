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

func TestAddFinalizer(t *testing.T) {
	type opts struct {
		instance          client.Object
		predefinedObjects []runtime.Object
		wantFinalizers    []string
	}

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		err := AddFinalizer(ctx, cl, o.instance)
		assert.NoError(t, err)
		assert.Equal(t, o.wantFinalizers, o.instance.GetFinalizers())
	}

	f(opts{
		instance: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
			},
		},
		wantFinalizers: []string{vmv1beta1.FinalizerName},
	})

	f(opts{
		instance: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-2",
				Namespace:  "default",
				Finalizers: []string{vmv1beta1.FinalizerName},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-2",
					Namespace:  "default",
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
		},
		wantFinalizers: []string{vmv1beta1.FinalizerName},
	})
}

func TestRemoveFinalizer(t *testing.T) {
	type opts struct {
		instance          client.Object
		predefinedObjects []runtime.Object
		wantFinalizers    []string
	}

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		err := RemoveFinalizer(ctx, cl, o.instance)
		assert.NoError(t, err)
		assert.Equal(t, o.wantFinalizers, o.instance.GetFinalizers())
	}

	f(opts{
		instance: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test",
				Namespace:  "default",
				Finalizers: []string{vmv1beta1.FinalizerName},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test",
					Namespace:  "default",
					Finalizers: []string{vmv1beta1.FinalizerName},
				},
			},
		},
		wantFinalizers: nil,
	})

	f(opts{
		instance: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "not-found",
				Namespace:  "default",
				Finalizers: []string{vmv1beta1.FinalizerName},
			},
		},
		predefinedObjects: nil,
		wantFinalizers:    []string{},
	})

	f(opts{
		instance: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-2",
				Namespace:  "default",
				Finalizers: []string{"other-finalizer"},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-2",
					Namespace:  "default",
					Finalizers: []string{"other-finalizer"},
				},
			},
		},
		wantFinalizers: []string{"other-finalizer"},
	})
}

func TestSafeDeleteWithFinalizer(t *testing.T) {
	type opts struct {
		cr                crObject
		objs              []client.Object
		predefinedObjects []runtime.Object
		verify            func(client.Client)
		wantErr           bool
	}
	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		err := SafeDeleteWithFinalizer(ctx, cl, o.objs, o.cr)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		if o.verify != nil {
			o.verify(cl)
		}
	}

	f(opts{
		cr: &vmv1beta1.VMAgent{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1beta1",
				Kind:       "VMAgent",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
		},
		objs: []client.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test",
					Namespace:  "default",
					Finalizers: []string{vmv1beta1.FinalizerName},
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmagent",
						"app.kubernetes.io/instance":  "vmagent",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "operator.victoriametrics.com/v1beta1",
							Kind:       "VMAgent",
							Name:       "vmagent",
						},
					},
				},
			},
		},
		verify: func(cl client.Client) {
			var cm corev1.ConfigMap
			err := cl.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, &cm)
			assert.Error(t, err)
		},
	})

	f(opts{
		cr: &vmv1beta1.VMAgent{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1beta1",
				Kind:       "VMAgent",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
		},
		objs: []client.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
			},
		},
		wantErr: true,
	})

	f(opts{
		cr: &vmv1beta1.VMAgent{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1beta1",
				Kind:       "VMAgent",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
		},
		objs: []client.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-found",
					Namespace: "default",
				},
			},
		},
	})

	f(opts{
		cr: &vmv1beta1.VMAgent{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "operator.victoriametrics.com/v1beta1",
				Kind:       "VMAgent",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
		},
		objs: []client.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cannot-remove",
					Namespace: "default",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cannot-remove",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name": "other",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "operator.victoriametrics.com/v1beta1",
							Kind:       "VMAgent",
							Name:       "other-agent",
						},
					},
				},
			},
		},
		verify: func(cl client.Client) {
			var cm corev1.ConfigMap
			err := cl.Get(ctx, types.NamespacedName{Name: "test-cannot-remove", Namespace: "default"}, &cm)
			assert.NoError(t, err)
		},
	})
}

func TestRemoveFinalizers(t *testing.T) {
	type opts struct {
		cr                    crObject
		objs                  []client.Object
		deleteOwnerReferences []bool
		predefinedObjects     []runtime.Object
		verify                func(client.Client)
		wantErr               bool
	}
	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		err := removeFinalizers(ctx, cl, o.objs, o.deleteOwnerReferences, o.cr)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		if o.verify != nil {
			o.verify(cl)
		}
	}

	cr := &vmv1beta1.VMAgent{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMAgent",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmagent",
			Namespace: "default",
		},
	}

	f(opts{
		cr: cr,
		objs: []client.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
			},
		},
		deleteOwnerReferences: []bool{true},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test",
					Namespace:  "default",
					Finalizers: []string{vmv1beta1.FinalizerName},
					OwnerReferences: []metav1.OwnerReference{
						cr.AsOwner(),
					},
				},
			},
		},
		verify: func(cl client.Client) {
			var cm corev1.ConfigMap
			err := cl.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, &cm)
			assert.NoError(t, err)
			assert.Empty(t, cm.GetFinalizers())
			assert.Empty(t, cm.GetOwnerReferences())
		},
	})

	f(opts{
		cr: cr,
		objs: []client.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-found",
					Namespace: "default",
				},
			},
		},
		deleteOwnerReferences: []bool{true},
		predefinedObjects:     []runtime.Object{},
		verify: func(cl client.Client) {
			var cm corev1.ConfigMap
			err := cl.Get(ctx, types.NamespacedName{Name: "not-found", Namespace: "default"}, &cm)
			assert.Error(t, err)
		},
	})

	f(opts{
		cr: cr,
		objs: []client.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
				},
			},
		},
		deleteOwnerReferences: []bool{true},
		wantErr:               true,
	})

	f(opts{
		cr: cr,
		objs: []client.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-keep-owner",
					Namespace: "default",
				},
			},
		},
		deleteOwnerReferences: []bool{false},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-keep-owner",
					Namespace:  "default",
					Finalizers: []string{vmv1beta1.FinalizerName},
					OwnerReferences: []metav1.OwnerReference{
						cr.AsOwner(),
					},
				},
			},
		},
		verify: func(cl client.Client) {
			var cm corev1.ConfigMap
			err := cl.Get(ctx, types.NamespacedName{Name: "test-keep-owner", Namespace: "default"}, &cm)
			assert.NoError(t, err)
			assert.Empty(t, cm.GetFinalizers())
			assert.Len(t, cm.GetOwnerReferences(), 1)
		},
	})

	f(opts{
		cr: cr,
		objs: []client.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-keep-other-owners",
					Namespace: "default",
				},
			},
		},
		deleteOwnerReferences: []bool{true},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-keep-other-owners",
					Namespace:  "default",
					Finalizers: []string{vmv1beta1.FinalizerName},
					OwnerReferences: []metav1.OwnerReference{
						cr.AsOwner(),
						{
							APIVersion: "v1",
							Kind:       "Pod",
							Name:       "other-owner",
						},
					},
				},
			},
		},
		verify: func(cl client.Client) {
			var cm corev1.ConfigMap
			err := cl.Get(ctx, types.NamespacedName{Name: "test-keep-other-owners", Namespace: "default"}, &cm)
			assert.NoError(t, err)
			assert.Empty(t, cm.GetFinalizers())
			assert.Len(t, cm.GetOwnerReferences(), 1)
			assert.Equal(t, "other-owner", cm.GetOwnerReferences()[0].Name)
		},
	})
}

func TestSafeDelete(t *testing.T) {
	cl := k8stools.GetTestClientWithObjects([]runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		},
	})
	ctx := context.TODO()

	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
	}
	err := SafeDelete(ctx, cl, obj)
	assert.NoError(t, err)

	// verify deleted
	var cm corev1.ConfigMap
	err = cl.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, &cm)
	assert.Error(t, err)

	// delete again, shouldn't error
	err = SafeDelete(ctx, cl, obj)
	assert.NoError(t, err)
}
