package factory

import (
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"k8s.io/apimachinery/pkg/runtime"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"

	v1 "k8s.io/api/core/v1"
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := reconcileServiceForCRD(tt.args.ctx, cl, tt.args.newService)
			if (err != nil) != tt.wantErr {
				t.Errorf("reconcileServiceForCRD() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err := tt.validate(got); err != nil {
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
		// TODO: Add test cases.
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
