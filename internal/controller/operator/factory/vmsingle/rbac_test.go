package vmsingle

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

func TestCreateVMSingleClusterAccess(t *testing.T) {
	f := func(cr *vmv1beta1.VMSingle, predefinedObjects []runtime.Object) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		if err := createK8sAPIAccess(context.TODO(), fclient, cr, nil, true); err != nil {
			t.Errorf("createK8sAPIAccess() error = %v", err)
		}
	}

	// ok create default rbac
	f(&vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "rbac-test",
		},
		Spec: vmv1beta1.VMSingleSpec{},
	}, nil)

	// ok with exist rbac
	f(&vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default-2",
			Name:      "rbac-test",
		},
		Spec: vmv1beta1.VMSingleSpec{},
	}, []runtime.Object{
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "monitoring:vmsingle-cluster-access-rbac-test",
				Namespace: "default-2",
				Labels: map[string]string{
					"app.kubernetes.io/name":      "vmsingle",
					"app.kubernetes.io/instance":  "rbac-test",
					"app.kubernetes.io/component": "monitoring",
					"managed-by":                  "vm-operator",
				},
			},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "monitoring:vmsingle-cluster-access-rbac-test",
				Namespace: "default-2",
				Labels: map[string]string{
					"app.kubernetes.io/name":      "vmsingle",
					"app.kubernetes.io/instance":  "rbac-test",
					"app.kubernetes.io/component": "monitoring",
					"managed-by":                  "vm-operator",
				},
			},
		},
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmsingle-rbac-test",
				Namespace: "default-2",
			},
		},
	})
}
