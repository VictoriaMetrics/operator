package mcp

import (
	"context"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
)

// CreateOrUpdateVMMCP performs an update for VMMCP resource
func CreateOrUpdateVMMCP(ctx context.Context, rclient client.Client, cr *vmv1.VMMCP) error {
	if cr.Paused() {
		return nil
	}
	var prevS *server
	if cr.Status.LastAppliedSpec != nil {
		prevCR := cr.DeepCopy()
		prevCR.Spec = *cr.Status.LastAppliedSpec
		prevS = newVMMCPServer(prevCR)
	}
	return createOrUpdate(ctx, rclient, newVMMCPServer(cr), prevS)
}

func newVMMCPServer(cr *vmv1.VMMCP) *server {
	spec := &cr.Spec
	var envs []corev1.EnvVar
	if spec.CloudAPIKey != nil {
		envs = append(envs, corev1.EnvVar{
			Name: "VMC_API_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: spec.CloudAPIKey,
			},
		})
	} else {
		instanceType := spec.InstanceType
		if instanceType == "" {
			instanceType = vmv1.VMMCPInstanceTypeSingle
		}
		envs = append(envs, corev1.EnvVar{Name: "VM_INSTANCE_TYPE", Value: string(instanceType)})
	}
	envs = append(envs, instanceEnvs("VM", spec.Entrypoint, spec.BearerToken, spec.Headers, spec.DefaultTenantID)...)
	envs = append(envs, passthroughHeadersEnvs(spec.PassthroughHeaders)...)
	if spec.DisableResources {
		envs = append(envs, corev1.EnvVar{Name: "MCP_DISABLE_RESOURCES", Value: strconv.FormatBool(spec.DisableResources)})
	}
	return &server{
		cr:   cr,
		spec: &spec.CommonMCPSpec,
		kind: "vmmcp",
		envs: envs,
	}
}
