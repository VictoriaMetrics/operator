package migrate

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/podutil"
)

var clusterComponentKinds = []vmv1beta1.ClusterComponent{
	vmv1beta1.ClusterComponentStorage,
	vmv1beta1.ClusterComponentSelect,
	vmv1beta1.ClusterComponentInsert,
}

// clusterCR is the shape Cluster needs from a cluster-mode target CR.
type clusterCR interface {
	client.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
	PrefixedName(kind vmv1beta1.ClusterComponent) string
	SelectorLabels(kind vmv1beta1.ClusterComponent) map[string]string
	GetRemoteWriteURL() string
}

// ClusterEngine bundles the per-component and buffer-agent behaviors Cluster needs.
type ClusterEngine[Cluster clusterCR, Agent bufferAgentCR] struct {
	ComponentPrefix     string
	ComponentEnabled    func(target Cluster, kind vmv1beta1.ClusterComponent) bool
	PVCTemplateName     func(target Cluster, kind vmv1beta1.ClusterComponent) string
	QueueMetricName     string
	BuildAgent          func(name, namespace, writeURL, bufferSize string) (Agent, error)
	AgentMatches        func(existing Agent, wantURL string) bool
	StorageReplicaCount func(target Cluster) int32
}

func (e ClusterEngine[Cluster, Agent]) componentLabel(kind vmv1beta1.ClusterComponent) string {
	return e.ComponentPrefix + string(kind)
}

func requiresServiceCutover(kind vmv1beta1.ClusterComponent) bool {
	return kind != vmv1beta1.ClusterComponentStorage
}

type discoveredComponent struct {
	kind       vmv1beta1.ClusterComponent
	d          *Discovered
	sts        *appsv1.StatefulSet
	resuming   bool
	baseLabels map[string]string
}

func findComponent(discovered []discoveredComponent, kind vmv1beta1.ClusterComponent) (discoveredComponent, bool) {
	for _, dc := range discovered {
		if dc.kind == kind {
			return dc, true
		}
	}
	return discoveredComponent{}, false
}

func specOf(obj client.Object) any {
	return reflect.ValueOf(obj).Elem().FieldByName("Spec").Interface()
}

func forceMergeComponentPods(ctx context.Context, c client.Client, httpClient *http.Client, namespace, serviceName, portName, pathPrefix string, containers []corev1.Container) {
	if hasForceMergeAuthKey(containers) {
		fmt.Printf("warning: %s sets -forceMergeAuthKey, which this tool cannot supply — skipping force-merge (continuing, snapshot will still be crash-consistent)\n", serviceName)
		return
	}
	addrs, err := podutil.DiscoverEndpointAddrs(ctx, c, namespace, serviceName, portName, "http", pathPrefix+"/internal/force_merge")
	if err != nil {
		fmt.Printf("warning: cannot discover %s pods for force-merge (continuing, snapshots will still be crash-consistent): %v\n", serviceName, err)
		return
	}
	if len(addrs) == 0 {
		fmt.Printf("warning: no ready pod endpoints found for %s (port %q) to force-merge — check the Service actually has a port named %q if this is unexpected (continuing, snapshots will still be crash-consistent)\n", serviceName, portName, portName)
		return
	}
	for addr := range addrs {
		if err := forceMerge(ctx, httpClient, addr); err != nil {
			fmt.Printf("warning: force-merge failed for %s (continuing, snapshot will still be crash-consistent): %v\n", addr, err)
		}
	}
}

func snapshotClusterPVCs(ctx context.Context, c client.Client, pvcs []corev1.PersistentVolumeClaim, targetPVCTemplateName, targetWorkloadName, targetNamespace, snapshotClassName string) error {
	byOrdinal, err := pvcsByOrdinal(pvcs)
	if err != nil {
		return err
	}
	for i := 0; i < len(byOrdinal); i++ {
		pvc, ok := byOrdinal[i]
		if !ok {
			return fmt.Errorf("PVC ordinals are not contiguous from 0 (missing ordinal %d among %d discovered PVCs) — refusing to guess a partial snapshot", i, len(pvcs))
		}
		if pvc.Namespace != targetNamespace {
			return fmt.Errorf("cannot restore PVC %s/%s into namespace %q: cross-namespace snapshot restore isn't supported", pvc.Namespace, pvc.Name, targetNamespace)
		}
		snapName := pvc.Name + "-migration-snapshot"
		targetName := fmt.Sprintf("%s-%s-%d", targetPVCTemplateName, targetWorkloadName, i)
		if err := deleteStaleRestoredPVC(ctx, c, types.NamespacedName{Name: targetName, Namespace: targetNamespace}, snapName); err != nil {
			return err
		}
		if err := createVolumeSnapshot(ctx, c, pvc.Namespace, snapName, pvc.Name, snapshotClassName); err != nil {
			return fmt.Errorf("cannot create VolumeSnapshot for PVC %q: %w", pvc.Name, err)
		}
		if err := waitVolumeSnapshotReady(ctx, c, pvc.Namespace, snapName); err != nil {
			return fmt.Errorf("PVC %q: %w", pvc.Name, err)
		}
		newPVC := newPVCFromSnapshot(targetName, targetNamespace, snapName, pvc)
		if err := c.Create(ctx, newPVC); err != nil {
			return fmt.Errorf("cannot create PVC %q from snapshot: %w", targetName, err)
		}
	}
	return nil
}

func isFromVolumeSnapshot(ds *corev1.TypedLocalObjectReference, snapshotName string) bool {
	return ds != nil && ds.Kind == "VolumeSnapshot" && ds.Name == snapshotName &&
		ds.APIGroup != nil && *ds.APIGroup == VolumeSnapshotGVK.Group
}

func deleteStaleRestoredPVC(ctx context.Context, c client.Client, nsn types.NamespacedName, sourceSnapshotName string) error {
	var existing corev1.PersistentVolumeClaim
	err := c.Get(ctx, nsn, &existing)
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot check for a stale PVC %s: %w", nsn, err)
	}
	if !isFromVolumeSnapshot(existing.Spec.DataSource, sourceSnapshotName) {
		return fmt.Errorf("PVC %s already exists and wasn't created by this migration's snapshot restore — refusing to delete it; remove it manually if it's a stale leftover", nsn)
	}
	if err := deleteAndAwaitGone(ctx, c, &corev1.PersistentVolumeClaim{}, nsn); err != nil {
		return fmt.Errorf("cannot delete stale PVC %s: %w", nsn, err)
	}
	return nil
}

func verifyRestoredPVC(ctx context.Context, c client.Client, nsn types.NamespacedName, snapshotName string) error {
	var pvc corev1.PersistentVolumeClaim
	if err := c.Get(ctx, nsn, &pvc); err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("PVC %s does not exist — refusing to adopt the existing target CR without a migration-restored PVC", nsn)
		}
		return fmt.Errorf("cannot check PVC %s: %w", nsn, err)
	}
	if !isFromVolumeSnapshot(pvc.Spec.DataSource, snapshotName) {
		return fmt.Errorf("PVC %s exists but wasn't restored from this migration's snapshot %q — refusing to adopt the existing target CR; delete it manually before retrying", nsn, snapshotName)
	}
	return nil
}

func verifyRestoredClusterPVCs(ctx context.Context, c client.Client, pvcs []corev1.PersistentVolumeClaim, targetPVCTemplateName, targetWorkloadName, targetNamespace string) error {
	byOrdinal, err := pvcsByOrdinal(pvcs)
	if err != nil {
		return err
	}
	for i := 0; i < len(byOrdinal); i++ {
		pvc, ok := byOrdinal[i]
		if !ok {
			return fmt.Errorf("PVC ordinals are not contiguous from 0 (missing ordinal %d among %d discovered PVCs)", i, len(pvcs))
		}
		snapName := pvc.Name + "-migration-snapshot"
		targetName := fmt.Sprintf("%s-%s-%d", targetPVCTemplateName, targetWorkloadName, i)
		if err := verifyRestoredPVC(ctx, c, types.NamespacedName{Name: targetName, Namespace: targetNamespace}, snapName); err != nil {
			return err
		}
	}
	return nil
}

func deleteAndAwaitGone(ctx context.Context, c client.Client, obj client.Object, nsn types.NamespacedName) error {
	obj.SetName(nsn.Name)
	obj.SetNamespace(nsn.Namespace)
	if err := c.Delete(ctx, obj); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	return wait.PollUntilContextTimeout(ctx, PVCDeletePollInterval, PVCDeleteTimeout, true, func(ctx context.Context) (bool, error) {
		getErr := c.Get(ctx, nsn, obj)
		if getErr == nil {
			return false, nil
		}
		if k8serrors.IsNotFound(getErr) {
			return true, nil
		}
		return false, getErr
	})
}

// Cluster runs a multi-component cluster-mode chart migration, shared across engines and both
// strategies via a buffer agent in front of insert (see the plan messages below for how each
// strategy handles old workloads, reads, and writes from connections established before
// cutover).
func Cluster[Cluster clusterCR, Agent bufferAgentCR](ctx context.Context, c client.Client, httpClient *http.Client, opts Options, target Cluster, engine ClusterEngine[Cluster, Agent], noDowntime bool) error {
	if !noDowntime {
		if err := refuseExistingTarget(ctx, c, target); err != nil {
			return err
		}
	}
	if !engine.ComponentEnabled(target, vmv1beta1.ClusterComponentInsert) {
		return fmt.Errorf("target has no insert component configured — migration requires one to safely buffer writes through")
	}
	targetWriteURL := target.GetRemoteWriteURL()
	agentName := target.PrefixedName(vmv1beta1.ClusterComponentInsert) + "-migration-buffer"
	agent, err := engine.BuildAgent(agentName, target.GetNamespace(), targetWriteURL, opts.AgentBufferSize)
	if err != nil {
		return fmt.Errorf("cannot build buffer agent spec: %w", err)
	}
	agentSelectorLabels := agent.SelectorLabels()

	discovered := make([]discoveredComponent, 0, len(clusterComponentKinds))
	for _, kind := range clusterComponentKinds {
		if !engine.ComponentEnabled(target, kind) {
			continue
		}
		helmLabel := engine.componentLabel(kind)
		d, err := discoverComponent(ctx, c, opts.Namespace, opts.ReleaseName, helmLabel)
		if err != nil {
			return fmt.Errorf("discovery failed for component %q: %w", helmLabel, err)
		}
		if d.Deployment == nil && len(d.StatefulSets) == 0 {
			if noDowntime {
				return fmt.Errorf("component %q: no Deployment or StatefulSet found — nothing to migrate reads from", helmLabel)
			}
			dc := discoveredComponent{kind: kind, d: d, resuming: true}
			if requiresServiceCutover(kind) {
				switch len(d.Services) {
				case 0:
					return fmt.Errorf("component %q: old workload is already gone (resuming a previous attempt), but no Service was discovered to cut over — nothing safe to do automatically; repoint it at %v manually", helmLabel, target.SelectorLabels(kind))
				case 1:
				default:
					// Without the workload's own pod labels (it's gone), there's no way to tell
					// which discovered Service was actually the workload's vs. an unrelated one
					// sharing its Helm labels.
					return fmt.Errorf("component %q: old workload is already gone (resuming a previous attempt) and %d Services were discovered — can't tell which one to cut over without the workload's own pod labels to check against; repoint the right one at %v manually", helmLabel, len(d.Services), target.SelectorLabels(kind))
				}
			}
			discovered = append(discovered, dc)
			continue
		}
		if d.Deployment != nil && len(d.StatefulSets) > 0 {
			return fmt.Errorf("component %q matched both a Deployment and %d StatefulSet(s) — this shape isn't supported", helmLabel, len(d.StatefulSets))
		}
		dc := discoveredComponent{kind: kind, d: d}
		if d.Deployment != nil {
			if len(d.PVCs) > 0 {
				return fmt.Errorf("component %q is a Deployment with %d existing PVC(s), which this tool cannot rebind — refusing to proceed and orphan data", helmLabel, len(d.PVCs))
			}
			if noDowntime && kind == vmv1beta1.ClusterComponentStorage {
				return fmt.Errorf("component %q: storage must be deployed as a StatefulSet for NoDowntime (found a Deployment) — NoDowntime snapshots per-ordinal PVCs, which a Deployment-shaped storage component doesn't have", helmLabel)
			}
			if kind == vmv1beta1.ClusterComponentInsert {
				if err := rejectTLSEnabled(d.Deployment.Namespace, d.Deployment.Name, d.Deployment.Spec.Template.Spec.Containers); err != nil {
					return err
				}
			}
			dc.baseLabels = d.Deployment.Spec.Selector.MatchLabels
		} else {
			sts, err := d.SingleStatefulSet()
			if err != nil {
				return fmt.Errorf("component %q: %w", helmLabel, err)
			}
			if kind == vmv1beta1.ClusterComponentInsert {
				if err := rejectTLSEnabled(sts.Namespace, sts.Name, sts.Spec.Template.Spec.Containers); err != nil {
					return err
				}
			}
			if !noDowntime && sts.Spec.PersistentVolumeClaimRetentionPolicy != nil && sts.Spec.PersistentVolumeClaimRetentionPolicy.WhenDeleted == appsv1.DeletePersistentVolumeClaimRetentionPolicyType {
				return fmt.Errorf("component %q's StatefulSet has persistentVolumeClaimRetentionPolicy.whenDeleted=Delete — deleting it could delete its PVCs before they're rebound, losing data; change the policy to Retain before migrating", helmLabel)
			}
			if len(d.PVCs) > 0 && engine.PVCTemplateName(target, kind) == "" && (!noDowntime || kind == vmv1beta1.ClusterComponentStorage) {
				return fmt.Errorf("component %q has %d existing PVC(s) but the target CR has no storage configured for it — refusing to proceed and orphan data", helmLabel, len(d.PVCs))
			}
			if kind == vmv1beta1.ClusterComponentStorage {
				if len(d.PVCs) == 0 {
					return fmt.Errorf("no PersistentVolumeClaims found for storage component in namespace %q — migrate doesn't support a PVC-less (e.g. emptyDir) storage source", opts.Namespace)
				}
				if wantReplicas := engine.StorageReplicaCount(target); int(wantReplicas) != len(d.PVCs) {
					return fmt.Errorf("target storage replicaCount (%d) doesn't match the %d discovered source PVC(s) — migrating a topology change silently would leave some shards' data orphaned in unused PVCs or produce missing/empty ordinals; set replicaCount to %d or migrate the topology change separately", wantReplicas, len(d.PVCs), len(d.PVCs))
				}
			}
			if noDowntime && kind == vmv1beta1.ClusterComponentStorage {
				if len(d.Services) != 1 {
					return fmt.Errorf("found %d Services for storage component, expected exactly 1", len(d.Services))
				}
			}
			dc.sts = sts
			dc.baseLabels = sts.Spec.Selector.MatchLabels
		}
		if requiresServiceCutover(kind) && !hasCutoverCandidate(d.Services, dc.baseLabels) &&
			(kind != vmv1beta1.ClusterComponentInsert || !hasCutoverCandidate(d.Services, agentSelectorLabels)) {
			return fmt.Errorf("component %q has no Service whose selector matches its own pod labels — nothing to cut over; repoint it at %v manually", helmLabel, target.SelectorLabels(kind))
		}
		discovered = append(discovered, dc)
	}

	targetAlreadyExists := false
	if noDowntime {
		existsCheck := target.DeepCopyObject().(Cluster)
		getErr := c.Get(ctx, types.NamespacedName{Name: target.GetName(), Namespace: target.GetNamespace()}, existsCheck)
		switch {
		case getErr == nil:
			targetAlreadyExists = true
			if !reflect.DeepEqual(specOf(existsCheck), specOf(target)) {
				return fmt.Errorf("target CR %s/%s already exists with a different spec — refusing to adopt it; delete it manually before retrying", target.GetNamespace(), target.GetName())
			}
			if storageDC, ok := findComponent(discovered, vmv1beta1.ClusterComponentStorage); ok {
				pvcTemplateName := engine.PVCTemplateName(target, vmv1beta1.ClusterComponentStorage)
				workloadName := target.PrefixedName(vmv1beta1.ClusterComponentStorage)
				if err := verifyRestoredClusterPVCs(ctx, c, storageDC.d.PVCs, pvcTemplateName, workloadName, target.GetNamespace()); err != nil {
					return err
				}
			}
		case !k8serrors.IsNotFound(getErr):
			return fmt.Errorf("cannot check for an existing target CR %s/%s: %w", target.GetNamespace(), target.GetName(), getErr)
		}
	}

	if !targetAlreadyExists {
		for _, dc := range discovered {
			if dc.resuming || len(dc.d.PVCs) == 0 {
				continue
			}
			pvcTemplateName := engine.PVCTemplateName(target, dc.kind)
			if pvcTemplateName == "" {
				continue
			}
			workloadName := target.PrefixedName(dc.kind)
			byOrdinal, err := pvcsByOrdinal(dc.d.PVCs)
			if err != nil {
				return fmt.Errorf("component %q: %w", engine.componentLabel(dc.kind), err)
			}
			for i := 0; i < len(byOrdinal); i++ {
				if _, ok := byOrdinal[i]; !ok {
					return fmt.Errorf("component %q: PVC ordinals are not contiguous from 0 (missing ordinal %d among %d discovered PVCs)", engine.componentLabel(dc.kind), i, len(dc.d.PVCs))
				}
				stubName := fmt.Sprintf("%s-%s-%d", pvcTemplateName, workloadName, i)
				stub := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: stubName, Namespace: target.GetNamespace()},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
						},
					},
				}
				if err := dryRunCreate(ctx, c, stub); err != nil && !k8serrors.IsAlreadyExists(err) {
					return fmt.Errorf("component %q: derived PVC name %q would be rejected by the API server: %w", engine.componentLabel(dc.kind), stubName, err)
				}
			}
		}
	}

	if noDowntime {
		fmt.Printf("plan: deploy a buffer agent pointed directly at the target's future insert endpoint, redirect insert's Service(s) to it, "+
			"force-merge + snapshot storage's PVC(s), create %s %s/%s from the snapshots, then cut insert/select traffic over to it "+
			"once ready — old workloads and all PVCs are left untouched throughout. select keeps serving reads from the pre-snapshot "+
			"data for the entire backfill window — buffered writes only become visible to reads once select is cut over at the very "+
			"end. A client with a connection already established to an old insert pod — directly, or routed through the Service — "+
			"can keep writing to it past the cutover below, since a selector change only affects new connections; that write is "+
			"captured only if it lands before storage's force-merge/snapshot above, an unavoidable limit of any Service-based "+
			"cutover while the old workload keeps running\n",
			target.GetObjectKind().GroupVersionKind().Kind, target.GetNamespace(), target.GetName())
	} else {
		fmt.Printf("plan: deploy a buffer agent pointed directly at the target's future insert endpoint and redirect insert's Service(s) to it; "+
			"then, for each of %d cluster components, delete the old workload and rebind any PVCs under the operator's naming, "+
			"then create %s %s/%s and repoint each component's Service(s) at the new pods — reads are unavailable for the whole window "+
			"since old storage and select are deleted rather than kept running. A client with a connection already established to an "+
			"old insert pod — directly, or routed through the Service — can keep writing to it past the cutover below, since a "+
			"selector change only affects new connections; unlike NoDowntime, that write isn't lost as long as it lands before the "+
			"old storage pod it eventually reaches has actually terminated, since its PVC is rebound (not snapshotted) under the "+
			"target — only a write attempted after that pod is fully gone fails outright, visibly, at the client\n",
			len(discovered), target.GetObjectKind().GroupVersionKind().Kind, target.GetNamespace(), target.GetName())
	}
	if !targetAlreadyExists {
		if err := dryRunCreate(ctx, c, target); err != nil {
			return fmt.Errorf("target CR %s/%s would be rejected by the API server: %w", target.GetNamespace(), target.GetName(), err)
		}
	}

	if opts.DryRun {
		fmt.Println("dry-run: stopping before any mutation")
		return nil
	}
	if !opts.Yes {
		if !confirm("proceed with the above plan?") {
			return fmt.Errorf("aborted by user")
		}
	}

	var insertSvcPtrs []*corev1.Service
	for i := range discovered {
		if discovered[i].kind != vmv1beta1.ClusterComponentInsert {
			continue
		}
		for j := range discovered[i].d.Services {
			selector := discovered[i].d.Services[j].Spec.Selector
			matchesBase := discovered[i].baseLabels != nil && !selectorDiffersFromBase(selector, discovered[i].baseLabels)
			matchesAgent := !selectorDiffersFromBase(selector, agentSelectorLabels)
			matchesTarget := !selectorDiffersFromBase(selector, target.SelectorLabels(vmv1beta1.ClusterComponentInsert))
			if !matchesBase && !matchesAgent && !matchesTarget {
				continue
			}
			insertSvcPtrs = append(insertSvcPtrs, &discovered[i].d.Services[j])
		}
	}
	if len(insertSvcPtrs) == 0 {
		return fmt.Errorf("insert component has no Service whose selector matches its own pod labels — nothing to redirect to the buffer agent; repoint it at %v manually", target.SelectorLabels(vmv1beta1.ClusterComponentInsert))
	}

	agent, err = ensureBufferAgentRunning(ctx, c, agent, targetWriteURL, engine.AgentMatches)
	if err != nil {
		return err
	}
	if err := cutoverServices(ctx, c, insertSvcPtrs, agent.SelectorLabels()); err != nil {
		return fmt.Errorf("cannot redirect incoming writes to the buffer agent: %w", err)
	}
	if noDowntime {
		time.Sleep(WriteQuiesceGracePeriod)
	}

	var storagePVCs []corev1.PersistentVolumeClaim
	var storagePVCTemplateName, storageWorkloadName string
	if dc, ok := findComponent(discovered, vmv1beta1.ClusterComponentStorage); ok {
		storagePVCTemplateName = engine.PVCTemplateName(target, dc.kind)
		storageWorkloadName = target.PrefixedName(dc.kind)
	}
	for _, dc := range discovered {
		helmLabel := engine.componentLabel(dc.kind)
		d := dc.d
		switch {
		case dc.resuming:
			if len(d.PVCs) == 0 {
				fmt.Printf("component %q: old workload already gone, assuming a previous attempt already rebound its PVCs — skipping deletion\n", helmLabel)
				continue
			}
			if engine.PVCTemplateName(target, dc.kind) == "" {
				return fmt.Errorf("component %q has %d PVC(s) left un-rebound by an interrupted previous attempt, but the target CR has no storage configured for it — refusing to proceed and orphan data", helmLabel, len(d.PVCs))
			}
			fmt.Printf("component %q: old workload already gone but %d PVC(s) weren't rebound by a previous attempt — resuming rebind\n", helmLabel, len(d.PVCs))
			if err := finishInterruptedRebind(ctx, c, d.PVCs, engine.PVCTemplateName(target, dc.kind), target.PrefixedName(dc.kind), target.GetNamespace()); err != nil {
				return fmt.Errorf("component %q (writes are safely buffered on disk): %w", helmLabel, err)
			}
		case noDowntime:
			if dc.kind == vmv1beta1.ClusterComponentStorage {
				storagePVCs = d.PVCs
				forceMergeComponentPods(ctx, c, httpClient, opts.Namespace, d.Services[0].Name, "http", httpPathPrefix(dc.sts.Spec.Template.Spec.Containers), dc.sts.Spec.Template.Spec.Containers)
			}
		case d.Deployment != nil:
			dependentConfigMaps, dependentSecrets := dependentConfigsOf(&d.Deployment.Spec.Template.Spec, d.ConfigMaps, d.Secrets)
			if err := c.Delete(ctx, d.Deployment); err != nil && !k8serrors.IsNotFound(err) {
				return fmt.Errorf("cannot delete old Deployment for component %q (writes are safely buffered on disk): %w", helmLabel, err)
			}
			if err := waitPodsGone(ctx, c, d.Deployment.Namespace, d.Deployment.Spec.Selector.MatchLabels); err != nil {
				return fmt.Errorf("component %q: %w", helmLabel, err)
			}
			if err := deleteDependentConfigs(ctx, c, dependentConfigMaps, dependentSecrets); err != nil {
				return err
			}
		default:
			sts := dc.sts
			dependentConfigMaps, dependentSecrets := dependentConfigsOf(&sts.Spec.Template.Spec, d.ConfigMaps, d.Secrets)
			if err := c.Delete(ctx, sts); err != nil && !k8serrors.IsNotFound(err) {
				return fmt.Errorf("cannot delete old StatefulSet for component %q (writes are safely buffered on disk): %w", helmLabel, err)
			}
			if err := waitPodsGone(ctx, c, sts.Namespace, sts.Spec.Selector.MatchLabels); err != nil {
				return fmt.Errorf("component %q: %w", helmLabel, err)
			}
			if err := deleteDependentConfigs(ctx, c, dependentConfigMaps, dependentSecrets); err != nil {
				return err
			}
			if len(d.PVCs) == 0 {
				continue
			}
			if err := rebindClusterPVCs(ctx, c, d.PVCs, engine.PVCTemplateName(target, dc.kind), target.PrefixedName(dc.kind), target.GetNamespace()); err != nil {
				return fmt.Errorf("component %q (writes are safely buffered on disk): %w", helmLabel, err)
			}
		}
	}

	if !noDowntime && storageWorkloadName != "" {
		if err := verifyRebindComplete(ctx, c, engine.StorageReplicaCount(target), storagePVCTemplateName, storageWorkloadName, target.GetNamespace()); err != nil {
			return fmt.Errorf("storage PVC rebind incomplete (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
		}
	}

	if noDowntime {
		existingTarget := target.DeepCopyObject().(Cluster)
		getErr := c.Get(ctx, types.NamespacedName{Name: target.GetName(), Namespace: target.GetNamespace()}, existingTarget)
		switch {
		case getErr == nil:
			target = existingTarget
		case k8serrors.IsNotFound(getErr):
			if err := snapshotClusterPVCs(ctx, c, storagePVCs, storagePVCTemplateName, storageWorkloadName, target.GetNamespace(), opts.SnapshotClassName); err != nil {
				return fmt.Errorf("storage snapshot failed (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
			}
			if err := c.Create(ctx, target); err != nil {
				if !k8serrors.IsAlreadyExists(err) {
					return fmt.Errorf("cannot create target CR %s/%s (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", target.GetNamespace(), target.GetName(), err)
				}
				existing := target.DeepCopyObject().(Cluster)
				if err := c.Get(ctx, types.NamespacedName{Name: target.GetName(), Namespace: target.GetNamespace()}, existing); err != nil {
					return fmt.Errorf("cannot fetch existing target CR %s/%s: %w", target.GetNamespace(), target.GetName(), err)
				}
				if !reflect.DeepEqual(specOf(existing), specOf(target)) {
					return fmt.Errorf("target CR %s/%s already exists with a different spec — refusing to adopt it; delete it manually before retrying", target.GetNamespace(), target.GetName())
				}
				target = existing
			}
		default:
			return fmt.Errorf("cannot check for existing target CR %s/%s: %w", target.GetNamespace(), target.GetName(), getErr)
		}
	} else if err := c.Create(ctx, target); err != nil {
		return fmt.Errorf("cannot create target CR %s/%s (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", target.GetNamespace(), target.GetName(), err)
	}
	if err := waitForOperational(ctx, c, target); err != nil {
		return fmt.Errorf("target CR did not become ready (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
	}

	if err := waitEndpointsReady(ctx, c, target.GetNamespace(), target.PrefixedName(vmv1beta1.ClusterComponentInsert)); err != nil {
		return fmt.Errorf("component %q's target Service did not become ready: %w", engine.componentLabel(vmv1beta1.ClusterComponentInsert), err)
	}
	if err := cutoverServices(ctx, c, insertSvcPtrs, target.SelectorLabels(vmv1beta1.ClusterComponentInsert)); err != nil {
		return fmt.Errorf("final insert traffic cutover failed: %w", err)
	}

	time.Sleep(WriteQuiesceGracePeriod)
	if err := waitQueueDrained(ctx, c, httpClient, agent, engine.QueueMetricName, 0); err != nil {
		return fmt.Errorf("buffer agent queue did not fully drain after final cutover — leaving it running so buffered writes aren't lost; investigate and drain it manually, or re-run this command: %w", err)
	}

	for _, dc := range discovered {
		if dc.kind == vmv1beta1.ClusterComponentInsert {
			continue
		}
		helmLabel := engine.componentLabel(dc.kind)
		if requiresServiceCutover(dc.kind) {
			if err := waitEndpointsReady(ctx, c, target.GetNamespace(), target.PrefixedName(dc.kind)); err != nil {
				return fmt.Errorf("component %q's target Service did not become ready: %w", helmLabel, err)
			}
		}
		if noDowntime && !requiresServiceCutover(dc.kind) {
			continue
		}
		var svcPtrs []*corev1.Service
		for i := range dc.d.Services {
			if dc.baseLabels != nil && selectorDiffersFromBase(dc.d.Services[i].Spec.Selector, dc.baseLabels) {
				fmt.Printf("warning: Service %s/%s has a selector that doesn't match component %q's own pod labels, skipping cutover — update it manually\n",
					dc.d.Services[i].Namespace, dc.d.Services[i].Name, helmLabel)
				continue
			}
			svcPtrs = append(svcPtrs, &dc.d.Services[i])
		}
		if requiresServiceCutover(dc.kind) && len(svcPtrs) == 0 {
			return fmt.Errorf("component %q has no Service left to cut over — repoint it at %v manually", helmLabel, target.SelectorLabels(dc.kind))
		}
		if err := cutoverServices(ctx, c, svcPtrs, target.SelectorLabels(dc.kind)); err != nil {
			return fmt.Errorf("traffic cutover failed for component %q: %w", helmLabel, err)
		}
	}

	deleteBufferAgent(ctx, c, agent)

	fmt.Printf("migration complete — the release's insert/select Service(s) now point at %s %s/%s, but they're still tracked by Helm release %q: "+
		"running `helm uninstall` deletes them, taking down whichever endpoint clients are still using them through. Move clients to the "+
		"target's own Service(s) (e.g. %s/%s) first, then decommission the release once nothing depends on the old Service(s) anymore\n",
		target.GetObjectKind().GroupVersionKind().Kind, target.GetNamespace(), target.GetName(), opts.ReleaseName,
		target.GetNamespace(), target.PrefixedName(vmv1beta1.ClusterComponentInsert))
	if noDowntime {
		fmt.Println("note: old workloads and all PVCs were left untouched")
	}
	return nil
}

func rebindClusterPVCs(ctx context.Context, c client.Client, pvcs []corev1.PersistentVolumeClaim, targetPVCTemplateName, targetWorkloadName, targetNamespace string) error {
	byOrdinal, err := pvcsByOrdinal(pvcs)
	if err != nil {
		return err
	}
	for i := 0; i < len(byOrdinal); i++ {
		pvc, ok := byOrdinal[i]
		if !ok {
			return fmt.Errorf("PVC ordinals are not contiguous from 0 (missing ordinal %d among %d discovered PVCs) — refusing to guess a partial rebind", i, len(pvcs))
		}
		targetName := fmt.Sprintf("%s-%s-%d", targetPVCTemplateName, targetWorkloadName, i)
		if _, err := rebindPVC(ctx, c, pvc, targetName, targetNamespace); err != nil {
			return fmt.Errorf("cannot rebind PVC %q (ordinal %d): %w", pvc.Name, i, err)
		}
	}
	return nil
}

func finishInterruptedRebind(ctx context.Context, c client.Client, pvcs []corev1.PersistentVolumeClaim, targetPVCTemplateName, targetWorkloadName, targetNamespace string) error {
	for i := range pvcs {
		pvc := &pvcs[i]
		idx := strings.LastIndex(pvc.Name, "-")
		if idx < 0 {
			return fmt.Errorf("PVC %q doesn't look like a StatefulSet-managed PVC (no ordinal suffix)", pvc.Name)
		}
		ordinal, err := strconv.Atoi(pvc.Name[idx+1:])
		if err != nil {
			return fmt.Errorf("cannot parse ordinal from PVC %q: %w", pvc.Name, err)
		}
		targetName := fmt.Sprintf("%s-%s-%d", targetPVCTemplateName, targetWorkloadName, ordinal)
		if _, err := rebindPVC(ctx, c, pvc, targetName, targetNamespace); err != nil {
			return fmt.Errorf("cannot resume rebind of PVC %q (ordinal %d): %w", pvc.Name, ordinal, err)
		}
	}
	return nil
}

func verifyRebindComplete(ctx context.Context, c client.Client, wantReplicas int32, targetPVCTemplateName, targetWorkloadName, targetNamespace string) error {
	for i := 0; i < int(wantReplicas); i++ {
		targetName := fmt.Sprintf("%s-%s-%d", targetPVCTemplateName, targetWorkloadName, i)
		var pvc corev1.PersistentVolumeClaim
		if err := c.Get(ctx, types.NamespacedName{Name: targetName, Namespace: targetNamespace}, &pvc); err != nil {
			if k8serrors.IsNotFound(err) {
				return fmt.Errorf("target PVC %s/%s (ordinal %d) is missing — a previous attempt's rebind didn't complete; investigate manually before retrying", targetNamespace, targetName, i)
			}
			return fmt.Errorf("cannot check target PVC %s/%s: %w", targetNamespace, targetName, err)
		}
		if _, ok := pvc.Annotations[reboundFromAnnotation]; !ok {
			return fmt.Errorf("target PVC %s/%s (ordinal %d) exists but wasn't created by this migration's rebind — refusing to proceed and risk using unrelated or empty storage", targetNamespace, targetName, i)
		}
	}
	return nil
}

func pvcsByOrdinal(pvcs []corev1.PersistentVolumeClaim) (map[int]*corev1.PersistentVolumeClaim, error) {
	byOrdinal := make(map[int]*corev1.PersistentVolumeClaim, len(pvcs))
	for i := range pvcs {
		pvc := &pvcs[i]
		idx := strings.LastIndex(pvc.Name, "-")
		if idx < 0 {
			return nil, fmt.Errorf("PVC %q doesn't look like a StatefulSet-managed PVC (no ordinal suffix)", pvc.Name)
		}
		ordinal, err := strconv.Atoi(pvc.Name[idx+1:])
		if err != nil {
			return nil, fmt.Errorf("cannot parse ordinal from PVC %q: %w", pvc.Name, err)
		}
		if existing, ok := byOrdinal[ordinal]; ok {
			return nil, fmt.Errorf("PVCs %q and %q both resolved to ordinal %d — this component likely has multiple volumeClaimTemplates, which migration doesn't support yet", existing.Name, pvc.Name, ordinal)
		}
		byOrdinal[ordinal] = pvc
	}
	return byOrdinal, nil
}
