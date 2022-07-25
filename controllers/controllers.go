package controllers

import (
	"fmt"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"time"
)

var defaultOptions = controller.Options{
	RateLimiter: workqueue.NewItemExponentialFailureRateLimiter(2*time.Second, 2*time.Minute),
}

func isSelectorsMatches(sourceCRD, targetCRD client.Object, nsSelector, selector *v1.LabelSelector) (bool, error) {
	// in case of empty namespace object must be synchronized in any way,
	// coz we dont know source labels.
	// probably object already deleted.
	if sourceCRD.GetNamespace() == "" {
		return true, nil
	}
	if sourceCRD.GetNamespace() == targetCRD.GetNamespace() {
		return true, nil
	}
	// fast path config match all by default
	if selector == nil && nsSelector == nil {
		return true, nil
	}
	// fast path maybe namespace selector will match.
	if selector == nil {
		return true, nil
	}
	labelSelector, err := v1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, fmt.Errorf("cannot parse vmalert's RuleSelector selector as labelSelector: %w", err)
	}
	set := labels.Set(sourceCRD.GetLabels())
	// selector not match
	if !labelSelector.Matches(set) {
		return false, nil
	}
	return true, nil
}
