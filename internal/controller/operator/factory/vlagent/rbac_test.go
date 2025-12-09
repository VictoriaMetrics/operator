package vlagent

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateVLAgentClusterAccess(t *testing.T) {
	f := func(cr *vmv1.VLAgent, predefinedObjects []runtime.Object) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		if err := createK8sAPIAccess(context.TODO(), fclient, cr, nil); err != nil {
			t.Errorf("createK8sAPIAccess() error = %v", err)
		}
	}

	// ok create default rbac
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "rbac-test",
		},
		Spec: vmv1.VLAgentSpec{
			K8sCollector: vmv1.VLAgentK8sCollector{
				Enabled: true,
			},
		},
	}, nil)

	// ok with exist rbac
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default-2",
			Name:      "rbac-test",
		},
		Spec: vmv1.VLAgentSpec{
			K8sCollector: vmv1.VLAgentK8sCollector{
				Enabled: true,
			},
		},
	}, []runtime.Object{
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "monitoring:vlagent-cluster-access-rbac-test",
				Namespace: "default-2",
				Labels: map[string]string{
					"app.kubernetes.io/name":      "vlagent",
					"app.kubernetes.io/instance":  "rbac-test",
					"app.kubernetes.io/component": "monitoring",
					"managed-by":                  "vm-operator",
				},
			},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "monitoring:vlagent-cluster-access-rbac-test",
				Namespace: "default-2",
				Labels: map[string]string{
					"app.kubernetes.io/name":      "vlagent",
					"app.kubernetes.io/instance":  "rbac-test",
					"app.kubernetes.io/component": "monitoring",
					"managed-by":                  "vm-operator",
				},
			},
		},
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vlagent-rbac-test",
				Namespace: "default-2",
			},
		},
	})
}
