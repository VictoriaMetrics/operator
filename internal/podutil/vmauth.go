package podutil

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

const VMAuthDefaultStubAddr = "http://127.0.0.1:9999"

// HasOwnerReference reports whether owners contains the given owner reference.
func HasOwnerReference(owners []metav1.OwnerReference, owner *metav1.OwnerReference) bool {
	for i := range owners {
		o := &owners[i]
		if o.APIVersion == owner.APIVersion && o.Kind == owner.Kind && o.Name == owner.Name {
			return true
		}
	}
	return false
}

// VMAuthReadTargetRef builds VMAuth read targetRef for backend CRs.
func VMAuthReadTargetRef(backends []metav1.Object, kind string, paths []string, owner *metav1.OwnerReference, excludeIds ...int) vmv1beta1.TargetRef {
	return vmAuthTargetRef("read", "first_available", kind, paths, backends, owner, true, excludeIds...)
}

// VMAuthWriteTargetRef builds VMAuth write targetRef for agent CRs.
func VMAuthWriteTargetRef(agents []metav1.Object, kind string, paths []string, owner *metav1.OwnerReference, excludeIds ...int) vmv1beta1.TargetRef {
	return vmAuthTargetRef("write", "least_loaded", kind, paths, agents, owner, false, excludeIds...)
}

func vmAuthTargetRef(name, policy, kind string, paths []string, objects []metav1.Object, owner *metav1.OwnerReference, reverse bool, excludeIds ...int) vmv1beta1.TargetRef {
	var nsns []vmv1beta1.NamespacedName
	for i := range objects {
		idx := i
		if reverse {
			idx = len(objects) - 1 - i
		}
		if slices.Contains(excludeIds, idx) {
			continue
		}
		obj := objects[idx]
		if obj.GetCreationTimestamp().Time.IsZero() || !HasOwnerReference(obj.GetOwnerReferences(), owner) {
			continue
		}
		nsns = append(nsns, vmv1beta1.NamespacedName{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
		})
	}

	ref := vmv1beta1.TargetRef{
		Name: name,
		URLMapCommon: vmv1beta1.URLMapCommon{
			LoadBalancingPolicy: ptr.To(policy),
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
		ref.Static = &vmv1beta1.StaticRef{URL: VMAuthDefaultStubAddr}
	}
	return ref
}
