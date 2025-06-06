package k8stools

import (
	"context"
	"fmt"
	"slices"

	"github.com/VictoriaMetrics/operator/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type listing[L any] interface {
	client.ObjectList
	*L
}

type SelectorOpts struct {
	SelectAll         bool
	NamespaceSelector *metav1.LabelSelector
	ObjectSelector    *metav1.LabelSelector
	DefaultNamespace  string
}

func (s *SelectorOpts) IsUnmanaged() bool {
	return s.NamespaceSelector == nil && s.ObjectSelector == nil && !s.SelectAll
}

// VisitSelected applies given function to any T child object matched by selectors defined in parent obj selectors
func VisitSelected[L any, PL listing[L]](ctx context.Context, rclient client.Client, s *SelectorOpts, cb func(PL)) error {
	if s.IsUnmanaged() {
		return nil
	}
	nss, err := discoverNamespaces(ctx, rclient, s)
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
	// namespaces could still be empty and it's ok
	return ListObjectsByNamespace(ctx, rclient, nss, cb, opts)
}

// discoverNamespaces select namespaces by given label selector
func discoverNamespaces(ctx context.Context, rclient client.Client, s *SelectorOpts) ([]string, error) {
	namespaces := config.MustGetWatchNamespaces()
	if s.NamespaceSelector == nil && s.ObjectSelector != nil {
		// in single namespace mode, return object ns
		if len(namespaces) == 0 || slices.Contains(namespaces, s.DefaultNamespace) {
			namespaces = append(namespaces[:0], s.DefaultNamespace)
		}
	} else if len(namespaces) == 0 {
		// perform a cluster wide request for namespaces with given filters
		// filters by namespace is disabled, when WATCH_NAMESPACES is defined,
		// since operator cannot access cluster-wide APIs this case could be improved
		// to additionally filter by namespace name - metadata.name label
		opts := &client.ListOptions{}
		if s.NamespaceSelector != nil {
			selector, err := metav1.LabelSelectorAsSelector(s.NamespaceSelector)
			if err != nil {
				return nil, fmt.Errorf("cannot convert selector: %w", err)
			}
			opts.LabelSelector = selector
		}
		l := &corev1.NamespaceList{}
		if err := rclient.List(ctx, l, opts); err != nil {
			return nil, fmt.Errorf("cannot select namespaces for  match: %w", err)
		}
		for _, n := range l.Items {
			namespaces = append(namespaces, n.Name)
		}
	}
	return namespaces, nil
}
