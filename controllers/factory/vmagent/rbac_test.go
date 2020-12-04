package vmagent

import (
	"context"
	"testing"

	v12 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"

	"k8s.io/apimachinery/pkg/runtime"

	v1beta12 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

func TestCreateVMAgentClusterAccess(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *v1beta12.VMAgent
	}
	tests := []struct {
		name              string
		args              args
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "ok create default rbac",
			args: args{
				ctx: context.TODO(),
				cr: &v1beta12.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "rbac-test",
					},
					Spec: v1beta12.VMAgentSpec{},
				},
			},
			wantErr: false,
		},
		{
			name: "ok with exist rbac",
			args: args{
				ctx: context.TODO(),
				cr: &v1beta12.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "rbac-test",
					},
					Spec: v1beta12.VMAgentSpec{},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "monitoring:vmagent-cluster-access-rbac-test",
						Namespace: "default",
					},
				},
				&v1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "monitoring:vmagent-cluster-access-rbac-test",
						Namespace: "default",
					},
				},
				&v12.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmagent-rbac-test",
						Namespace: "default",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			if err := CreateVMAgentClusterAccess(tt.args.ctx, tt.args.cr, fclient); (err != nil) != tt.wantErr {
				t.Errorf("CreateVMAgentClusterAccess() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
