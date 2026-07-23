package vmsingle

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateVMSingleRBAC(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMSingle
		validate          func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle)
		namespaces        []string
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		assert.NoError(t, createK8sAPIAccess(ctx, fclient, o.cr, nil, o.namespaces))
		if o.validate != nil {
			o.validate(ctx, fclient, o.cr)
		}
	}

	// ok with existing rbac
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default-2",
				Name:      "rbac-test",
			},
			Spec: vmv1beta1.VMSingleSpec{},
		},
		namespaces: []string{},
		predefinedObjects: []runtime.Object{
			&rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "monitoring:vmsingle-cluster-access-rbac-test",
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
					Name: "monitoring:vmsingle-cluster-access-rbac-test",
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
		},
	})
}
