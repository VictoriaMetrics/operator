package build

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type parentOpts interface {
	client.Object
	PrefixedName(vmv1beta1.ClusterComponent) string
	SelectorLabels(vmv1beta1.ClusterComponent) map[string]string
	GetServiceAccountName() string
	GetAdditionalService(vmv1beta1.ClusterComponent) *vmv1beta1.AdditionalServiceSpec
	IsOwnsServiceAccount() bool
	FinalAnnotations() map[string]string
	FinalLabels(vmv1beta1.ClusterComponent) map[string]string
	AsOwner() metav1.OwnerReference
}

type ChildBuilder struct {
	parentOpts
	kind           vmv1beta1.ClusterComponent
	finalLabels    map[string]string
	selectorLabels map[string]string
}

// PrefixedName implements build.svcBuilderArgs interface
func (b *ChildBuilder) PrefixedName() string {
	return b.parentOpts.PrefixedName(b.kind)
}

// AllLabels implements build.svcBuilderArgs interface
func (b *ChildBuilder) AllLabels() map[string]string {
	return b.finalLabels
}

// AnnotationsFiltered implements build.svcBuilderArgs interface
func (b *ChildBuilder) AnnotationsFiltered() map[string]string {
	return b.FinalAnnotations()
}

// SelectorLabels implements build.svcBuilderArgs interface
func (b *ChildBuilder) SelectorLabels() map[string]string {
	return b.selectorLabels
}

// GetAdditionalService implements build.svcBuilderArgs interface
func (b *ChildBuilder) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return b.parentOpts.GetAdditionalService(b.kind)
}

func (b *ChildBuilder) SetFinalLabels(ls map[string]string) {
	b.finalLabels = ls
}

func (b *ChildBuilder) SetSelectorLabels(ls map[string]string) {
	b.selectorLabels = ls
}

func NewChildBuilder(cr parentOpts, kind vmv1beta1.ClusterComponent) *ChildBuilder {
	return &ChildBuilder{
		parentOpts:     cr,
		kind:           kind,
		finalLabels:    cr.FinalLabels(kind),
		selectorLabels: cr.SelectorLabels(kind),
	}
}
