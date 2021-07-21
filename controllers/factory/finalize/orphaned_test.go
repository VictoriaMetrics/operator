package finalize

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func Test_reconcileMissingServices(t *testing.T) {
	type args struct {
		ctx  context.Context
		args RemoveSvcArgs
		spec *victoriametricsv1beta1.ServiceSpec
	}
	tests := []struct {
		name              string
		args              args
		wantErr           bool
		wantServiceNames  map[string]struct{}
		predefinedObjects []runtime.Object
	}{
		{
			name: "remove 1 missing",
			args: args{args: RemoveSvcArgs{
				SelectorLabels: func() map[string]string {
					return map[string]string{
						"selector": "app-1",
					}
				},
				PrefixedName: func() string {
					return "keep-this-one"
				},
				GetNameSpace: func() string {
					return "default"
				},
			},
				ctx: context.TODO(),
				spec: &victoriametricsv1beta1.ServiceSpec{
					EmbeddedObjectMetadata: victoriametricsv1beta1.EmbeddedObjectMetadata{
						Name: "keep-another-one",
					},
				},
			},
			wantServiceNames: map[string]struct{}{
				"keep-another-one": struct{}{},
				"keep-this-one":    struct{}{},
			},
			predefinedObjects: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "keep-another-one",
						Labels:    map[string]string{"selector": "app-1"},
					},
				},
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "keep-this-one",
						Labels:    map[string]string{"selector": "app-1"},
					},
				},
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "remove-this-1",
						Labels:    map[string]string{"selector": "app-1"},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			if err := RemoveOrphanedServices(tt.args.ctx, cl.GetCRClient(), tt.args.args, tt.args.spec); (err != nil) != tt.wantErr {
				t.Errorf("RemoveOrphanedServices() error = %v, wantErr %v", err, tt.wantErr)
			}
			var wantSvcs v1.ServiceList
			if err := cl.GetCRClient().List(tt.args.ctx, &wantSvcs); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(wantSvcs.Items) != len(tt.wantServiceNames) {
				t.Fatalf("unexpected count if services: %v", len(wantSvcs.Items))
			}
		})
	}
}

func TestRemoveOrphanedDeployments(t *testing.T) {
	type args struct {
		ctx             context.Context
		cr              orphanedCRD
		keepDeployments map[string]struct{}
	}
	tests := []struct {
		name              string
		args              args
		wantDepCount      int
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "remove nothing",
			args: args{
				ctx: context.TODO(),
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "base",
						Namespace: "default",
					},
				},
				keepDeployments: map[string]struct{}{"base": {}},
			},
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
			args: args{
				ctx: context.TODO(),
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "base",
						Namespace: "default",
					},
				},
				keepDeployments: map[string]struct{}{"base-0": {}},
			},
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
			if err := RemoveOrphanedDeployments(tt.args.ctx, cl.GetCRClient(), tt.args.cr, tt.args.keepDeployments); (err != nil) != tt.wantErr {
				t.Errorf("RemoveOrphanedDeployments() error = %v, wantErr %v", err, tt.wantErr)
			}
			var existDep appsv1.DeploymentList
			opts := client.ListOptions{Namespace: tt.args.cr.GetNSName(), LabelSelector: labels.SelectorFromSet(tt.args.cr.SelectorLabels())}
			if err := cl.GetCRClient().List(tt.args.ctx, &existDep, &opts); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantDepCount != len(existDep.Items) {
				t.Fatalf("unexpected count of deployments, got:%v, want: %v", len(existDep.Items), tt.wantDepCount)
			}
		})
	}
}
