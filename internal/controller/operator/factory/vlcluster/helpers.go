package vlcluster

import (
	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

const (
	vmauthLBServiceProxyJobNameLabel = "operator.victoriametrics.com/vmauthlb-proxy-job-name"
	vmauthLBServiceProxyTargetLabel  = "operator.victoriametrics.com/vmauthlb-proxy-name"
	tlsAssetsDir                     = "/etc/vm/tls-server-secrets"
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
