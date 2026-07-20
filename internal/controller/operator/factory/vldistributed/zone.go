package vldistributed

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

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	"github.com/VictoriaMetrics/operator/internal/podutil"
)

// VLBackend is implemented by *vmv1.VLCluster and *vmv1.VLSingle.
type VLBackend interface {
	metav1.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
	GetRemoteWriteURL() string
}

// vlBackend wraps a VLCluster or VLSingle with zone-level reconcile state.
type vlBackend struct {
	obj         VLBackend
	prevAccepts bool
}

type zones struct {
	httpClient   *http.Client
	vlagents     []*vmv1.VLAgent
	backends     []vlBackend
	hasChanges   []bool
	trafficModes []vmv1alpha1.VLDistributedTrafficMode
}

func (zs *zones) Len() int { return len(zs.vlagents) }

// Apply custom sorting for storage backends and VLAgents updating first not existing or failed zones
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
	zs.vlagents[i], zs.vlagents[j] = zs.vlagents[j], zs.vlagents[i]
	zs.backends[i], zs.backends[j] = zs.backends[j], zs.backends[i]
	zs.hasChanges[i], zs.hasChanges[j] = zs.hasChanges[j], zs.hasChanges[i]
	zs.trafficModes[i], zs.trafficModes[j] = zs.trafficModes[j], zs.trafficModes[i]
}

func (zs *zones) clusterObjects() []*vmv1.VLCluster {
	result := make([]*vmv1.VLCluster, len(zs.backends))
	for i, b := range zs.backends {
		var ok bool
		result[i], ok = b.obj.(*vmv1.VLCluster)
		if !ok {
			panic(fmt.Sprintf("expected *VLCluster, got %T", b.obj))
		}
	}
	return result
}

func (zs *zones) singleObjects() []*vmv1.VLSingle {
	result := make([]*vmv1.VLSingle, len(zs.backends))
	for i, b := range zs.backends {
		var ok bool
		result[i], ok = b.obj.(*vmv1.VLSingle)
		if !ok {
			panic(fmt.Sprintf("expected *VLSingle, got %T", b.obj))
		}
	}
	return result
}

// getZones builds desired zones
func getZones(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VLDistributed) (*zones, error) {
	zs := &zones{
		httpClient:   &http.Client{Timeout: httpTimeout},
		vlagents:     make([]*vmv1.VLAgent, len(cr.Spec.Zones)),
		backends:     make([]vlBackend, len(cr.Spec.Zones)),
		hasChanges:   make([]bool, len(cr.Spec.Zones)),
		trafficModes: make([]vmv1alpha1.VLDistributedTrafficMode, len(cr.Spec.Zones)),
	}
	isVLSingle := cr.Spec.BackendType == vmv1alpha1.VLDistributedBackendTypeVLSingle
	for i := range cr.Spec.Zones {
		z := &cr.Spec.Zones[i]
		zs.trafficModes[i] = z.TrafficMode
		var (
			backend   vlBackend
			hasChange bool
			err       error
		)
		if isVLSingle {
			backend, hasChange, err = buildVLSingleBackend(ctx, rclient, cr, z)
		} else {
			backend, hasChange, err = buildVLClusterBackend(ctx, rclient, cr, z)
		}
		if err != nil {
			return nil, fmt.Errorf("spec.zones[%d]: %w", i, err)
		}
		zs.backends[i] = backend
		zs.hasChanges[i] = hasChange
	}

	for i := range cr.Spec.Zones {
		z := &cr.Spec.Zones[i]
		vlAgentName := z.VLAgentName(cr)
		nsn := types.NamespacedName{
			Name:      vlAgentName,
			Namespace: cr.Namespace,
		}
		var vlAgent vmv1.VLAgent
		if err := rclient.Get(ctx, nsn, &vlAgent); err != nil {
			if !k8serrors.IsNotFound(err) {
				return nil, fmt.Errorf("unexpected error during attempt to get VLAgent=%s: %w", nsn.String(), err)
			}
			vlAgent.ObjectMeta = metav1.ObjectMeta{
				Name:            vlAgentName,
				Namespace:       cr.Namespace,
				OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			}
		}
		vlAgentCustomSpec, err := podutil.MergeSpecs(&cr.Spec.ZoneCommon.VLAgent.Spec, &z.VLAgent.Spec, z.Name)
		if err != nil {
			return nil, fmt.Errorf("spec.zones[%d].vlagent.spec: %w", i, err)
		}
		vlAgentSpec, err := vlAgentCustomSpec.ToVLAgentSpec()
		if err != nil {
			return nil, fmt.Errorf("spec.zones[%d].vlagent.spec: %w", i, err)
		}
		if vlAgentSpec.RemoteWriteSettings == nil {
			vlAgentSpec.RemoteWriteSettings = &vmv1.VLAgentRemoteWriteSettings{}
		}
		vlAgentSpec.RemoteWrite = vlAgentSpec.RemoteWrite[:0]
		for j := range cr.Spec.Zones {
			cz := &cr.Spec.Zones[j]
			customRemoteWrite, err := podutil.MergeSpecs(&cr.Spec.ZoneCommon.RemoteWrite, &cz.RemoteWrite, cz.Name)
			if err != nil {
				return nil, fmt.Errorf("spec.zones[%d].vlagent: failed to build remote write for zone[%d]: %w", i, j, err)
			}
			backendURL := zs.backends[j].obj.GetRemoteWriteURL()
			remoteWrite, err := customRemoteWrite.ToVLAgentRemoteWriteSpec(backendURL)
			if err != nil {
				return nil, fmt.Errorf("spec.zones[%d].vlagent: spec.zones[%d].remoteWrite: %w", i, j, err)
			}
			vlAgentSpec.RemoteWrite = append(vlAgentSpec.RemoteWrite, *remoteWrite)
		}
		// sorting VLAgent's remote write configurations
		orderMap := make(map[string]int)
		for j, rw := range vlAgent.Spec.RemoteWrite {
			orderMap[rw.URL] = j
		}
		sort.SliceStable(vlAgentSpec.RemoteWrite, func(k, j int) bool {
			idxK, okK := orderMap[vlAgentSpec.RemoteWrite[k].URL]
			idxJ, okJ := orderMap[vlAgentSpec.RemoteWrite[j].URL]
			if !okK {
				return false
			}
			if !okJ {
				return true
			}
			return idxK < idxJ
		})
		prevAgentSpec := vlAgent.Spec
		vlAgent.Spec = *vlAgentSpec
		rclient.Scheme().Default(&vlAgent)
		zs.hasChanges[i] = zs.hasChanges[i] || !equality.Semantic.DeepEqual(&vlAgent.Spec, &prevAgentSpec)
		zs.vlagents[i] = &vlAgent
	}

	sort.Sort(zs)
	return zs, nil
}

func buildVLClusterBackend(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VLDistributed, z *vmv1alpha1.VLDistributedZone) (vlBackend, bool, error) {
	nsn := types.NamespacedName{
		Name:      z.VLClusterName(cr),
		Namespace: cr.Namespace,
	}
	var vlCluster vmv1.VLCluster
	if err := rclient.Get(ctx, nsn, &vlCluster); err != nil {
		if !k8serrors.IsNotFound(err) {
			return vlBackend{}, false, fmt.Errorf("unexpected error during attempt to get VLCluster=%s: %w", nsn.String(), err)
		}
		vlCluster.ObjectMeta = metav1.ObjectMeta{
			Name:            z.VLClusterName(cr),
			Namespace:       cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		}
	}
	vlClusterSpec, err := podutil.MergeSpecs(&cr.Spec.ZoneCommon.VLCluster.Spec, &z.VLCluster.Spec, z.Name)
	if err != nil {
		return vlBackend{}, false, fmt.Errorf("vlcluster.spec: %w", err)
	}
	prevClusterSpec := vlCluster.Spec
	vlCluster.Spec = *vlClusterSpec
	rclient.Scheme().Default(&vlCluster)
	// for maintenance and read-only modes setting VLInsert replicas down to 0 to enable VLAgent buffering
	if (z.TrafficMode == vmv1alpha1.VLDistributedTrafficModeReadOnly || z.TrafficMode == vmv1alpha1.VLDistributedTrafficModeMaintenance) && vlCluster.Spec.VLInsert != nil {
		vlCluster.Spec.VLInsert.ReplicaCount = ptr.To(int32(0))
	}
	hasChange := !equality.Semantic.DeepEqual(&vlCluster.Spec, &prevClusterSpec)
	return vlBackend{
		obj:         &vlCluster,
		prevAccepts: prevClusterSpec.VLInsert != nil && ptr.Deref(prevClusterSpec.VLInsert.ReplicaCount, 1) > 0,
	}, hasChange, nil
}

func buildVLSingleBackend(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VLDistributed, z *vmv1alpha1.VLDistributedZone) (vlBackend, bool, error) {
	nsn := types.NamespacedName{
		Name:      z.VLSingleName(cr),
		Namespace: cr.Namespace,
	}
	var vlSingle vmv1.VLSingle
	if err := rclient.Get(ctx, nsn, &vlSingle); err != nil {
		if !k8serrors.IsNotFound(err) {
			return vlBackend{}, false, fmt.Errorf("unexpected error during attempt to get VLSingle=%s: %w", nsn.String(), err)
		}
		vlSingle.ObjectMeta = metav1.ObjectMeta{
			Name:            z.VLSingleName(cr),
			Namespace:       cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		}
	}
	var commonSpec *vmv1.VLSingleSpec
	if cr.Spec.ZoneCommon.VLSingle != nil {
		commonSpec = cr.Spec.ZoneCommon.VLSingle.Spec
	}
	if commonSpec == nil {
		commonSpec = &vmv1.VLSingleSpec{}
	}
	var zoneSpec *vmv1.VLSingleSpec
	if z.VLSingle != nil {
		zoneSpec = z.VLSingle.Spec
	}
	if zoneSpec == nil {
		zoneSpec = &vmv1.VLSingleSpec{}
	}
	vlSingleSpec, err := podutil.MergeSpecs(commonSpec, zoneSpec, z.Name)
	if err != nil {
		return vlBackend{}, false, fmt.Errorf("vlsingle.spec: %w", err)
	}
	prevSingleSpec := vlSingle.Spec
	vlSingle.Spec = *vlSingleSpec
	rclient.Scheme().Default(&vlSingle)
	hasChange := !equality.Semantic.DeepEqual(&vlSingle.Spec, &prevSingleSpec)
	return vlBackend{
		obj:         &vlSingle,
		prevAccepts: !vlSingle.CreationTimestamp.IsZero(),
	}, hasChange, nil
}

func (zs *zones) upgrade(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VLDistributed, i int) error {
	ctx, cancel := context.WithTimeout(ctx, cr.Spec.ZoneCommon.ReadyTimeout.Duration)
	defer cancel()

	owner := cr.AsOwner()
	vlAgent := zs.vlagents[i]
	backend := zs.backends[i]
	item := fmt.Sprintf("%d/%d", i+1, len(cr.Spec.Zones))

	backendCreated := !backend.obj.GetCreationTimestamp().Time.IsZero()
	// backend or vlagent have been created
	needsLBUpdate := backendCreated && !vlAgent.CreationTimestamp.IsZero()
	// No backend or vlagent spec changes required
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

	// reconcile storage backend
	switch obj := backend.obj.(type) {
	case *vmv1.VLCluster:
		nsnCluster := types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}
		logger.WithContext(ctx).Info("reconciling VLCluster", "item", item, "name", nsnCluster)
		if err := reconcile.VLCluster(ctx, rclient, obj, nil, &owner); err != nil {
			return fmt.Errorf("zone=%s: failed to reconcile VLCluster=%s: %w", item, nsnCluster.String(), err)
		}
	case *vmv1.VLSingle:
		nsnSingle := types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}
		logger.WithContext(ctx).Info("reconciling VLSingle", "item", item, "name", nsnSingle)
		if err := reconcile.VLSingle(ctx, rclient, obj, nil, &owner); err != nil {
			return fmt.Errorf("zone=%s: failed to reconcile VLSingle=%s: %w", item, nsnSingle.String(), err)
		}
	}

	// reconcile VLAgent
	nsnAgent := types.NamespacedName{Name: vlAgent.Name, Namespace: vlAgent.Namespace}
	if err := reconcile.VLAgent(ctx, rclient, vlAgent, nil, &owner); err != nil {
		return fmt.Errorf("zone=%s: failed to reconcile VLAgent=%s: %w", item, nsnAgent.String(), err)
	}

	mode := zs.trafficModes[i]
	var newAcceptsWrites bool
	switch obj := backend.obj.(type) {
	case *vmv1.VLCluster:
		newAcceptsWrites = obj.Spec.VLInsert != nil && ptr.Deref(obj.Spec.VLInsert.ReplicaCount, 1) > 0
	case *vmv1.VLSingle:
		newAcceptsWrites = mode != vmv1alpha1.VLDistributedTrafficModeReadOnly && mode != vmv1alpha1.VLDistributedTrafficModeMaintenance
	}
	if newAcceptsWrites {
		// wait for empty persistent queue before restoring in LB
		zs.waitForEmptyPQ(ctx, rclient, defaultMetricsCheckInterval, i)
		if ctx.Err() != nil {
			return fmt.Errorf("zone=%s: failed to wait till VLAgent queue for backend=%s is drained", item, backend.obj.GetName())
		}
	}

	// restore zone in VMAuth LB
	if err := zs.updateLB(ctx, rclient, cr); err != nil {
		return fmt.Errorf("zone=%s: failed to update VMAuth LB: %w", item, err)
	}

	return nil
}

func (zs *zones) updateLB(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VLDistributed, excludeIds ...int) error {
	owner := cr.AsOwner()
	if !isVMAuthEnabled(cr) {
		return nil
	}
	vmAuth := buildVMAuthLB(cr, zs, excludeIds...)
	if vmAuth == nil {
		return nil
	}
	return reconcile.VMAuth(ctx, rclient, vmAuth, nil, &owner)
}

func isVMAuthEnabled(cr *vmv1alpha1.VLDistributed) bool {
	if cr.Spec.VMAuth.Enabled == nil {
		return true
	}
	return *cr.Spec.VMAuth.Enabled
}

func (zs *zones) waitForEmptyPQ(ctx context.Context, rclient client.Client, interval time.Duration, clusterIdx int) {
	backendURL := zs.backends[clusterIdx].obj.GetRemoteWriteURL()

	backendName := zs.backends[clusterIdx].obj.GetName()
	nsnCluster := types.NamespacedName{Name: backendName, Namespace: zs.vlagents[clusterIdx].Namespace}
	logger.WithContext(ctx).Info("ensuring persistent queues are drained", "name", nsnCluster.String())

	pollMetrics := func(pctx context.Context, nsn types.NamespacedName, addr string) error {
		return wait.PollUntilContextCancel(pctx, interval, true, func(ctx context.Context) (done bool, err error) {
			var metricValues map[string]float64
			if metricValues, err = podutil.FetchMetricValues(ctx, zs.httpClient, addr, vlAgentQueueMetricName, "url"); err != nil {
				logger.WithContext(ctx).Error(err, "attempt to get metrics failed", "url", addr, "name", nsn.String())
				return false, nil
			}
			for u, v := range metricValues {
				if u != backendURL {
					continue
				}
				if v > 0 {
					logger.WithContext(ctx).V(1).Info("persistent queue on VLAgent instance is not ready", "url", addr, "name", nsn.String(), "size", v)
					return false, nil
				}
			}
			logger.WithContext(ctx).V(1).Info("all persistent queues on VLAgent for given backend were drained", "url", addr, "name", nsn.String())
			return true, nil
		})
	}

	var wg sync.WaitGroup
	for i := range zs.vlagents {
		vlAgent := zs.vlagents[i]
		if vlAgent.CreationTimestamp.IsZero() {
			continue
		}
		nsn := types.NamespacedName{
			Name:      vlAgent.Name,
			Namespace: vlAgent.Namespace,
		}
		m := podutil.NewManager(ctx)
		wg.Go(func() {
			wait.UntilWithContext(m.Ctx(), func(ctx context.Context) {
				addrs := podutil.GetMetricsAddrs(ctx, rclient, vlAgent)
				for addr := range addrs {
					if m.Has(addr) {
						continue
					}
					logger.WithContext(ctx).V(1).Info("start polling metrics from VLAgent instance", "url", addr, "name", nsn.String())
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
						logger.WithContext(ctx).V(1).Info("stop polling metrics from VLAgent instance", "url", addr, "name", nsn.String())
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
