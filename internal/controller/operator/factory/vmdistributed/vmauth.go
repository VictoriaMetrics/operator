package vmdistributed

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

const defaultStubAddr = "http://127.0.0.1:9999"

func hasOwnerReference(owners []metav1.OwnerReference, owner *metav1.OwnerReference) bool {
	for i := range owners {
		o := &owners[i]
		if o.APIVersion == owner.APIVersion && o.Kind == owner.Kind && o.Name == owner.Name {
			return true
		}
	}
	return false
}

func vmClusterTargetRef(vmClusters []*vmv1beta1.VMCluster, owner *metav1.OwnerReference, excludeIds ...int) vmv1beta1.TargetRef {
	var nsns []vmv1beta1.NamespacedName
	// iterate in reverse so that vmauth would be more likely to route requests to older, stable vmClusters
	for i := len(vmClusters) - 1; i >= 0; i-- {
		if slices.Contains(excludeIds, i) {
			continue
		}
		vmCluster := vmClusters[i]
		if vmCluster.CreationTimestamp.IsZero() || !hasOwnerReference(vmCluster.OwnerReferences, owner) {
			continue
		}
		nsns = append(nsns, vmv1beta1.NamespacedName{
			Name:      vmCluster.Name,
			Namespace: vmCluster.Namespace,
		})
	}
	ref := vmv1beta1.TargetRef{
		Name: "read",
		URLMapCommon: vmv1beta1.URLMapCommon{
			LoadBalancingPolicy: ptr.To("first_available"),
			RetryStatusCodes:    []int{500, 502, 503},
		},
		Paths: []string{"/select/.+", "/admin/tenants"},
	}
	if len(nsns) > 0 {
		ref.CRD = &vmv1beta1.CRDRef{
			Kind:    "VMCluster/vmselect",
			Objects: nsns,
		}
	} else {
		ref.Static = &vmv1beta1.StaticRef{
			URL: defaultStubAddr,
		}
	}
	return ref
}

func vmAgentTargetRef(vmAgents []*vmv1beta1.VMAgent, owner *metav1.OwnerReference, excludeIds ...int) vmv1beta1.TargetRef {
	var nsns []vmv1beta1.NamespacedName
	for i := range vmAgents {
		if slices.Contains(excludeIds, i) {
			continue
		}
		vmAgent := vmAgents[i]
		if vmAgent.CreationTimestamp.IsZero() || !hasOwnerReference(vmAgent.OwnerReferences, owner) {
			continue
		}
		nsns = append(nsns, vmv1beta1.NamespacedName{
			Name:      vmAgent.Name,
			Namespace: vmAgent.Namespace,
		})
	}
	ref := vmv1beta1.TargetRef{
		Name: "write",
		URLMapCommon: vmv1beta1.URLMapCommon{
			LoadBalancingPolicy: ptr.To("least_loaded"),
			RetryStatusCodes:    []int{500, 502, 503},
		},
		Paths: []string{"/insert/.+", "/api/v1/write"},
	}
	if len(nsns) > 0 {
		ref.CRD = &vmv1beta1.CRDRef{
			Kind:    "VMAgent",
			Objects: nsns,
		}
	} else {
		ref.Static = &vmv1beta1.StaticRef{
			URL: defaultStubAddr,
		}
	}
	return ref
}

func buildVMAuthLB(cr *vmv1alpha1.VMDistributed, vmAgents []*vmv1beta1.VMAgent, vmClusters []*vmv1beta1.VMCluster, trafficModes []vmv1alpha1.VMDistributedTrafficMode, excludeIds ...int) *vmv1beta1.VMAuth {
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
	writeExcludeIds := slices.Clone(excludeIds)
	readExcludeIds := slices.Clone(excludeIds)
	for i, mode := range trafficModes {
		switch mode {
		case vmv1alpha1.VMDistributedTrafficModeReadOnly:
			writeExcludeIds = append(writeExcludeIds, i)
		case vmv1alpha1.VMDistributedTrafficModeWriteOnly:
			readExcludeIds = append(readExcludeIds, i)
		case vmv1alpha1.VMDistributedTrafficModeMaintenance:
			writeExcludeIds = append(writeExcludeIds, i)
			readExcludeIds = append(readExcludeIds, i)
		}
	}
	var targetRefs []vmv1beta1.TargetRef
	owner := cr.AsOwner()
	targetRefs = append(targetRefs, vmAgentTargetRef(vmAgents, &owner, writeExcludeIds...))
	targetRefs = append(targetRefs, vmClusterTargetRef(vmClusters, &owner, readExcludeIds...))
	vmAuth.Spec.DefaultTargetRefs = targetRefs
	return &vmAuth
}
