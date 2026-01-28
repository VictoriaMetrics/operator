package vmdistributed

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

func appendVMClusterTargetRefs(targetRefs []vmv1beta1.TargetRef, vmClusters []*vmv1beta1.VMCluster) []vmv1beta1.TargetRef {
	for _, cluster := range vmClusters {
		targetRefs = append(targetRefs, vmv1beta1.TargetRef{
			URLMapCommon: vmv1beta1.URLMapCommon{
				LoadBalancingPolicy: ptr.To("first_available"),
				RetryStatusCodes:    []int{500, 502, 503},
			},
			Paths: []string{"/select/.+", "/admin/tenants"},
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster/vmselect",
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			},
		})
	}
	return targetRefs
}

func appendVMAgentTargetRef(targetRefs []vmv1beta1.TargetRef, vmAgent *vmv1beta1.VMAgent) []vmv1beta1.TargetRef {
	return append(targetRefs, vmv1beta1.TargetRef{
		URLMapCommon: vmv1beta1.URLMapCommon{
			LoadBalancingPolicy: ptr.To("first_available"),
			RetryStatusCodes:    []int{500, 502, 503},
		},
		Paths: []string{"/insert/.+"},
		CRD: &vmv1beta1.CRDRef{
			Kind:      "VMAgent",
			Name:      vmAgent.Name,
			Namespace: vmAgent.Namespace,
		},
	})
}

func buildVMAuthLB(cr *vmv1alpha1.VMDistributed, vmAgent *vmv1beta1.VMAgent, vmClusters []*vmv1beta1.VMCluster) *vmv1beta1.VMAuth {
	vmAuth := vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.VMAuthName(),
			Namespace:       cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: *cr.Spec.VMAuth.Spec.DeepCopy(),
	}
	if vmAuth.Spec.UnauthorizedUserAccessSpec == nil {
		vmAuth.Spec.UnauthorizedUserAccessSpec = &vmv1beta1.VMAuthUnauthorizedUserAccessSpec{}
	}
	var targetRefs []vmv1beta1.TargetRef
	targetRefs = appendVMClusterTargetRefs(targetRefs, vmClusters)
	targetRefs = appendVMAgentTargetRef(targetRefs, vmAgent)
	slices.SortFunc(targetRefs, func(a, b vmv1beta1.TargetRef) int {
		return cmp.Or(
			cmp.Compare(a.CRD.Kind, b.CRD.Kind),
			cmp.Compare(a.CRD.Name, b.CRD.Name),
		)
	})
	vmAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs = targetRefs
	return &vmAuth
}

func reconcileVMAuthLB(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, vmAgent *vmv1beta1.VMAgent, vmClusters []*vmv1beta1.VMCluster) error {
	vmAuth := buildVMAuthLB(cr, vmAgent, vmClusters)
	if err := createOrUpdateVMAuthLB(ctx, rclient, cr, vmAuth); err != nil {
		return err
	}
	if err := reconcile.WaitForStatus(ctx, rclient, vmAuth.DeepCopy(), defaultStatusCheckInterval, vmv1beta1.UpdateStatusOperational); err != nil {
		return fmt.Errorf("failed to wait for VMAuth ready: %w", err)
	}
	return nil
}

func createOrUpdateVMAuthLB(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, vmAuth *vmv1beta1.VMAuth) error {
	var prevVMAuth vmv1beta1.VMAuth
	nsn := types.NamespacedName{Name: vmAuth.Name, Namespace: vmAuth.Namespace}
	if err := rclient.Get(ctx, nsn, &prevVMAuth); err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get VMAuth: %w", err)
		}
		logger.WithContext(ctx).Info("creating VMAuth", "name", nsn)
		return rclient.Create(ctx, vmAuth)
	}
	ownerRefChanged, err := setOwnerRefIfNeeded(cr, &prevVMAuth, rclient.Scheme())
	if err != nil {
		return err
	}
	if !ownerRefChanged && equality.Semantic.DeepEqual(prevVMAuth.Spec, vmAuth.Spec) {
		return nil
	}
	prevVMAuth.Spec = vmAuth.Spec
	return rclient.Update(ctx, &prevVMAuth)
}
