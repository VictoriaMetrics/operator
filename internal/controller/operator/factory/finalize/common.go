package finalize

import (
	"context"
	"encoding/json"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type crdObject interface {
	AnnotationsFiltered() map[string]string
	GetLabels() map[string]string
	PrefixedName() string
	GetServiceAccountName() string
	IsOwnsServiceAccount() bool
	GetNSName() string
}

func patchReplaceFinalizers(ctx context.Context, rclient client.Client, instance client.Object) error {
	op := []map[string]interface{}{{
		"op":    "replace",
		"path":  "/metadata/finalizers",
		"value": instance.GetFinalizers(),
	}}

	patchData, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("cannot marshal finalizers patch for=%q: %w", instance.GetName(), err)
	}
	if err := rclient.Patch(ctx, instance, client.RawPatch(types.JSONPatchType, patchData)); err != nil {
		return fmt.Errorf("cannot patch finalizers for object=%q with name=%q: %w", instance.GetObjectKind().GroupVersionKind(), instance.GetName(), err)
	}
	return nil
}

// AddFinalizer adds finalizer to instance if needed.
func AddFinalizer(ctx context.Context, rclient client.Client, instance client.Object) error {
	return vmv1beta1.AddFinalizerAndThen(instance, func(o client.Object) error {
		return patchReplaceFinalizers(ctx, rclient, o)
	})
}

// RemoveFinalizer removes finalizer from instance if needed.
func RemoveFinalizer(ctx context.Context, rclient client.Client, instance client.Object) error {
	return vmv1beta1.RemoveFinalizer(instance, func(o client.Object) error {
		return patchReplaceFinalizers(ctx, rclient, o)
	})
}

func removeFinalizeObjByName(ctx context.Context, rclient client.Client, obj client.Object, name, ns string) error {
	if err := rclient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, obj); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return vmv1beta1.RemoveFinalizer(obj, func(o client.Object) error {
		return patchReplaceFinalizers(ctx, rclient, o)
	})
}

// SafeDelete removes object, ignores notfound error.
func SafeDelete(ctx context.Context, rclient client.Client, r client.Object) error {
	if err := rclient.Delete(ctx, r); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func deleteSA(ctx context.Context, rclient client.Client, crd crdObject) error {
	if !crd.IsOwnsServiceAccount() {
		return nil
	}
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.ServiceAccount{}, crd.GetServiceAccountName(), crd.GetNSName()); err != nil {
		return err
	}
	return SafeDelete(ctx, rclient, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: crd.GetNSName(), Name: crd.GetServiceAccountName()}})
}

func finalizePBD(ctx context.Context, rclient client.Client, crd crdObject) error {
	return removeFinalizeObjByName(ctx, rclient, &policyv1.PodDisruptionBudget{}, crd.PrefixedName(), crd.GetNSName())
}

func finalizePBDWithName(ctx context.Context, rclient client.Client, name, ns string) error {
	return removeFinalizeObjByName(ctx, rclient, &policyv1.PodDisruptionBudget{}, name, ns)
}

func removeConfigReloaderRole(ctx context.Context, rclient client.Client, crd crdObject) error {
	if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.RoleBinding{}, crd.PrefixedName(), crd.GetNSName()); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &rbacv1.Role{}, crd.PrefixedName(), crd.GetNSName()); err != nil {
		return err
	}
	if err := SafeDelete(ctx, rclient, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: crd.PrefixedName(), Namespace: crd.GetNSName()}}); err != nil {
		return err
	}

	if err := SafeDelete(ctx, rclient, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: crd.PrefixedName(), Namespace: crd.GetNSName()}}); err != nil {
		return err
	}
	return nil
}

// FreeIfNeeded checks if resource must be freed from finalizer and garbage collected by kubernetes
func FreeIfNeeded(ctx context.Context, rclient client.Client, object client.Object) error {
	if object.GetDeletionTimestamp().IsZero() {
		// fast path
		return nil
	}
	if err := RemoveFinalizer(ctx, rclient, object); err != nil {
		return fmt.Errorf("cannot remove finalizer from object=%s/%s, kind=%q: %w", object.GetNamespace(), object.GetName(), object.GetObjectKind().GroupVersionKind(), err)
	}
	return fmt.Errorf("deletionTimestamp is not zero=%q for object=%s/%s kind=%s, recreating it at next reconcile loop. Warning never delete object manually", object.GetDeletionTimestamp(), object.GetNamespace(), object.GetName(), object.GetObjectKind().GroupVersionKind())
}
