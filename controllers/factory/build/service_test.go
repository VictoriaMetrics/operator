package build

import (
	"fmt"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/go-test/deep"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_mergeServiceSpec(t *testing.T) {
	type args struct {
		svc     *v1.Service
		svcSpec *victoriametricsv1beta1.AdditionalServiceSpec
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
				svcSpec: &victoriametricsv1beta1.AdditionalServiceSpec{
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
				svcSpec: &victoriametricsv1beta1.AdditionalServiceSpec{
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
				svcSpec: &victoriametricsv1beta1.AdditionalServiceSpec{
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
			additionalSvc := AdditionalServiceFromDefault(tt.args.svc, tt.args.svcSpec)
			if err := tt.validate(additionalSvc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
