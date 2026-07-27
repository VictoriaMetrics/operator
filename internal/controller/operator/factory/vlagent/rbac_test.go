package vlagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateVLAgentClusterAccess(t *testing.T) {
	f := func(cr *vmv1.VLAgent, predefinedObjects []runtime.Object) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		assert.NoError(t, createK8sAPIAccess(context.TODO(), fclient, cr, nil))
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
				Name: "monitoring:vlagent-cluster-access-rbac-test",
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
				Name: "monitoring:vlagent-cluster-access-rbac-test",
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

	checkRules := func(cr *vmv1.VLAgent, wantNodes bool) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(nil)
		assert.NoError(t, createK8sAPIAccess(context.TODO(), fclient, cr, nil))
		var got rbacv1.ClusterRole
		assert.NoError(t, fclient.Get(context.TODO(), types.NamespacedName{Name: cr.GetRBACName()}, &got))
		hasNodes := false
		for _, rule := range got.Rules {
			for _, res := range rule.Resources {
				if res == "nodes" {
					hasNodes = true
				}
			}
		}
		assert.Equal(t, wantNodes, hasNodes, "unexpected nodes permission in ClusterRole rules")
	}

	// nodes permission is always present when k8sCollector is enabled
	checkRules(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "default-config"},
		Spec: vmv1.VLAgentSpec{
			K8sCollector: vmv1.VLAgentK8sCollector{Enabled: true},
		},
	}, true)
}
