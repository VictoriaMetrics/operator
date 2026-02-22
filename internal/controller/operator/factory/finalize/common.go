package finalize

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type crObject interface {
	PrefixedName() string
	GetServiceAccountName() string
	AsOwner() metav1.OwnerReference
	GetNamespace() string
	SelectorLabels() map[string]string
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

func removeFinalizers(ctx context.Context, rclient client.Client, objs []client.Object, clusterWide bool) error {
	if !clusterWide {
		for _, o := range objs {
			if err := removeFinalizerWithOwnerReference(ctx, rclient, o, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func removeFinalizerWithOwnerReference(ctx context.Context, rclient client.Client, obj client.Object, keepOwnerReference bool) error {
	if err := rclient.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, obj); err != nil {
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

// SafeDeleteWithFinalizer removes object, ignores notfound error.
func SafeDeleteWithFinalizer(ctx context.Context, rclient client.Client, objs []client.Object, selector map[string]string, owner *metav1.OwnerReference) error {
	delete := func(r client.Object) error {
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
		if !canBeRemoved(r, selector, owner) {
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
	for _, r := range objs {
		if err := delete(r); err != nil {
			return err
		}
	}
	return nil
}
