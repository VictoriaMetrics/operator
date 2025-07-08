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
	f := func(svc *corev1.Service, predefinedObjects []runtime.Object, validate func(svc *corev1.Service) error) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(predefinedObjects)
		ctx := context.TODO()
		err := Service(ctx, cl, svc, nil)
		if err != nil {
			t.Errorf("reconcileServiceForCRD() error = %v", err)
			return
		}
		var updatedSvc corev1.Service
		if err := cl.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}, &updatedSvc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := validate(&updatedSvc); err != nil {
			t.Errorf("reconcileServiceForCRD() unexpected error: %v.", err)
		}
	}

	// create new svc
	f(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prefixed-1",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
		},
	}, nil, func(svc *corev1.Service) error {
		if svc.Name != "prefixed-1" {
			return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
		}
		return nil
	})

	// update svc from headless to clusterIP
	f(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prefixed-1",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
		},
	}, []runtime.Object{
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
	}, func(svc *corev1.Service) error {
		if svc.Name != "prefixed-1" {
			return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
		}
		if svc.Spec.ClusterIP == "None" {
			return fmt.Errorf("unexpected value for clusterIP, want ip, got: %v", svc.Spec.ClusterIP)
		}
		return nil
	})

	// update svc from clusterIP to headless
	f(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prefixed-1",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "None",
		},
	}, []runtime.Object{
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
	}, func(svc *corev1.Service) error {
		if svc.Name != "prefixed-1" {
			return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
		}
		if svc.Spec.ClusterIP != "None" {
			return fmt.Errorf("unexpected value for clusterIP, want ip, got: %v", svc.Spec.ClusterIP)
		}
		return nil
	})

	// update svc clusterIP value
	f(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prefixed-1",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "192.168.1.5",
		},
	}, []runtime.Object{
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
	}, func(svc *corev1.Service) error {
		if svc.Name != "prefixed-1" {
			return fmt.Errorf("unexpected name, got: %v, want: prefixed-1", svc.Name)
		}
		if svc.Spec.ClusterIP != "192.168.1.5" {
			return fmt.Errorf("unexpected value for clusterIP, want ip, got: %v", svc.Spec.ClusterIP)
		}
		return nil
	})

	// update svc from nodePort to clusterIP with value
	f(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prefixed-1",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "192.168.1.5",
		},
	}, []runtime.Object{
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
	}, func(svc *corev1.Service) error {
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
	})

	// keep node port
	f(&corev1.Service{
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
	}, []runtime.Object{
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
	}, func(svc *corev1.Service) error {
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
	})
}
