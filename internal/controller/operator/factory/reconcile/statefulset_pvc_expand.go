package reconcile

import (
	"context"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// isSTSRecreateRequired checks whether the StatefulSet requires recreation and whether pods must be recreated.
// There are three different cases:
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
func isSTSRecreateRequired(ctx context.Context, existingObj, newObj *appsv1.StatefulSet, prevVCTs []corev1.PersistentVolumeClaim) (bool, bool) {
	l := logger.WithContext(ctx)
	if newObj.Spec.ServiceName != existingObj.Spec.ServiceName {
		return true, true
	}

	// if vct got added, removed or changed, recreate the sts
	if len(newObj.Spec.VolumeClaimTemplates) != len(existingObj.Spec.VolumeClaimTemplates) {
		l.Info("VolumeClaimTemplates count changes, recreating statefulset")
		return true, true
	}

	var vctChanged bool
	for _, newVCT := range newObj.Spec.VolumeClaimTemplates {
		existingVCT := getPVCByName(existingObj.Spec.VolumeClaimTemplates, newVCT.Name)
		if existingVCT == nil {
			l.Info(fmt.Sprintf("VolumeClaimTemplate=%s was not found at VolumeClaimTemplates, recreating statefulset", newVCT.Name))
			return true, true
		}
		prevVCT := getPVCByName(prevVCTs, newVCT.Name)
		if changedVCTFields(ctx, existingVCT, &newVCT, prevVCT) {
			l.Info(fmt.Sprintf("VolumeClaimTemplate=%s have some changes, recreating StatefulSet", newVCT.Name))
			vctChanged = true
		}
	}
	// some VolumeClaimTemplate's attributes beside name changed, there is no need to recreate pods
	if vctChanged {
		return true, false
	}
	if changedImmutableSTSFields(ctx, existingObj, newObj) {
		return true, false
	}

	return false, false
}

func getPVCByName(vcts []corev1.PersistentVolumeClaim, name string) *corev1.PersistentVolumeClaim {
	for _, claim := range vcts {
		if claim.Name == name {
			return &claim
		}
	}
	return nil
}

func updateSTSPVC(ctx context.Context, rclient client.Client, sts *appsv1.StatefulSet, prevVCTs []corev1.PersistentVolumeClaim) error {
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
	sort.Slice(pvcs.Items, func(i, j int) bool {
		return pvcs.Items[i].Name < pvcs.Items[j].Name
	})
	if len(pvcs.Items) == 0 {
		return fmt.Errorf("got 0 pvcs under %s for selector %v, statefulset could not be working", sts.Namespace, sts.Spec.Selector.MatchLabels)
	}
	nsn := types.NamespacedName{Name: sts.Name, Namespace: sts.Namespace}
	for _, pvc := range pvcs.Items {
		idx := strings.LastIndexByte(pvc.Name, '-')
		if idx <= 0 {
			return fmt.Errorf("not expected name for PVC=%q, it must have - as separator for StatefulSet=%q", pvc.Name, nsn.String())
		}
		// pvc created by sts always has name of CLAIM_NAME-STS_NAME-REPLICA_IDX
		stsClaimName := pvc.Name[:idx]
		stsClaim, ok := targetClaimsByName[stsClaimName]
		if !ok {
			l.Info(fmt.Sprintf("possible BUG, cannot find target PVC=%s in new statefulset by claim name=%s", pvc.Name, stsClaimName))
			continue
		}
		prevVCT := getPVCByName(prevVCTs, stsClaimName)
		// update PVC size and metadata if it's needed
		if err := updatePVC(ctx, rclient, &pvc, &stsClaim, prevVCT, nil); err != nil {
			return err
		}
	}
	for _, pvc := range pvcs.Items {
		nsnPvc := types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}
		size := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		if err := waitForPVCReady(ctx, rclient, nsnPvc, size); err != nil {
			return err
		}
	}
	return nil
}

func modifyPVC(ctx context.Context, rclient client.Client, existingObj, newObj, prevObj *corev1.PersistentVolumeClaim, owner *metav1.OwnerReference) (bool, error) {
	existingSize := existingObj.Spec.Resources.Requests.Storage()
	newSize := newObj.Spec.Resources.Requests.Storage()
	if existingSize == nil || newSize == nil {
		return false, nil
	}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	direction := newSize.Cmp(*existingSize)
	metaChanged, err := mergeMeta(existingObj, newObj, prevMeta, owner, true)
	if err != nil {
		return false, err
	}
	if !metaChanged && direction == 0 {
		return false, nil
	}
	if direction != 0 {
		// do not perform any checks if user set annotation explicitly.
		var expandable bool
		v, ok := existingObj.Annotations[vmv1beta1.PVCExpandableLabel]
		if ok {
			switch strings.ToLower(v) {
			case "false":
				return metaChanged, nil
			case "true":
				expandable = true
			default:
				return false, fmt.Errorf("unexpected value format for annotation=%q: %q, want true or false", vmv1beta1.PVCExpandableLabel, v)
			}
		}

		l := logger.WithContext(ctx)
		if direction < 0 {
			// probably, user updated pvc manually
			// without applying this changes to the configuration.
			l.Info(fmt.Sprintf("cannot decrease PVC=%s size from=%s to=%s, please check VolumeClaimTemplate configuration", newObj.Name, existingSize.String(), newSize.String()))
			return metaChanged, nil
		}

		l.Info(fmt.Sprintf("need to expand pvc=%s size from=%s to=%s", newObj.Name, existingSize, newSize))
		if !expandable {
			// check if storage class is expandable
			var err error
			expandable, err = isStorageClassExpandable(ctx, rclient, existingObj)
			if err != nil {
				return false, fmt.Errorf("failed to check storageClass expandability for PVC=%s: %v", newObj.Name, err)
			}
		}
		if !expandable {
			// don't return error to caller, since there is no point to requeue and reconcile this when sc is unexpandable
			sc := ptr.Deref(newObj.Spec.StorageClassName, "default")
			l.Info(fmt.Sprintf("storage class=%s for PVC=%s doesn't support live resizing", sc, newObj.Name))
			return metaChanged, nil
		}
		existingObj.Spec.Resources = *newObj.Spec.Resources.DeepCopy()
	}
	return true, nil
}

func updatePVC(ctx context.Context, rclient client.Client, existingObj, newObj, prevObj *corev1.PersistentVolumeClaim, owner *metav1.OwnerReference) error {
	modified, err := modifyPVC(ctx, rclient, existingObj, newObj, prevObj, owner)
	if err != nil {
		return err
	}
	if !modified {
		return nil
	}
	if err := rclient.Update(ctx, existingObj); err != nil {
		return fmt.Errorf("failed to expand size for pvc %s: %v", newObj.Name, err)
	}
	return nil
}

// isStorageClassExpandable check is it possible to update size of given pvc
func isStorageClassExpandable(ctx context.Context, rclient client.Client, pvc *corev1.PersistentVolumeClaim) (bool, error) {
	// fast path at single namespace mode, listing storage classes is disabled
	if !config.IsClusterWideAccessAllowed() {
		// don't return error to caller, since there is no point to requeue and reconcile this
		logger.WithContext(ctx).Info(fmt.Sprintf("cannot detect if storageClass expandable at single namespace mode"+
			`need to expand PVC manually or enforce resizing by adding annotation %s: "true" to PVC`,
			vmv1beta1.PVCExpandableLabel))
		return false, nil
	}
	var storageClasses storagev1.StorageClassList
	if err := rclient.List(ctx, &storageClasses); err != nil {
		return false, fmt.Errorf("cannot list storageClass: %w", err)
	}
	var className string
	if pvc.Spec.StorageClassName != nil {
		className = *pvc.Spec.StorageClassName
	}
	if name, ok := pvc.Annotations["volume.beta.kubernetes.io/storage-class"]; ok {
		className = name
	}
	for i := range storageClasses.Items {
		class := &storageClasses.Items[i]
		if len(className) > 0 {
			if class.Name == className {
				return ptr.Deref(class.AllowVolumeExpansion, false), nil
			}
		} else if annotation, ok := class.Annotations["storageclass.kubernetes.io/is-default-class"]; ok && annotation == "true" {
			return ptr.Deref(class.AllowVolumeExpansion, false), nil
		}
	}
	return false, nil
}

func changedVCTFields(ctx context.Context, existingObj, newObj, prevObj *corev1.PersistentVolumeClaim) bool {
	if existingObj == nil && newObj == nil {
		return false
	}
	if existingObj == nil || newObj == nil {
		return true
	}
	var prevLabels, prevAnnotations map[string]string
	if prevObj != nil {
		prevLabels = prevObj.Labels
		prevAnnotations = prevObj.Annotations
	}
	newAnnotations := mergeMaps(existingObj.Annotations, newObj.Annotations, prevAnnotations)
	newLabels := mergeMaps(existingObj.Labels, newObj.Labels, prevLabels)
	isEqual := equality.Semantic.DeepEqual(existingObj.Labels, newLabels) &&
		equality.Semantic.DeepEqual(existingObj.Annotations, newAnnotations)
	specDiff := diffDeepDerivative(newObj.Spec, existingObj.Spec, "spec")
	isEqual = isEqual && len(specDiff) == 0
	if isEqual {
		return false
	}
	nsn := types.NamespacedName{Name: existingObj.Name, Namespace: existingObj.Namespace}
	logger.WithContext(ctx).Info(fmt.Sprintf("changes detected for PVC=%s", nsn.String()), "spec_diff", specDiff)
	return true
}

// changedImmutableSTSFields checks if immutable newObj fields were changed
//
// logic was borrowed from
// https://github.com/kubernetes/kubernetes/blob/a866cbe2e5bbaa01cfd5e969aa3e033f3282a8a2/pkg/apis/apps/validation/validation.go#L166
func changedImmutableSTSFields(ctx context.Context, existingObj, newObj *appsv1.StatefulSet) bool {
	// statefulset updates aren't super common and general updates are likely to be touching spec, so we'll do this
	// deep copy right away.  This avoids mutating our inputs
	newObjClone := newObj.DeepCopy()

	// VolumeClaimTemplates must be checked before performing this check
	newObjClone.Spec.VolumeClaimTemplates = existingObj.Spec.VolumeClaimTemplates

	newObjClone.Spec.Replicas = existingObj.Spec.Replicas
	newObjClone.Spec.Template = existingObj.Spec.Template
	newObjClone.Spec.UpdateStrategy = existingObj.Spec.UpdateStrategy
	newObjClone.Spec.MinReadySeconds = existingObj.Spec.MinReadySeconds
	newObjClone.Spec.PersistentVolumeClaimRetentionPolicy = existingObj.Spec.PersistentVolumeClaimRetentionPolicy
	newObjClone.Spec.RevisionHistoryLimit = existingObj.Spec.RevisionHistoryLimit

	specDiff := diffDeep(newObjClone.Spec, existingObj.Spec, "spec")
	if len(specDiff) == 0 {
		return false
	}
	nsn := types.NamespacedName{Name: existingObj.Name, Namespace: existingObj.Namespace}
	logger.WithContext(ctx).Info(fmt.Sprintf("immutable StatefulSet=%s field changed, spec_diff=%v", nsn.String(), specDiff))
	return true
}
