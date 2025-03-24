package reconcile

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-test/deep"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// recreateSTSIfNeed will check if sts needs recreate and perform recreate if needed,
// there are three different cases:
// 1. sts's VolumeClaimTemplate's element changed[added or deleted];
// 2. other VolumeClaimTemplate's attributes beside name changed, like size or storageClassName
// since pod's volume only related to VCT's name, so when c2 happened, we don't need to recreate pods
// 3. sts's serviceName changed, which requires to recreate pods for proper service discovery
//
// Note, in some cases its possible to get orphaned objects,
// if sts was deleted and user updates configuration with different STS name.
// One of possible solutions - save current sts to the object annotation and remove it later if needed.
// Other solution, to check orphaned objects by selector.
// Lets leave it as this for now and handle later.
func recreateSTSIfNeed(ctx context.Context, rclient client.Client, newSTS, oldStatefulSet *appsv1.StatefulSet) (bool, bool, error) {
	l := logger.WithContext(ctx)
	handleRemove := func() error {
		return removeStatefulSetKeepPods(ctx, rclient, newSTS, oldStatefulSet)
	}
	if newSTS.Spec.ServiceName != oldStatefulSet.Spec.ServiceName {
		return true, true, handleRemove()
	}

	// if vct got added, removed or changed, recreate the sts
	if len(newSTS.Spec.VolumeClaimTemplates) != len(oldStatefulSet.Spec.VolumeClaimTemplates) {
		l.Info("VolumeClaimTemplates count changes, recreating statefulset")
		return true, true, handleRemove()
	}

	var vctChanged bool
	for _, newVCT := range newSTS.Spec.VolumeClaimTemplates {
		actualPVC := getPVCFromSTS(newVCT.Name, oldStatefulSet)
		if actualPVC == nil {
			l.Info(fmt.Sprintf("VolumeClaimTemplate=%s was not found at VolumeClaimTemplates, recreating statefulset", newVCT.Name))
			return true, true, handleRemove()
		}
		if shouldRecreateSTSOnStorageChange(ctx, actualPVC, &newVCT) {
			l.Info(fmt.Sprintf("VolumeClaimTemplate=%s have some changes, recreating StatefulSet", newVCT.Name))
			vctChanged = true
		}
	}
	// some VolumeClaimTemplate's attributes beside name changed, there is no need to recreate pods
	if vctChanged {
		return true, false, handleRemove()
	}
	if shouldRecreateSTSOnImmutableFieldChange(ctx, newSTS, oldStatefulSet) {
		return true, false, handleRemove()
	}

	return false, false, nil
}

func isPVClaimPolicyEqual(left, right *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy) bool {
	// current kubernetes version doesn't support claim retention feature gate
	// https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#persistentvolumeclaim-retention
	if right == nil {
		return true
	}
	if left == nil {
		return true
	}

	return *left == *right
}

func getPVCFromSTS(pvcName string, sts *appsv1.StatefulSet) *corev1.PersistentVolumeClaim {
	for _, claim := range sts.Spec.VolumeClaimTemplates {
		if claim.Name == pvcName {
			return &claim
		}
	}
	return nil
}

func growSTSPVC(ctx context.Context, rclient client.Client, sts *appsv1.StatefulSet) error {
	// fast path
	if sts.Spec.Replicas != nil && *sts.Spec.Replicas == 0 {
		return nil
	}
	l := logger.WithContext(ctx)
	targetClaimsByName := make(map[string]corev1.PersistentVolumeClaim)
	for _, stsClaim := range sts.Spec.VolumeClaimTemplates {
		targetClaimsByName[fmt.Sprintf("%s-%s", stsClaim.Name, sts.Name)] = stsClaim
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
			return fmt.Errorf("not expected name for PVC=%q, it must have - as separator for sts=%q", pvc.Name, sts.Name)
		}
		// pvc created by sts always has name of CLAIM_NAME-STS_NAME-REPLICA_IDX
		stsClaimName := pvc.Name[:idx]
		stsClaim, ok := targetClaimsByName[stsClaimName]
		if !ok {
			l.Info(fmt.Sprintf("possible BUG, cannot find target PVC=%s in new statefulset by claim name=%s", pvc.Name, stsClaimName))
			continue
		}
		// check if pvc need to grow
		newSize := stsClaim.Spec.Resources.Requests.Storage()
		oldSize := pvc.Spec.Resources.Requests.Storage()
		if !mayGrow(ctx, &pvc, newSize, oldSize) {
			continue
		}
		l.Info(fmt.Sprintf("need to expand pvc=%s size from=%s to=%s", pvc.Name, oldSize, newSize))
		// check if storage class is expandable
		isExpandable, err := isStorageClassExpandable(ctx, rclient, &stsClaim)
		if err != nil {
			return fmt.Errorf("failed to check storageClass expandability for pvc %s: %v", pvc.Name, err)
		}
		if !isExpandable {
			// don't return error to caller, since there is no point to requeue and reconcile this when sc is unexpandable
			l.Info(fmt.Sprintf("storage class=%s for PVC=%s doesn't support live resizing", ptr.Deref(pvc.Spec.StorageClassName, "default"), pvc.Name))
			continue
		}
		err = growPVCs(ctx, rclient, newSize, &pvc)
		if err != nil {
			return fmt.Errorf("failed to expand size for pvc %s: %v", pvc.Name, err)
		}
	}
	return nil
}

// isStorageClassExpandable check is it possible to update size of given pvc
func isStorageClassExpandable(ctx context.Context, rclient client.Client, pvc *corev1.PersistentVolumeClaim) (bool, error) {
	// do not perform any checks if user set annotation explicitly.
	v, ok := pvc.Annotations[vmv1beta1.PVCExpandableLabel]
	if ok {
		switch v {
		case "true", "True":
			return true, nil
		case "false", "False":
			return false, nil
		default:
			return false, fmt.Errorf("not expected value format for annotation=%q: %q, want true or false", vmv1beta1.PVCExpandableLabel, v)
		}
	}
	// fast path at single namespace mode, listing storage classes is disabled
	if !config.IsClusterWideAccessAllowed() {
		// don't return error to caller, since there is no point to requeue and reconcile this
		logger.WithContext(ctx).Info(fmt.Sprintf("cannot detect if storageClass expandable at single namespace mode"+
			`need to expand PVC manually or enforce resizing by adding annotation %s: "true" to PVC`,
			vmv1beta1.PVCExpandableLabel))
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
	var storageClasses storagev1.StorageClassList
	if err := rclient.List(ctx, &storageClasses); err != nil {
		return false, fmt.Errorf("cannot list storageClass: %w", err)
	}
	allowExpansion := func(class storagev1.StorageClass) bool {
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
	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = *size
	return rclient.Update(ctx, pvc)
}

// checks if pvc needs to be resized.
func mayGrow(ctx context.Context, pvc *corev1.PersistentVolumeClaim, newSize, existSize *resource.Quantity) bool {
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
		logger.WithContext(ctx).Error(fmt.Errorf("cannot decrease PVC=%s size from=%s to=%s", pvc.Name, newSize.String(), existSize.String()),
			"cannot decrease PVC size, please check VolumeClaimTemplate configuration")
		return false
	default: // increase
		return true
	}
}

func shouldRecreateSTSOnStorageChange(ctx context.Context, actualPVC, newPVC *corev1.PersistentVolumeClaim) bool {
	// fast path
	if actualPVC == nil && newPVC == nil {
		return false
	}
	// one of pvcs are not nil
	hasNotNilPVC := (actualPVC == nil && newPVC != nil) || (actualPVC != nil && newPVC == nil)
	if hasNotNilPVC {
		return true
	}

	if i := newPVC.Spec.Resources.Requests.Storage().Cmp(*actualPVC.Spec.Resources.Requests.Storage()); i != 0 {
		sizeDiff := resource.NewQuantity(0, resource.BinarySI)
		sizeDiff.Add(*newPVC.Spec.Resources.Requests.Storage())
		sizeDiff.Sub(*actualPVC.Spec.Resources.Requests.Storage())
		logger.WithContext(ctx).Info(fmt.Sprintf("must re-recreate sts, its pvc claim size=%s was changed", sizeDiff.String()))
		return true
	}

	// compare meta and spec for pvc
	if !equality.Semantic.DeepEqual(newPVC.ObjectMeta.Labels, actualPVC.ObjectMeta.Labels) ||
		!equality.Semantic.DeepEqual(newPVC.ObjectMeta.Annotations, actualPVC.ObjectMeta.Annotations) ||
		!equality.Semantic.DeepDerivative(newPVC.Spec, actualPVC.Spec) {
		diff := deep.Equal(newPVC.ObjectMeta, actualPVC.ObjectMeta)
		specDiff := deep.Equal(newPVC.Spec, actualPVC.Spec)

		logMsg := fmt.Sprintf("changes detected for PVC=%s metaDiff=%v, specDiff=%v", newPVC.Name, strings.Join(diff, ","), strings.Join(specDiff, ","))
		logger.WithContext(ctx).Info(logMsg)
		return true
	}

	return false
}

// shouldRecreateSTSOnImmutableFieldChange checks if immutable statefulSet fields were changed
//
// logic was borrowed from
// https://github.com/kubernetes/kubernetes/blob/a866cbe2e5bbaa01cfd5e969aa3e033f3282a8a2/pkg/apis/apps/validation/validation.go#L166
func shouldRecreateSTSOnImmutableFieldChange(ctx context.Context, statefulSet, oldStatefulSet *appsv1.StatefulSet) bool {
	// statefulset updates aren't super common and general updates are likely to be touching spec, so we'll do this
	// deep copy right away.  This avoids mutating our inputs
	newStatefulSetClone := statefulSet.DeepCopy()

	// VolumeClaimTemplates must be checked before performing this check
	newStatefulSetClone.Spec.VolumeClaimTemplates = oldStatefulSet.Spec.VolumeClaimTemplates

	newStatefulSetClone.Spec.Replicas = oldStatefulSet.Spec.Replicas
	newStatefulSetClone.Spec.Template = oldStatefulSet.Spec.Template
	newStatefulSetClone.Spec.UpdateStrategy = oldStatefulSet.Spec.UpdateStrategy
	newStatefulSetClone.Spec.MinReadySeconds = oldStatefulSet.Spec.MinReadySeconds
	newStatefulSetClone.Spec.PersistentVolumeClaimRetentionPolicy = oldStatefulSet.Spec.PersistentVolumeClaimRetentionPolicy

	isEqual := equality.Semantic.DeepEqual(newStatefulSetClone.Spec, oldStatefulSet.Spec)
	if !isEqual {
		logger.WithContext(ctx).Info("immutable StatefulSet field changed")
	}
	return !isEqual
}

func removeStatefulSetKeepPods(ctx context.Context, rclient client.Client, statefulSet, oldStatefulSet *appsv1.StatefulSet) error {
	// removes finalizer from exist sts, it allows to delete it
	if err := finalize.RemoveFinalizer(ctx, rclient, oldStatefulSet); err != nil {
		return fmt.Errorf("failed to remove finalizer from sts: %w", err)
	}
	opts := client.DeleteOptions{PropagationPolicy: func() *metav1.DeletionPropagation {
		p := metav1.DeletePropagationOrphan
		return &p
	}()}
	if err := rclient.Delete(ctx, oldStatefulSet, &opts); err != nil {
		return err
	}
	nsn := types.NamespacedName{Name: oldStatefulSet.Name, Namespace: oldStatefulSet.Namespace}

	// wait until sts disappears
	if err := wait.PollUntilContextTimeout(ctx, time.Second, time.Second*30, false, func(_ context.Context) (done bool, err error) {
		err = rclient.Get(ctx, nsn, &appsv1.StatefulSet{})
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("unexpected error for polling, want notFound, got: %w", err)
	}); err != nil {
		return fmt.Errorf("cannot wait for sts to be deleted: %w", err)
	}

	if err := rclient.Create(ctx, statefulSet); err != nil {
		// try to restore previous one and throw error
		oldStatefulSet.ResourceVersion = ""
		if err2 := rclient.Create(ctx, oldStatefulSet); err2 != nil {
			return fmt.Errorf("cannot restore previous sts: %s configuration after remove original error: %s: restore error %w", oldStatefulSet.Name, err, err2)
		}
		return fmt.Errorf("cannot create new sts: %s instead of replaced, perform manual action to handle this error or report BUG, err: %w", statefulSet.Name, err)
	}
	return nil
}
