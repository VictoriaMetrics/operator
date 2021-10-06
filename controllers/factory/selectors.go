package factory

import (
	"context"
	"fmt"

	"github.com/VictoriaMetrics/operator/internal/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// may return namespace names and objects selector
func getNSWithSelector(ctx context.Context, rclient client.Client, nsSelector, objectSelector *metav1.LabelSelector, objNS string) ([]string, labels.Selector, error) {
	watchNS := config.MustGetWatchNamespace()
	// fast path
	if nsSelector == nil && objectSelector == nil && len(watchNS) == 0 {
		return nil, nil, nil
	}
	var namespaces []string
	//list namespaces matched by  namespaceselector
	//for each namespace apply list with  selector
	//combine result
	switch {
	case nsSelector == nil:
		namespaces = append(namespaces, objNS)
	default:
		nsSelector, err := metav1.LabelSelectorAsSelector(nsSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot convert  selector: %w", err)
		}
		namespaces, err = selectNamespaces(ctx, rclient, nsSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot select namespaces for  match: %w", err)
		}
	}

	// if namespaces isn't nil, then nameSpaceSelector is defined
	// but userSelector maybe be nil and we must set it to catch all value
	if objectSelector == nil {
		objectSelector = &metav1.LabelSelector{}
	}
	objLabelSelector, err := metav1.LabelSelectorAsSelector(objectSelector)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot convert  to Selector: %w", err)
	}

	return namespaces, objLabelSelector, nil
}

// lists api objects for given api objects type matched given selectors
func visitObjectsWithSelector(ctx context.Context, rclient client.Client, ns []string, objectListType client.ObjectList, selector labels.Selector, cb func(list client.ObjectList)) error {

	// list across all namespaces
	if ns == nil {
		if err := rclient.List(ctx, objectListType, &client.ListOptions{LabelSelector: selector}, config.MustGetNamespaceListOptions()); err != nil {
			return err
		}
		cb(objectListType)
		return nil
	}

	watchNamespace := config.MustGetWatchNamespace()
	// list only for given namespaces
	for i := range ns {
		if watchNamespace != "" && ns[i] != watchNamespace {
			continue
		}

		if err := rclient.List(ctx, objectListType, &client.ListOptions{LabelSelector: selector, Namespace: ns[i]}); err != nil {
			return err
		}

		cb(objectListType)
	}
	return nil
}
