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
	ns := cr.GetNamespace()
	objsToRemove := []client.Object{}
	objsToDisown := []client.Object{}
	if len(cr.Spec.VMAgent.Name) > 0 {
		vmAgentMeta := metav1.ObjectMeta{
			Namespace: ns,
			Name:      cr.Spec.VMAgent.Name,
		}
		objsToRemove = append(objsToRemove, &vmv1beta1.VMAgent{ObjectMeta: vmAgentMeta})
	}
	if len(cr.Spec.VMAuth.Name) > 0 {
		vmAuthMeta := metav1.ObjectMeta{
			Namespace: ns,
			Name:      cr.Spec.VMAuth.Name,
		}
		objsToRemove = append(objsToRemove, &vmv1beta1.VMAuth{ObjectMeta: vmAuthMeta})
	}
	for _, vmclusterSpec := range cr.Spec.Zones.VMClusters {
		if vmclusterSpec.Ref != nil {
			objsToDisown = append(objsToDisown, &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vmclusterSpec.Ref.Name,
					Namespace: cr.Namespace,
				},
			})
			continue
		}
		vmcluster := &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmclusterSpec.Name,
				Namespace: cr.Namespace,
			},
		}
		objsToRemove = append(objsToRemove, vmcluster)
	}
	for _, objToRemove := range objsToRemove {
		if err := removeFinalizeObjByNameWithOwnerReference(ctx, rclient, objToRemove, cr.Name, cr.Namespace, false); err != nil {
			return err
		}
		if err := SafeDelete(ctx, rclient, objToRemove); err != nil {
			return err
		}
	}
	for _, objToDisown := range objsToDisown {
		err := wait.PollUntilContextTimeout(ctx, time.Second, 5*time.Second, true, func(ctx context.Context) (done bool, err error) {
			err = rclient.Get(ctx, types.NamespacedName{Name: objToDisown.GetName(), Namespace: objToDisown.GetNamespace()}, objToDisown)
			if err != nil && !k8serrors.IsNotFound(err) {
				return false, fmt.Errorf("error looking for object: %w", err)
			}
			objToDisown.SetOwnerReferences([]metav1.OwnerReference{})

			if err := rclient.Update(ctx, objToDisown); err != nil {
				return false, fmt.Errorf("error on update: %w", err)
			}
			return true, nil
		})
		if err != nil {
			return fmt.Errorf("failed to disown object %s %s/%s: %w", objToDisown.GetObjectKind().GroupVersionKind(), objToDisown.GetName(), objToDisown.GetNamespace(), err)
		}
	}
	// Remove the CR
	return removeFinalizeObjByName(ctx, rclient, cr, cr.Name, cr.Namespace)
}
