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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
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
	BuildAgent          func(name, namespace, writeURL, bufferSize, pathPrefix string) (Agent, error)
	BuildAgentWriteURL  func(target Cluster, remoteWriteURL string) string
	AgentMatches        func(existing, want Agent) bool
	StorageReplicaCount func(target Cluster) int32
	StorageUsesHPA      func(target Cluster) bool
	StorageMaxReplicas  func(target Cluster) int32
}

func (e ClusterEngine[Cluster, Agent]) componentLabel(kind vmv1beta1.ClusterComponent) string {
	return e.ComponentPrefix + string(kind)
}

func requiresServiceCutover(kind vmv1beta1.ClusterComponent) bool {
	return kind != vmv1beta1.ClusterComponentStorage
}

type discoveredComponent struct {
	kind      vmv1beta1.ClusterComponent
	d         *Discovered
	sts       *appsv1.StatefulSet
	resuming  bool
	podLabels map[string]string
}

func findComponent(discovered []discoveredComponent, kind vmv1beta1.ClusterComponent) (discoveredComponent, bool) {
	for _, dc := range discovered {
		if dc.kind == kind {
			return dc, true
		}
	}
	return discoveredComponent{}, false
}

func cutoverCandidateServices(dc discoveredComponent, targetSelectorLabels, agentSelectorLabels map[string]string, acceptWhenBaseLabelsNil bool) []*corev1.Service {
	var candidates []*corev1.Service
	for i := range dc.d.Services {
		selector := dc.d.Services[i].Spec.Selector
		matchesBase := dc.podLabels != nil && selectorMatchesBase(selector, dc.podLabels)
		matchesTarget := !selectorDiffersFromBase(selector, targetSelectorLabels)
		matchesAgent := dc.kind == vmv1beta1.ClusterComponentInsert && !selectorDiffersFromBase(selector, agentSelectorLabels)
		acceptsNilBase := dc.podLabels == nil && acceptWhenBaseLabelsNil && dc.d.Services[i].Annotations[serviceCutoverAnnotation] == "true"
		if !matchesBase && !matchesTarget && !matchesAgent && !acceptsNilBase {
			continue
		}
		candidates = append(candidates, &dc.d.Services[i])
	}
	return candidates
}

func selectorMatchesSiblingPods(selector map[string]string, discovered []discoveredComponent, own vmv1beta1.ClusterComponent) (vmv1beta1.ClusterComponent, bool) {
	if len(selector) == 0 {
		return "", false
	}
	sel := labels.SelectorFromValidatedSet(selector)
	for _, sibling := range discovered {
		if sibling.kind == own || sibling.podLabels == nil {
			continue
		}
		if sel.Matches(labels.Set(sibling.podLabels)) {
			return sibling.kind, true
		}
	}
	return "", false
}

func specOf(obj client.Object) any {
	return reflect.ValueOf(obj).Elem().FieldByName("Spec").Interface()
}

func verifySameSpec(existing, target client.Object) error {
	if !reflect.DeepEqual(specOf(existing), specOf(target)) {
		return fmt.Errorf("target CR %s/%s already exists with a different spec — refusing to adopt it; delete it manually before retrying", target.GetNamespace(), target.GetName())
	}
	return nil
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
		if err := deleteStaleRestoredPVC(ctx, c, types.NamespacedName{Name: targetName, Namespace: targetNamespace}, pvc.Namespace, snapName, pvc.Name); err != nil {
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

func deleteStaleRestoredPVC(ctx context.Context, c client.Client, nsn types.NamespacedName, snapshotNamespace, sourceSnapshotName, sourcePVCName string) error {
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
	snapNsn := types.NamespacedName{Name: sourceSnapshotName, Namespace: snapshotNamespace}
	snap := &unstructured.Unstructured{}
	snap.SetGroupVersionKind(VolumeSnapshotGVK)
	if err := c.Get(ctx, snapNsn, snap); err != nil {
		return fmt.Errorf("cannot verify source VolumeSnapshot %s referenced by stale PVC %s: %w", snapNsn, nsn, err)
	}
	if actual, _, _ := unstructured.NestedString(snap.Object, "spec", "source", "persistentVolumeClaimName"); actual != sourcePVCName {
		return fmt.Errorf("PVC %s references VolumeSnapshot %s, but that snapshot was taken from PVC %q, not %q — refusing to delete what may be an unrelated, coincidentally-named PVC; remove it manually if it's a stale leftover", nsn, snapNsn, actual, sourcePVCName)
	}
	if err := deleteAndAwaitGone(ctx, c, &corev1.PersistentVolumeClaim{}, nsn); err != nil {
		return fmt.Errorf("cannot delete stale PVC %s: %w", nsn, err)
	}
	return nil
}

func verifyRestoredPVC(ctx context.Context, c client.Client, nsn types.NamespacedName, snapshotNamespace, snapshotName, sourcePVCName string) error {
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
	snapNsn := types.NamespacedName{Name: snapshotName, Namespace: snapshotNamespace}
	snap := &unstructured.Unstructured{}
	snap.SetGroupVersionKind(VolumeSnapshotGVK)
	if err := c.Get(ctx, snapNsn, snap); err != nil {
		return fmt.Errorf("cannot verify VolumeSnapshot %s referenced by PVC %s: %w", snapNsn, nsn, err)
	}
	if actual, _, _ := unstructured.NestedString(snap.Object, "spec", "source", "persistentVolumeClaimName"); actual != sourcePVCName {
		return fmt.Errorf("PVC %s references VolumeSnapshot %s, but that snapshot was taken from PVC %q, not %q — refusing to adopt the existing target CR; delete it manually before retrying", nsn, snapNsn, actual, sourcePVCName)
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
		if err := verifyRestoredPVC(ctx, c, types.NamespacedName{Name: targetName, Namespace: targetNamespace}, pvc.Namespace, snapName, pvc.Name); err != nil {
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
	m := &clusterMigration[Cluster, Agent]{
		ctx: ctx, c: c, httpClient: httpClient, opts: opts, target: target, engine: engine, noDowntime: noDowntime,
	}
	return m.run()
}

// clusterMigration holds the state threaded through one Cluster run, so the otherwise linear,
// single-pass procedure can be split into named phases without a growing parameter list.
type clusterMigration[Cluster clusterCR, Agent bufferAgentCR] struct {
	ctx        context.Context
	c          client.Client
	httpClient *http.Client
	opts       Options
	target     Cluster
	engine     ClusterEngine[Cluster, Agent]
	noDowntime bool

	targetWriteURL      string
	agent               Agent
	agentSelectorLabels map[string]string

	discovered []discoveredComponent

	targetAlreadyExists bool
	insertSvcPtrs       []*corev1.Service

	storagePVCs            []corev1.PersistentVolumeClaim
	storagePVCTemplateName string
	storageWorkloadName    string
	storageSourceCount     int

	allDependentConfigMaps []corev1.ConfigMap
	allDependentSecrets    []corev1.Secret
}

func (m *clusterMigration[Cluster, Agent]) run() error {
	if err := runPhases(m.validateTarget, m.resolveBufferAgent, m.discoverComponents, m.validateSiblingServices, m.checkExistingTarget, m.preflightPVCNames); err != nil {
		return err
	}

	m.printPlan()
	stop, err := m.dryRunAndConfirm()
	if err != nil {
		return err
	}
	if stop {
		return nil
	}

	if err := runPhases(m.resolveInsertServices, m.cutoverWrites, m.prepareStorageInfo, m.deleteOldWorkloadsAndRebind, m.verifyRebind,
		m.createOrAdoptTarget, m.waitReadyAndCleanupConfigs, m.finalInsertCutover, m.drainQueue, m.finalComponentCutover); err != nil {
		return err
	}
	m.cleanupAndPrintCompletion()
	return nil
}

func (m *clusterMigration[Cluster, Agent]) validateTarget() error {
	if m.target.GetNamespace() != m.opts.Namespace {
		return fmt.Errorf("target %s/%s is in a different namespace than the Helm release %q — cross-namespace migration isn't supported: PVC snapshot data sources and Service selectors can't cross namespaces", m.target.GetNamespace(), m.target.GetName(), m.opts.Namespace)
	}
	if !m.engine.ComponentEnabled(m.target, vmv1beta1.ClusterComponentInsert) {
		return fmt.Errorf("target has no insert component configured — migration requires one to safely buffer writes through")
	}
	m.targetWriteURL = m.target.GetRemoteWriteURL()
	if strings.HasPrefix(m.targetWriteURL, "https://") {
		return fmt.Errorf("target %s/%s has TLS enabled — the buffer agent has no way to be configured with its certificate automatically, so migration isn't supported for it", m.target.GetNamespace(), m.target.GetName())
	}
	return nil
}

func (m *clusterMigration[Cluster, Agent]) resolveBufferAgent() error {
	insertHelmLabel := m.engine.componentLabel(vmv1beta1.ClusterComponentInsert)
	insertD, err := discoverComponent(m.ctx, m.c, m.opts.Namespace, m.opts.ReleaseName, insertHelmLabel)
	if err != nil {
		return fmt.Errorf("discovery failed for component %q: %w", insertHelmLabel, err)
	}
	agentName := m.target.PrefixedName(vmv1beta1.ClusterComponentInsert) + "-migration-buffer"
	var agentPathPrefix string
	switch {
	case insertD.Deployment != nil:
		p, prefixErr := httpPathPrefix(insertD.Deployment.Spec.Template.Spec.Containers)
		if prefixErr != nil {
			return fmt.Errorf("cannot resolve insert's http.pathPrefix for the buffer agent: %w", prefixErr)
		}
		agentPathPrefix = p
	case len(insertD.StatefulSets) == 1:
		p, prefixErr := httpPathPrefix(insertD.StatefulSets[0].Spec.Template.Spec.Containers)
		if prefixErr != nil {
			return fmt.Errorf("cannot resolve insert's http.pathPrefix for the buffer agent: %w", prefixErr)
		}
		agentPathPrefix = p
	default:
		if probe, buildErr := m.engine.BuildAgent(agentName, m.target.GetNamespace(), m.targetWriteURL, m.opts.AgentBufferSize, ""); buildErr == nil {
			if getErr := m.c.Get(m.ctx, types.NamespacedName{Name: agentName, Namespace: m.target.GetNamespace()}, probe); getErr == nil {
				agentPathPrefix = probe.GetExtraArgs()[HTTPPathPrefixArg]
			}
		}
	}
	agentWriteURL := m.engine.BuildAgentWriteURL(m.target, m.targetWriteURL)
	agent, err := m.engine.BuildAgent(agentName, m.target.GetNamespace(), agentWriteURL, m.opts.AgentBufferSize, agentPathPrefix)
	if err != nil {
		return fmt.Errorf("cannot build buffer agent spec: %w", err)
	}
	m.agent = agent
	m.agentSelectorLabels = agent.SelectorLabels()
	return nil
}

func (m *clusterMigration[Cluster, Agent]) discoverComponents() error {
	discovered := make([]discoveredComponent, 0, len(clusterComponentKinds))
	for _, kind := range clusterComponentKinds {
		helmLabel := m.engine.componentLabel(kind)
		if !m.engine.ComponentEnabled(m.target, kind) {
			d, err := discoverComponent(m.ctx, m.c, m.opts.Namespace, m.opts.ReleaseName, helmLabel)
			if err != nil {
				return fmt.Errorf("discovery failed for component %q: %w", helmLabel, err)
			}
			if d.Deployment != nil || len(d.StatefulSets) > 0 || len(d.PVCs) > 0 || len(d.Services) > 0 {
				return fmt.Errorf("component %q exists in the Helm release but the target CR doesn't have it configured — refusing to leave it (or its PVCs/Services) running unmigrated and orphaned; either configure it in the target or decommission it yourself before migrating", helmLabel)
			}
			continue
		}
		d, err := discoverComponent(m.ctx, m.c, m.opts.Namespace, m.opts.ReleaseName, helmLabel)
		if err != nil {
			return fmt.Errorf("discovery failed for component %q: %w", helmLabel, err)
		}
		if d.Deployment == nil && len(d.StatefulSets) == 0 {
			if m.noDowntime {
				return fmt.Errorf("component %q: no Deployment or StatefulSet found — nothing to migrate reads from", helmLabel)
			}
			dc := discoveredComponent{kind: kind, d: d, resuming: true}
			if requiresServiceCutover(kind) {
				switch len(d.Services) {
				case 0:
					return fmt.Errorf("component %q: old workload is already gone (resuming a previous attempt), but no Service was discovered to cut over — nothing safe to do automatically; repoint it at %v manually", helmLabel, m.target.SelectorLabels(kind))
				case 1:
					if d.Services[0].Annotations[serviceCutoverAnnotation] != "true" {
						return fmt.Errorf("component %q: old workload is already gone (resuming a previous attempt), but Service %s/%s wasn't previously redirected by this tool (missing %q annotation) — refusing to repoint what may be an unrelated, coincidentally-labeled Service; investigate why the old workload is missing, or repoint it manually", helmLabel, d.Services[0].Namespace, d.Services[0].Name, serviceCutoverAnnotation)
					}
				default:
					return fmt.Errorf("component %q: old workload is already gone (resuming a previous attempt) and %d Services were discovered — can't tell which one to cut over without the workload's own pod labels to check against; repoint the right one at %v manually", helmLabel, len(d.Services), m.target.SelectorLabels(kind))
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
			if kind == vmv1beta1.ClusterComponentStorage {
				return fmt.Errorf("component %q: storage must be deployed as a StatefulSet (found a Deployment) — this migration cannot preserve a Deployment-shaped storage component's data", helmLabel)
			}
			if kind == vmv1beta1.ClusterComponentInsert {
				if err := rejectTLSEnabled(d.Deployment.Namespace, d.Deployment.Name, d.Deployment.Spec.Template.Spec.Containers); err != nil {
					return err
				}
				if err := rejectHTTPAuthEnabled(d.Deployment.Namespace, d.Deployment.Name, d.Deployment.Spec.Template.Spec.Containers); err != nil {
					return err
				}
			}
			dc.podLabels = d.Deployment.Spec.Template.Labels
		} else {
			sts, err := d.SingleStatefulSet()
			if err != nil {
				return fmt.Errorf("component %q: %w", helmLabel, err)
			}
			if kind == vmv1beta1.ClusterComponentInsert {
				if err := rejectTLSEnabled(sts.Namespace, sts.Name, sts.Spec.Template.Spec.Containers); err != nil {
					return err
				}
				if err := rejectHTTPAuthEnabled(sts.Namespace, sts.Name, sts.Spec.Template.Spec.Containers); err != nil {
					return err
				}
			}
			if !m.noDowntime && sts.Spec.PersistentVolumeClaimRetentionPolicy != nil && sts.Spec.PersistentVolumeClaimRetentionPolicy.WhenDeleted == appsv1.DeletePersistentVolumeClaimRetentionPolicyType {
				return fmt.Errorf("component %q's StatefulSet has persistentVolumeClaimRetentionPolicy.whenDeleted=Delete — deleting it could delete its PVCs before they're rebound, losing data; change the policy to Retain before migrating", helmLabel)
			}
			if len(d.PVCs) > 0 && m.engine.PVCTemplateName(m.target, kind) == "" && (!m.noDowntime || kind == vmv1beta1.ClusterComponentStorage) {
				return fmt.Errorf("component %q has %d existing PVC(s) but the target CR has no storage configured for it — refusing to proceed and orphan data", helmLabel, len(d.PVCs))
			}
			if kind == vmv1beta1.ClusterComponentStorage {
				if len(d.PVCs) == 0 {
					return fmt.Errorf("no PersistentVolumeClaims found for storage component in namespace %q — migrate doesn't support a PVC-less (e.g. emptyDir) storage source", m.opts.Namespace)
				}
				wantReplicas := m.engine.StorageReplicaCount(m.target)
				switch {
				case m.engine.StorageUsesHPA(m.target):
					if len(d.PVCs) < int(wantReplicas) {
						return fmt.Errorf("target storage's HPA minReplicas (%d) exceeds the %d discovered source PVC(s) — the target wouldn't have enough shards populated from migrated data; lower minReplicas or migrate the topology change separately", wantReplicas, len(d.PVCs))
					}
					if maxReplicas := m.engine.StorageMaxReplicas(m.target); len(d.PVCs) > int(maxReplicas) {
						return fmt.Errorf("target storage's HPA maxReplicas (%d) is below the %d discovered source PVC(s) — the target could never scale up enough to run every rebound shard, leaving some inaccessible; raise maxReplicas to %d or migrate the topology change separately", maxReplicas, len(d.PVCs), len(d.PVCs))
					}
				case int(wantReplicas) != len(d.PVCs):
					return fmt.Errorf("target storage replicaCount (%d) doesn't match the %d discovered source PVC(s) — migrating a topology change silently would leave some shards' data orphaned in unused PVCs or produce missing/empty ordinals; set replicaCount to %d or migrate the topology change separately", wantReplicas, len(d.PVCs), len(d.PVCs))
				}
			} else if len(d.PVCs) > 0 {
				wantReplicas := int32(1)
				if sts.Spec.Replicas != nil {
					wantReplicas = *sts.Spec.Replicas
				}
				if int(wantReplicas) != len(d.PVCs) {
					return fmt.Errorf("component %q: StatefulSet has %d replica(s) but %d PVC(s) were discovered — refusing to proceed with a mismatched count", helmLabel, wantReplicas, len(d.PVCs))
				}
			}
			if m.noDowntime && kind == vmv1beta1.ClusterComponentStorage {
				if len(d.Services) != 1 {
					return fmt.Errorf("found %d Services for storage component, expected exactly 1", len(d.Services))
				}
			}
			for i := range d.PVCs {
				if d.PVCs[i].Status.Phase != corev1.ClaimBound || d.PVCs[i].Spec.VolumeName == "" {
					return fmt.Errorf("component %q: PVC %s/%s is not Bound (phase=%q) — cannot proceed without a bound source volume to rebind or snapshot", helmLabel, d.PVCs[i].Namespace, d.PVCs[i].Name, d.PVCs[i].Status.Phase)
				}
			}
			dc.sts = sts
			dc.podLabels = sts.Spec.Template.Labels
		}
		if requiresServiceCutover(kind) && len(cutoverCandidateServices(dc, m.target.SelectorLabels(kind), m.agentSelectorLabels, false)) == 0 {
			return fmt.Errorf("component %q has no Service whose selector matches its own pod labels — nothing to cut over; repoint it at %v manually", helmLabel, m.target.SelectorLabels(kind))
		}
		if m.noDowntime && requiresServiceCutover(kind) && selectorMatchesBase(m.target.SelectorLabels(kind), dc.podLabels) {
			return fmt.Errorf("component %q: the old workload's pod labels already satisfy target's future pod labels %v, and NoDowntime never deletes the old workload — cutting traffic over would route it to both the old and target pods at once, bypassing the buffer agent; use WithDowntime instead, or give the target different pod labels", helmLabel, m.target.SelectorLabels(kind))
		}
		discovered = append(discovered, dc)
	}
	m.discovered = discovered
	return nil
}

func (m *clusterMigration[Cluster, Agent]) validateSiblingServices() error {
	for _, dc := range m.discovered {
		if !requiresServiceCutover(dc.kind) {
			continue
		}
		acceptWhenBaseLabelsNil := dc.kind != vmv1beta1.ClusterComponentInsert
		for _, svc := range cutoverCandidateServices(dc, m.target.SelectorLabels(dc.kind), m.agentSelectorLabels, acceptWhenBaseLabelsNil) {
			if sibling, ok := selectorMatchesSiblingPods(svc.Spec.Selector, m.discovered, dc.kind); ok {
				return fmt.Errorf("component %q's Service %s/%s has a selector that also matches component %q's pods — refusing to redirect it during cutover and risk affecting unrelated traffic; narrow the Service's selector to this component's own pods, or repoint it manually",
					m.engine.componentLabel(dc.kind), svc.Namespace, svc.Name, m.engine.componentLabel(sibling))
			}
		}
	}
	return nil
}

func (m *clusterMigration[Cluster, Agent]) checkExistingTarget() error {
	existsCheck := m.target.DeepCopyObject().(Cluster)
	getErr := m.c.Get(m.ctx, types.NamespacedName{Name: m.target.GetName(), Namespace: m.target.GetNamespace()}, existsCheck)
	switch {
	case getErr == nil:
		m.targetAlreadyExists = true
		if err := verifySameSpec(existsCheck, m.target); err != nil {
			return err
		}
		if m.noDowntime {
			if storageDC, ok := findComponent(m.discovered, vmv1beta1.ClusterComponentStorage); ok {
				pvcTemplateName := m.engine.PVCTemplateName(m.target, vmv1beta1.ClusterComponentStorage)
				workloadName := m.target.PrefixedName(vmv1beta1.ClusterComponentStorage)
				if err := verifyRestoredClusterPVCs(m.ctx, m.c, storageDC.d.PVCs, pvcTemplateName, workloadName, m.target.GetNamespace()); err != nil {
					return err
				}
			}
		}
	case !k8serrors.IsNotFound(getErr):
		return fmt.Errorf("cannot check for an existing target CR %s/%s: %w", m.target.GetNamespace(), m.target.GetName(), getErr)
	}
	return nil
}

func (m *clusterMigration[Cluster, Agent]) preflightPVCNames() error {
	if m.targetAlreadyExists {
		return nil
	}
	for _, dc := range m.discovered {
		if dc.resuming || len(dc.d.PVCs) == 0 {
			continue
		}
		pvcTemplateName := m.engine.PVCTemplateName(m.target, dc.kind)
		if pvcTemplateName == "" {
			continue
		}
		workloadName := m.target.PrefixedName(dc.kind)
		byOrdinal, err := pvcsByOrdinal(dc.d.PVCs)
		if err != nil {
			return fmt.Errorf("component %q: %w", m.engine.componentLabel(dc.kind), err)
		}
		for i := 0; i < len(byOrdinal); i++ {
			if _, ok := byOrdinal[i]; !ok {
				return fmt.Errorf("component %q: PVC ordinals are not contiguous from 0 (missing ordinal %d among %d discovered PVCs)", m.engine.componentLabel(dc.kind), i, len(dc.d.PVCs))
			}
			stubName := fmt.Sprintf("%s-%s-%d", pvcTemplateName, workloadName, i)
			stub := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: stubName, Namespace: m.target.GetNamespace()},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
					},
				},
			}
			if err := dryRunCreate(m.ctx, m.c, stub); err != nil && !k8serrors.IsAlreadyExists(err) {
				return fmt.Errorf("component %q: derived PVC name %q would be rejected by the API server: %w", m.engine.componentLabel(dc.kind), stubName, err)
			}
		}
	}
	return nil
}

func (m *clusterMigration[Cluster, Agent]) printPlan() {
	if m.noDowntime {
		fmt.Printf("plan: deploy a buffer agent pointed directly at the target's future insert endpoint, redirect insert's Service(s) to it, "+
			"force-merge + snapshot storage's PVC(s), create %s %s/%s from the snapshots, then cut insert/select traffic over to it "+
			"once ready — old workloads and all PVCs are left untouched throughout. select keeps serving reads from the pre-snapshot "+
			"data for the entire backfill window — buffered writes only become visible to reads once select is cut over at the very "+
			"end. A client with a connection already established to an old insert pod — directly, or routed through the Service — "+
			"can keep writing to it past the cutover below, since a selector change only affects new connections; that write is "+
			"captured only if it lands before storage's force-merge/snapshot above, an unavoidable limit of any Service-based "+
			"cutover while the old workload keeps running\n",
			m.target.GetObjectKind().GroupVersionKind().Kind, m.target.GetNamespace(), m.target.GetName())
	} else {
		fmt.Printf("plan: deploy a buffer agent pointed directly at the target's future insert endpoint and redirect insert's Service(s) to it; "+
			"then, for each of %d cluster components, delete the old workload and rebind any PVCs under the operator's naming, "+
			"then create %s %s/%s and repoint each component's Service(s) at the new pods — reads are unavailable for the whole window "+
			"since old storage and select are deleted rather than kept running. A client with a connection already established to an "+
			"old insert pod — directly, or routed through the Service — can keep writing to it past the cutover below, since a "+
			"selector change only affects new connections; unlike NoDowntime, that write isn't lost as long as it lands before the "+
			"old storage pod it eventually reaches has actually terminated, since its PVC is rebound (not snapshotted) under the "+
			"target — only a write attempted after that pod is fully gone fails outright, visibly, at the client\n",
			len(m.discovered), m.target.GetObjectKind().GroupVersionKind().Kind, m.target.GetNamespace(), m.target.GetName())
	}
}

func (m *clusterMigration[Cluster, Agent]) dryRunAndConfirm() (stop bool, err error) {
	if !m.targetAlreadyExists {
		if err := dryRunCreate(m.ctx, m.c, m.target); err != nil {
			return false, fmt.Errorf("target CR %s/%s would be rejected by the API server: %w", m.target.GetNamespace(), m.target.GetName(), err)
		}
	}
	if m.opts.DryRun {
		fmt.Println("dry-run: stopping before any mutation")
		return true, nil
	}
	if !m.opts.Yes && !confirm("proceed with the above plan?") {
		return false, fmt.Errorf("aborted by user")
	}
	return false, nil
}

func (m *clusterMigration[Cluster, Agent]) resolveInsertServices() error {
	var insertSvcPtrs []*corev1.Service
	if insertDC, ok := findComponent(m.discovered, vmv1beta1.ClusterComponentInsert); ok {
		insertSvcPtrs = cutoverCandidateServices(insertDC, m.target.SelectorLabels(vmv1beta1.ClusterComponentInsert), m.agentSelectorLabels, false)
	}
	if len(insertSvcPtrs) == 0 {
		return fmt.Errorf("insert component has no Service whose selector matches its own pod labels — nothing to redirect to the buffer agent; repoint it at %v manually", m.target.SelectorLabels(vmv1beta1.ClusterComponentInsert))
	}
	m.insertSvcPtrs = insertSvcPtrs
	return nil
}

func (m *clusterMigration[Cluster, Agent]) cutoverWrites() error {
	agent, err := ensureBufferAgentRunning(m.ctx, m.c, m.agent, m.targetWriteURL, m.engine.AgentMatches)
	if err != nil {
		return err
	}
	m.agent = agent
	for _, svc := range m.insertSvcPtrs {
		if err := verifyAgentPortCompatible(m.ctx, m.c, svc, m.agent); err != nil {
			return err
		}
	}
	if err := cutoverServices(m.ctx, m.c, m.insertSvcPtrs, m.agent.SelectorLabels()); err != nil {
		return fmt.Errorf("cannot redirect incoming writes to the buffer agent: %w", err)
	}
	if m.noDowntime {
		time.Sleep(WriteQuiesceGracePeriod)
	}
	return nil
}

func (m *clusterMigration[Cluster, Agent]) prepareStorageInfo() error {
	if dc, ok := findComponent(m.discovered, vmv1beta1.ClusterComponentStorage); ok {
		m.storagePVCTemplateName = m.engine.PVCTemplateName(m.target, dc.kind)
		m.storageWorkloadName = m.target.PrefixedName(dc.kind)
		if !dc.resuming {
			m.storageSourceCount = len(dc.d.PVCs)
		}
	}
	return nil
}

func (m *clusterMigration[Cluster, Agent]) deleteOldWorkloadsAndRebind() error {
	for _, dc := range m.discovered {
		helmLabel := m.engine.componentLabel(dc.kind)
		d := dc.d
		switch {
		case dc.resuming:
			if len(d.PVCs) == 0 {
				fmt.Printf("component %q: old workload already gone, assuming a previous attempt already rebound its PVCs — skipping deletion\n", helmLabel)
				if pvcTemplateName := m.engine.PVCTemplateName(m.target, dc.kind); pvcTemplateName != "" {
					namePrefix := pvcTemplateName + "-" + m.target.PrefixedName(dc.kind) + "-"
					if err := ensureClusterReclaimPolicyRestored(m.ctx, m.c, m.target.GetNamespace(), m.target.SelectorLabels(dc.kind), namePrefix); err != nil {
						return fmt.Errorf("component %q: %w", helmLabel, err)
					}
				}
				continue
			}
			if m.engine.PVCTemplateName(m.target, dc.kind) == "" {
				return fmt.Errorf("component %q has %d PVC(s) left un-rebound by an interrupted previous attempt, but the target CR has no storage configured for it — refusing to proceed and orphan data", helmLabel, len(d.PVCs))
			}
			fmt.Printf("component %q: old workload already gone but %d PVC(s) weren't rebound by a previous attempt — resuming rebind\n", helmLabel, len(d.PVCs))
			if err := finishInterruptedRebind(m.ctx, m.c, d.PVCs, m.engine.PVCTemplateName(m.target, dc.kind), m.target.PrefixedName(dc.kind), m.target.GetNamespace(), m.target.SelectorLabels(dc.kind)); err != nil {
				return fmt.Errorf("component %q (writes are safely buffered on disk): %w", helmLabel, err)
			}
		case m.noDowntime:
			if dc.kind == vmv1beta1.ClusterComponentStorage {
				m.storagePVCs = d.PVCs
				if pathPrefix, err := httpPathPrefix(dc.sts.Spec.Template.Spec.Containers); err != nil {
					fmt.Printf("warning: cannot resolve http.pathPrefix for %s (continuing, snapshot will still be crash-consistent): %v\n", d.Services[0].Name, err)
				} else {
					forceMergeComponentPods(m.ctx, m.c, m.httpClient, m.opts.Namespace, d.Services[0].Name, "http", pathPrefix, dc.sts.Spec.Template.Spec.Containers)
				}
			}
		case d.Deployment != nil:
			dependentConfigMaps, dependentSecrets := dependentConfigsOf(&d.Deployment.Spec.Template.Spec, d.ConfigMaps, d.Secrets)
			m.allDependentConfigMaps = append(m.allDependentConfigMaps, dependentConfigMaps...)
			m.allDependentSecrets = append(m.allDependentSecrets, dependentSecrets...)
			if err := m.c.Delete(m.ctx, d.Deployment); err != nil && !k8serrors.IsNotFound(err) {
				return fmt.Errorf("cannot delete old Deployment for component %q (writes are safely buffered on disk): %w", helmLabel, err)
			}
			if err := waitPodsGone(m.ctx, m.c, d.Deployment.Namespace, d.Deployment.Spec.Selector.MatchLabels); err != nil {
				return fmt.Errorf("component %q: %w", helmLabel, err)
			}
		default:
			sts := dc.sts
			if len(d.PVCs) > 0 {
				if err := checkTargetPVCsRebindable(m.ctx, m.c, d.PVCs, m.engine.PVCTemplateName(m.target, dc.kind), m.target.PrefixedName(dc.kind), m.target.GetNamespace()); err != nil {
					return fmt.Errorf("component %q: %w", helmLabel, err)
				}
			}
			dependentConfigMaps, dependentSecrets := dependentConfigsOf(&sts.Spec.Template.Spec, d.ConfigMaps, d.Secrets)
			m.allDependentConfigMaps = append(m.allDependentConfigMaps, dependentConfigMaps...)
			m.allDependentSecrets = append(m.allDependentSecrets, dependentSecrets...)
			if err := m.c.Delete(m.ctx, sts); err != nil && !k8serrors.IsNotFound(err) {
				return fmt.Errorf("cannot delete old StatefulSet for component %q (writes are safely buffered on disk): %w", helmLabel, err)
			}
			if err := waitPodsGone(m.ctx, m.c, sts.Namespace, sts.Spec.Selector.MatchLabels); err != nil {
				return fmt.Errorf("component %q: %w", helmLabel, err)
			}
			if len(d.PVCs) == 0 {
				continue
			}
			if err := rebindClusterPVCs(m.ctx, m.c, d.PVCs, m.engine.PVCTemplateName(m.target, dc.kind), m.target.PrefixedName(dc.kind), m.target.GetNamespace(), m.target.SelectorLabels(dc.kind)); err != nil {
				return fmt.Errorf("component %q (writes are safely buffered on disk): %w", helmLabel, err)
			}
		}
	}
	return nil
}

func (m *clusterMigration[Cluster, Agent]) verifyRebind() error {
	if m.noDowntime || m.storageWorkloadName == "" {
		return nil
	}
	wantReplicas := m.engine.StorageReplicaCount(m.target)
	if m.storageSourceCount > int(wantReplicas) {
		wantReplicas = int32(m.storageSourceCount)
	}
	if err := verifyRebindComplete(m.ctx, m.c, wantReplicas, m.storagePVCTemplateName, m.storageWorkloadName, m.target.GetNamespace()); err != nil {
		return fmt.Errorf("storage PVC rebind incomplete (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
	}
	return nil
}

func (m *clusterMigration[Cluster, Agent]) createOrAdoptTarget() error {
	if m.noDowntime {
		existingTarget := m.target.DeepCopyObject().(Cluster)
		getErr := m.c.Get(m.ctx, types.NamespacedName{Name: m.target.GetName(), Namespace: m.target.GetNamespace()}, existingTarget)
		switch {
		case getErr == nil:
			m.target = existingTarget
		case k8serrors.IsNotFound(getErr):
			if err := snapshotClusterPVCs(m.ctx, m.c, m.storagePVCs, m.storagePVCTemplateName, m.storageWorkloadName, m.target.GetNamespace(), m.opts.SnapshotClassName); err != nil {
				return fmt.Errorf("storage snapshot failed (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
			}
			if err := m.c.Create(m.ctx, m.target); err != nil {
				if !k8serrors.IsAlreadyExists(err) {
					return fmt.Errorf("cannot create target CR %s/%s (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", m.target.GetNamespace(), m.target.GetName(), err)
				}
				existing := m.target.DeepCopyObject().(Cluster)
				if err := m.c.Get(m.ctx, types.NamespacedName{Name: m.target.GetName(), Namespace: m.target.GetNamespace()}, existing); err != nil {
					return fmt.Errorf("cannot fetch existing target CR %s/%s: %w", m.target.GetNamespace(), m.target.GetName(), err)
				}
				if err := verifySameSpec(existing, m.target); err != nil {
					return err
				}
				m.target = existing
			}
		default:
			return fmt.Errorf("cannot check for existing target CR %s/%s: %w", m.target.GetNamespace(), m.target.GetName(), getErr)
		}
	} else if !m.targetAlreadyExists {
		if err := m.c.Create(m.ctx, m.target); err != nil {
			return fmt.Errorf("cannot create target CR %s/%s (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", m.target.GetNamespace(), m.target.GetName(), err)
		}
	}
	return nil
}

func (m *clusterMigration[Cluster, Agent]) waitReadyAndCleanupConfigs() error {
	if err := waitForOperational(m.ctx, m.c, m.target); err != nil {
		return fmt.Errorf("target CR did not become ready (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
	}
	if len(m.allDependentConfigMaps) > 0 || len(m.allDependentSecrets) > 0 {
		if err := deleteDependentConfigs(m.ctx, m.c, m.target, m.allDependentConfigMaps, m.allDependentSecrets); err != nil {
			return err
		}
	}
	return nil
}

func (m *clusterMigration[Cluster, Agent]) finalInsertCutover() error {
	if err := waitEndpointsReady(m.ctx, m.c, m.target.GetNamespace(), m.target.PrefixedName(vmv1beta1.ClusterComponentInsert)); err != nil {
		return fmt.Errorf("component %q's target Service did not become ready: %w", m.engine.componentLabel(vmv1beta1.ClusterComponentInsert), err)
	}
	if err := cutoverServices(m.ctx, m.c, m.insertSvcPtrs, m.target.SelectorLabels(vmv1beta1.ClusterComponentInsert)); err != nil {
		return fmt.Errorf("final insert traffic cutover failed: %w", err)
	}
	return nil
}

func (m *clusterMigration[Cluster, Agent]) drainQueue() error {
	time.Sleep(WriteQuiesceGracePeriod)
	if err := waitQueueDrained(m.ctx, m.c, m.httpClient, m.agent, m.engine.QueueMetricName); err != nil {
		return fmt.Errorf("buffer agent queue did not fully drain after final cutover — leaving it running so buffered writes aren't lost; investigate and drain it manually, or re-run this command: %w", err)
	}
	return nil
}

func (m *clusterMigration[Cluster, Agent]) finalComponentCutover() error {
	for _, dc := range m.discovered {
		if dc.kind == vmv1beta1.ClusterComponentInsert {
			continue
		}
		helmLabel := m.engine.componentLabel(dc.kind)
		if requiresServiceCutover(dc.kind) {
			if err := waitEndpointsReady(m.ctx, m.c, m.target.GetNamespace(), m.target.PrefixedName(dc.kind)); err != nil {
				return fmt.Errorf("component %q's target Service did not become ready: %w", helmLabel, err)
			}
		}
		if m.noDowntime && !requiresServiceCutover(dc.kind) {
			continue
		}
		svcPtrs := cutoverCandidateServices(dc, m.target.SelectorLabels(dc.kind), m.agentSelectorLabels, true)
		candidateSet := make(map[*corev1.Service]bool, len(svcPtrs))
		for _, svc := range svcPtrs {
			candidateSet[svc] = true
		}
		for i := range dc.d.Services {
			if !candidateSet[&dc.d.Services[i]] {
				fmt.Printf("warning: Service %s/%s has a selector that doesn't match component %q's own pod labels, skipping cutover — update it manually\n",
					dc.d.Services[i].Namespace, dc.d.Services[i].Name, helmLabel)
			}
		}
		if requiresServiceCutover(dc.kind) && len(svcPtrs) == 0 {
			return fmt.Errorf("component %q has no Service left to cut over — repoint it at %v manually", helmLabel, m.target.SelectorLabels(dc.kind))
		}
		if err := cutoverServices(m.ctx, m.c, svcPtrs, m.target.SelectorLabels(dc.kind)); err != nil {
			return fmt.Errorf("traffic cutover failed for component %q: %w", helmLabel, err)
		}
	}
	return nil
}

func (m *clusterMigration[Cluster, Agent]) cleanupAndPrintCompletion() {
	deleteBufferAgent(m.ctx, m.c, m.agent)

	fmt.Printf("migration complete — the release's insert/select Service(s) now point at %s %s/%s, but they're still tracked by Helm release %q: "+
		"running `helm uninstall` deletes them, taking down whichever endpoint clients are still using them through. Move clients to the "+
		"target's own Service(s) (e.g. %s/%s) first, then decommission the release once nothing depends on the old Service(s) anymore\n",
		m.target.GetObjectKind().GroupVersionKind().Kind, m.target.GetNamespace(), m.target.GetName(), m.opts.ReleaseName,
		m.target.GetNamespace(), m.target.PrefixedName(vmv1beta1.ClusterComponentInsert))
	if m.noDowntime {
		fmt.Println("note: old workloads and all PVCs were left untouched")
	}
}

func ensureClusterReclaimPolicyRestored(ctx context.Context, c client.Client, namespace string, selectorLabels map[string]string, namePrefix string) error {
	var pvcs corev1.PersistentVolumeClaimList
	listOpts := []client.ListOption{client.InNamespace(namespace)}
	if len(selectorLabels) > 0 {
		listOpts = append(listOpts, client.MatchingLabels(selectorLabels))
	}
	if err := c.List(ctx, &pvcs, listOpts...); err != nil {
		return fmt.Errorf("cannot list PVCs in %s to check for a pending reclaim-policy restore: %w", namespace, err)
	}
	for i := range pvcs.Items {
		if !strings.HasPrefix(pvcs.Items[i].Name, namePrefix) {
			continue
		}
		if err := ensureReclaimPolicyRestored(ctx, c, types.NamespacedName{Name: pvcs.Items[i].Name, Namespace: namespace}); err != nil {
			return err
		}
	}
	return nil
}

func checkTargetPVCsRebindable(ctx context.Context, c client.Client, pvcs []corev1.PersistentVolumeClaim, targetPVCTemplateName, targetWorkloadName, targetNamespace string) error {
	byOrdinal, err := pvcsByOrdinal(pvcs)
	if err != nil {
		return err
	}
	for i := 0; i < len(byOrdinal); i++ {
		pvc, ok := byOrdinal[i]
		if !ok {
			return fmt.Errorf("PVC ordinals are not contiguous from 0 (missing ordinal %d among %d discovered PVCs) — refusing to guess a partial rebind", i, len(pvcs))
		}
		if pvc.Spec.VolumeName == "" {
			return fmt.Errorf("source PVC %s/%s has no bound PersistentVolume (spec.volumeName is empty)", pvc.Namespace, pvc.Name)
		}
		targetName := fmt.Sprintf("%s-%s-%d", targetPVCTemplateName, targetWorkloadName, i)
		var existing corev1.PersistentVolumeClaim
		getErr := c.Get(ctx, types.NamespacedName{Name: targetName, Namespace: targetNamespace}, &existing)
		switch {
		case getErr == nil:
			if existing.Spec.VolumeName != pvc.Spec.VolumeName {
				return fmt.Errorf("target PVC %s/%s already exists but is bound to PersistentVolume %q, not %q — refusing to proceed and risk data loss",
					targetNamespace, targetName, existing.Spec.VolumeName, pvc.Spec.VolumeName)
			}
			if existing.DeletionTimestamp != nil {
				return fmt.Errorf("target PVC %s/%s already exists but is terminating — wait for it to finish deleting, or delete it manually, before retrying", targetNamespace, targetName)
			}
		case !k8serrors.IsNotFound(getErr):
			return fmt.Errorf("cannot check for existing target PVC %s/%s: %w", targetNamespace, targetName, getErr)
		}
	}
	return nil
}

func rebindClusterPVCs(ctx context.Context, c client.Client, pvcs []corev1.PersistentVolumeClaim, targetPVCTemplateName, targetWorkloadName, targetNamespace string, pvcLabels map[string]string) error {
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
		if _, err := rebindPVC(ctx, c, pvc, targetName, targetNamespace, pvcLabels); err != nil {
			return fmt.Errorf("cannot rebind PVC %q (ordinal %d): %w", pvc.Name, i, err)
		}
	}
	return nil
}

func finishInterruptedRebind(ctx context.Context, c client.Client, pvcs []corev1.PersistentVolumeClaim, targetPVCTemplateName, targetWorkloadName, targetNamespace string, pvcLabels map[string]string) error {
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
		if _, err := rebindPVC(ctx, c, pvc, targetName, targetNamespace, pvcLabels); err != nil {
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
		reboundFrom := pvc.Annotations[reboundFromAnnotation]
		if !strings.HasPrefix(reboundFrom, targetNamespace+"/") {
			return fmt.Errorf("target PVC %s/%s (ordinal %d) exists but wasn't created by this migration's rebind from a source PVC in this namespace (annotation %q=%q) — refusing to proceed and risk using unrelated or empty storage", targetNamespace, targetName, i, reboundFromAnnotation, reboundFrom)
		}
		if !strings.HasSuffix(reboundFrom, fmt.Sprintf("-%d", i)) {
			return fmt.Errorf("target PVC %s/%s (ordinal %d) is marked as rebound from %q, which doesn't look like this ordinal's own source PVC — refusing to proceed and risk mixing up shards' data", targetNamespace, targetName, i, reboundFrom)
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
