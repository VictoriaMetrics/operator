package finalize

import (
	"context"
	"fmt"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// OnVMDistributedDelete removes all objects related to VMDistributed component
func OnVMDistributedDelete(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed) error {
	objsToDisown := []client.Object{}
	for i := range cr.Spec.Zones {
		zone := &cr.Spec.Zones[i]
		if zone.VMCluster.Ref != nil && len(zone.VMCluster.Ref.Name) > 0 {
			objsToDisown = append(objsToDisown, &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      zone.VMCluster.Ref.Name,
					Namespace: cr.Namespace,
				},
			})
		}
	}
	owner := cr.AsOwner()
	for _, objToDisown := range objsToDisown {
		nsn := types.NamespacedName{
			Name:      objToDisown.GetName(),
			Namespace: objToDisown.GetNamespace(),
		}
		err := wait.PollUntilContextTimeout(ctx, time.Second, 5*time.Second, true, func(ctx context.Context) (done bool, err error) {
			if err := rclient.Get(ctx, nsn, objToDisown); err != nil {
				if k8serrors.IsNotFound(err) {
					return true, nil
				}
				return false, fmt.Errorf("error looking for object: %w", err)
			}
			ownerReferences := objToDisown.GetOwnerReferences()
			refs := ownerReferences[:0]
			for _, ref := range ownerReferences {
				if ref.APIVersion == owner.APIVersion && ref.Name == owner.Name && ref.Kind == owner.Kind {
					continue
				}
				refs = append(refs, ref)
			}
			objToDisown.SetOwnerReferences(refs)
			if err := rclient.Update(ctx, objToDisown); err != nil {
				if k8serrors.IsConflict(err) {
					return false, nil
				}
				return false, fmt.Errorf("error on update: %w", err)
			}
			return true, nil
		})
		if err != nil {
			return fmt.Errorf("failed to disown object %T=%s: %w", objToDisown, nsn.String(), err)
		}
	}
	// Remove the CR
	return removeFinalizeObjByName(ctx, rclient, cr, cr.Name, cr.Namespace)
}
