package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// OnVLogsDelete deletes all vlogs related resources
func OnVLogsDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VLogs) error {
	ns := cr.GetNamespace()
	objMeta := metav1.ObjectMeta{
		Namespace: ns,
		Name:      cr.PrefixedName(),
	}
	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetServiceAccountName(),
			Namespace: ns,
		}},
		&corev1.PersistentVolumeClaim{ObjectMeta: objMeta},
	}
	if cr.Spec.ServiceSpec != nil {
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Spec.ServiceSpec.NameOrDefault(cr.PrefixedName()),
			Namespace: ns,
		}})
	}
	deleteOwnerReferences := make([]bool, len(objsToRemove))

	deleteOwnerReferences = append(deleteOwnerReferences, !cr.Spec.RemovePvcAfterDelete)
	objsToRemove = append(objsToRemove, &corev1.PersistentVolumeClaim{ObjectMeta: objMeta})

	deleteOwnerReferences = append(deleteOwnerReferences, false)
	objsToRemove = append(objsToRemove, cr)

	return removeFinalizers(ctx, rclient, objsToRemove, deleteOwnerReferences, cr)
}
