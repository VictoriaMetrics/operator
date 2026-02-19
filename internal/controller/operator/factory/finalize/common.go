package finalize

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type crObject interface {
	GetLabels() map[string]string
	SelectorLabels() map[string]string
	PrefixedName() string
	GetServiceAccountName() string
	AsOwner() metav1.OwnerReference
	GetNamespace() string
}

func patchReplaceFinalizers(ctx context.Context, rclient client.Client, instance client.Object) error {
	op := []map[string]any{
		{
			"op":    "replace",
			"path":  "/metadata/finalizers",
			"value": instance.GetFinalizers(),
		}, {
			"op":    "replace",
			"path":  "/metadata/ownerReferences",
			"value": instance.GetOwnerReferences(),
		},
	}

	patchData, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("cannot marshal finalizers patch for=%q: %w", instance.GetName(), err)
	}
	if err := rclient.Patch(ctx, instance, client.RawPatch(types.JSONPatchType, patchData)); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("cannot patch finalizers for object=%q with name=%q: %w", instance.GetObjectKind().GroupVersionKind(), instance.GetName(), err)
	}
	return nil
}

// AddFinalizer adds finalizer to instance if needed.
func AddFinalizer(ctx context.Context, rclient client.Client, instance client.Object) error {
	if controllerutil.AddFinalizer(instance, vmv1beta1.FinalizerName) {
		return patchReplaceFinalizers(ctx, rclient, instance)
	}
	return nil
}

// RemoveFinalizer removes finalizer from instance if needed.
func RemoveFinalizer(ctx context.Context, rclient client.Client, instance client.Object) error {
	if controllerutil.RemoveFinalizer(instance, vmv1beta1.FinalizerName) {
		return patchReplaceFinalizers(ctx, rclient, instance)
	}
	return nil
}

func removeFinalizeObjByName(ctx context.Context, rclient client.Client, obj client.Object, name, ns string) error {
	return removeFinalizeObjByNameWithOwnerReference(ctx, rclient, obj, name, ns, true)
}

func removeFinalizeObjByNameWithOwnerReference(ctx context.Context, rclient client.Client, obj client.Object, name, ns string, keepOwnerReference bool) error {
	if err := rclient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, obj); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	needsPatching := controllerutil.RemoveFinalizer(obj, vmv1beta1.FinalizerName)
	if !keepOwnerReference {
		existOwnerReferences := obj.GetOwnerReferences()
		dstOwnerReferences := existOwnerReferences[:0]
		// filter in-place
		for _, s := range existOwnerReferences {
			if strings.HasPrefix(s.APIVersion, vmv1beta1.APIGroup) {
				needsPatching = true
				continue
			}
			dstOwnerReferences = append(dstOwnerReferences, s)
		}
		obj.SetOwnerReferences(dstOwnerReferences)
	}
	if needsPatching {
		return patchReplaceFinalizers(ctx, rclient, obj)
	}
	return nil
}

// SafeDelete removes object, ignores notfound error.
func SafeDelete(ctx context.Context, rclient client.Client, r client.Object) error {
	if err := rclient.Delete(ctx, r); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// SafeDeleteForSelectorsWithFinalizer removes given object if it matches provided label selectors
func SafeDeleteForSelectorsWithFinalizer(ctx context.Context, rclient client.Client, r client.Object, selectors map[string]string, owner *metav1.OwnerReference) error {
	objName, objNs := r.GetName(), r.GetNamespace()
	if objName == "" || objNs == "" {
		return fmt.Errorf("BUG: object name=%q or object namespace=%q cannot be empty", objName, objNs)
	}
	// reload object from API to properly remove finalizer
	if err := rclient.Get(ctx, types.NamespacedName{
		Namespace: objNs,
		Name:      objName,
	}, r); err != nil {
		// fast path
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !isLabelsMatchSelectors(r.GetLabels(), selectors) {
		// object has a different set of labels
		// most probably it is not managed by operator
		return nil
	}
	if !canBeRemoved(r, owner) {
		return nil
	}
	if err := RemoveFinalizer(ctx, rclient, r); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
		return nil
	}
	if err := rclient.Delete(ctx, r); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func isLabelsMatchSelectors(objLabels map[string]string, selectorLabels map[string]string) bool {
	for k, v := range selectorLabels {
		isFound := false
		objV, ok := objLabels[k]
		if ok {
			if objV != v {
				return false
			}
			isFound = true
		}
		if !isFound {
			return false
		}
	}
	return true
}

// SafeDeleteWithFinalizer removes object, ignores notfound error.
func SafeDeleteWithFinalizer(ctx context.Context, rclient client.Client, r client.Object, owner *metav1.OwnerReference) error {
	nsn := types.NamespacedName{
		Name:      r.GetName(),
		Namespace: r.GetNamespace(),
	}
	if len(nsn.Name) == 0 {
		return fmt.Errorf("BUG: object name cannot be empty")
	}
	// reload object from API to properly remove finalizer
	if err := rclient.Get(ctx, nsn, r); err != nil {
		// fast path
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !canBeRemoved(r, owner) {
		return nil
	}
	if err := RemoveFinalizer(ctx, rclient, r); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
	}
	if err := rclient.Delete(ctx, r); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func deleteSA(ctx context.Context, rclient client.Client, cr crObject) error {
	owner := cr.AsOwner()
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: cr.GetNamespace(), Name: cr.GetServiceAccountName()}}
	return SafeDeleteForSelectorsWithFinalizer(ctx, rclient, sa, cr.SelectorLabels(), &owner)
}

func finalizePDB(ctx context.Context, rclient client.Client, cr crObject) error {
	return removeFinalizeObjByName(ctx, rclient, &policyv1.PodDisruptionBudget{}, cr.PrefixedName(), cr.GetNamespace())
}

func removeConfigReloaderRole(ctx context.Context, rclient client.Client, cr crObject) error {
	if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.RoleBinding{}, cr.PrefixedName(), cr.GetNamespace()); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.Role{}, cr.PrefixedName(), cr.GetNamespace()); err != nil {
		return err
	}
	if err := SafeDelete(ctx, rclient, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.GetNamespace()}}); err != nil {
		return err
	}
	if err := SafeDelete(ctx, rclient, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.GetNamespace()}}); err != nil {
		return err
	}
	return nil
}
