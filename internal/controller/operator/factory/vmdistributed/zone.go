package vmdistributed

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	"github.com/VictoriaMetrics/operator/internal/podutil"
)

// VMBackend is implemented by *vmv1beta1.VMCluster and *vmv1beta1.VMSingle.
type VMBackend interface {
	metav1.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
	GetRemoteWriteURL() string
}

// vmBackend wraps a VMCluster or VMSingle with zone-level reconcile state.
type vmBackend struct {
	obj         VMBackend
	prevAccepts bool
}

type zones struct {
	httpClient   *http.Client
	vmagents     []*vmv1beta1.VMAgent
	backends     []vmBackend
	hasChanges   []bool
	trafficModes []vmv1alpha1.VMDistributedTrafficMode
}

func (zs *zones) Len() int { return len(zs.vmagents) }

// Apply custom sorting for storage backends and VMAgents updating first not existing or failed zones
// The rest is sorted by observedGeneration in descending order
// If all above is equal then zones are sorted by backend name in ascending order
func (zs *zones) Less(i, j int) bool {
	bi, bj := zs.backends[i].obj, zs.backends[j].obj
	isNonOperationalI := bi.GetStatusMetadata().UpdateStatus != vmv1beta1.UpdateStatusOperational
	isNonOperationalJ := bj.GetStatusMetadata().UpdateStatus != vmv1beta1.UpdateStatusOperational
	if isNonOperationalI != isNonOperationalJ {
		return isNonOperationalI
	}
	isZeroI := bi.GetCreationTimestamp().Time.IsZero()
	isZeroJ := bj.GetCreationTimestamp().Time.IsZero()
	if isZeroI != isZeroJ {
		return isZeroI
	}
	genI := bi.GetStatusMetadata().ObservedGeneration
	genJ := bj.GetStatusMetadata().ObservedGeneration
	if genI != genJ {
		return genI > genJ
	}
	return bi.GetName() < bj.GetName()
}

func (zs *zones) Swap(i, j int) {
	zs.vmagents[i], zs.vmagents[j] = zs.vmagents[j], zs.vmagents[i]
	zs.backends[i], zs.backends[j] = zs.backends[j], zs.backends[i]
	zs.hasChanges[i], zs.hasChanges[j] = zs.hasChanges[j], zs.hasChanges[i]
	zs.trafficModes[i], zs.trafficModes[j] = zs.trafficModes[j], zs.trafficModes[i]
}

func (zs *zones) clusterObjects() []*vmv1beta1.VMCluster {
	result := make([]*vmv1beta1.VMCluster, len(zs.backends))
	for i, b := range zs.backends {
		var ok bool
		result[i], ok = b.obj.(*vmv1beta1.VMCluster)
		if !ok {
			panic(fmt.Sprintf("expected *VMCluster, got %T", b.obj))
		}
	}
	return result
}

func (zs *zones) singleObjects() []*vmv1beta1.VMSingle {
	result := make([]*vmv1beta1.VMSingle, len(zs.backends))
	for i, b := range zs.backends {
		var ok bool
		result[i], ok = b.obj.(*vmv1beta1.VMSingle)
		if !ok {
			panic(fmt.Sprintf("expected *VMSingle, got %T", b.obj))
		}
	}
	return result
}

// getZones builds desired zones
func getZones(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed) (*zones, error) {
	zs := &zones{
		httpClient:   &http.Client{Timeout: httpTimeout},
		vmagents:     make([]*vmv1beta1.VMAgent, len(cr.Spec.Zones)),
		backends:     make([]vmBackend, len(cr.Spec.Zones)),
		hasChanges:   make([]bool, len(cr.Spec.Zones)),
		trafficModes: make([]vmv1alpha1.VMDistributedTrafficMode, len(cr.Spec.Zones)),
	}
	isVMSingle := cr.Spec.BackendType == vmv1alpha1.VMDistributedBackendTypeVMSingle
	for i := range cr.Spec.Zones {
		z := &cr.Spec.Zones[i]
		zs.trafficModes[i] = z.TrafficMode
		var (
			backend   vmBackend
			hasChange bool
			err       error
		)
		if isVMSingle {
			backend, hasChange, err = buildVMSingleBackend(ctx, rclient, cr, z)
		} else {
			backend, hasChange, err = buildVMClusterBackend(ctx, rclient, cr, z)
		}
		if err != nil {
			return nil, fmt.Errorf("spec.zones[%d]: %w", i, err)
		}
		zs.backends[i] = backend
		zs.hasChanges[i] = hasChange
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
				return nil, fmt.Errorf("unexpected error during attempt to get VMAgent=%s: %w", nsn.String(), err)
			}
			vmAgent.ObjectMeta = metav1.ObjectMeta{
				Name:            vmAgentName,
				Namespace:       cr.Namespace,
				OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			}
		}
		vmAgentCustomSpec, err := podutil.MergeSpecs(&cr.Spec.ZoneCommon.VMAgent.Spec, &z.VMAgent.Spec, z.Name)
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
			customRemoteWrite, err := podutil.MergeSpecs(&cr.Spec.ZoneCommon.RemoteWrite, &cz.RemoteWrite, cz.Name)
			if err != nil {
				return nil, fmt.Errorf("spec.zones[%d].vmagent: failed to build remote write for zone[%d]: %w", i, j, err)
			}
			remoteWrite, err := customRemoteWrite.ToVMAgentRemoteWriteSpec()
			if err != nil {
				return nil, fmt.Errorf("spec.zones[%d].vmagent: spec.zones[%d].remoteWrite: %w", i, j, err)
			}
			remoteWrite.URL = zs.backends[j].obj.GetRemoteWriteURL()
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

func buildVMClusterBackend(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, z *vmv1alpha1.VMDistributedZone) (vmBackend, bool, error) {
	nsn := types.NamespacedName{
		Name:      z.VMClusterName(cr),
		Namespace: cr.Namespace,
	}
	var vmCluster vmv1beta1.VMCluster
	if err := rclient.Get(ctx, nsn, &vmCluster); err != nil {
		if !k8serrors.IsNotFound(err) {
			return vmBackend{}, false, fmt.Errorf("unexpected error during attempt to get VMCluster=%s: %w", nsn.String(), err)
		}
		vmCluster.ObjectMeta = metav1.ObjectMeta{
			Name:            z.VMClusterName(cr),
			Namespace:       cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		}
	}
	vmClusterSpec, err := podutil.MergeSpecs(&cr.Spec.ZoneCommon.VMCluster.Spec, &z.VMCluster.Spec, z.Name)
	if err != nil {
		return vmBackend{}, false, fmt.Errorf("vmcluster.spec: %w", err)
	}
	prevClusterSpec := vmCluster.Spec
	vmCluster.Spec = *vmClusterSpec
	rclient.Scheme().Default(&vmCluster)
	// for maintenance and read-only modes setting VMInsert replicas down to 0 to enable VMAgent buffering
	if (z.TrafficMode == vmv1alpha1.VMDistributedTrafficModeReadOnly || z.TrafficMode == vmv1alpha1.VMDistributedTrafficModeMaintenance) && vmCluster.Spec.VMInsert != nil {
		vmCluster.Spec.VMInsert.ReplicaCount = ptr.To(int32(0))
	}
	hasChange := !equality.Semantic.DeepEqual(&vmCluster.Spec, &prevClusterSpec)
	return vmBackend{
		obj:         &vmCluster,
		prevAccepts: prevClusterSpec.VMInsert != nil && ptr.Deref(prevClusterSpec.VMInsert.ReplicaCount, 1) > 0,
	}, hasChange, nil
}

func buildVMSingleBackend(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, z *vmv1alpha1.VMDistributedZone) (vmBackend, bool, error) {
	nsn := types.NamespacedName{
		Name:      z.VMSingleName(cr),
		Namespace: cr.Namespace,
	}
	var vmSingle vmv1beta1.VMSingle
	if err := rclient.Get(ctx, nsn, &vmSingle); err != nil {
		if !k8serrors.IsNotFound(err) {
			return vmBackend{}, false, fmt.Errorf("unexpected error during attempt to get VMSingle=%s: %w", nsn.String(), err)
		}
		vmSingle.ObjectMeta = metav1.ObjectMeta{
			Name:            z.VMSingleName(cr),
			Namespace:       cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		}
	}
	var commonSpec *vmv1beta1.VMSingleSpec
	if cr.Spec.ZoneCommon.VMSingle != nil {
		commonSpec = cr.Spec.ZoneCommon.VMSingle.Spec
	}
	if commonSpec == nil {
		commonSpec = &vmv1beta1.VMSingleSpec{}
	}
	var zoneSpec *vmv1beta1.VMSingleSpec
	if z.VMSingle != nil {
		zoneSpec = z.VMSingle.Spec
	}
	if zoneSpec == nil {
		zoneSpec = &vmv1beta1.VMSingleSpec{}
	}
	vmSingleSpec, err := podutil.MergeSpecs(commonSpec, zoneSpec, z.Name)
	if err != nil {
		return vmBackend{}, false, fmt.Errorf("vmsingle.spec: %w", err)
	}
	prevSingleSpec := vmSingle.Spec
	vmSingle.Spec = *vmSingleSpec
	rclient.Scheme().Default(&vmSingle)
	hasChange := !equality.Semantic.DeepEqual(&vmSingle.Spec, &prevSingleSpec)
	// VMSingle accepts writes if it already exists.
	// Traffic mode changes are handled via LB exclusion in buildVMAuthLB;
	// prevAccepts only controls whether to drain the VMAgent PQ before excluding.
	return vmBackend{
		obj:         &vmSingle,
		prevAccepts: !vmSingle.CreationTimestamp.IsZero(),
	}, hasChange, nil
}

func (zs *zones) upgrade(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, i int) error {
	ctx, cancel := context.WithTimeout(ctx, cr.Spec.ZoneCommon.ReadyTimeout.Duration)
	defer cancel()

	owner := cr.AsOwner()
	vmAgent := zs.vmagents[i]
	backend := zs.backends[i]
	item := fmt.Sprintf("%d/%d", i+1, len(cr.Spec.Zones))

	backendCreated := !backend.obj.GetCreationTimestamp().Time.IsZero()
	// backend or vmAgent have been created
	needsLBUpdate := backendCreated && !vmAgent.CreationTimestamp.IsZero()
	// No backend or vmagent spec changes required
	if !zs.hasChanges[i] {
		needsLBUpdate = false
	}

	if needsLBUpdate {
		if backend.prevAccepts {
			// wait for empty persistent queue before excluding from LB
			zs.waitForEmptyPQ(ctx, rclient, defaultMetricsCheckInterval, i)
			if ctx.Err() != nil {
				return fmt.Errorf("zone=%s: failed to wait till backend=%s queue is empty", item, backend.obj.GetName())
			}
		}

		// excluding zone from VMAuth LB
		if err := zs.updateLB(ctx, rclient, cr, i); err != nil {
			return fmt.Errorf("zone=%s: failed to update VMAuth LB with excluded backend=%s: %w", item, backend.obj.GetName(), err)
		}
	}

	// reconcile storage backend — only type switch in upgrade
	var newAcceptsWrites bool
	switch obj := backend.obj.(type) {
	case *vmv1beta1.VMCluster:
		newAcceptsWrites = obj.Spec.VMInsert != nil && ptr.Deref(obj.Spec.VMInsert.ReplicaCount, 1) > 0
		nsnCluster := types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}
		logger.WithContext(ctx).Info("reconciling VMCluster", "item", item, "name", nsnCluster)
		if err := reconcile.VMCluster(ctx, rclient, obj, nil, &owner); err != nil {
			return fmt.Errorf("zone=%s: failed to reconcile VMCluster=%s: %w", item, nsnCluster.String(), err)
		}
	case *vmv1beta1.VMSingle:
		mode := zs.trafficModes[i]
		newAcceptsWrites = mode != vmv1alpha1.VMDistributedTrafficModeReadOnly && mode != vmv1alpha1.VMDistributedTrafficModeMaintenance
		nsnSingle := types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}
		logger.WithContext(ctx).Info("reconciling VMSingle", "item", item, "name", nsnSingle)
		if err := reconcile.VMSingle(ctx, rclient, obj, nil, &owner); err != nil {
			return fmt.Errorf("zone=%s: failed to reconcile VMSingle=%s: %w", item, nsnSingle.String(), err)
		}
	}

	// reconcile VMAgent
	nsnAgent := types.NamespacedName{Name: vmAgent.Name, Namespace: vmAgent.Namespace}
	if err := reconcile.VMAgent(ctx, rclient, vmAgent, nil, &owner); err != nil {
		return fmt.Errorf("zone=%s: failed to reconcile VMAgent=%s: %w", item, nsnAgent.String(), err)
	}

	if newAcceptsWrites {
		// wait for empty persistent queue before restoring in LB
		zs.waitForEmptyPQ(ctx, rclient, defaultMetricsCheckInterval, i)
		if ctx.Err() != nil {
			return fmt.Errorf("zone=%s: failed to wait till VMAgent queue for backend=%s is drained", item, backend.obj.GetName())
		}
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
	vmAuth := buildVMAuthLB(cr, zs, excludeIds...)
	if vmAuth == nil {
		return nil
	}
	return reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner)
}

var vmAgentQueueMetricQueries = []podutil.MetricQuery{
	{Name: vmAgentQueueMetricName, Dimension: "url"},
}

func (zs *zones) waitForEmptyPQ(ctx context.Context, rclient client.Client, interval time.Duration, clusterIdx int) {
	backendURL := zs.backends[clusterIdx].obj.GetRemoteWriteURL()

	backendName := zs.backends[clusterIdx].obj.GetName()
	nsnCluster := types.NamespacedName{Name: backendName, Namespace: zs.vmagents[clusterIdx].Namespace}
	logger.WithContext(ctx).Info("ensuring persistent queues are drained", "name", nsnCluster.String())

	pollMetrics := func(pctx context.Context, nsn types.NamespacedName, addr string) error {
		return wait.PollUntilContextCancel(pctx, interval, true, func(ctx context.Context) (done bool, err error) {
			// Query each discovered ip. If any returns non-zero metric, continue polling.
			var metrics map[string]map[string]float64
			if metrics, err = podutil.FetchMetricsValues(ctx, zs.httpClient, addr, vmAgentQueueMetricQueries); err != nil {
				logger.WithContext(ctx).Error(err, "attempt to get metrics failed", "url", addr, "name", nsn.String())
				// Treat fetch errors as transient -> not ready, continue polling.
				return false, nil
			}
			for u, v := range metrics[vmAgentQueueMetricName] {
				if u != backendURL {
					continue
				}
				if v > 0 {
					logger.WithContext(ctx).V(1).Info("persistent queue on VMAgent instance is not ready", "url", addr, "name", nsn.String(), "size", v)
					return false, nil
				}
			}
			logger.WithContext(ctx).V(1).Info("all persistent queues on VMAgent for given cluster were drained", "url", addr, "name", nsn.String())
			return true, nil
		})
	}

	var wg sync.WaitGroup
	for i := range zs.vmagents {
		vmAgent := zs.vmagents[i]
		if vmAgent.CreationTimestamp.IsZero() {
			continue
		}
		nsn := types.NamespacedName{
			Name:      vmAgent.Name,
			Namespace: vmAgent.Namespace,
		}
		m := podutil.NewManager(ctx)
		wg.Go(func() {
			wait.UntilWithContext(m.Ctx(), func(ctx context.Context) {
				addrs := podutil.GetMetricsAddrs(ctx, rclient, vmAgent)
				for addr := range addrs {
					if m.Has(addr) {
						continue
					}
					logger.WithContext(ctx).V(1).Info("start polling metrics from VMAgent instance", "url", addr, "name", nsn.String())
					pctx := m.Add(addr)
					wg.Go(func() {
						if err := pollMetrics(pctx, nsn, addr); err != nil {
							return
						}
						m.Stop(addr)
					})
				}
				for _, addr := range m.IDs() {
					if _, ok := addrs[addr]; !ok {
						logger.WithContext(ctx).V(1).Info("stop polling metrics from VMAgent instance", "url", addr, "name", nsn.String())
						m.Delete(addr)
					}
				}
			}, defaultEndpointsUpdateInterval)
		})
	}
	wg.Wait()

	if ctx.Err() == nil {
		logger.WithContext(ctx).Info("all persistent queues were drained", "name", nsnCluster.String())
	}
}
