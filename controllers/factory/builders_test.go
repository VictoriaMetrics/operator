package factory

import (
	"context"
	"fmt"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/go-test/deep"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func Test_reconcileServiceForCRD(t *testing.T) {
	type args struct {
		ctx        context.Context
		newService *v1.Service
	}
	tests := []struct {
		name              string
		args              args
		predefinedObjects []runtime.Object
		validate          func(svc *v1.Service) error
		wantErr           bool
	}{
		{
			name: "create new svc",
			args: args{
				newService: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeNodePort,
					},
				},
				ctx: context.TODO(),
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				return nil
			},
		},
		{
			name: "update svc from headless to clusterIP",
			args: args{
				newService: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeClusterIP,
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeClusterIP,
						ClusterIP: "None",
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				if svc.Spec.ClusterIP == "None" {
					return fmt.Errorf("unexpected value for clusterIP, want ip, got: %v", svc.Spec.ClusterIP)
				}
				return nil
			},
		},
		{
			name: "update svc from clusterIP to headless",
			args: args{
				newService: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeClusterIP,
						ClusterIP: "None",
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeClusterIP,
						ClusterIP: "192.168.1.5",
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				if svc.Spec.ClusterIP != "None" {
					return fmt.Errorf("unexpected value for clusterIP, want ip, got: %v", svc.Spec.ClusterIP)
				}
				return nil
			},
		},
		{
			name: "update svc clusterIP value",
			args: args{
				newService: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeClusterIP,
						ClusterIP: "192.168.1.5",
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeClusterIP,
						ClusterIP: "192.168.1.4",
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				if svc.Spec.ClusterIP != "192.168.1.5" {
					return fmt.Errorf("unexpected value for clusterIP, want ip, got: %v", svc.Spec.ClusterIP)
				}
				return nil
			},
		},
		{
			name: "update svc from nodePort to clusterIP with value",
			args: args{
				newService: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeClusterIP,
						ClusterIP: "192.168.1.5",
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeNodePort,
						ClusterIP: "192.168.1.1",
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				if svc.Spec.Type != v1.ServiceTypeClusterIP {
					return fmt.Errorf("unexpected type: %v", svc.Spec.Type)
				}
				if svc.Spec.ClusterIP != "192.168.1.5" {
					return fmt.Errorf("unexpected value for clusterIP, want ip, got: %v", svc.Spec.ClusterIP)
				}
				return nil
			},
		},
		{
			name: "keep node port",
			args: args{
				newService: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeNodePort,
						ClusterIP: "192.168.1.5",
						Ports: []v1.ServicePort{
							{
								Name:     "web",
								Protocol: "TCP",
							},
						},
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: v1.ServiceSpec{
						Type:      v1.ServiceTypeNodePort,
						ClusterIP: "192.168.1.5",
						Ports: []v1.ServicePort{
							{
								Name:     "web",
								Protocol: "TCP",
								NodePort: 331,
							},
						},
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				if svc.Spec.Type != v1.ServiceTypeNodePort {
					return fmt.Errorf("unexpected type: %v", svc.Spec.Type)
				}
				if svc.Spec.ClusterIP != "192.168.1.5" {
					return fmt.Errorf("unexpected value for clusterIP, want ip, got: %v", svc.Spec.ClusterIP)
				}
				if svc.Spec.Ports[0].NodePort != 331 {
					return fmt.Errorf("unexpected value for node port: %v", svc.Spec.Ports[0])
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			_, err := reconcileServiceForCRD(tt.args.ctx, cl, tt.args.newService)
			if (err != nil) != tt.wantErr {
				t.Errorf("reconcileServiceForCRD() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			var updatedSvc v1.Service
			if err := cl.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.newService.Namespace, Name: tt.args.newService.Name}, &updatedSvc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if err := tt.validate(&updatedSvc); err != nil {
				t.Errorf("reconcileServiceForCRD() unexpected error: %v.", err)
			}
		})
	}
}

func Test_mergeServiceSpec(t *testing.T) {
	type args struct {
		svc     *v1.Service
		svcSpec *victoriametricsv1beta1.ServiceSpec
	}
	tests := []struct {
		name     string
		args     args
		validate func(svc *v1.Service) error
	}{
		{
			name: "override ports",
			args: args{
				svc: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "some-name",
					},
					Spec: v1.ServiceSpec{
						Ports: []v1.ServicePort{
							{Name: "web"},
						},
					},
				},
				svcSpec: &victoriametricsv1beta1.ServiceSpec{
					Spec: v1.ServiceSpec{
						Ports: []v1.ServicePort{
							{Name: "metrics"},
						},
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Name != "some-name-additional-service" {
					return fmt.Errorf("expect name to be empty, got: %v", svc.Name)
				}
				if len(svc.Spec.Ports) != 1 && svc.Spec.Ports[0].Name != "metrics" {
					return fmt.Errorf("unexpected value for ports: %v", svc.Spec.Ports)
				}
				return nil
			},
		},
		{
			name: "change clusterIP ports",
			args: args{
				svc: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "some-name",
					},
					Spec: v1.ServiceSpec{
						Ports: []v1.ServicePort{
							{Name: "metrics"},
						},
					},
				},
				svcSpec: &victoriametricsv1beta1.ServiceSpec{
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeNodePort,
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Spec.Type != v1.ServiceTypeNodePort {
					return fmt.Errorf("unexpected value for spec.type want nodePort, got: %v", svc.Spec.Type)
				}
				if len(svc.Spec.Ports) != 1 && svc.Spec.Ports[0].Name != "metrics" {
					return fmt.Errorf("unexpected value for ports: %v", svc.Spec.Ports)
				}
				return nil
			},
		},
		{
			name: "change selector",
			args: args{
				svc: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "some-name",
					},
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeNodePort,
						Ports: []v1.ServicePort{
							{Name: "metrics"},
						},
						Selector: map[string]string{
							"app": "value",
						},
					},
				},
				svcSpec: &victoriametricsv1beta1.ServiceSpec{
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeNodePort,
						Selector: map[string]string{
							"app-2": "value-3",
						},
					},
				},
			},
			validate: func(svc *v1.Service) error {
				if svc.Spec.Type != v1.ServiceTypeNodePort {
					return fmt.Errorf("unexpected value for spec.type want nodePort, got: %v", svc.Spec.Type)
				}
				if len(svc.Spec.Ports) != 1 && svc.Spec.Ports[0].Name != "metrics" {
					return fmt.Errorf("unexpected value for ports: %v", svc.Spec.Ports)
				}
				if diff := deep.Equal(svc.Spec.Selector, map[string]string{"app-2": "value-3"}); len(diff) > 0 {
					return fmt.Errorf("unexpected value for selector: %v", svc.Spec.Selector)
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mergeServiceSpec(tt.args.svc, tt.args.svcSpec)
			if err := tt.validate(tt.args.svc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func Test_reconcileMissingServices(t *testing.T) {
	type args struct {
		ctx  context.Context
		args rSvcArgs
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
			args: args{args: rSvcArgs{
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
			if err := removeOrphanedServices(tt.args.ctx, cl, tt.args.args, tt.args.spec); (err != nil) != tt.wantErr {
				t.Errorf("removeOrphanedServices() error = %v, wantErr %v", err, tt.wantErr)
			}
			var wantSvcs v1.ServiceList
			if err := cl.List(tt.args.ctx, &wantSvcs); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(wantSvcs.Items) != len(tt.wantServiceNames) {
				t.Fatalf("unexpected count if services: %v", len(wantSvcs.Items))
			}
		})
	}
}
