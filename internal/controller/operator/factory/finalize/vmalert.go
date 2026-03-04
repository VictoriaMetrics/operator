package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// OnVMAlertDelete deletes all vmalert related resources
func OnVMAlertDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) error {
	if err := RemoveOrphanedConfigMaps(ctx, rclient, cr, nil, false); err != nil {
		return err
	}
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
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:      build.ResourceName(build.SecretConfigResourceKind, cr),
			Namespace: ns,
		}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:      build.ResourceName(build.TLSAssetsResourceKind, cr),
			Namespace: ns,
		}},
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
