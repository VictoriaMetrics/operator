package vmagent

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateClusterAccess(t *testing.T) {
	tests := []struct {
		name              string
		cr                *vmv1beta1.VMAgent
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "ok create default rbac",
			cr: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "rbac-test",
				},
				Spec: vmv1beta1.VMAgentSpec{},
			},
			wantErr: false,
		},
		{
			name: "ok with exist rbac",
			cr: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default-2",
					Name:      "rbac-test",
				},
				Spec: vmv1beta1.VMAgentSpec{},
			},
			predefinedObjects: []runtime.Object{
				&rbacv1.ClusterRole{
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
				&rbacv1.ClusterRoleBinding{
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
				&corev1.ServiceAccount{
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
			ctx := context.TODO()
			if err := createK8sAPIAccess(ctx, fclient, tt.cr, nil, true); (err != nil) != tt.wantErr {
				t.Errorf("createK8sAPIAccess() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
