package build

import (
	"fmt"
	"testing"

	"github.com/go-test/deep"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func Test_mergeServiceSpec(t *testing.T) {
	type opts struct {
		svc      *corev1.Service
		svcSpec  *vmv1beta1.AdditionalServiceSpec
		validate func(svc *corev1.Service) error
	}
	f := func(opts opts) {
		t.Helper()
		additionalSvc := AdditionalServiceFromDefault(opts.svc, opts.svcSpec)
		if err := opts.validate(additionalSvc); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// override ports
	o := opts{
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "some-name",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "web"},
				},
			},
		},
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "metrics"},
				},
			},
		},
		validate: func(svc *corev1.Service) error {
			if svc.Name != "some-name-additional-service" {
				return fmt.Errorf("expect name to be empty, got: %v", svc.Name)
			}
			if len(svc.Spec.Ports) != 1 && svc.Spec.Ports[0].Name != "metrics" {
				return fmt.Errorf("unexpected value for ports: %v", svc.Spec.Ports)
			}
			return nil
		},
	}
	f(o)

	// change clusterIP ports
	o = opts{
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "some-name",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "metrics"},
				},
			},
		},
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
			},
		},
		validate: func(svc *corev1.Service) error {
			if svc.Spec.Type != corev1.ServiceTypeNodePort {
				return fmt.Errorf("unexpected value for spec.type want nodePort, got: %v", svc.Spec.Type)
			}
			if len(svc.Spec.Ports) != 1 && svc.Spec.Ports[0].Name != "metrics" {
				return fmt.Errorf("unexpected value for ports: %v", svc.Spec.Ports)
			}
			return nil
		},
	}
	f(o)

	// change selector
	o = opts{
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "some-name",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Ports: []corev1.ServicePort{
					{Name: "metrics"},
				},
				Selector: map[string]string{
					"app": "value",
				},
			},
		},
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Selector: map[string]string{
					"app-2": "value-3",
				},
			},
		},
		validate: func(svc *corev1.Service) error {
			if svc.Spec.Type != corev1.ServiceTypeNodePort {
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
	}
	f(o)
}
