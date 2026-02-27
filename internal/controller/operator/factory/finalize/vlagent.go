package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

// OnVLAgentDelete deletes all vlagent related resources
func OnVLAgentDelete(ctx context.Context, rclient client.Client, cr *vmv1.VLAgent) error {
	ns := cr.GetNamespace()
	if config.IsClusterWideAccessAllowed() {
		objMeta := metav1.ObjectMeta{
			Name: cr.GetRBACName(),
		}
		objsToRemove := []client.Object{
			&rbacv1.ClusterRoleBinding{ObjectMeta: objMeta},
			&rbacv1.ClusterRole{ObjectMeta: objMeta},
		}
		if err := SafeDeleteWithFinalizer(ctx, rclient, objsToRemove, cr); err != nil {
			return err
		}
	}
	objMeta := metav1.ObjectMeta{
		Namespace: ns,
		Name:      cr.PrefixedName(),
	}
	objsToRemove := []client.Object{
		&corev1.Service{ObjectMeta: objMeta},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetServiceAccountName(),
			Namespace: ns,
		}},
		&appsv1.DaemonSet{ObjectMeta: objMeta},
		&appsv1.StatefulSet{ObjectMeta: objMeta},
		&corev1.PersistentVolumeClaim{ObjectMeta: objMeta},
		&policyv1.PodDisruptionBudget{ObjectMeta: objMeta},
	}
	if cr.Spec.ServiceSpec != nil {
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Spec.ServiceSpec.NameOrDefault(cr.PrefixedName()),
			Namespace: ns,
		}})
	}
	objsToRemove = append(objsToRemove, cr)
	deleteOwnerReferences := make([]bool, len(objsToRemove))
	return removeFinalizers(ctx, rclient, objsToRemove, deleteOwnerReferences, cr)
}
