package finalize

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestRemoveOrphanedDeployments(t *testing.T) {
	type opts struct {
		cr                orphanedCRD
		keepDeployments   map[string]struct{}
		wantDepCount      int
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		assert.NoError(t, RemoveOrphanedDeployments(ctx, cl, o.cr, o.keepDeployments))
		var existDep appsv1.DeploymentList
		lo := client.ListOptions{Namespace: o.cr.GetNamespace(), LabelSelector: labels.SelectorFromSet(o.cr.SelectorLabels())}
		assert.NoError(t, cl.List(ctx, &existDep, &lo))
		assert.Len(t, existDep.Items, o.wantDepCount)
	}

	// remove nothing
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
		},
		keepDeployments: map[string]struct{}{"base": {}},
		predefinedObjects: []runtime.Object{
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "base",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmagent",
						"app.kubernetes.io/instance":  "base",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
			},
		},
		wantDepCount: 1,
	})

	// remove 1 orphaned
	f(opts{
		cr: &vmv1beta1.VMAgent{
			TypeMeta: metav1.TypeMeta{
				Kind:       "VMAgent",
				APIVersion: "v1beta1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
		},
		keepDeployments: map[string]struct{}{"base-0": {}},
		predefinedObjects: []runtime.Object{
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "base-0",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmagent",
						"app.kubernetes.io/instance":  "base",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
			},
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "base-1",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmagent",
						"app.kubernetes.io/instance":  "base",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
					Finalizers: []string{
						vmv1beta1.FinalizerName,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1beta1",
							Kind:       "VMAgent",
							Name:       "base",
						},
					},
				},
			},
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "base-2",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmagent",
						"app.kubernetes.io/instance":  "base",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
			},
		},
		wantDepCount: 2,
	})
}
