package k8stools

import (
	"context"
	"fmt"
	"strings"
	"time"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/logger"
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

// recreateSTSIfNeed will check if sts needs recreate and perform recreate if needed,
// there are two different cases:
// 1. sts's VolumeClaimTemplate's element changed[added or deleted];
// 2. other VolumeClaimTemplate's attributes beside name changed, like size or storageClassName
// since pod's volume only related to VCT's name, so when c2 happened, we don't need to recreate pods
//
// Note, in some cases its possible to get orphaned objects,
// if sts was deleted and user updates configuration with different STS name.
// One of possible solutions - save current sts to the object annotation and remove it later if needed.
// Other solution, to check orphaned objects by selector.
// Lets leave it as this for now and handle later.
func recreateSTSIfNeed(ctx context.Context, rclient client.Client, newSTS, existingSTS *appsv1.StatefulSet) (bool, bool, error) {
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
		return nil
	}
	needRecreateOnStorageChange := func(actualPVC, newPVC *corev1.PersistentVolumeClaim) bool {
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
			logger.WithContext(ctx).Info("must re-recreate sts, its pvc claim was changed", "size-diff", sizeDiff.String())
			return true
		}

		// compare meta and spec for pvc
		if !equality.Semantic.DeepEqual(newPVC.ObjectMeta.Labels, actualPVC.ObjectMeta.Labels) || !equality.Semantic.DeepEqual(newPVC.ObjectMeta.Annotations, actualPVC.ObjectMeta.Annotations) || !equality.Semantic.DeepDerivative(newPVC.Spec, actualPVC.Spec) {
			diff := deep.Equal(newPVC.ObjectMeta, actualPVC.ObjectMeta)
			specDiff := deep.Equal(newPVC.Spec, actualPVC.Spec)
			logger.WithContext(ctx).Info("pvc changes detected", "metaDiff", diff, "specDiff", specDiff, "pvc", newPVC.Name)
			return true
		}

		return false
	}

	// if vct got added, removed or changed, recreate the sts
	if len(newSTS.Spec.VolumeClaimTemplates) != len(existingSTS.Spec.VolumeClaimTemplates) {
		logger.WithContext(ctx).Info("VolumeClaimTemplates for statefulset was changed, recreating it", "sts", newSTS.Name)
		return true, true, handleRemove()
	}
	var vctChanged bool
	for _, newVCT := range newSTS.Spec.VolumeClaimTemplates {
		actualPVC := getPVCFromSTS(newVCT.Name, existingSTS)
		if actualPVC == nil {
			logger.WithContext(ctx).Info("VolumeClaimTemplate for statefulset was changed, recreating it", "sts", newSTS.Name, "VolumeClaimTemplates", newVCT.Name)
			return true, true, handleRemove()
		}
		if needRecreateOnStorageChange(actualPVC, &newVCT) {
			logger.WithContext(ctx).Info("VolumeClaimTemplate for statefulset was changed, recreating it", "sts", newSTS.Name, "VolumeClaimTemplates", newVCT.Name)
			vctChanged = true
		}
	}
	// some VolumeClaimTemplate's attributes beside name changed, there is no need to recreate pods
	if vctChanged {
		return true, false, handleRemove()
	}
	if newSTS.Spec.MinReadySeconds != existingSTS.Spec.MinReadySeconds {
		return true, false, handleRemove()
	}
	return false, false, nil
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
	// fast path
	if sts.Spec.Replicas != nil && *sts.Spec.Replicas == 0 {
		return nil
	}
	targetClaimsByName := make(map[string]*corev1.PersistentVolumeClaim)
	for _, stsClaim := range sts.Spec.VolumeClaimTemplates {
		targetClaimsByName[fmt.Sprintf("%s-%s", stsClaim.Name, sts.Name)] = &stsClaim
	}
	// list current pvcs that belongs to sts
	var pvcs corev1.PersistentVolumeClaimList
	opts := &client.ListOptions{
		Namespace:     sts.Namespace,
		LabelSelector: labels.SelectorFromSet(sts.Spec.Selector.MatchLabels),
	}
	if err := rclient.List(ctx, &pvcs, opts); err != nil {
		return err
	}
	if len(pvcs.Items) == 0 {
		return fmt.Errorf("got 0 pvcs under %s for selector %v, statefulset could not be working", sts.Namespace, sts.Spec.Selector.MatchLabels)
	}
	for _, pvc := range pvcs.Items {
		idx := strings.LastIndexByte(pvc.Name, '-')
		if idx <= 0 {
			return fmt.Errorf("not expected name for pvc=%q, it must have - as separator for sts=%q", pvc.Name, sts.Name)
		}
		// pvc created by sts always has name of CLAIM_NAME-STS_NAME-REPLICA_IDX
		stsClaimName := pvc.Name[:idx]
		stsClaim, ok := targetClaimsByName[stsClaimName]
		if !ok {
			logger.WithContext(ctx).Info("cannot find target pvc in new statefulset, please check if the old one is still needed", "pvc", pvc.Name, "sts", sts.Name, "claimName", stsClaimName)
			continue
		}
		// check if storage class is expandable
		isExpandable, err := isStorageClassExpandable(ctx, rclient, stsClaim)
		if err != nil {
			return fmt.Errorf("failed to check storageClass expandability for pvc %s: %v", pvc.Name, err)
		}
		if !isExpandable {
			// don't return error to caller, since there is no point to requeue and reconcile this when sc is unexpandable
			logger.WithContext(ctx).Info("storage class for PVC doesn't support live resizing", "pvc", pvc.Name)
			continue
		}
		err = growPVCs(ctx, rclient, stsClaim.Spec.Resources.Requests.Storage(), &pvc)
		if err != nil {
			return fmt.Errorf("failed to expand size for pvc %s: %v", pvc.Name, err)
		}

	}
	return nil
}

// isStorageClassExpandable check is it possible to update size of given pvc
func isStorageClassExpandable(ctx context.Context, rclient client.Client, pvc *corev1.PersistentVolumeClaim) (bool, error) {
	// do not perform any checks if user set annotation explicitly.
	v, ok := pvc.Annotations[victoriametricsv1beta1.PVCExpandableLabel]
	if ok {
		switch v {
		case "true", "True":
			return true, nil
		case "false", "False":
			return false, nil
		default:
			return false, fmt.Errorf("not expected value format for annotation=%q: %q, want true or false", victoriametricsv1beta1.PVCExpandableLabel, v)
		}
	}
	// fast path at single namespace mode, listing storage classes is disabled
	if !config.IsClusterWideAccessAllowed() {
		// don't return error to caller, since there is no point to requeue and reconcile this
		logger.WithContext(ctx).Info("cannot detect if storageClass expandable at single namespace mode, need to expand PVC manually or enforce resizing by adding specific annotation to true", "pvc annotation", victoriametricsv1beta1.PVCExpandableLabel)
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
		return false, fmt.Errorf("cannot list storageClass: %w", err)
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
	if mayGrow(ctx, size, pvc.Spec.Resources.Requests.Storage()) {
		logger.WithContext(ctx).Info("need to expand pvc size", "name", pvc.Name, "from", pvc.Spec.Resources.Requests.Storage(), "to", size.String())
		pvc.Spec.Resources.Requests[corev1.ResourceStorage] = *size
		err = rclient.Update(ctx, pvc)
	}
	return err
}

// checks is pvc needs to be resized.
func mayGrow(ctx context.Context, newSize, existSize *resource.Quantity) bool {
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
		logger.WithContext(ctx).Error(fmt.Errorf("cannot decrease pvc size, want: %s, got: %s", newSize.String(), existSize.String()), "update pvc manually")
		return false
	default: // increase
		return true
	}
}
