package reconcile

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_reconcileServiceForCRD(t *testing.T) {

	type opts struct {
		serviceToReconcile *corev1.Service
		predefinedObjects  []runtime.Object
		validate           func(svc *corev1.Service) error
	}
	f := func(opts opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		ctx := context.Background()
		err := Service(ctx, cl, opts.serviceToReconcile, nil)
		if err != nil {
			t.Fatalf("unexpected reconcileServiceForCRD() error = %s", err)
		}
		var gotSvc corev1.Service
		if err := cl.Get(ctx, types.NamespacedName{Namespace: opts.serviceToReconcile.Namespace, Name: opts.serviceToReconcile.Name}, &gotSvc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := opts.validate(&gotSvc); err != nil {
			t.Errorf("unexpected result service error: %s", err)
		}
	}

	f(opts{
		serviceToReconcile: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prefixed-1",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
			},
		},
		validate: func(svc *corev1.Service) error {
			if svc.Name != "prefixed-1" {
				return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
			}
			return nil
		},
	})

	// update loadbalancer class
	f(opts{
		serviceToReconcile: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prefixed-1",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Type:              corev1.ServiceTypeLoadBalancer,
				LoadBalancerClass: ptr.To("some-class"),
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prefixed-1",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
		},
		validate: func(svc *corev1.Service) error {
			if *svc.Spec.LoadBalancerClass != "some-class" {
				return fmt.Errorf("unexpected LoadBalancerClass: %s, want: some-classs", *svc.Spec.LoadBalancerClass)
			}
			return nil
		},
	})

	// TODO: migrate other tests for f-test
	type args struct {
		ctx        context.Context
		newService *corev1.Service
	}
	tests := []struct {
		name              string
		args              args
		predefinedObjects []runtime.Object
		validate          func(svc *corev1.Service) error
		wantErr           bool
	}{

		{
			name: "update svc from headless to clusterIP",
			args: args{
				newService: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: "None",
					},
				},
			},
			validate: func(svc *corev1.Service) error {
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
				newService: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: "None",
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: "192.168.1.5",
					},
				},
			},
			validate: func(svc *corev1.Service) error {
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
				newService: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: "192.168.1.5",
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: "192.168.1.4",
					},
				},
			},
			validate: func(svc *corev1.Service) error {
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
				newService: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: "192.168.1.5",
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeNodePort,
						ClusterIP: "192.168.1.1",
					},
				},
			},
			validate: func(svc *corev1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				if svc.Spec.Type != corev1.ServiceTypeClusterIP {
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
				newService: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeNodePort,
						ClusterIP: "192.168.1.5",
						Ports: []corev1.ServicePort{
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
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type:      corev1.ServiceTypeNodePort,
						ClusterIP: "192.168.1.5",
						Ports: []corev1.ServicePort{
							{
								Name:     "web",
								Protocol: "TCP",
								NodePort: 331,
							},
						},
					},
				},
			},
			validate: func(svc *corev1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				if svc.Spec.Type != corev1.ServiceTypeNodePort {
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
		{
			name: "keep custom labels on svc",
			args: args{
				newService: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
					},
				},
				ctx: context.TODO(),
			},
			predefinedObjects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "prefixed-1",
						Namespace: "default",
						Labels:    map[string]string{"custom": "label"},
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
					},
				},
			},
			validate: func(svc *corev1.Service) error {
				if svc.Name != "prefixed-1" {
					return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
				}
				l, ok := svc.Labels["custom"]
				if !ok {
					return fmt.Errorf("missing 'custom' label on svc")
				}
				if l != "label" {
					return fmt.Errorf("unexpected value of 'custom' label on svc, got: %v, want: 'value'", l)
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			err := Service(tt.args.ctx, cl, tt.args.newService, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("reconcileServiceForCRD() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			var updatedSvc corev1.Service
			if err := cl.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.newService.Namespace, Name: tt.args.newService.Name}, &updatedSvc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if err := tt.validate(&updatedSvc); err != nil {
				t.Errorf("reconcileServiceForCRD() unexpected error: %v.", err)
			}
		})
	}
}
