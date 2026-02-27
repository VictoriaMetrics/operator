package vmdistributed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

type zones struct {
	httpClient *http.Client
	vmagents   []*vmv1beta1.VMAgent
	vmclusters []*vmv1beta1.VMCluster
	hasChanges []bool
}

func (zs *zones) Len() int {
	return len(zs.vmclusters)
}

// Apply custom sorting for VMClusters and VMAgents updating first not existing or failed zones
// The rest is sorted by observedGeneration in descending order
// If all above is equal then zones are sorted by VMCluster name in ascending order
func (zs *zones) Less(i, j int) bool {
	statusI := zs.vmclusters[i].Status
	statusJ := zs.vmclusters[j].Status
	if statusI.UpdateStatus != statusJ.UpdateStatus {
		return statusI.UpdateStatus == vmv1beta1.UpdateStatusFailed
	}
	isZeroI := zs.vmclusters[i].CreationTimestamp.IsZero()
	isZeroJ := zs.vmclusters[j].CreationTimestamp.IsZero()
	if isZeroI != isZeroJ {
		return isZeroI
	}
	if statusI.ObservedGeneration != statusJ.ObservedGeneration {
		return statusI.ObservedGeneration > statusJ.ObservedGeneration
	}
	return zs.vmclusters[i].Name < zs.vmclusters[j].Name
}

func (zs *zones) Swap(i, j int) {
	zs.vmagents[i], zs.vmagents[j] = zs.vmagents[j], zs.vmagents[i]
	zs.vmclusters[i], zs.vmclusters[j] = zs.vmclusters[j], zs.vmclusters[i]
	zs.hasChanges[i], zs.hasChanges[j] = zs.hasChanges[j], zs.hasChanges[i]
}

// getZones builds desired zones
func getZones(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed) (*zones, error) {
	zs := &zones{
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
		vmagents:   make([]*vmv1beta1.VMAgent, len(cr.Spec.Zones)),
		vmclusters: make([]*vmv1beta1.VMCluster, len(cr.Spec.Zones)),
		hasChanges: make([]bool, len(cr.Spec.Zones)),
	}
	for i := range cr.Spec.Zones {
		z := &cr.Spec.Zones[i]
		nsn := types.NamespacedName{
			Name:      z.VMClusterName(cr),
			Namespace: cr.Namespace,
		}
		var vmCluster vmv1beta1.VMCluster
		if err := rclient.Get(ctx, nsn, &vmCluster); err != nil {
			if !k8serrors.IsNotFound(err) {
				return nil, fmt.Errorf("unexpected error during attempt to get VMCluster=%s: %w", nsn, err)
			}
			vmCluster.ObjectMeta = metav1.ObjectMeta{
				Name:            z.VMClusterName(cr),
				Namespace:       cr.Namespace,
				OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			}
		}
		vmClusterSpec, err := mergeSpecs(&cr.Spec.ZoneCommon.VMCluster.Spec, &z.VMCluster.Spec, z.Name)
		if err != nil {
			return nil, fmt.Errorf("spec.zones[%d].vmcluster.spec: %w", i, err)
		}
		prevClusterSpec := vmCluster.Spec
		vmCluster.Spec = *vmClusterSpec
		rclient.Scheme().Default(&vmCluster)
		zs.hasChanges[i] = !equality.Semantic.DeepEqual(&vmCluster.Spec, &prevClusterSpec)
		zs.vmclusters[i] = &vmCluster
	}

	for i := range cr.Spec.Zones {
		z := &cr.Spec.Zones[i]
		vmAgentName := z.VMAgentName(cr)
		nsn := types.NamespacedName{
			Name:      vmAgentName,
			Namespace: cr.Namespace,
		}
		var vmAgent vmv1beta1.VMAgent
		if err := rclient.Get(ctx, nsn, &vmAgent); err != nil {
			if !k8serrors.IsNotFound(err) {
				return nil, fmt.Errorf("unexpected error during attempt to get VMAgent=%s: %w", nsn, err)
			}
			vmAgent.ObjectMeta = metav1.ObjectMeta{
				Name:            vmAgentName,
				Namespace:       cr.Namespace,
				OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			}
		}
		vmAgentCustomSpec, err := mergeSpecs(&cr.Spec.ZoneCommon.VMAgent.Spec, &z.VMAgent.Spec, z.Name)
		if err != nil {
			return nil, fmt.Errorf("spec.zones[%d].vmagent.spec: %w", i, err)
		}
		vmAgentSpec, err := vmAgentCustomSpec.ToVMAgentSpec()
		if err != nil {
			return nil, fmt.Errorf("spec.zones[%d].vmagent.spec: %w", i, err)
		}
		vmAgentSpec.IngestOnlyMode = ptr.To(true)
		if vmAgentSpec.RemoteWriteSettings == nil {
			vmAgentSpec.RemoteWriteSettings = &vmv1beta1.VMAgentRemoteWriteSettings{}
		}
		vmAgentSpec.RemoteWriteSettings.UseMultiTenantMode = true
		vmAgentSpec.RemoteWrite = vmAgentSpec.RemoteWrite[:0]
		for j := range cr.Spec.Zones {
			cz := &cr.Spec.Zones[j]
			customRemoteWrite, err := mergeSpecs(&cr.Spec.ZoneCommon.RemoteWrite, &cz.RemoteWrite, cz.Name)
			if err != nil {
				return nil, fmt.Errorf("spec.zones[%d].vmagent: failed to build remote write for zone[%d]: %w", i, j, err)
			}
			remoteWrite, err := customRemoteWrite.ToVMAgentRemoteWriteSpec()
			if err != nil {
				return nil, fmt.Errorf("spec.zones[%d].vmagent: spec.zones[%d].remoteWrite: %w", i, j, err)
			}
			remoteWrite.URL = zs.vmclusters[j].GetRemoteWriteURL()
			vmAgentSpec.RemoteWrite = append(vmAgentSpec.RemoteWrite, *remoteWrite)
		}
		// sorting VMAgent's remote write configurations
		orderMap := make(map[string]int)
		for j, rw := range vmAgent.Spec.RemoteWrite {
			orderMap[rw.URL] = j
		}
		sort.SliceStable(vmAgentSpec.RemoteWrite, func(k, j int) bool {
			idxK, okK := orderMap[vmAgentSpec.RemoteWrite[k].URL]
			idxJ, okJ := orderMap[vmAgentSpec.RemoteWrite[j].URL]
			if !okK {
				return false
			}
			if !okJ {
				return true
			}
			return idxK < idxJ
		})
		prevAgentSpec := vmAgent.Spec
		vmAgent.Spec = *vmAgentSpec
		rclient.Scheme().Default(&vmAgent)
		zs.hasChanges[i] = zs.hasChanges[i] || !equality.Semantic.DeepEqual(&vmAgent.Spec, &prevAgentSpec)
		zs.vmagents[i] = &vmAgent
	}

	sort.Sort(zs)
	return zs, nil
}

func (zs *zones) upgrade(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, i int) error {
	ctx, cancel := context.WithTimeout(ctx, cr.Spec.ZoneCommon.ReadyTimeout.Duration)
	defer cancel()

	owner := cr.AsOwner()
	vmCluster := zs.vmclusters[i]
	vmAgent := zs.vmagents[i]
	item := fmt.Sprintf("%d/%d", i+1, len(cr.Spec.Zones))
	nsnCluster := types.NamespacedName{Name: vmCluster.Name, Namespace: vmCluster.Namespace}
	logger.WithContext(ctx).Info("reconciling VMCluster", "item", item, "name", nsnCluster)

	// vmCluster or vmAgent have been created
	needsLBUpdate := !vmCluster.CreationTimestamp.IsZero() && !vmAgent.CreationTimestamp.IsZero()
	// No vmcluster or vmagent spec changes required
	if !zs.hasChanges[i] {
		needsLBUpdate = false
	}
	if needsLBUpdate {
		// wait for empty persistent queue
		if err := zs.waitForEmptyPQ(ctx, rclient, defaultMetricsCheckInterval, i); err != nil {
			return fmt.Errorf("zone=%s: failed to wait till VMCluster=%s queue is empty: %w", item, nsnCluster, err)
		}

		// excluding zone from VMAuth LB
		if err := zs.updateLB(ctx, rclient, cr, i); err != nil {
			return fmt.Errorf("zone=%s: failed to update VMAuth LB with excluded VMCluster=%s: %w", item, nsnCluster, err)
		}
	}

	// reconcile VMCluster
	if err := reconcile.VMCluster(ctx, rclient, vmCluster, nil, &owner); err != nil {
		return fmt.Errorf("zone=%s: failed to reconcile VMCluster=%s: %w", item, nsnCluster, err)
	}

	// reconcile VMAgent
	nsnAgent := types.NamespacedName{Name: vmAgent.Name, Namespace: vmAgent.Namespace}
	if err := reconcile.VMAgent(ctx, rclient, vmAgent, nil, &owner); err != nil {
		return fmt.Errorf("zone=%s: failed to reconcile VMAgent=%s: %w", item, nsnAgent, err)
	}

	// wait for empty persistent queue
	if err := zs.waitForEmptyPQ(ctx, rclient, defaultMetricsCheckInterval, i); err != nil {
		return fmt.Errorf("zone=%s: failed to wait till VMAgent queue for VMCluster=%s is drained: %w", item, nsnCluster, err)
	}

	// restore zone in VMAuth LB
	if err := zs.updateLB(ctx, rclient, cr); err != nil {
		return fmt.Errorf("zone=%s: failed to update VMAuth LB: %w", item, err)
	}

	return nil
}

func (zs *zones) updateLB(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, excludeIds ...int) error {
	owner := cr.AsOwner()
	if !ptr.Deref(cr.Spec.VMAuth.Enabled, true) {
		return nil
	}
	vmAuth := buildVMAuthLB(cr, zs.vmagents, zs.vmclusters, excludeIds...)
	if vmAuth == nil {
		return nil
	}
	return reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner)
}

func getMetricsAddrs(ctx context.Context, rclient client.Client, vmAgent *vmv1beta1.VMAgent) map[string]struct{} {
	var esl discoveryv1.EndpointSliceList
	o := client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{discoveryv1.LabelServiceName: vmAgent.PrefixedName()}),
		Namespace:     vmAgent.Namespace,
	}
	if err := rclient.List(ctx, &esl, &o); err != nil {
		logger.WithContext(ctx).Error(err, "failed to load endpointslices", "service", vmAgent.PrefixedName())
		return nil
	}
	if len(esl.Items) == 0 {
		return nil
	}
	addrs := make(map[string]struct{})
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
					Scheme: strings.ToLower(vmAgent.ProbeScheme()),
					Path:   vmAgent.GetMetricsPath(),
				}
				addrs[u.String()] = struct{}{}
			}
		}
	}
	return addrs
}

func (zs *zones) waitForEmptyPQ(ctx context.Context, rclient client.Client, interval time.Duration, clusterIdx int) error {
	vmCluster := zs.vmclusters[clusterIdx]
	clusterURLHash := fmt.Sprintf("%016X", xxhash.Sum64([]byte(vmCluster.GetRemoteWriteURL())))

	pollMetrics := func(pctx context.Context, nsn types.NamespacedName, addr string) error {
		return wait.PollUntilContextCancel(pctx, interval, true, func(ctx context.Context) (done bool, err error) {
			// Query each discovered ip. If any returns non-zero metric, continue polling.
			var metricValues map[string]float64
			if metricValues, err = fetchMetricValues(ctx, zs.httpClient, addr, vmAgentQueueMetricName, "path"); err != nil {
				logger.WithContext(ctx).Error(err, "attempt to get metrics failed", "url", addr, "name", nsn)
				// Treat fetch errors as transient -> not ready, continue polling.
				return false, nil
			}
			for p, v := range metricValues {
				pqDir := filepath.Base(p)
				idx := strings.Index(pqDir, "_")
				if idx == -1 {
					continue
				}
				urlHash := pqDir[idx+1:]
				if clusterURLHash != urlHash {
					continue
				}
				if v > 0 {
					logger.WithContext(ctx).Info("persistent queue on VMAgent instance is not ready", "url", addr, "name", nsn, "size", v)
					return false, nil
				}
			}
			logger.WithContext(ctx).Info("all persistent queues on VMAgent for given cluster were drained", "url", addr, "name", nsn)
			return true, nil
		})
	}

	var wg sync.WaitGroup
	var resultErr error
	var once sync.Once
	gctx, gcancel := context.WithCancel(ctx)
	defer gcancel()
	cancel := func(err error) {
		once.Do(func() {
			resultErr = err
			gcancel()
		})
	}

	for i := range zs.vmagents {
		vmAgent := zs.vmagents[i]
		if vmAgent.CreationTimestamp.IsZero() {
			continue
		}
		nsn := types.NamespacedName{
			Name:      vmAgent.Name,
			Namespace: vmAgent.Namespace,
		}
		m := newManager(gctx)
		wg.Go(func() {
			wait.UntilWithContext(m.ctx, func(ctx context.Context) {
				addrs := getMetricsAddrs(ctx, rclient, vmAgent)
				for addr := range addrs {
					if m.has(addr) {
						continue
					}
					logger.WithContext(ctx).Info("start polling metrics from VMAgent instance", "url", addr, "name", nsn)
					pctx := m.add(addr)
					wg.Go(func() {
						if err := pollMetrics(pctx, nsn, addr); err != nil {
							if !errors.Is(err, context.Canceled) {
								cancel(err)
								return
							}
						}
						m.stop(addr)
					})
				}
				for _, addr := range m.ids() {
					if _, ok := addrs[addr]; !ok {
						logger.WithContext(ctx).Info("stop polling metrics from VMAgent instance", "url", addr, "name", nsn)
						m.delete(addr)
					}
				}
			}, defaultEndpointsUpdateInterval)
		})
	}
	wg.Wait()
	if resultErr != nil {
		return fmt.Errorf("failed to wait for VMAgent metrics: %w", resultErr)
	}
	return nil
}

func newManager(ctx context.Context) *manager {
	actx, acancel := context.WithCancel(ctx)
	return &manager{
		ctx:     actx,
		cancel:  acancel,
		cancels: make(map[string]context.CancelFunc),
	}
}

type manager struct {
	ctx     context.Context
	cancel  context.CancelFunc
	cancels map[string]context.CancelFunc
	mu      sync.Mutex
}

func (m *manager) add(id string) context.Context {
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

func (m *manager) stop(id string) {
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

func (m *manager) has(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.cancels[id]
	return ok
}

func (m *manager) delete(id string) {
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

func (m *manager) cancelIfNeeded() {
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

func (m *manager) ids() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []string
	for id := range m.cancels {
		ids = append(ids, id)
	}
	return ids
}
