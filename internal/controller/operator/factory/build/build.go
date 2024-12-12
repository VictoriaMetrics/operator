package build

import (
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type builderOpts interface {
	client.Object
	PrefixedName() string
	AnnotationsFiltered() map[string]string
	AllLabels() map[string]string
	SelectorLabels() map[string]string
	AsOwner() []metav1.OwnerReference
	GetNSName() string
	GetAdditionalService() *vmv1beta1.AdditionalServiceSpec
}
