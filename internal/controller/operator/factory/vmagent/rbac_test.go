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

func TestCreateVMAgentClusterAccess(t *testing.T) {
	f := func(cr *vmv1beta1.VMAgent, predefinedObjects []runtime.Object) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		if err := createVMAgentK8sAPIAccess(context.TODO(), fclient, cr, nil, true); err != nil {
			t.Errorf("CreateVMAgentK8sAPIAccess() error = %v", err)
		}
	}

	// ok create default rbac
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "rbac-test",
		},
		Spec: vmv1beta1.VMAgentSpec{},
	}, nil)

	// ok with exist rbac
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default-2",
			Name:      "rbac-test",
		},
		Spec: vmv1beta1.VMAgentSpec{},
	}, []runtime.Object{
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
	})
}
