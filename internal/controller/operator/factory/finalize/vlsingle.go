package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

// OnVLSingleDelete deletes all vlogs related resources
func OnVLSingleDelete(ctx context.Context, rclient client.Client, cr *vmv1.VLSingle) error {
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
	for key, spec := range vmv1beta1.ResolveExtraServiceSpecs(cr.Spec.ServiceSpec, cr.Spec.ServiceSpecs) {
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      spec.NameOrDefaultForKey(cr.PrefixedName(), key),
			Namespace: ns,
		}})
	}
	if config.MustGetBaseConfig().VPAAPIEnabled {
		objsToRemove = append(objsToRemove, &vpav1.VerticalPodAutoscaler{ObjectMeta: objMeta})
	}
	objsToRemove = append(objsToRemove, cr)
	deleteOwnerReferences := make([]bool, len(objsToRemove))
	return removeFinalizers(ctx, rclient, objsToRemove, deleteOwnerReferences, cr)
}
