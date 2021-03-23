package factory

import (
	"context"
	"fmt"
	"time"

	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// probably, this function can be useful for changing sts storageclass?
func reCreateSTS(ctx context.Context, rclient client.Client, pvcName string, newSTS, existingSTS *appsv1.StatefulSet) (*appsv1.StatefulSet, error) {
	// compare both.

	handleRemove := func() error {
		if err := finalize.RemoveFinalizer(ctx, rclient, existingSTS); err != nil {
			return err
		}
		opts := client.DeleteOptions{PropagationPolicy: func() *metav1.DeletionPropagation {
			p := metav1.DeletePropagationOrphan
			return &p
		}()}
		if err := rclient.Delete(ctx, existingSTS, &opts); err != nil {
			return err
		}
		obj := types.NamespacedName{Name: existingSTS.Name, Namespace: existingSTS.Namespace}

		if err := wait.Poll(time.Second, time.Second*30, func() (done bool, err error) {
			err = rclient.Get(ctx, obj, &appsv1.StatefulSet{})
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, fmt.Errorf("unexpected error for polling, want notFound, got: %w", err)
		}); err != nil {
			return err
		}
		return nil
	}
	needRecreateOnStorageChange := func() bool {
		actualPVC := getPVCFromSTS(pvcName, existingSTS)
		newPVC := getPVCFromSTS(pvcName, newSTS)
		if actualPVC == nil || newPVC == nil {
			return false
		}
		if i := newPVC.Spec.Resources.Requests.Storage().Cmp(*actualPVC.Spec.Resources.Requests.Storage()); i == 0 {
			return false
		} else {
			log.Info("must re-recreate sts, its pvc claim was changed", "size-diff", i)
		}
		return true
	}
	needRecreateOnSpecChange := func() bool {
		// vct changed - added or removed.
		if len(newSTS.Spec.VolumeClaimTemplates) != len(existingSTS.Spec.VolumeClaimTemplates) {
			log.Info("VolumeClaimTemplate for statefulset was changed, recreating it", "sts", newSTS.Name)
			return true
		}
		return false
	}

	if needRecreateOnStorageChange() {
		return newSTS, handleRemove()
	}
	if needRecreateOnSpecChange() {
		return newSTS, handleRemove()
	}

	return newSTS, nil
}

func getPVCFromSTS(pvcName string, sts *appsv1.StatefulSet) *corev1.PersistentVolumeClaim {
	var pvc *corev1.PersistentVolumeClaim
	for _, claim := range sts.Spec.VolumeClaimTemplates {
		if claim.Name == pvcName {
			pvc = &claim
			break
		}
	}
	return pvc
}
func growSTSPVC(ctx context.Context, rclient client.Client, sts *appsv1.StatefulSet, pvcName string) error {
	pvc := getPVCFromSTS(pvcName, sts)
	// fast path
	if pvc == nil {
		return nil
	}
	// check storage class
	ok, err := isStorageClassExpandable(ctx, rclient, pvc)
	if err != nil {
		return err
	}
	if !ok {
		log.Info("storage class for given pvc is not expandable", "pvc", pvc.Name, "sts", sts.Name)
		return nil
	}
	return growPVCs(ctx, rclient, pvc.Spec.Resources.Requests.Storage(), sts.Namespace, sts.Labels)
}

func isStorageClassExpandable(ctx context.Context, rclient client.Client, pvc *corev1.PersistentVolumeClaim) (bool, error) {
	var isNotDefault bool
	var className string
	if pvc.Spec.StorageClassName != nil {
		className = *pvc.Spec.StorageClassName
		isNotDefault = true
	}
	if name, ok := pvc.Annotations["volume.beta.kubernetes.io/storage-class"]; ok {
		className = name
		isNotDefault = true
	}
	var storageClasses v1.StorageClassList
	if err := rclient.List(ctx, &storageClasses); err != nil {
		return false, fmt.Errorf("cannot list storageclasses: %w", err)
	}
	allowExpansion := func(class v1.StorageClass) bool {
		if class.AllowVolumeExpansion != nil && *class.AllowVolumeExpansion {
			return true
		}
		return false
	}
	for i := range storageClasses.Items {
		class := storageClasses.Items[i]
		if !isNotDefault {
			if annotation, ok := class.Annotations["storageclass.kubernetes.io/is-default-class"]; ok {
				if annotation == "true" {
					return allowExpansion(class), nil
				}
			}
		}
		if class.Name == className {
			return allowExpansion(class), nil
		}
	}
	return false, nil
}
func growPVCs(ctx context.Context, rclient client.Client, size *resource.Quantity, ns string, selector map[string]string) error {
	var pvcs corev1.PersistentVolumeClaimList
	opts := &client.ListOptions{
		Namespace:     ns,
		LabelSelector: labels.SelectorFromSet(selector),
	}
	if err := rclient.List(ctx, &pvcs, opts); err != nil {
		return err
	}
	for i := range pvcs.Items {
		pvc := pvcs.Items[i]
		ok, err := mayGrow(size, pvc.Spec.Resources.Requests.Storage())
		if err != nil {
			return err
		}
		if ok {
			log.Info("need to expand pvc", "name", pvc.Name, "size", size.String())
			// check is it possible?
			pvc.Spec.Resources.Requests[corev1.ResourceStorage] = *size
			if err := rclient.Update(ctx, &pvc); err != nil {
				return err
			}
		}
	}
	return nil
}

func mayGrow(newSize, existSize *resource.Quantity) (bool, error) {
	if newSize == nil || existSize == nil {
		return false, nil
	}
	switch newSize.Cmp(*existSize) {
	case 0:
		return false, nil
	case -1:
		return false, fmt.Errorf("cannot decrease pvc size, want: %s, got: %s", newSize.String(), existSize.String())
	default: // increase
		return true, nil
	}
}
