package vldistributed

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/podutil"
)

const defaultStubAddr = podutil.VMAuthDefaultStubAddr

func vlBackendTargetRef(backends []vlBackend, kind string, paths []string, owner *metav1.OwnerReference, excludeIds ...int) vmv1beta1.TargetRef {
	objects := make([]metav1.Object, 0, len(backends))
	for i := range backends {
		objects = append(objects, backends[i].obj)
	}
	return podutil.VMAuthReadTargetRef(objects, kind, paths, owner, excludeIds...)
}

func vlAgentWriteTargetRef(vlAgents []*vmv1.VLAgent, owner *metav1.OwnerReference, excludeIds ...int) vmv1beta1.TargetRef {
	objects := make([]metav1.Object, 0, len(vlAgents))
	for i := range vlAgents {
		objects = append(objects, vlAgents[i])
	}
	return podutil.VMAuthWriteTargetRef(objects, "VLAgent", []string{"/insert/.+"}, owner, excludeIds...)
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
