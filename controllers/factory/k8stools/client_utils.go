package k8stools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var log = logf.Log.WithName("client_utils")

var invalidDNS1123Characters = regexp.MustCompile("[^-a-z0-9]+")

func SanitizeVolumeName(name string) string {
	name = strings.ToLower(name)
	name = invalidDNS1123Characters.ReplaceAllString(name, "-")
	if len(name) > validation.DNS1123LabelMaxLength {
		name = name[0:validation.DNS1123LabelMaxLength]
	}
	return strings.Trim(name, "-")
}

// MergePatchContainers adds patches to base using a strategic merge patch and iterating by container name, failing on the first error
func MergePatchContainers(base, patches []v1.Container) ([]v1.Container, error) {
	var out []v1.Container

	// map of containers that still need to be patched by name
	containersToPatch := make(map[string]v1.Container)
	for _, c := range patches {
		containersToPatch[c.Name] = c
	}

	for _, container := range base {
		// If we have a patch result, iterate over each container and try and calculate the patch
		if patchContainer, ok := containersToPatch[container.Name]; ok {
			// Get the json for the container and the patch
			containerBytes, err := json.Marshal(container)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal json for container %s, err: %w", container.Name, err)
			}
			patchBytes, err := json.Marshal(patchContainer)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal json for patch container %s, err: %w", container.Name, err)
			}

			// Calculate the patch result
			jsonResult, err := strategicpatch.StrategicMergePatch(containerBytes, patchBytes, v1.Container{})
			if err != nil {
				return nil, fmt.Errorf("failed to generate merge patch for %s, err: %w", container.Name, err)
			}
			var patchResult v1.Container
			if err := json.Unmarshal(jsonResult, &patchResult); err != nil {
				return nil, fmt.Errorf("failed to unmarshal merged container %s, err: %w", container.Name, err)
			}

			// Add the patch result and remove the corresponding key from the to do list
			out = append(out, patchResult)
			delete(containersToPatch, container.Name)
		} else {
			// This container didn't need to be patched
			out = append(out, container)
		}
	}

	// Iterate over the patches and add all the containers that were not previously part of a patch result
	for _, container := range patches {
		if _, ok := containersToPatch[container.Name]; ok {
			out = append(out, container)
		}
	}

	return out, nil
}

// UpdatePodAnnotations - updates configmap-sync-time annotation
// it triggers config rules reload for vmalert
func UpdatePodAnnotations(ctx context.Context, rclient client.Client, selector map[string]string, ns string) error {
	var podsToUpdate v1.PodList
	opts := client.ListOptions{
		Namespace:     ns,
		LabelSelector: labels.SelectorFromSet(selector),
	}
	err := rclient.List(ctx, &podsToUpdate, &opts)
	if err != nil {
		return fmt.Errorf("failed to list pod items: %w", err)
	}
	updateTime := time.Now().Format("2006-01-02T15-04-05")
	pt := client.RawPatch(types.MergePatchType,
		[]byte(fmt.Sprintf(`{"metadata": {"annotations": {"configmap-sync-lastupdate-at": "%s"} } }`, updateTime)))
	for _, pod := range podsToUpdate.Items {
		err := rclient.Patch(ctx, &pod, pt)
		if err != nil {
			return fmt.Errorf("failed to patch pod item with annotation: %s, err: %w", updateTime, err)
		}
	}
	return nil
}

// ListObjectsByNamespace performs object list for given namespaces
func ListObjectsByNamespace[T any, PT interface {
	*T
	client.ObjectList
}](ctx context.Context, rclient client.Client, nss []string, collect func(PT), opts ...client.ListOption) error {
	dst := PT(new(T))
	if len(nss) == 0 {
		if err := rclient.List(ctx, dst, opts...); err != nil {
			return fmt.Errorf("cannot list objects at cluster scope: %w", err)
		}
		collect(dst)
		return nil
	}
	nsOpts := append(opts, &client.ListOptions{})
	for _, ns := range nss {
		nsOpts[len(nsOpts)-1] = &client.ListOptions{Namespace: ns}
		if err := rclient.List(ctx, dst, nsOpts...); err != nil {
			return fmt.Errorf("cannot list objects for ns=%q: %w", ns, err)
		}
		collect(dst)
	}
	return nil
}

var (
	activeWatchers         = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "operator_prometheus_converter_active_watchers"}, []string{"namespace"})
	watchEventsTotalByType = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "operator_prometheus_converter_watch_events_total"}, []string{"event_type", "namespace", "object_type_name"})
	initMetrics            sync.Once
)

// ObjectWatcherForNamespaces performs a watch operation for multiple namespaces
// without using cluster wide permissions
// with empty namaspaces uses cluster wide mode
type ObjectWatcherForNamespaces struct {
	result         chan watch.Event
	objectWatchers []watch.Interface
	wg             sync.WaitGroup
	eventsTotal    *prometheus.CounterVec
	activeWatchers *atomic.Int64
}

// NewObjectWatcherForNamespaces returns a watcher for events at multiple namespaces  for given object
// in case of empty namespaces, performs cluster wide watch
func NewObjectWatcherForNamespaces[T any, PT interface {
	*T
	client.ObjectList
}](ctx context.Context, rclient client.WithWatch, crdTypeName string, namespaces []string) (watch.Interface, error) {
	initMetrics.Do(func() {
		metrics.Registry.MustRegister(activeWatchers, watchEventsTotalByType)
	})
	ownss := ObjectWatcherForNamespaces{
		result: make(chan watch.Event),
	}
	// fast path
	if len(namespaces) == 0 {
		dst := PT(new(T))
		w, err := rclient.Watch(ctx, dst)
		if err != nil {
			return w, fmt.Errorf("cannot start watcher for cluster wide: %w", err)
		}
		ownss.wg.Add(1)
		activeWatchers.WithLabelValues("ALL_NAMESPACES").Add(1)
		go func() {
			defer ownss.wg.Done()
			defer activeWatchers.WithLabelValues("ALL_NAMESPACES").Add(-1)
			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-w.ResultChan():
					if !ok {
						close(ownss.result)
						return
					}
					select {
					case ownss.result <- ev:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		ownss.objectWatchers = append(ownss.objectWatchers, w)
		return &ownss, nil
	}

	// all watchers must be gracefully stopped at any child channel close
	localCtx, cancel := context.WithCancel(ctx)
	for _, ns := range namespaces {
		dst := PT(new(T))
		w, err := rclient.Watch(localCtx, dst, &client.ListOptions{Namespace: ns})
		if err != nil {
			cancel()
			return w, fmt.Errorf("cannot start watcher for ns=%q wide: %w", ns, err)
		}
		ownss.objectWatchers = append(ownss.objectWatchers, w)
		ownss.wg.Add(1)
		activeWatchers.WithLabelValues(ns).Add(1)
		go func(w watch.Interface, ns string) {
			defer ownss.wg.Done()
			defer activeWatchers.WithLabelValues(ns).Add(-1)
			for {
				select {
				case <-localCtx.Done():
					return
				case ev, ok := <-w.ResultChan():
					if !ok {
						cancel()
						return
					}
					watchEventsTotalByType.WithLabelValues(string(ev.Type), ns, crdTypeName).Inc()
					select {
					case ownss.result <- ev:
					case <-localCtx.Done():
						return
					}
				}
			}
		}(w, ns)
	}
	go func() {
		ownss.wg.Wait()
		cancel()
		close(ownss.result)
	}()
	return &ownss, nil
}

// ResultChan returns a channel with events
func (ow *ObjectWatcherForNamespaces) ResultChan() <-chan watch.Event {
	return ow.result
}

// Stop performs a stop on all watchers and waits for it's finish
func (ow *ObjectWatcherForNamespaces) Stop() {
	for _, objectWatcher := range ow.objectWatchers {
		objectWatcher.Stop()
	}
	ow.wg.Wait()
}
