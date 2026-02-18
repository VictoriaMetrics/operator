package vmdistributed

import (
	"cmp"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func appendVMClusterTargetRefs(targetRefs []vmv1beta1.TargetRef, vmClusters []*vmv1beta1.VMCluster, excludeIds ...int) []vmv1beta1.TargetRef {
	for i := range vmClusters {
		if slices.Contains(excludeIds, i) {
			continue
		}
		vmCluster := vmClusters[i]
		if vmCluster.CreationTimestamp.IsZero() {
			continue
		}
		targetRefs = append(targetRefs, vmv1beta1.TargetRef{
			URLMapCommon: vmv1beta1.URLMapCommon{
				LoadBalancingPolicy: ptr.To("first_available"),
				RetryStatusCodes:    []int{500, 502, 503},
			},
			Paths: []string{"/select/.+", "/admin/tenants"},
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster/vmselect",
				Name:      vmCluster.Name,
				Namespace: vmCluster.Namespace,
			},
		})
	}
	return targetRefs
}

func appendVMAgentTargetRefs(targetRefs []vmv1beta1.TargetRef, vmAgents []*vmv1beta1.VMAgent, excludeIds ...int) []vmv1beta1.TargetRef {
	for i := range vmAgents {
		if slices.Contains(excludeIds, i) {
			continue
		}
		vmAgent := vmAgents[i]
		if vmAgent.CreationTimestamp.IsZero() {
			continue
		}
		targetRefs = append(targetRefs, vmv1beta1.TargetRef{
			URLMapCommon: vmv1beta1.URLMapCommon{
				LoadBalancingPolicy: ptr.To("first_available"),
				RetryStatusCodes:    []int{500, 502, 503},
			},
			Paths: []string{"/insert/.+", "/api/v1/write"},
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMAgent",
				Name:      vmAgent.Name,
				Namespace: vmAgent.Namespace,
			},
		})
	}
	return targetRefs
}

func buildVMAuthLB(cr *vmv1alpha1.VMDistributed, vmAgents []*vmv1beta1.VMAgent, vmClusters []*vmv1beta1.VMCluster, excludeIds ...int) *vmv1beta1.VMAuth {
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
	targetRefs = appendVMClusterTargetRefs(targetRefs, vmClusters, excludeIds...)
	targetRefs = appendVMAgentTargetRefs(targetRefs, vmAgents, excludeIds...)
	slices.SortFunc(targetRefs, func(a, b vmv1beta1.TargetRef) int {
		return cmp.Or(
			cmp.Compare(a.CRD.Kind, b.CRD.Kind),
			cmp.Compare(a.CRD.Name, b.CRD.Name),
		)
	})
	vmAuth.Spec.UnauthorizedUserAccessSpec.URLMap = nil
	vmAuth.Spec.UnauthorizedUserAccessSpec.URLPrefix = nil
	vmAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs = targetRefs
	return &vmAuth
}
