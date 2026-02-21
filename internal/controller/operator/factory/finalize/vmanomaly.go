package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// OnVMAnomalyDelete deletes all anomaly related resources
func OnVMAnomalyDelete(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) error {
	if err := RemoveOrphanedSTSs(ctx, rclient, cr, nil, false); err != nil {
		return err
	}
	if err := RemoveOrphanedPDBs(ctx, rclient, cr, nil, false); err != nil {
		return err
	}
	ns := cr.GetNamespace()
	objMeta := metav1.ObjectMeta{
		Namespace: ns,
		Name:      cr.PrefixedName(),
	}
	objsToRemove := []client.Object{
		&appsv1.StatefulSet{ObjectMeta: objMeta},
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
		cr,
	}
	deleteOwnerReferences := make([]bool, len(objsToRemove))
	return removeFinalizers(ctx, rclient, objsToRemove, deleteOwnerReferences, cr)
}
