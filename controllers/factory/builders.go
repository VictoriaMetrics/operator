package factory

import (
	"github.com/VictoriaMetrics/operator/internal/config"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func buildResources(crdResources v1.ResourceRequirements, defaultResources config.Resource, useDefault bool) v1.ResourceRequirements {
	if crdResources.Requests == nil {
		crdResources.Requests = v1.ResourceList{}
	}
	if crdResources.Limits == nil {
		crdResources.Limits = v1.ResourceList{}
	}

	var cpuResourceIsSet bool
	var memResourceIsSet bool

	if _, ok := crdResources.Limits[v1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := crdResources.Limits[v1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if _, ok := crdResources.Requests[v1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := crdResources.Requests[v1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}

	if !cpuResourceIsSet && useDefault {
		qr := resource.MustParse(defaultResources.Request.Cpu)
		if !qr.IsZero() {
			crdResources.Requests[v1.ResourceCPU] = qr
		}
		ql := resource.MustParse(defaultResources.Limit.Cpu)
		if !ql.IsZero() {
			crdResources.Limits[v1.ResourceCPU] = ql
		}
	}
	if !memResourceIsSet && useDefault {
		qr := resource.MustParse(defaultResources.Request.Mem)
		if !qr.IsZero() {
			crdResources.Requests[v1.ResourceMemory] = qr
		}
		ql := resource.MustParse(defaultResources.Limit.Mem)
		if !ql.IsZero() {
			crdResources.Limits[v1.ResourceMemory] = ql
		}
	}
	return crdResources
}
