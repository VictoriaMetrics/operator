package finalize

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// OnVMDistributedDelete disowns referenced and removes created by VMDistributed components
func OnVMDistributedDelete(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed) error {
	var objsToDisown []client.Object

	if cr.Spec.Retain {
		for i := range cr.Spec.Zones {
			zone := &cr.Spec.Zones[i]
			// Disown both backend types: a backend-type change leaves old resources with
			// owner references still pointing to this CR; NotFound is treated as success.
			if !build.IsControllerDisabled("VMCluster") {
				objsToDisown = append(objsToDisown, &vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      zone.VMClusterName(cr),
						Namespace: cr.Namespace,
					},
				})
			}
			if !build.IsControllerDisabled("VMSingle") {
				objsToDisown = append(objsToDisown, &vmv1beta1.VMSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      zone.VMSingleName(cr),
						Namespace: cr.Namespace,
					},
				})
			}
			if !build.IsControllerDisabled("VMAgent") {
				objsToDisown = append(objsToDisown, &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      zone.VMAgentName(cr),
						Namespace: cr.Namespace,
					},
				})
			}
		}
		if !build.IsControllerDisabled("VMAuth") {
			objsToDisown = append(objsToDisown, &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cr.VMAuthName(),
					Namespace: cr.Namespace,
				},
			})
		}
	}

	if err := disownObjects(ctx, rclient, objsToDisown, cr.AsOwner()); err != nil {
		return err
	}

	// Remove the CR
	return removeFinalizers(ctx, rclient, []client.Object{cr}, []bool{false}, cr)
}
