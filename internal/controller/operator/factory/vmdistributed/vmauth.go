package vmdistributed

import (
	"cmp"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func vmClusterTargetRef(vmClusters []*vmv1beta1.VMCluster, excludeIds ...int) vmv1beta1.TargetRef {
	var crds []vmv1beta1.CRDRef
	for i := range vmClusters {
		if slices.Contains(excludeIds, i) {
			continue
		}
		vmCluster := vmClusters[i]
		if vmCluster.CreationTimestamp.IsZero() {
			continue
		}
		crds = append(crds, vmv1beta1.CRDRef{
			Kind:      "VMCluster/vmselect",
			Name:      vmCluster.Name,
			Namespace: vmCluster.Namespace,
		})
	}
	return vmv1beta1.TargetRef{
		URLMapCommon: vmv1beta1.URLMapCommon{
			LoadBalancingPolicy: ptr.To("first_available"),
			RetryStatusCodes:    []int{500, 502, 503},
		},
		Paths: []string{"/select/.+", "/admin/tenants"},
		CRDs:  crds,
	}
}

func vmAgentTargetRef(vmAgents []*vmv1beta1.VMAgent, excludeIds ...int) vmv1beta1.TargetRef {
	var crds []vmv1beta1.CRDRef
	for i := range vmAgents {
		if slices.Contains(excludeIds, i) {
			continue
		}
		vmAgent := vmAgents[i]
		if vmAgent.CreationTimestamp.IsZero() {
			continue
		}
		crds = append(crds, vmv1beta1.CRDRef{
			Kind:      "VMAgent",
			Name:      vmAgent.Name,
			Namespace: vmAgent.Namespace,
		})
	}
	return vmv1beta1.TargetRef{
		URLMapCommon: vmv1beta1.URLMapCommon{
			LoadBalancingPolicy: ptr.To("first_available"),
			RetryStatusCodes:    []int{500, 502, 503},
		},
		Paths: []string{"/insert/.+", "/api/v1/write"},
		CRDs:  crds,
	}
}

func buildVMAuthLB(cr *vmv1alpha1.VMDistributed, vmAgents []*vmv1beta1.VMAgent, vmClusters []*vmv1beta1.VMCluster, excludeIds ...int) *vmv1beta1.VMAuth {
	if !ptr.Deref(cr.Spec.VMAuth.Enabled, true) || build.IsControllerDisabled("VMAuth") {
		return nil
	}
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
	targetRefs = append(targetRefs, vmAgentTargetRef(vmAgents, excludeIds...))
	targetRefs = append(targetRefs, vmClusterTargetRef(vmClusters, excludeIds...))
	for i := range targetRefs {
		targetRef := &targetRefs[i]
		slices.SortFunc(targetRef.CRDs, func(a, b vmv1beta1.CRDRef) int {
			return cmp.Compare(a.Name, b.Name)
		})
	}
	vmAuth.Spec.UnauthorizedUserAccessSpec.URLMap = nil
	vmAuth.Spec.UnauthorizedUserAccessSpec.URLPrefix = nil
	vmAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs = targetRefs
	return &vmAuth
}
