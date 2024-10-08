package finalize

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"

	appsv1 "k8s.io/api/apps/v1"
	v2 "k8s.io/api/autoscaling/v2"
	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OnVMInsertDelete removes all objects related to vminsert component
func OnVMInsertDelete(ctx context.Context, rclient client.Client, crd *vmv1beta1.VMCluster, obj *vmv1beta1.VMInsert) error {
	objMeta := metav1.ObjectMeta{
		Namespace: crd.Namespace,
		Name:      obj.GetNameWithPrefix(crd.Name),
	}
	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&v1.Service{ObjectMeta: objMeta},
	}
	if obj.ServiceSpec != nil && !obj.ServiceSpec.UseAsDefault {
		objsToRemove = append(objsToRemove, &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: crd.Namespace,
				Name:      obj.ServiceSpec.NameOrDefault(obj.GetNameWithPrefix(crd.Name)),
			},
		})
	}
	if obj.PodDisruptionBudget != nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta})
	}
	if obj.HPA != nil {
		objsToRemove = append(objsToRemove, &v2.HorizontalPodAutoscaler{ObjectMeta: objMeta})
	}
	if !ptr.Deref(obj.DisableSelfServiceScrape, false) {
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta})
	}

	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnVMInsertDelete removes all objects related to vminsert component
func OnVMSelectDelete(ctx context.Context, rclient client.Client, crd *vmv1beta1.VMCluster, obj *vmv1beta1.VMSelect) error {
	objMeta := metav1.ObjectMeta{
		Namespace: crd.Namespace,
		Name:      obj.GetNameWithPrefix(crd.Name),
	}
	objsToRemove := []client.Object{
		&appsv1.StatefulSet{ObjectMeta: objMeta},
		&v1.Service{ObjectMeta: objMeta},
	}
	if obj.ServiceSpec != nil && !obj.ServiceSpec.UseAsDefault {
		objsToRemove = append(objsToRemove, &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: crd.Namespace,
				Name:      obj.ServiceSpec.NameOrDefault(obj.GetNameWithPrefix(crd.Name)),
			},
		})
	}
	if obj.PodDisruptionBudget != nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta})
	}
	if obj.HPA != nil {
		objsToRemove = append(objsToRemove, &v2.HorizontalPodAutoscaler{ObjectMeta: objMeta})
	}
	if !ptr.Deref(obj.DisableSelfServiceScrape, false) {
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta})
	}
	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnVMInsertDelete removes all objects related to vminsert component
func OnVMStorageDelete(ctx context.Context, rclient client.Client, crd *vmv1beta1.VMCluster, obj *vmv1beta1.VMStorage) error {
	objMeta := metav1.ObjectMeta{
		Namespace: crd.Namespace,
		Name:      obj.GetNameWithPrefix(crd.Name),
	}
	objsToRemove := []client.Object{
		&appsv1.StatefulSet{ObjectMeta: objMeta},
		&v1.Service{ObjectMeta: objMeta},
	}
	if obj.ServiceSpec != nil && !obj.ServiceSpec.UseAsDefault {
		objsToRemove = append(objsToRemove, &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: crd.Namespace,
				Name:      obj.ServiceSpec.NameOrDefault(obj.GetNameWithPrefix(crd.Name)),
			},
		})
	}
	if obj.PodDisruptionBudget != nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta})
	}
	if !ptr.Deref(obj.DisableSelfServiceScrape, false) {
		objsToRemove = append(objsToRemove, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta})
	}

	for _, objToRemove := range objsToRemove {
		if err := SafeDeleteWithFinalizer(ctx, rclient, objToRemove); err != nil {
			return fmt.Errorf("failed to remove object=%s: %w", objToRemove.GetObjectKind().GroupVersionKind(), err)
		}
	}
	return nil
}

// OnVMClusterDelete deletes all vmcluster related resources
func OnVMClusterDelete(ctx context.Context, rclient client.Client, crd *vmv1beta1.VMCluster) error {
	// check deployment

	if crd.Spec.VMInsert != nil {
		if err := OnVMInsertDelete(ctx, rclient, crd, crd.Spec.VMInsert); err != nil {
			return fmt.Errorf("cannot remove vminsert component objects: %w", err)
		}
	}

	if crd.Spec.VMSelect != nil {
		if err := OnVMSelectDelete(ctx, rclient, crd, crd.Spec.VMSelect); err != nil {
			return fmt.Errorf("cannot remove vmselect component objects: %w", err)
		}
	}
	if crd.Spec.VMStorage != nil {
		if err := OnVMStorageDelete(ctx, rclient, crd, crd.Spec.VMStorage); err != nil {
			return fmt.Errorf("cannot remove vmstorage component objects: %w", err)
		}
	}

	if err := deleteSA(ctx, rclient, crd); err != nil {
		return err
	}
	return removeFinalizeObjByName(ctx, rclient, crd, crd.Name, crd.Namespace)
}
