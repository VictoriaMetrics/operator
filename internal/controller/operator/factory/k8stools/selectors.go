package k8stools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/config"
)

type listing[L any] interface {
	client.ObjectList
	*L
}

// SelectorOpts defines object query objects for given selectors
type SelectorOpts struct {
	SelectAll         bool
	NamespaceSelector *metav1.LabelSelector
	ObjectSelector    *metav1.LabelSelector
	DefaultNamespace  string
}

// isUnmanaged checks if provided Selectors are not defined
// and there is no need to perform any query requests for Kubernetes objects
func (s *SelectorOpts) isUnmanaged() bool {
	return s.NamespaceSelector == nil && s.ObjectSelector == nil && !s.SelectAll
}

// VisitSelected applies given function to any T child object matched by selectors defined in parent obj selectors
func VisitSelected[T any, PT listing[T]](ctx context.Context, rclient client.Client, s *SelectorOpts, cb func(PT)) error {
	if s.isUnmanaged() {
		return nil
	}
	dnsr, err := discoverNamespaces(ctx, rclient, s)
	if err != nil {
		return err
	}
	opts := &client.ListOptions{}
	if s.ObjectSelector != nil {
		// if s is nil, we must set it to catch all values
		selector, err := metav1.LabelSelectorAsSelector(s.ObjectSelector)
		if err != nil {
			return fmt.Errorf("cannot convert selector: %w", err)
		}
		opts.LabelSelector = selector
	}
	if dnsr == nil {
		// special case, nil value must be treated as selectors match nothing
		return nil
	}
	nss := dnsr.namespaces
	// namespaces could still be empty and it's ok
	return ListObjectsByNamespace(ctx, rclient, nss, cb, opts)
}

type discoverNamespacesResponse struct {
	namespaces []string
}

// discoverNamespaces select namespaces by given label selector
func discoverNamespaces(ctx context.Context, rclient client.Client, s *SelectorOpts) (*discoverNamespacesResponse, error) {
	cfg := config.MustGetBaseConfig()
	var namespaces []string
	switch {
	case len(cfg.WatchNamespaces) > 0:
		// perform match only for watched namespaces
		// filters by namespace is disabled, since operator cannot access cluster-wide APIs
		// this case could be improved to additionally filter by namespace name - metadata.name label

		// impossible case
		if !slices.Contains(cfg.WatchNamespaces, s.DefaultNamespace) {
			panic(fmt.Sprintf("BUG: watch namespaces: %s must contain namespace for the current reconcile object: %s", strings.Join(cfg.WatchNamespaces, ","), s.DefaultNamespace))
		}
		namespaces = append(namespaces, cfg.WatchNamespaces...)

	case s.ObjectSelector != nil && s.NamespaceSelector == nil:
		// in single namespace mode, return object ns
		namespaces = append(namespaces, s.DefaultNamespace)
	case s.NamespaceSelector != nil:
		if len(s.NamespaceSelector.MatchExpressions) == 0 && len(s.NamespaceSelector.MatchLabels) == 0 {
			// fast path, match everything
			return &discoverNamespacesResponse{namespaces: namespaces}, nil
		}
		// perform a cluster wide request for namespaces with given filters
		opts := &client.ListOptions{}
		selector, err := metav1.LabelSelectorAsSelector(s.NamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot convert selector: %w", err)
		}
		opts.LabelSelector = selector
		l := &corev1.NamespaceList{}
		if err := rclient.List(ctx, l, opts); err != nil {
			return nil, fmt.Errorf("cannot select namespaces for match: %w", err)
		}
		if len(l.Items) == 0 {
			return nil, nil
		}
		for _, n := range l.Items {
			namespaces = append(namespaces, n.Name)
		}
	}
	return &discoverNamespacesResponse{namespaces: namespaces}, nil
}
