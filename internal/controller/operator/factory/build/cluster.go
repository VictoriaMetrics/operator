package build

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type ParentOpts interface {
	client.Object
	PrefixedInternalName(vmv1beta1.ClusterComponent) string
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
	ParentOpts
	kind           vmv1beta1.ClusterComponent
	finalLabels    map[string]string
	selectorLabels map[string]string
	prefixedName   string
}

// PrefixedName implements build.svcBuilderArgs interface
func (b *ChildBuilder) PrefixedName() string {
	return b.prefixedName
}

// FinalLabels implements build.svcBuilderArgs interface
func (b *ChildBuilder) FinalLabels() map[string]string {
	return b.finalLabels
}

// SelectorLabels implements build.svcBuilderArgs interface
func (b *ChildBuilder) SelectorLabels() map[string]string {
	return b.selectorLabels
}

// GetAdditionalService implements build.svcBuilderArgs interface
func (b *ChildBuilder) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return b.ParentOpts.GetAdditionalService(b.kind)
}

func (b *ChildBuilder) SetFinalLabels(ls map[string]string) {
	b.finalLabels = ls
}

func (b *ChildBuilder) SetSelectorLabels(ls map[string]string) {
	b.selectorLabels = ls
}

// poolLabelKey is the label added to all pool-scoped resources.
const poolLabelKey = "app.kubernetes.io/pool"

func NewChildBuilder(cr ParentOpts, kind vmv1beta1.ClusterComponent) *ChildBuilder {
	return NewPoolBuilder(cr, kind, "")
}

// NewPoolBuilder is like NewChildBuilder but scopes the builder to a named pool.
// PrefixedName appends "-<poolName>", and both SelectorLabels and FinalLabels include
// the pool label so that multiple pools in the same namespace have non-overlapping selectors.
func NewPoolBuilder(cr ParentOpts, kind vmv1beta1.ClusterComponent, poolName string) *ChildBuilder {
	selectorLabels := cr.SelectorLabels(kind)
	finalLabels := cr.FinalLabels(kind)
	name := cr.PrefixedName(kind)
	if poolName != "" {
		name += "-" + poolName
		selectorLabels[poolLabelKey] = poolName
		finalLabels[poolLabelKey] = poolName
	}
	return &ChildBuilder{
		ParentOpts:     cr,
		kind:           kind,
		finalLabels:    finalLabels,
		selectorLabels: selectorLabels,
		prefixedName:   name,
	}
}
