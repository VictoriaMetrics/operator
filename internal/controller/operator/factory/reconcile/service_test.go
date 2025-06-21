package reconcile

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_reconcileServiceForCRD(t *testing.T) {
	tests := []struct {
		name              string
		newService        *corev1.Service
		predefinedObjects []runtime.Object
		validate          func(svc *corev1.Service) error
		wantErr           bool
	}{
		{
			name: "create new svc",
			newService: &corev1.Service{
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
		},
		{
			name: "update svc from headless to clusterIP",
			newService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prefixed-1",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
				},
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			ctx := context.TODO()
			err := Service(ctx, cl, tt.newService, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("reconcileServiceForCRD() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			var updatedSvc corev1.Service
			if err := cl.Get(ctx, types.NamespacedName{Namespace: tt.newService.Namespace, Name: tt.newService.Name}, &updatedSvc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if err := tt.validate(&updatedSvc); err != nil {
				t.Errorf("reconcileServiceForCRD() unexpected error: %v.", err)
			}
		})
	}
}
