package k8stools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-test/deep"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func cleanUpFinalize(ctx context.Context, rclient client.Client, instance client.Object) error {
	if victoriametricsv1beta1.IsContainsFinalizer(instance.GetFinalizers(), victoriametricsv1beta1.FinalizerName) {
		instance.SetFinalizers(victoriametricsv1beta1.RemoveFinalizer(instance.GetFinalizers(), victoriametricsv1beta1.FinalizerName))
		return rclient.Update(ctx, instance)
	}
	return nil
}

// recreateSTS if needed.
// Note, in some cases its possible to get orphaned objects,
// if sts was deleted and user updates configuration with different STS name.
// One of possible solutions - save current sts to the object annotation and remove it later if needed.
// Other solution, to check orphaned objects by selector.
// Lets leave it as this for now and handle later.
func wasCreatedSTS(ctx context.Context, rclient client.Client, newSTS, existingSTS *appsv1.StatefulSet) (bool, error) {
	handleRemove := func() error {
		// removes finalizer from exist sts, it allows to delete it
		if err := cleanUpFinalize(ctx, rclient, existingSTS); err != nil {
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

		// wait until sts disappears
		if err := wait.Poll(time.Second, time.Second*30, func() (done bool, err error) {
			err = rclient.Get(ctx, obj, &appsv1.StatefulSet{})
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, fmt.Errorf("unexpected error for polling, want notFound, got: %w", err)
		}); err != nil {
			return err
		}

		if err := rclient.Create(ctx, newSTS); err != nil {
			// try to restore previous one and throw error
			existingSTS.ResourceVersion = ""
			if err2 := rclient.Create(ctx, existingSTS); err2 != nil {
				return fmt.Errorf("cannot restore previous sts: %s configuration after remove original error: %s: restore error %w", existingSTS.Name, err, err2)
			}
			return fmt.Errorf("cannot create new sts: %s instead of replaced, some manual action is required, err: %w", newSTS.Name, err)
		}

		// this is hack
		// for some reason, kubernetes doesn't update sts status after its re-creation
		// so, manually set currentVersion to the version of previous sts.
		// updateRevision will be fetched from first re-created pod
		// https://github.com/VictoriaMetrics/operator/issues/344
		newSTS.Status.CurrentRevision = existingSTS.Status.CurrentRevision
		if err := rclient.Status().Update(ctx, newSTS); err != nil {
			return fmt.Errorf("cannot update re-created statefulset status version: %w", err)
		}
		return nil
	}
	needRecreateOnStorageChange := func(pvcName string) bool {
		actualPVC := getPVCFromSTS(pvcName, existingSTS)
		newPVC := getPVCFromSTS(pvcName, newSTS)
		// fast path
		if actualPVC == nil && newPVC == nil {
			return false
		}
		// one of pvc is not nil
		hasNotNilPVC := (actualPVC == nil && newPVC != nil) || (actualPVC != nil && newPVC == nil)
		if hasNotNilPVC {
			return true
		}

		if i := newPVC.Spec.Resources.Requests.Storage().Cmp(*actualPVC.Spec.Resources.Requests.Storage()); i != 0 {
			sizeDiff := resource.NewQuantity(0, resource.BinarySI)
			sizeDiff.Add(*newPVC.Spec.Resources.Requests.Storage())
			sizeDiff.Sub(*actualPVC.Spec.Resources.Requests.Storage())
			log.Info("must re-recreate sts, its pvc claim was changed", "size-diff", sizeDiff.String())
			return true
		}

		// compare meta and spec for pvc
		if !equality.Semantic.DeepDerivative(newPVC.ObjectMeta, actualPVC.ObjectMeta) || !equality.Semantic.DeepDerivative(newPVC.Spec, actualPVC.Spec) {
			diff := deep.Equal(newPVC.ObjectMeta, actualPVC.ObjectMeta)
			specDiff := deep.Equal(newPVC.Spec, actualPVC.Spec)
			log.Info("pvc changes detected", "metaDiff", diff, "specDiff", specDiff, "pvc", pvcName)
			return true
		}

		return false
	}

	// if vct got added, removed or changed, recreate the sts
	if len(newSTS.Spec.VolumeClaimTemplates) != len(existingSTS.Spec.VolumeClaimTemplates) {
		log.Info("VolumeClaimTemplates for statefulset was changed, recreating it", "sts", newSTS.Name)
		return true, handleRemove()
	}
	for _, newVCT := range newSTS.Spec.VolumeClaimTemplates {
		if needRecreateOnStorageChange(newVCT.Name) {
			log.Info("VolumeClaimTemplate for statefulset was changed, recreating it", "sts", newSTS.Name, "VolumeClaimTemplates", newVCT.Name)
			return true, handleRemove()
		}
	}
	return false, nil
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

func growSTSPVC(ctx context.Context, rclient client.Client, sts *appsv1.StatefulSet) error {
	targetPVCs := sts.Spec.VolumeClaimTemplates
	// list current pvcs
	var pvcs corev1.PersistentVolumeClaimList
	opts := &client.ListOptions{
		Namespace:     sts.Namespace,
		LabelSelector: labels.SelectorFromSet(sts.Spec.Selector.MatchLabels),
	}
	if err := rclient.List(ctx, &pvcs, opts); err != nil {
		return err
	}
	if len(pvcs.Items) == 0 {
		log.Info("PVCs select call returned 0 pvcs, it could be a bug, want match %v in namespace %s", sts.Spec.Selector.MatchLabels, sts.Namespace)
		return nil
	}
	for _, pvc := range pvcs.Items {
		var isExist bool
		for _, tpvc := range targetPVCs {
			if strings.HasPrefix(pvc.Name, fmt.Sprintf("%s-%s", tpvc.Name, sts.Name)) {
				isExist = true
				// check if storage class is expandable
				isExpandable, err := isStorageClassExpandable(ctx, rclient, &pvc)
				if err != nil {
					logger.Errorf("failed to check storageClass expandability for pvc %s: %v", pvc.Name, err)
					break
				}
				if !isExpandable {
					log.Info("want to expand pvc %s but storageClass doesn't support it, need to handle this case manually", pvc.Name)
				}
				err = growPVCs(ctx, rclient, tpvc.Spec.Resources.Requests.Storage(), &pvc)
				if err != nil {
					logger.Errorf("failed to expand size for pvc %s: %v", pvc.Name, err)
				}
				break
			}
		}
		if !isExist {
			log.Info("cannot find target pvc in new statefulset, please check if the old one is still needed", "pvc", pvc.Name, "sts", sts.Name)
		}
	}
	return nil
}

// isStorageClassExpandable check is it possible to update size of given pvc
func isStorageClassExpandable(ctx context.Context, rclient client.Client, pvc *corev1.PersistentVolumeClaim) (bool, error) {
	// do not perform any checks if user set annotation explicitly.
	if pvc.Annotations[victoriametricsv1beta1.PVCExpandableLabel] == "true" {
		return true, nil
	}
	// fast path at single namespace mode, listing storage classes is disabled
	if !config.IsClusterWideAccessAllowed() {
		log.Info("cannot detect if storageClass expandable at single namespace mode, expand PVC manually or enforce resizing with annotation to true", "pvc annotation", victoriametricsv1beta1.PVCExpandableLabel)
		return false, nil
	}
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
		// look for default storageClass.
		if !isNotDefault {
			if annotation, ok := class.Annotations["storageclass.kubernetes.io/is-default-class"]; ok {
				if annotation == "true" {
					return allowExpansion(class), nil
				}
			}
		}
		// check class name.
		if isNotDefault {
			if class.Name == className {
				return allowExpansion(class), nil
			}
		}
	}
	return false, nil
}

func growPVCs(ctx context.Context, rclient client.Client, size *resource.Quantity, pvc *corev1.PersistentVolumeClaim) error {
	var err error
	if mayGrow(size, pvc.Spec.Resources.Requests.Storage()) {
		log.Info("need to expand pvc size", "name", pvc.Name, "from", pvc.Spec.Resources.Requests.Storage(), "to", size.String())
		pvc.Spec.Resources.Requests[corev1.ResourceStorage] = *size
		err = rclient.Update(ctx, pvc)
	}
	return err
}

// checks is pvc needs to be resized.
func mayGrow(newSize, existSize *resource.Quantity) bool {
	if newSize == nil || existSize == nil {
		return false
	}
	switch newSize.Cmp(*existSize) {
	case 0:
		return false
	case -1:
		// do no return error
		// probably, user updated pvc manually
		// without applying this changes to the configuration.
		log.Error(fmt.Errorf("cannot decrease pvc size, want: %s, got: %s", newSize.String(), existSize.String()), "update pvc manually")
		return false
	default: // increase
		return true
	}
}
