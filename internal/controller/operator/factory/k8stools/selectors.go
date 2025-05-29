package k8stools

import (
	"context"
	"fmt"

	"github.com/VictoriaMetrics/operator/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VisitObjectsForSelectorsAtNs applies given function to any object
// matched given selectors
func VisitObjectsForSelectorsAtNs[T any, PT interface {
	*T
	client.ObjectList
}](ctx context.Context, rclient client.Client,
	nsSelector, objectSelector *metav1.LabelSelector,
	objNamespace string, selectAllByDefault bool, cb func(PT),
) error {
	watchNS := config.MustGetWatchNamespaces()
	// fast path, empty selectors and cannot select all by default
	if nsSelector == nil && objectSelector == nil && !selectAllByDefault {
		return nil
	}
	var namespaces []string
	// list namespaces matched by  namespaceselector
	// for each namespace apply list with  selector
	// combine result
	switch {
	case len(watchNS) > 0:
		// perform match only for watched namespaces
		// filters by namespace is disabled, since operator cannot access cluster-wide APIs
		// this case could be improved to additionally filter by namespace name - metadata.name label
		namespaces = append(namespaces, watchNS...)
	case objectSelector != nil && nsSelector == nil:
		// in single namespace mode, return object ns
		namespaces = append(namespaces, objNamespace)
	case nsSelector != nil:
		// perform a cluster wide request for namespaces with given filters
		nsSelector, err := metav1.LabelSelectorAsSelector(nsSelector)
		if err != nil {
			return fmt.Errorf("cannot convert selector: %w", err)
		}
		namespaces, err = SelectNamespaces(ctx, rclient, nsSelector)
		if err != nil {
			return fmt.Errorf("cannot select namespaces for  match: %w", err)
		}
		// if nsSelector is specified and no match, return directly
		if len(namespaces) == 0 {
			return nil
		}
	}

	// if userSelector is nil, we must set it to catch all values
	if objectSelector == nil {
		objectSelector = &metav1.LabelSelector{}
	}
	objLabelSelector, err := metav1.LabelSelectorAsSelector(objectSelector)
	if err != nil {
		return fmt.Errorf("cannot convert  to Selector: %w", err)
	}
	// namespaces could still be empty if nsSelector&objectSelector are nil and selectAllByDefault=true, and it's ok
	return ListObjectsByNamespace(ctx, rclient, namespaces, cb, &client.ListOptions{LabelSelector: objLabelSelector})
}

// SelectNamespaces select namespaces by given label selector
func SelectNamespaces(ctx context.Context, rclient client.Client, selector labels.Selector) ([]string, error) {
	var matchedNs []string
	ns := &corev1.NamespaceList{}

	if err := rclient.List(ctx, ns, &client.ListOptions{LabelSelector: selector}); err != nil {
		return nil, err
	}

	for _, n := range ns.Items {
		matchedNs = append(matchedNs, n.Name)
	}

	return matchedNs, nil
}
