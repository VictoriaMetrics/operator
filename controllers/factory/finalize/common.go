package finalize

import (
	"context"
	"encoding/json"
	"fmt"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	v12 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	v1 "k8s.io/api/rbac/v1"
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
	if !victoriametricsv1beta1.IsContainsFinalizer(instance.GetFinalizers(), victoriametricsv1beta1.FinalizerName) {
		instance.SetFinalizers(append(instance.GetFinalizers(), victoriametricsv1beta1.FinalizerName))
		return patchReplaceFinalizers(ctx, rclient, instance)
	}
	return nil
}

// RemoveFinalizer removes finalizer from instance if needed.
func RemoveFinalizer(ctx context.Context, rclient client.Client, instance client.Object) error {
	if victoriametricsv1beta1.IsContainsFinalizer(instance.GetFinalizers(), victoriametricsv1beta1.FinalizerName) {
		instance.SetFinalizers(victoriametricsv1beta1.RemoveFinalizer(instance.GetFinalizers(), victoriametricsv1beta1.FinalizerName))
		return patchReplaceFinalizers(ctx, rclient, instance)
	}
	return nil
}

func removeFinalizeObjByName(ctx context.Context, rclient client.Client, obj client.Object, name, ns string) error {
	if err := rclient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, obj); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	// fast path
	if !victoriametricsv1beta1.IsContainsFinalizer(obj.GetFinalizers(), victoriametricsv1beta1.FinalizerName) {
		return nil
	}
	obj.SetFinalizers(victoriametricsv1beta1.RemoveFinalizer(obj.GetFinalizers(), victoriametricsv1beta1.FinalizerName))
	return patchReplaceFinalizers(ctx, rclient, obj)
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
	if err := removeFinalizeObjByName(ctx, rclient, &v12.ServiceAccount{}, crd.GetServiceAccountName(), crd.GetNSName()); err != nil {
		return err
	}
	return SafeDelete(ctx, rclient, &v12.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: crd.GetNSName(), Name: crd.GetServiceAccountName()}})
}

func finalizePBD(ctx context.Context, rclient client.Client, crd crdObject) error {
	return removeFinalizeObjByName(ctx, rclient, &policyv1.PodDisruptionBudget{}, crd.PrefixedName(), crd.GetNSName())
}

func finalizePBDWithName(ctx context.Context, rclient client.Client, ns, name string) error {
	return removeFinalizeObjByName(ctx, rclient, &policyv1.PodDisruptionBudget{}, name, ns)
}

func removeConfigReloaderRole(ctx context.Context, rclient client.Client, crd crdObject) error {
	if err := removeFinalizeObjByName(ctx, rclient, &v1.RoleBinding{}, crd.PrefixedName(), crd.GetNSName()); err != nil {
		return err
	}
	if err := removeFinalizeObjByName(ctx, rclient, &v1.Role{}, crd.PrefixedName(), crd.GetNSName()); err != nil {
		return err
	}
	if err := SafeDelete(ctx, rclient, &v1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: crd.PrefixedName(), Namespace: crd.GetNSName()}}); err != nil {
		return err
	}

	if err := SafeDelete(ctx, rclient, &v1.Role{ObjectMeta: metav1.ObjectMeta{Name: crd.PrefixedName(), Namespace: crd.GetNSName()}}); err != nil {
		return err
	}
	return nil
}
