package vmagent

import (
	"context"
	"testing"

	v1beta12 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	v12 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
						Namespace: "default-2",
						Name:      "rbac-test",
					},
					Spec: v1beta12.VMAgentSpec{},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "monitoring:vmagent-cluster-access-rbac-test",
						Namespace: "default-2",
						Labels: map[string]string{
							"app.kubernetes.io/name":      "vmagent",
							"app.kubernetes.io/instance":  "rbac-test",
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
				},
				&v1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "monitoring:vmagent-cluster-access-rbac-test",
						Namespace: "default-2",
						Labels: map[string]string{
							"app.kubernetes.io/name":      "vmagent",
							"app.kubernetes.io/instance":  "rbac-test",
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
				},
				&v12.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmagent-rbac-test",
						Namespace: "default-2",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			if err := createVMAgentK8sAPIAccess(tt.args.ctx, tt.args.cr, fclient, true); (err != nil) != tt.wantErr {
				t.Errorf("CreateVMAgentK8sAPIAccess() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
