package podutil

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// AgentMetrics is a monitoring agent capable of exposing Prometheus-format metrics.
// Both *vmv1beta1.VMAgent and *vmv1.VLAgent satisfy this interface.
type AgentMetrics interface {
	client.Object
	PrefixedName() string
	GetMetricsPath() string
	ProbeScheme() string
}

// GetMetricsAddrs discovers the agent's active endpoints from EndpointSlices and
// returns the full metrics URL for each ready endpoint.
func GetMetricsAddrs(ctx context.Context, rclient client.Client, agent AgentMetrics) sets.Set[string] {
	var esl discoveryv1.EndpointSliceList
	if err := rclient.List(ctx, &esl,
		client.MatchingLabels{discoveryv1.LabelServiceName: agent.PrefixedName()},
		client.InNamespace(agent.GetNamespace()),
	); err != nil {
		logger.WithContext(ctx).Error(err, "failed to load endpointslices", "service", agent.PrefixedName())
		return nil
	}
	if len(esl.Items) == 0 {
		return nil
	}
	addrs := sets.New[string]()
	for i := range esl.Items {
		es := &esl.Items[i]
		var port int32
		for _, p := range es.Ports {
			if p.Name != nil && *p.Name == "http" && p.Port != nil {
				port = *p.Port
			}
		}
		if port == 0 {
			continue
		}
		for _, ep := range es.Endpoints {
			if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
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
					Scheme: strings.ToLower(agent.ProbeScheme()),
					Path:   agent.GetMetricsPath(),
				}
				addrs.Insert(u.String())
			}
		}
	}
	return addrs
}

// Manager tracks per-address polling goroutines with individual cancel contexts.
// It cancels itself when all tracked goroutines have stopped.
type Manager struct {
	ctx     context.Context
	cancel  context.CancelFunc
	cancels map[string]context.CancelFunc
	mu      sync.Mutex
}

// NewManager creates a Manager that is a child of ctx.
func NewManager(ctx context.Context) *Manager {
	actx, acancel := context.WithCancel(ctx)
	return &Manager{
		ctx:     actx,
		cancel:  acancel,
		cancels: make(map[string]context.CancelFunc),
	}
}

// Ctx returns the manager's context.
func (m *Manager) Ctx() context.Context { return m.ctx }

// Add registers id and returns a child context for it.
func (m *Manager) Add(id string) context.Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	cancel, ok := m.cancels[id]
	if ok && cancel != nil {
		cancel()
	}
	pctx, pcancel := context.WithCancel(m.ctx)
	m.cancels[id] = pcancel
	return pctx
}

// Stop marks id as finished and triggers self-cancel when all goroutines are done.
func (m *Manager) Stop(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.cancels[id]; ok {
		if cancel != nil {
			cancel()
			m.cancels[id] = nil
		}
	}
	m.cancelIfNeeded()
}

// Has reports whether id is currently tracked.
func (m *Manager) Has(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.cancels[id]
	return ok
}

// Delete cancels and removes id.
func (m *Manager) Delete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.cancels[id]; ok {
		if cancel != nil {
			cancel()
		}
		delete(m.cancels, id)
	}
	m.cancelIfNeeded()
}

func (m *Manager) cancelIfNeeded() {
	if len(m.cancels) == 0 {
		return
	}
	for _, cancel := range m.cancels {
		if cancel != nil {
			return
		}
	}
	m.cancel()
}

// IDs returns the list of currently tracked IDs.
func (m *Manager) IDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []string
	for id := range m.cancels {
		ids = append(ids, id)
	}
	return ids
}

// WaitForEmptyPQOpts configures WaitForEmptyPQ behaviour.
type WaitForEmptyPQOpts struct {
	HTTPClient    *http.Client
	MetricName    string
	Dimension     string
	CheckInterval time.Duration
	// MatchesBackend returns true when the metric label value corresponds to backendURL.
	MatchesBackend func(labelValue, backendURL string) bool
}

// WaitForEmptyPQ polls agent metrics across all agents until the persistent queue
// for backendURL is empty (or the context is cancelled).
func WaitForEmptyPQ[Agent AgentMetrics](
	ctx context.Context,
	rclient client.Client,
	agents []Agent,
	backendURL string,
	opts WaitForEmptyPQOpts,
	updateInterval time.Duration,
) {
	pollMetrics := func(pctx context.Context, agentName, addr string) error {
		return wait.PollUntilContextCancel(pctx, opts.CheckInterval, true, func(ctx context.Context) (bool, error) {
			queries := []MetricQuery{{Name: opts.MetricName, Dimension: opts.Dimension}}
			metricValues, err := FetchMetricsValues(ctx, opts.HTTPClient, addr, queries)
			if err != nil {
				logger.WithContext(ctx).Error(err, "attempt to get metrics failed", "url", addr, "agent", agentName)
				return false, nil
			}
			vs, ok := metricValues[opts.MetricName]
			if !ok {
				return false, nil
			}
			for labelVal, v := range vs {
				if !opts.MatchesBackend(labelVal, backendURL) {
					continue
				}
				if v > 0 {
					logger.WithContext(ctx).V(1).Info("persistent queue is not empty", "url", addr, "agent", agentName, "size", v)
					return false, nil
				}
			}
			logger.WithContext(ctx).V(1).Info("persistent queue drained", "url", addr, "agent", agentName)
			return true, nil
		})
	}

	var wg sync.WaitGroup
	for i := range agents {
		agent := agents[i]
		if agent.GetCreationTimestamp().Time.IsZero() {
			continue
		}
		agentName := agent.GetNamespace() + "/" + agent.GetName()
		m := NewManager(ctx)
		wg.Go(func() {
			wait.UntilWithContext(m.Ctx(), func(ctx context.Context) {
				addrs := GetMetricsAddrs(ctx, rclient, agent)
				for addr := range addrs {
					if m.Has(addr) {
						continue
					}
					logger.WithContext(ctx).V(1).Info("start polling metrics", "url", addr, "agent", agentName)
					pctx := m.Add(addr)
					wg.Go(func() {
						if err := pollMetrics(pctx, agentName, addr); err != nil {
							return
						}
						m.Stop(addr)
					})
				}
				for _, addr := range m.IDs() {
					if _, ok := addrs[addr]; !ok {
						logger.WithContext(ctx).V(1).Info("stop polling metrics", "url", addr, "agent", agentName)
						m.Delete(addr)
					}
				}
			}, updateInterval)
		})
	}
	wg.Wait()
}

// MergeSpecs substitutes ZonePlaceholder in a with name, then deep-merges b on top.
func MergeSpecs[T any](a, b *T, name string) (*T, error) {
	merged, err := k8stools.RenderPlaceholders(a, map[string]string{
		vmv1alpha1.ZonePlaceholder: name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to render spec: %w", err)
	}
	if err := build.MergeDeep(merged, b, false); err != nil {
		return nil, fmt.Errorf("failed to merge spec: %w", err)
	}
	return merged, nil
}

// MergeRemoteWriteTargets merges zone-level additional remoteWrite targets on top of the common
// ones, matching entries by url. urlOf must return the url of the given entry.
//
// Unlike MergeSpecs, which replaces arrays as a whole, entries here are keyed:
//   - an entry whose url is present in both lists is deep-merged, zone values win field-by-field
//   - an entry present only in the zone list is appended after the common ones
//   - an entry present only in the common list is kept as is
//
// ZonePlaceholder is substituted with name in BOTH lists before urls are matched, so a common
// entry templated with %ZONE% can be addressed from the zone list by either the templated or the
// already rendered url.
//
// The result is deterministic: common entries in their declared order, then zone-only entries in
// their declared order. Urls must be non-empty and unique within each list after rendering.
func MergeRemoteWriteTargets[T any](common, zone []T, name string, urlOf func(*T) string) ([]T, error) {
	renderList := func(src []T, path string) ([]T, error) {
		if len(src) == 0 {
			// RenderPlaceholders marshals a nil slice into "null" and returns a nil *[]T,
			// which would panic on dereference below.
			return nil, nil
		}
		out, err := k8stools.RenderPlaceholders(&src, map[string]string{
			vmv1alpha1.ZonePlaceholder: name,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		// rendering may produce a collision which did not exist before it,
		// so uniqueness must be re-checked here and not only during CR validation.
		seen := make(map[string]struct{}, len(*out))
		for i := range *out {
			u := urlOf(&(*out)[i])
			if len(u) == 0 {
				return nil, fmt.Errorf("%s[%d].url cannot be empty", path, i)
			}
			if _, ok := seen[u]; ok {
				return nil, fmt.Errorf("%s[%d].url=%q is duplicated, urls must be unique within a list", path, i, u)
			}
			seen[u] = struct{}{}
		}
		return *out, nil
	}

	commonTargets, err := renderList(common, "zoneCommon.vmagent.spec.additionalRemoteWriteTargets")
	if err != nil {
		return nil, err
	}
	zoneTargets, err := renderList(zone, "vmagent.spec.additionalRemoteWriteTargets")
	if err != nil {
		return nil, err
	}
	if len(zoneTargets) == 0 {
		return commonTargets, nil
	}

	result := make([]T, 0, len(commonTargets)+len(zoneTargets))
	result = append(result, commonTargets...)
	idxByURL := make(map[string]int, len(result))
	for i := range result {
		idxByURL[urlOf(&result[i])] = i
	}
	for i := range zoneTargets {
		override := &zoneTargets[i]
		u := urlOf(override)
		idx, ok := idxByURL[u]
		if !ok {
			idxByURL[u] = len(result)
			result = append(result, *override)
			continue
		}
		if err := build.MergeDeep(&result[idx], override, false); err != nil {
			return nil, fmt.Errorf("failed to merge additionalRemoteWriteTargets with url=%q: %w", u, err)
		}
	}
	return result, nil
}
