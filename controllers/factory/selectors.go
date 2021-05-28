package factory

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getNSWithSelector(ctx context.Context, rclient client.Client, nsSelector, objectSelector *metav1.LabelSelector, objNS string) ([]string, labels.Selector, error) {
	var namespaces []string
	//list namespaces matched by  namespaceselector
	//for each namespace apply list with  selector
	//combine result
	switch {
	case nsSelector == nil:
		namespaces = append(namespaces, objNS)
	case nsSelector.MatchExpressions == nil && nsSelector.MatchLabels == nil:
		namespaces = nil
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
	if namespaces != nil && objectSelector == nil {
		objectSelector = &metav1.LabelSelector{}
	}
	objLabelSelector, err := metav1.LabelSelectorAsSelector(objectSelector)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot convert  to Selector: %w", err)
	}

	return namespaces, objLabelSelector, nil
}

func selectWithMerge(ctx context.Context, rclient client.Client, ns []string, objectList client.ObjectList, selector labels.Selector, cb func(list client.ObjectList)) error {

	if ns == nil {
		if err := rclient.List(ctx, objectList, &client.ListOptions{LabelSelector: selector}); err != nil {
			return err
		}
		cb(objectList)
		return nil
	}
	for i := range ns {
		if err := rclient.List(ctx, objectList, &client.ListOptions{LabelSelector: selector, Namespace: ns[i]}); err != nil {
			return err
		}

		cb(objectList)
	}
	return nil
}
