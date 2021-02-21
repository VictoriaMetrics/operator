package psp

import (
	"context"

	"k8s.io/api/policy/v1beta1"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeletePSPChain - removes psp, cluster role and cluster role binding,
// on finalize request for given CRD
func DeletePSPChain(ctx context.Context, rclient client.Client, crd CRDObject) error {
	if err := ensurePSPRemoved(ctx, rclient, crd); err != nil {
		return err
	}
	if err := ensureCRBRemoved(ctx, rclient, crd); err != nil {
		return err
	}
	return ensureCRRemoved(ctx, rclient, crd)
}

func ensurePSPRemoved(ctx context.Context, rclient client.Client, crd CRDObject) error {
	return safeDelete(ctx, rclient, &v1beta1.PodSecurityPolicy{ObjectMeta: metav1.ObjectMeta{
		Name: crd.GetPSPName()}})
}

func ensureCRRemoved(ctx context.Context, rclient client.Client, crd CRDObject) error {
	return safeDelete(ctx, rclient, &v1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: crd.PrefixedName()}})
}

func ensureCRBRemoved(ctx context.Context, rclient client.Client, crd CRDObject) error {
	return safeDelete(ctx, rclient, &v1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: crd.PrefixedName()}})
}

func safeDelete(ctx context.Context, rclient client.Client, r client.Object) error {
	if err := rclient.Delete(ctx, r); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
