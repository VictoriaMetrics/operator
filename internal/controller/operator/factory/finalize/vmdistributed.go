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

// OnVMDistributedDelete disowns referenced and removes created by VMDistributed components
func OnVMDistributedDelete(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed) error {
	var objsToDisown []client.Object

	if cr.Spec.Retain {
		for i := range cr.Spec.Zones {
			zone := &cr.Spec.Zones[i]
			objsToDisown = append(objsToDisown, &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      zone.VMClusterName(cr),
					Namespace: cr.Namespace,
				},
			}, &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      zone.VMAgentName(cr),
					Namespace: cr.Namespace,
				},
			})
		}
		objsToDisown = append(objsToDisown, &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.VMAuthName(),
				Namespace: cr.Namespace,
			},
		})
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
	return removeFinalizers(ctx, rclient, []client.Object{cr}, []bool{false}, cr)
}
