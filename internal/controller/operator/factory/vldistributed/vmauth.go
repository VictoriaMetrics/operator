package vldistributed

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
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

func vlBackendTargetRef(backends []vlBackend, kind string, paths []string, owner *metav1.OwnerReference, excludeIds ...int) vmv1beta1.TargetRef {
	var nsns []vmv1beta1.NamespacedName
	// iterate in reverse so that vmauth would be more likely to route requests to older, stable backends
	for i := len(backends) - 1; i >= 0; i-- {
		if slices.Contains(excludeIds, i) {
			continue
		}
		b := backends[i].obj
		if b.GetCreationTimestamp().Time.IsZero() || !hasOwnerReference(b.GetOwnerReferences(), owner) {
			continue
		}
		nsns = append(nsns, vmv1beta1.NamespacedName{
			Name:      b.GetName(),
			Namespace: b.GetNamespace(),
		})
	}
	ref := vmv1beta1.TargetRef{
		Name: "read",
		URLMapCommon: vmv1beta1.URLMapCommon{
			LoadBalancingPolicy: ptr.To("first_available"),
			RetryStatusCodes:    []int{500, 502, 503},
		},
		Paths: paths,
	}
	if len(nsns) > 0 {
		ref.CRD = &vmv1beta1.CRDRef{
			Kind:    kind,
			Objects: nsns,
		}
	} else {
		ref.Static = &vmv1beta1.StaticRef{
			URL: defaultStubAddr,
		}
	}
	return ref
}

func vlAgentWriteTargetRef(vlAgents []*vmv1.VLAgent, owner *metav1.OwnerReference, excludeIds ...int) vmv1beta1.TargetRef {
	var nsns []vmv1beta1.NamespacedName
	for i := range vlAgents {
		if slices.Contains(excludeIds, i) {
			continue
		}
		vlAgent := vlAgents[i]
		if vlAgent.CreationTimestamp.IsZero() || !hasOwnerReference(vlAgent.OwnerReferences, owner) {
			continue
		}
		nsns = append(nsns, vmv1beta1.NamespacedName{
			Name:      vlAgent.Name,
			Namespace: vlAgent.Namespace,
		})
	}
	ref := vmv1beta1.TargetRef{
		Name: "write",
		URLMapCommon: vmv1beta1.URLMapCommon{
			LoadBalancingPolicy: ptr.To("least_loaded"),
			RetryStatusCodes:    []int{500, 502, 503},
		},
		Paths: []string{"/insert/.+"},
	}
	if len(nsns) > 0 {
		ref.CRD = &vmv1beta1.CRDRef{
			Kind:    "VLAgent",
			Objects: nsns,
		}
	} else {
		ref.Static = &vmv1beta1.StaticRef{
			URL: defaultStubAddr,
		}
	}
	return ref
}

func buildVMAuthLB(cr *vmv1alpha1.VLDistributed, zs *zones, excludeIds ...int) *vmv1beta1.VMAuth {
	if !isVMAuthEnabled(cr) || build.IsControllerDisabled("VMAuth") {
		return nil
	}
	vmAuth := vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.VLAuthName(),
			Namespace:       cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: *cr.Spec.VMAuth.Spec.DeepCopy(),
	}
	writeExcludeIds := slices.Clone(excludeIds)
	readExcludeIds := slices.Clone(excludeIds)
	for i, mode := range zs.trafficModes {
		switch mode {
		case vmv1alpha1.VLDistributedTrafficModeReadOnly:
			writeExcludeIds = append(writeExcludeIds, i)
		case vmv1alpha1.VLDistributedTrafficModeWriteOnly:
			readExcludeIds = append(readExcludeIds, i)
		case vmv1alpha1.VLDistributedTrafficModeMaintenance:
			writeExcludeIds = append(writeExcludeIds, i)
			readExcludeIds = append(readExcludeIds, i)
		}
	}

	owner := cr.AsOwner()
	var kind string
	paths := []string{"/flags", "/metrics"}
	if cr.Spec.BackendType == vmv1alpha1.VLDistributedBackendTypeVLSingle {
		kind = "VLSingle"
		paths = append(paths, "/select/.+")
	} else {
		kind = "VLCluster/vlselect"
		paths = append(paths, "/select/.+")
	}

	var targetRefs []vmv1beta1.TargetRef
	targetRefs = append(targetRefs, vlAgentWriteTargetRef(zs.vlagents, &owner, writeExcludeIds...))
	targetRefs = append(targetRefs, vlBackendTargetRef(zs.backends, kind, paths, &owner, readExcludeIds...))

	vmAuth.Spec.DefaultTargetRefs = targetRefs
	return &vmAuth
}
