package vmdistributed

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/podutil"
)

const defaultStubAddr = podutil.VMAuthDefaultStubAddr

func vmBackendTargetRef(backends []vmBackend, kind string, paths []string, owner *metav1.OwnerReference, excludeIds ...int) vmv1beta1.TargetRef {
	objects := make([]metav1.Object, 0, len(backends))
	for i := range backends {
		objects = append(objects, backends[i].obj)
	}
	return podutil.VMAuthReadTargetRef(objects, kind, paths, owner, excludeIds...)
}

func vmAgentTargetRef(vmAgents []*vmv1beta1.VMAgent, owner *metav1.OwnerReference, excludeIds ...int) vmv1beta1.TargetRef {
	objects := make([]metav1.Object, 0, len(vmAgents))
	for i := range vmAgents {
		objects = append(objects, vmAgents[i])
	}
	return podutil.VMAuthWriteTargetRef(objects, "VMAgent", []string{"/insert/.+", "/api/v1/write"}, owner, excludeIds...)
}

func buildVMAuthLB(cr *vmv1alpha1.VMDistributed, zs *zones, excludeIds ...int) *vmv1beta1.VMAuth {
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
	for i, mode := range zs.trafficModes {
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

	owner := cr.AsOwner()
	var kind string
	paths := []string{"/flags", "/metrics"}
	if cr.Spec.BackendType == vmv1alpha1.VMDistributedBackendTypeVMSingle {
		kind = "VMSingle"
		paths = append(paths, "/api/v1/.+")
	} else {
		kind = "VMCluster/vmselect"
		paths = append(paths, "/select/.+", "/admin/tenants")
	}
	var targetRefs []vmv1beta1.TargetRef
	targetRefs = append(targetRefs, vmAgentTargetRef(zs.vmagents, &owner, writeExcludeIds...))
	targetRefs = append(targetRefs, vmBackendTargetRef(zs.backends, kind, paths, &owner, readExcludeIds...))

	vmAuth.Spec.DefaultTargetRefs = targetRefs
	return &vmAuth
}
