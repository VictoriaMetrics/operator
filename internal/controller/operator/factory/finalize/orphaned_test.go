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
	tests := []struct {
		name              string
		cr                orphanedCRD
		keepDeployments   map[string]struct{}
		wantDepCount      int
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "remove nothing",
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
		},
		{
			name: "remove 1 orphaned",
			cr: &vmv1beta1.VMAgent{
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
					},
				},
			},
			wantDepCount: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			ctx := context.TODO()
			if err := RemoveOrphanedDeployments(ctx, cl, tt.cr, tt.keepDeployments); (err != nil) != tt.wantErr {
				t.Errorf("RemoveOrphanedDeployments() error = %v, wantErr %v", err, tt.wantErr)
			}
			var existDep appsv1.DeploymentList
			opts := client.ListOptions{Namespace: tt.cr.GetNamespace(), LabelSelector: labels.SelectorFromSet(tt.cr.SelectorLabels())}
			if err := cl.List(ctx, &existDep, &opts); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantDepCount != len(existDep.Items) {
				t.Fatalf("unexpected count of deployments, got:%v, want: %v", len(existDep.Items), tt.wantDepCount)
			}
		})
	}
}
