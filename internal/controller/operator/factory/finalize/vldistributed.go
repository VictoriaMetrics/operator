package finalize

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// OnVLDistributedDelete disowns referenced and removes created by VLDistributed components
func OnVLDistributedDelete(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VLDistributed) error {
	var objsToDisown []client.Object

	if cr.Spec.Retain {
		for i := range cr.Spec.Zones {
			zone := &cr.Spec.Zones[i]
			// Release every kind this CR may own, even if its controller is disabled,
			// and both backend kinds, since a backendType change keeps the previous backend.
			// Objects or kinds that do not exist are skipped.
			objsToDisown = append(objsToDisown, &vmv1.VLCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      zone.VLClusterName(cr),
					Namespace: cr.Namespace,
				},
			})
			objsToDisown = append(objsToDisown, &vmv1.VLSingle{
				ObjectMeta: metav1.ObjectMeta{
					Name:      zone.VLSingleName(cr),
					Namespace: cr.Namespace,
				},
			})
			objsToDisown = append(objsToDisown, &vmv1.VLAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      zone.VLAgentName(cr),
					Namespace: cr.Namespace,
				},
			})
		}
		objsToDisown = append(objsToDisown, &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.VLAuthName(),
				Namespace: cr.Namespace,
			},
		})
	}

	if err := disownObjects(ctx, rclient, objsToDisown, cr.AsOwner()); err != nil {
		return err
	}

	// Remove the CR
	return removeFinalizers(ctx, rclient, []client.Object{cr}, []bool{false}, cr)
}
