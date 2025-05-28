package vlcluster

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

const (
	vmauthLBServiceProxyJobNameLabel = "operator.victoriametrics.com/vmauthlb-proxy-job-name"
	vmauthLBServiceProxyTargetLabel  = "operator.victoriametrics.com/vmauthlb-proxy-name"
	tlsServerConfigMountPath         = "/etc/vm/tls-server-secrets"
)

type optsBuilder struct {
	*vmv1.VLCluster
	prefixedName      string
	finalLabels       map[string]string
	selectorLabels    map[string]string
	additionalService *vmv1beta1.AdditionalServiceSpec
}

// PrefixedName implements build.svcBuilderArgs interface
func (csb *optsBuilder) PrefixedName() string {
	return csb.prefixedName
}

// AllLabels implements build.svcBuilderArgs interface
func (csb *optsBuilder) AllLabels() map[string]string {
	return csb.finalLabels
}

// AnnotationsFiltered implements build.svcBuilderArgs interface
func (csb *optsBuilder) AnnotationsFiltered() map[string]string {
	return csb.FinalAnnotations()
}

// SelectorLabels implements build.svcBuilderArgs interface
func (csb *optsBuilder) SelectorLabels() map[string]string {
	return csb.selectorLabels
}

// GetAdditionalService implements build.svcBuilderArgs interface
func (csb *optsBuilder) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return csb.additionalService
}

func newOptsBuilder(cr *vmv1.VLCluster, name string, selectorLabels map[string]string) *optsBuilder {
	return &optsBuilder{
		VLCluster:      cr,
		prefixedName:   name,
		finalLabels:    cr.FinalLabels(selectorLabels),
		selectorLabels: selectorLabels,
	}
}

func addTLSConfigToVolumes(dst []corev1.Volume, tlsC *vmv1.TLSServerConfig) []corev1.Volume {
	if tlsC == nil {
		return dst
	}
	addSecretVolume := func(sr *corev1.SecretKeySelector) {
		name := fmt.Sprintf("secret-tls-%s", sr.Name)
		for _, dst := range dst {
			if dst.Name == name {
				return
			}
		}
		dst = append(dst, corev1.Volume{
			Name: name,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: sr.Name,
				},
			},
		})
	}
	switch {
	case tlsC.CertFile != "":
	case tlsC.CertSecret != nil:
		addSecretVolume(tlsC.CertSecret)
	}

	switch {
	case tlsC.KeyFile != "":
	case tlsC.KeySecret != nil:
		addSecretVolume(tlsC.KeySecret)
	}
	return dst
}

func addTLSConfigToVolumeMounts(dst []corev1.VolumeMount, tlsC *vmv1.TLSServerConfig) []corev1.VolumeMount {
	if tlsC == nil {
		return dst
	}
	addSecretVolume := func(sr *corev1.SecretKeySelector) {
		name := fmt.Sprintf("secret-tls-%s", sr.Name)
		for _, dst := range dst {
			if dst.Name == name {
				return
			}
		}
		dst = append(dst, corev1.VolumeMount{
			Name:      name,
			MountPath: fmt.Sprintf("%s/%s", tlsServerConfigMountPath, sr.Name),
		})
	}
	switch {
	case tlsC.CertFile != "":
	case tlsC.CertSecret != nil:
		addSecretVolume(tlsC.CertSecret)
	}

	switch {
	case tlsC.KeyFile != "":
	case tlsC.KeySecret != nil:
		addSecretVolume(tlsC.KeySecret)
	}
	return dst
}
