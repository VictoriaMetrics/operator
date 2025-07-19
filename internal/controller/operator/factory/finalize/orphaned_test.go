package finalize

import (
	"context"
	"testing"

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
	f := func(opts opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		ctx := context.TODO()
		if err := RemoveOrphanedDeployments(ctx, cl, opts.cr, opts.keepDeployments); err != nil {
			t.Errorf("RemoveOrphanedDeployments() error = %v", err)
		}
		var existDep appsv1.DeploymentList
		o := client.ListOptions{Namespace: opts.cr.GetNamespace(), LabelSelector: labels.SelectorFromSet(opts.cr.SelectorLabels())}
		if err := cl.List(ctx, &existDep, &o); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.wantDepCount != len(existDep.Items) {
			t.Fatalf("unexpected count of deployments, got:%v, want: %v", len(existDep.Items), opts.wantDepCount)
		}
	}

	// remove nothing
	o := opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
		},
		keepDeployments: map[string]struct{}{"base": {}},
		wantDepCount:    1,
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
	}
	f(o)

	// remove 1 orphaned
	o = opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
		},
		keepDeployments: map[string]struct{}{"base-0": {}},
		wantDepCount:    1,
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
				},
			},
		},
	}
	f(o)
}
