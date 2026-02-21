package build

import (
	"fmt"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/utils/ptr"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// HTTPRoute creates HTTPRoute object
func HTTPRoute(cr builderOpts, port string, httpRoute *vmv1beta1.EmbeddedHTTPRoute) (*gwapiv1.HTTPRoute, error) {

	lbls := labels.Merge(httpRoute.Labels, cr.SelectorLabels())

	spec := gwapiv1.HTTPRouteSpec{
		CommonRouteSpec: gwapiv1.CommonRouteSpec{
			ParentRefs: httpRoute.ParentRefs,
		},
		Hostnames: httpRoute.Hostnames,
	}

	var err error
	if spec.Rules, err = httpRouteRule(cr, port, httpRoute); err != nil {
		return nil, err
	}

	return &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.GetNamespace(),
			Labels:          lbls,
			Annotations:     httpRoute.Annotations,
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: spec,
	}, nil
}

func httpRouteRule(cr builderOpts, port string, httpRoute *vmv1beta1.EmbeddedHTTPRoute) ([]gwapiv1.HTTPRouteRule, error) {
	defaultPortVal, err := strconv.ParseInt(port, 10, 32)
	if err != nil {
		return nil, err
	}
	defaultPort := gwapiv1.PortNumber(defaultPortVal)
	refs := []gwapiv1.HTTPBackendRef{
		{
			BackendRef: gwapiv1.BackendRef{
				BackendObjectReference: gwapiv1.BackendObjectReference{
					Name: gwapiv1.ObjectName(cr.PrefixedName()),
					Port: &defaultPort,
				},
			},
		},
	}

	var rules []gwapiv1.HTTPRouteRule
	if httpRoute.ExtraRules != nil {
		var r gwapiv1.HTTPRouteRule
		for _, ext := range httpRoute.ExtraRules {
			if len(ext.Raw) == 0 {
				continue
			}
			if err := json.Unmarshal(ext.Raw, &r); err != nil {
				return nil, fmt.Errorf("failed to decode extraRule: %w", err)
			}
			r.BackendRefs = append([]gwapiv1.HTTPBackendRef{}, refs...)
			rules = append(rules, r)
		}
	} else {
		rules = []gwapiv1.HTTPRouteRule{
			{
				Matches: []gwapiv1.HTTPRouteMatch{
					{
						Path: &gwapiv1.HTTPPathMatch{
							Type:  ptr.To(gwapiv1.PathMatchPathPrefix),
							Value: ptr.To("/"),
						},
					},
				},
				BackendRefs: refs,
			},
		}
	}
	return rules, nil

}
