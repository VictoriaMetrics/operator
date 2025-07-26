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
	type opts struct {
		cr                *vmv1beta1.VMAgent
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		ctx := context.TODO()
		if err := createVMAgentK8sAPIAccess(ctx, fclient, opts.cr, nil, true); err != nil {
			t.Errorf("CreateVMAgentK8sAPIAccess() error = %v", err)
		}
	}

	// ok create default rbac
	o := opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMAgentSpec{},
		},
	}
	f(o)

	// ok with exist rbac
	o = opts{
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
	}
	f(o)
}
