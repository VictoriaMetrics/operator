// Package podutil provides shared helpers for discovering pod endpoints via
// EndpointSlices and calling admin/metrics HTTP endpoints on them directly
// (in-cluster pod-to-pod traffic, not routed through the Kubernetes API).
package podutil

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DiscoverEndpointAddrs lists ready endpoint addresses for the given Service (identified via
// the standard discoveryv1.LabelServiceName label) and builds a URL for each, using portName
// to pick the right port, scheme for http/https, and path for the request path.
func DiscoverEndpointAddrs(ctx context.Context, rclient client.Client, namespace, serviceName, portName, scheme, path string) (sets.Set[string], error) {
	var esl discoveryv1.EndpointSliceList
	o := client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{discoveryv1.LabelServiceName: serviceName}),
		Namespace:     namespace,
	}
	if err := rclient.List(ctx, &esl, &o); err != nil {
		return nil, fmt.Errorf("failed to load endpointslices for service=%s: %w", serviceName, err)
	}
	addrs := sets.New[string]()
	for i := range esl.Items {
		es := &esl.Items[i]
		var port int32
		for _, p := range es.Ports {
			if p.Name != nil && *p.Name == portName && p.Port != nil {
				port = *p.Port
			}
		}
		if port == 0 {
			continue
		}
		for _, ep := range es.Endpoints {
			if ep.Conditions.Ready == nil || !*ep.Conditions.Ready {
				continue
			}
			for _, a := range ep.Addresses {
				if a == "" {
					continue
				}
				host := fmt.Sprintf("%s:%d", a, port)
				if es.AddressType == discoveryv1.AddressTypeIPv6 {
					host = fmt.Sprintf("[%s]:%d", a, port)
				}
				u := &url.URL{
					Host:   host,
					Scheme: strings.ToLower(scheme),
					Path:   path,
				}
				addrs.Insert(u.String())
			}
		}
	}
	return addrs, nil
}
