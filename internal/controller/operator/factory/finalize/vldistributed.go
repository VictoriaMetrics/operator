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

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

// OnVLDistributedDelete disowns referenced and removes created by VLDistributed components
func OnVLDistributedDelete(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VLDistributed) error {
	var objsToDisown []client.Object

	if cr.Spec.Retain {
		for i := range cr.Spec.Zones {
			zone := &cr.Spec.Zones[i]
			// Disown both backend types: a backend-type change leaves old resources with
			// owner references still pointing to this CR; NotFound is treated as success.
			if !build.IsControllerDisabled("VLCluster") {
				objsToDisown = append(objsToDisown, &vmv1.VLCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      zone.VLClusterName(cr),
						Namespace: cr.Namespace,
					},
				})
			}
			if !build.IsControllerDisabled("VLSingle") {
				objsToDisown = append(objsToDisown, &vmv1.VLSingle{
					ObjectMeta: metav1.ObjectMeta{
						Name:      zone.VLSingleName(cr),
						Namespace: cr.Namespace,
					},
				})
			}
			if !build.IsControllerDisabled("VLAgent") {
				objsToDisown = append(objsToDisown, &vmv1.VLAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      zone.VLAgentName(cr),
						Namespace: cr.Namespace,
					},
				})
			}
		}
		if !build.IsControllerDisabled("VMAuth") {
			objsToDisown = append(objsToDisown, &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cr.VLAuthName(),
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
	return removeFinalizers(ctx, rclient, []client.Object{cr}, []bool{false}, cr)
}
