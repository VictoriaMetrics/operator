package build

import (
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// HTTPRoute creates HTTPRoute object
func HTTPRoute(cr builderOpts, port string, httpRoute *vmv1beta1.EmbeddedHTTPRoute) (*gwapiv1.HTTPRoute, error) {
	var (
		defaultPort gwapiv1.PortNumber
	)
	if defaultPortVal, err := strconv.Atoi(port); err != nil {
		return nil, err
	} else {
		defaultPort = gwapiv1.PortNumber(defaultPortVal)
	}

	defaultBackendRef := gwapiv1.HTTPBackendRef{
		BackendRef: gwapiv1.BackendRef{
			BackendObjectReference: gwapiv1.BackendObjectReference{
				Name: gwapiv1.ObjectName(cr.PrefixedName()),
				Port: &defaultPort,
			},
		},
	}

	lbls := labels.Merge(httpRoute.Labels, cr.SelectorLabels())
	spec := httpRoute.Spec
	for i := range spec.Rules {
		if spec.Rules[i].BackendRefs == nil {
			spec.Rules[i].BackendRefs = []gwapiv1.HTTPBackendRef{}
		}
		spec.Rules[i].BackendRefs = append(spec.Rules[i].BackendRefs, defaultBackendRef)
	}

	return &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.GetNamespace(),
			Labels:          lbls,
			Annotations:     httpRoute.Annotations,
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: spec,
	}, nil
}
