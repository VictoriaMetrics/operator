package migrate

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// singleNodeCR is the shape shared by VMSingle and VLSingle that SingleNode needs.
type singleNodeCR interface {
	client.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
	PrefixedName() string
	SelectorLabels() map[string]string
	GetRemoteWriteURL() string
}

// SingleNodeEngine bundles the per-engine behaviors SingleNode needs.
type SingleNodeEngine[Target singleNodeCR, Agent bufferAgentCR] struct {
	QueueMetricName string

	HasStorage         func(target Target) bool
	BuildAgent         func(name, namespace, writeURL, bufferSize, pathPrefix string) (Agent, error)
	BuildAgentWriteURL func(target Target, remoteWriteURL string) string
	AgentMatches       func(existing, want Agent) bool
}

// SingleNode runs a single-node chart migration, shared across engines and both strategies via
// a buffer agent (see the plan messages below for how each strategy handles the old Deployment,
// reads, and writes from connections already established before cutover). Resumable in both
// modes: NoDowntime never deletes anything, so every step just adopts what a previous attempt
// already created; WithDowntime picks up from an already-rebound PVC if the old Deployment is
// already gone.
func SingleNode[Target singleNodeCR, Agent bufferAgentCR](ctx context.Context, c client.Client, httpClient *http.Client, opts Options, target Target, engine SingleNodeEngine[Target, Agent], noDowntime bool) error {
	m := &singleNodeMigration[Target, Agent]{
		ctx: ctx, c: c, httpClient: httpClient, opts: opts, target: target, engine: engine, noDowntime: noDowntime,
	}
	return m.run()
}

// singleNodeMigration holds the state threaded through one SingleNode run, so the otherwise
// linear, single-pass procedure can be split into named phases without a growing parameter list.
type singleNodeMigration[Target singleNodeCR, Agent bufferAgentCR] struct {
	ctx        context.Context
	c          client.Client
	httpClient *http.Client
	opts       Options
	target     Target
	engine     SingleNodeEngine[Target, Agent]
	noDowntime bool

	targetWriteURL      string
	d                   *Discovered
	targetPVCName       string
	agentName           string
	agent               Agent
	agentSelectorLabels map[string]string

	resuming            bool
	resumeRebindPending bool
	pvc                 *corev1.PersistentVolumeClaim
	oldSvc              *corev1.Service

	readsAlias          string
	targetAlreadyExists bool
	readsAliasSvc       *corev1.Service
	oldSvcServesReads   bool

	dependentConfigMaps []corev1.ConfigMap
	dependentSecrets    []corev1.Secret
}

func (m *singleNodeMigration[Target, Agent]) run() (err error) {
	if err := runPhases(m.validateTarget, m.discover, m.resolveBufferAgent, m.resolveResumeState, m.checkOldService, m.checkHasStorage); err != nil {
		return err
	}

	m.printPlan()
	if err := m.checkExistingTarget(); err != nil {
		return err
	}
	stop, err := m.dryRunAndConfirm()
	if err != nil {
		return err
	}
	if stop {
		return nil
	}

	if m.noDowntime {
		if err := m.createReadsAlias(); err != nil {
			return err
		}
		defer m.cleanupReadsAliasOnFailure(&err)
	}

	if err := runPhases(m.cutoverWrites, m.deleteOldWorkloadAndRebind, m.createOrAdoptTarget, m.waitReadyAndCleanupConfigs, m.finalCutover); err != nil {
		return err
	}
	m.printCompletion()
	return nil
}

func (m *singleNodeMigration[Target, Agent]) checkHasStorage() error {
	if !m.engine.HasStorage(m.target) {
		return fmt.Errorf("target %s/%s has no storage configured — it would never mount the migrated data", m.target.GetNamespace(), m.target.GetName())
	}
	return nil
}

func (m *singleNodeMigration[Target, Agent]) validateTarget() error {
	if m.target.GetNamespace() != m.opts.Namespace {
		return fmt.Errorf("target %s/%s is in a different namespace than the Helm release %q — cross-namespace migration isn't supported: PVC snapshot data sources and Service selectors can't cross namespaces", m.target.GetNamespace(), m.target.GetName(), m.opts.Namespace)
	}
	m.targetWriteURL = m.target.GetRemoteWriteURL()
	if strings.HasPrefix(m.targetWriteURL, "https://") {
		return fmt.Errorf("target %s/%s has TLS enabled — the buffer agent has no way to be configured with its certificate automatically, so migration isn't supported for it", m.target.GetNamespace(), m.target.GetName())
	}
	return nil
}

func (m *singleNodeMigration[Target, Agent]) discover() error {
	d, err := Discover(m.ctx, m.c, m.opts.Namespace, m.opts.ReleaseName)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}
	m.d = d
	m.targetPVCName = m.target.PrefixedName()
	m.agentName = m.target.PrefixedName() + "-migration-buffer"
	return nil
}

func (m *singleNodeMigration[Target, Agent]) resolveBufferAgent() error {
	var agentPathPrefix string
	switch {
	case m.d.Deployment != nil:
		p, prefixErr := httpPathPrefix(m.d.Deployment.Spec.Template.Spec.Containers)
		if prefixErr != nil {
			return fmt.Errorf("cannot resolve http.pathPrefix for the buffer agent: %w", prefixErr)
		}
		agentPathPrefix = p
	default:
		if probe, buildErr := m.engine.BuildAgent(m.agentName, m.target.GetNamespace(), m.targetWriteURL, m.opts.AgentBufferSize, ""); buildErr == nil {
			if getErr := m.c.Get(m.ctx, types.NamespacedName{Name: m.agentName, Namespace: m.target.GetNamespace()}, probe); getErr == nil {
				agentPathPrefix = probe.GetExtraArgs()[HTTPPathPrefixArg]
			}
		}
	}

	agentWriteURL := m.engine.BuildAgentWriteURL(m.target, m.targetWriteURL)
	agent, err := m.engine.BuildAgent(m.agentName, m.target.GetNamespace(), agentWriteURL, m.opts.AgentBufferSize, agentPathPrefix)
	if err != nil {
		return fmt.Errorf("cannot build buffer agent spec: %w", err)
	}
	m.agent = agent
	m.agentSelectorLabels = agent.SelectorLabels()
	return nil
}

func (m *singleNodeMigration[Target, Agent]) resolveResumeState() error {
	m.resuming = !m.noDowntime && m.d.Deployment == nil
	switch {
	case m.resuming:
		var existingTargetPVC corev1.PersistentVolumeClaim
		getErr := m.c.Get(m.ctx, types.NamespacedName{Name: m.targetPVCName, Namespace: m.target.GetNamespace()}, &existingTargetPVC)
		switch {
		case k8serrors.IsNotFound(getErr):
			sourcePVC, pvcErr := m.d.SingleNodePVC()
			if pvcErr != nil {
				return fmt.Errorf("no Deployment found for release %q in namespace %q, and no rebound PVC %s/%s either — nothing safe to do automatically; the release may already be fully migrated, or something deleted the Deployment outside this tool", m.opts.ReleaseName, m.opts.Namespace, m.target.GetNamespace(), m.targetPVCName)
			}
			m.pvc = sourcePVC
			m.resumeRebindPending = true
		case getErr != nil:
			return fmt.Errorf("cannot check for an already-rebound target PVC %s/%s: %w", m.target.GetNamespace(), m.targetPVCName, getErr)
		case existingTargetPVC.Status.Phase != corev1.ClaimBound:
			return fmt.Errorf("target PVC %s/%s exists from a previous attempt but isn't Bound (phase=%q) — inspect it manually before retrying", m.target.GetNamespace(), m.targetPVCName, existingTargetPVC.Status.Phase)
		default:
			reboundFrom := existingTargetPVC.Annotations[reboundFromAnnotation]
			if reboundFrom == "" || !strings.HasPrefix(reboundFrom, m.opts.Namespace+"/") {
				return fmt.Errorf("no Deployment found for release %q in namespace %q, and PVC %s/%s exists but wasn't created by this tool's rebindPVC from a source PVC in this namespace (annotation %q=%q) — refusing to adopt what may be an unrelated, coincidentally-named PVC from another release; delete it manually first if that's intended, or investigate why the old Deployment is missing", m.opts.ReleaseName, m.opts.Namespace, m.target.GetNamespace(), m.targetPVCName, reboundFromAnnotation, reboundFrom)
			}
			var pv corev1.PersistentVolume
			if err := m.c.Get(m.ctx, types.NamespacedName{Name: existingTargetPVC.Spec.VolumeName}, &pv); err != nil {
				return fmt.Errorf("cannot verify PersistentVolume %q bound to PVC %s/%s: %w", existingTargetPVC.Spec.VolumeName, m.target.GetNamespace(), m.targetPVCName, err)
			}
			if err := verifyClaimRefIdentifies(&pv, &existingTargetPVC); err != nil {
				return fmt.Errorf("target PVC %s/%s is marked Bound and rebound, but %w", m.target.GetNamespace(), m.targetPVCName, err)
			}
		}
	case m.d.Deployment == nil:
		return fmt.Errorf("no Deployment found for release %q in namespace %q", m.opts.ReleaseName, m.opts.Namespace)
	default:
		if err := rejectTLSEnabled(m.d.Deployment.Namespace, m.d.Deployment.Name, m.d.Deployment.Spec.Template.Spec.Containers); err != nil {
			return err
		}
		if err := rejectHTTPAuthEnabled(m.d.Deployment.Namespace, m.d.Deployment.Name, m.d.Deployment.Spec.Template.Spec.Containers); err != nil {
			return err
		}
		pvc, err := m.d.SingleNodePVC()
		if err != nil {
			return fmt.Errorf("storage discovery failed: %w", err)
		}
		if pvc.Spec.VolumeName == "" || pvc.Status.Phase != corev1.ClaimBound {
			return fmt.Errorf("PVC %s/%s is not Bound (phase=%q) — cannot proceed without a bound source volume to rebind or snapshot", pvc.Namespace, pvc.Name, pvc.Status.Phase)
		}
		m.pvc = pvc
	}
	return nil
}

func (m *singleNodeMigration[Target, Agent]) checkOldService() error {
	if len(m.d.Services) != 1 {
		return fmt.Errorf("found %d Services for release %q, expected exactly 1", len(m.d.Services), m.opts.ReleaseName)
	}
	oldSvc := &m.d.Services[0]
	m.oldSvc = oldSvc
	matchesOldPods := !m.resuming && selectorMatchesBase(oldSvc.Spec.Selector, m.d.Deployment.Spec.Selector.MatchLabels)
	matchesAgent := !selectorDiffersFromBase(oldSvc.Spec.Selector, m.agentSelectorLabels)
	matchesTarget := !selectorDiffersFromBase(oldSvc.Spec.Selector, m.target.SelectorLabels())
	switch {
	case !m.resuming && !matchesOldPods && !matchesAgent && !matchesTarget:
		return fmt.Errorf("service %s/%s has a selector that doesn't match the release's own pod labels — nothing to cut over; repoint it at %v manually", oldSvc.Namespace, oldSvc.Name, m.target.SelectorLabels())
	case !m.resuming && matchesTarget && m.noDowntime && selectorMatchesBase(m.target.SelectorLabels(), m.d.Deployment.Spec.Selector.MatchLabels):
		return fmt.Errorf("service %s/%s's selector already matches target %s/%s's future pod labels, and NoDowntime never deletes the old Deployment — cutting traffic over would route it to both the old and target pods at once, bypassing the buffer agent; use WithDowntime instead, or give the target different pod labels", oldSvc.Namespace, oldSvc.Name, m.target.GetNamespace(), m.target.GetName())
	case m.resuming && oldSvc.Annotations[serviceCutoverAnnotation] != "true":
		return fmt.Errorf("no Deployment found for release %q in namespace %q, and Service %s/%s wasn't previously redirected by this tool (missing %q annotation) — refusing to repoint what may be an unrelated, coincidentally-labeled Service; investigate why the old Deployment is missing, or repoint it manually", m.opts.ReleaseName, m.opts.Namespace, oldSvc.Namespace, oldSvc.Name, serviceCutoverAnnotation)
	}
	return nil
}

func (m *singleNodeMigration[Target, Agent]) printPlan() {
	switch {
	case m.resumeRebindPending:
		fmt.Printf("plan: resume a previously-interrupted migration — the old Deployment is already gone but PVC %s/%s -> %s/%s (PV %s) was never finished; finish the rebind, ensure the buffer agent is forwarding to the target, create %s %s/%s, cut Service %s/%s over to it\n",
			m.pvc.Namespace, m.pvc.Name, m.target.GetNamespace(), m.targetPVCName, m.pvc.Spec.VolumeName,
			m.target.GetObjectKind().GroupVersionKind().Kind, m.target.GetNamespace(), m.target.GetName(),
			m.oldSvc.Namespace, m.oldSvc.Name)
	case m.resuming:
		fmt.Printf("plan: resume a previously-interrupted migration — the old Deployment is already gone and PVC %s/%s is already rebound; ensure the buffer agent is forwarding to the target, create %s %s/%s, cut Service %s/%s over to it\n",
			m.target.GetNamespace(), m.targetPVCName,
			m.target.GetObjectKind().GroupVersionKind().Kind, m.target.GetNamespace(), m.target.GetName(),
			m.oldSvc.Namespace, m.oldSvc.Name)
	case m.noDowntime:
		m.readsAlias = m.target.PrefixedName() + "-migration-reads"
		fmt.Printf("plan: deploy buffer agent pointed directly at the target's future write endpoint, redirect Service %s/%s to it, "+
			"snapshot PVC %s/%s, create %s/%s from the snapshot, then cut traffic over to it once ready — the old Deployment and PVC "+
			"are never deleted. Service %s/%s serves both reads and writes, and the buffer agent only handles writes, so queries "+
			"against %s/%s will fail as soon as writes are redirected to the buffer agent; %s/%s offers a read-only path to the old "+
			"storage for the rest of the migration — point read-only clients at the target once it's ready. A client with a "+
			"connection already established to the old Deployment before that redirect — directly, or routed through the Service, "+
			"since a selector change only affects new connections — can keep writing to it afterward; that write is captured only "+
			"if it lands before the snapshot above, an unavoidable limit of any Service-based cutover while the old workload keeps "+
			"running\n",
			m.oldSvc.Namespace, m.oldSvc.Name, m.pvc.Namespace, m.pvc.Name, m.target.GetNamespace(), m.target.GetName(),
			m.oldSvc.Namespace, m.oldSvc.Name, m.oldSvc.Namespace, m.oldSvc.Name, m.target.GetNamespace(), m.readsAlias)
	default:
		fmt.Printf("plan: deploy a buffer agent pointed at the target's future write endpoint and redirect Service %s/%s to it, delete Deployment %s/%s, "+
			"rebind PVC %s/%s -> %s/%s (PV %s), create %s %s/%s, cut Service %s/%s over to the target once ready — reads are unavailable for the "+
			"whole window since the buffer agent only handles writes and old storage is deleted rather than kept running. A client with a "+
			"connection already established to the old Deployment before that redirect — directly, or routed through the Service, since a "+
			"selector change only affects new connections — can keep writing to it afterward; unlike NoDowntime, that write isn't lost as "+
			"long as it lands before the old pod has actually terminated, since its PVC is rebound (not snapshotted) under the target — "+
			"only a write attempted after that pod is fully gone fails outright, visibly, at the client\n",
			m.oldSvc.Namespace, m.oldSvc.Name, m.d.Deployment.Namespace, m.d.Deployment.Name,
			m.pvc.Namespace, m.pvc.Name, m.target.GetNamespace(), m.targetPVCName, m.pvc.Spec.VolumeName,
			m.target.GetObjectKind().GroupVersionKind().Kind, m.target.GetNamespace(), m.target.GetName(),
			m.oldSvc.Namespace, m.oldSvc.Name)
	}
}

func (m *singleNodeMigration[Target, Agent]) checkExistingTarget() error {
	existsCheck := m.target.DeepCopyObject().(Target)
	getErr := m.c.Get(m.ctx, types.NamespacedName{Name: m.target.GetName(), Namespace: m.target.GetNamespace()}, existsCheck)
	switch {
	case getErr == nil:
		m.targetAlreadyExists = true
		if err := verifySameSpec(existsCheck, m.target); err != nil {
			return err
		}
		if m.noDowntime {
			if err := verifyRestoredPVC(m.ctx, m.c, types.NamespacedName{Name: m.target.PrefixedName(), Namespace: m.target.GetNamespace()}, m.pvc.Namespace, m.pvc.Name+"-migration-snapshot", m.pvc.Name); err != nil {
				return err
			}
		}
	case !k8serrors.IsNotFound(getErr):
		return fmt.Errorf("cannot check for an existing target CR %s/%s: %w", m.target.GetNamespace(), m.target.GetName(), getErr)
	}
	return nil
}

func (m *singleNodeMigration[Target, Agent]) dryRunAndConfirm() (stop bool, err error) {
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

func (m *singleNodeMigration[Target, Agent]) createReadsAlias() error {
	m.oldSvcServesReads = selectorDiffersFromBase(m.oldSvc.Spec.Selector, m.agentSelectorLabels)
	originalSelector := m.d.Deployment.Spec.Selector.MatchLabels
	readsAliasSvc, err := createAliasService(m.ctx, m.c, m.readsAlias, m.target.GetNamespace(), m.oldSvc, originalSelector)
	if err != nil {
		return fmt.Errorf("cannot create read alias Service: %w", err)
	}
	m.readsAliasSvc = readsAliasSvc
	return nil
}

func (m *singleNodeMigration[Target, Agent]) cleanupReadsAliasOnFailure(err *error) {
	if *err == nil {
		return
	}
	if m.oldSvcServesReads {
		if delErr := m.c.Delete(m.ctx, m.readsAliasSvc); delErr != nil && !k8serrors.IsNotFound(delErr) {
			fmt.Printf("warning: migration failed and left read alias Service %s/%s behind, please remove it manually: %v\n", m.readsAliasSvc.Namespace, m.readsAliasSvc.Name, delErr)
		}
		return
	}
	fmt.Printf("warning: migration failed after redirecting %s/%s away from reads — leaving read alias Service %s/%s running as the only read path; remove it manually once you've recovered\n",
		m.oldSvc.Namespace, m.oldSvc.Name, m.readsAliasSvc.Namespace, m.readsAliasSvc.Name)
}

func (m *singleNodeMigration[Target, Agent]) cutoverWrites() error {
	agent, err := ensureBufferAgentRunning(m.ctx, m.c, m.agent, m.targetWriteURL, m.engine.AgentMatches)
	if err != nil {
		return err
	}
	m.agent = agent
	if err := verifyAgentPortCompatible(m.ctx, m.c, m.oldSvc, m.agent); err != nil {
		return err
	}
	if err := cutoverServices(m.ctx, m.c, []*corev1.Service{m.oldSvc}, m.agent.SelectorLabels()); err != nil {
		return fmt.Errorf("cannot redirect incoming writes to the buffer agent: %w", err)
	}
	m.oldSvcServesReads = false
	if m.noDowntime {
		time.Sleep(WriteQuiesceGracePeriod)
	}
	return nil
}

func (m *singleNodeMigration[Target, Agent]) deleteOldWorkloadAndRebind() error {
	switch {
	case m.noDowntime:
		if hasForceMergeAuthKey(m.d.Deployment.Spec.Template.Spec.Containers) {
			fmt.Printf("warning: %s/%s sets -forceMergeAuthKey, which this tool cannot supply — skipping force-merge (continuing, snapshot will still be crash-consistent)\n", m.d.Deployment.Namespace, m.d.Deployment.Name)
		} else if pathPrefix, prefixErr := httpPathPrefix(m.d.Deployment.Spec.Template.Spec.Containers); prefixErr != nil {
			fmt.Printf("warning: cannot resolve http.pathPrefix (continuing, snapshot will still be crash-consistent): %v\n", prefixErr)
		} else if forceMergeURL, urlErr := forceMergeURL(m.readsAliasSvc, pathPrefix); urlErr != nil {
			fmt.Printf("warning: cannot build force-merge URL (continuing, snapshot will still be crash-consistent): %v\n", urlErr)
		} else if err := forceMerge(m.ctx, m.httpClient, forceMergeURL); err != nil {
			fmt.Printf("warning: force-merge on old storage failed (continuing, snapshot will still be crash-consistent): %v\n", err)
		}
	case m.resumeRebindPending:
		if _, err := rebindPVC(m.ctx, m.c, m.pvc, m.targetPVCName, m.target.GetNamespace(), nil); err != nil {
			return fmt.Errorf("storage rebind failed (writes are safely buffered on disk): %w", err)
		}
	case m.resuming:
		if err := ensureReclaimPolicyRestored(m.ctx, m.c, types.NamespacedName{Name: m.targetPVCName, Namespace: m.target.GetNamespace()}); err != nil {
			return err
		}
	case !m.resuming:
		m.dependentConfigMaps, m.dependentSecrets = dependentConfigsOf(&m.d.Deployment.Spec.Template.Spec, m.d.ConfigMaps, m.d.Secrets)
		if err := m.c.Delete(m.ctx, m.d.Deployment); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("cannot delete old Deployment %s/%s (writes are safely buffered on disk): %w", m.d.Deployment.Namespace, m.d.Deployment.Name, err)
		}
		if err := waitPodsGone(m.ctx, m.c, m.d.Deployment.Namespace, m.d.Deployment.Spec.Selector.MatchLabels); err != nil {
			return err
		}
		if _, err := rebindPVC(m.ctx, m.c, m.pvc, m.targetPVCName, m.target.GetNamespace(), nil); err != nil {
			return fmt.Errorf("storage rebind failed (writes are safely buffered on disk): %w", err)
		}
	}
	if m.resuming {
		fmt.Printf("warning: resuming after the old Deployment was already deleted (by a previous, interrupted attempt) — this tool can no longer tell which ConfigMaps/Secrets it referenced, so any left over from the Helm release won't be cleaned up automatically; check for orphaned ones manually\n")
	}
	return nil
}

func (m *singleNodeMigration[Target, Agent]) createOrAdoptTarget() error {
	if m.noDowntime {
		snapName := m.pvc.Name + "-migration-snapshot"
		newPVCName := types.NamespacedName{Name: m.target.PrefixedName(), Namespace: m.target.GetNamespace()}
		existingTarget := m.target.DeepCopyObject().(Target)
		getErr := m.c.Get(m.ctx, types.NamespacedName{Name: m.target.GetName(), Namespace: m.target.GetNamespace()}, existingTarget)
		switch {
		case getErr == nil:
			m.target = existingTarget
		case k8serrors.IsNotFound(getErr):
			if err := deleteStaleRestoredPVC(m.ctx, m.c, newPVCName, m.pvc.Namespace, snapName, m.pvc.Name); err != nil {
				return err
			}
			if err := createVolumeSnapshot(m.ctx, m.c, m.pvc.Namespace, snapName, m.pvc.Name, m.opts.SnapshotClassName); err != nil {
				return fmt.Errorf("cannot create VolumeSnapshot (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
			}
			if err := waitVolumeSnapshotReady(m.ctx, m.c, m.pvc.Namespace, snapName); err != nil {
				return err
			}
			newPVC := newPVCFromSnapshot(m.target.PrefixedName(), m.target.GetNamespace(), snapName, m.pvc)
			if err := m.c.Create(m.ctx, newPVC); err != nil {
				return fmt.Errorf("cannot create PVC from snapshot (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
			}
			if err := m.c.Create(m.ctx, m.target); err != nil {
				if !k8serrors.IsAlreadyExists(err) {
					return fmt.Errorf("cannot create target CR %s/%s (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", m.target.GetNamespace(), m.target.GetName(), err)
				}
				existing := m.target.DeepCopyObject().(Target)
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

func (m *singleNodeMigration[Target, Agent]) waitReadyAndCleanupConfigs() error {
	if err := waitForOperational(m.ctx, m.c, m.target); err != nil {
		return fmt.Errorf("target CR did not become ready (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
	}
	if len(m.dependentConfigMaps) > 0 || len(m.dependentSecrets) > 0 {
		if err := deleteDependentConfigs(m.ctx, m.c, m.target, m.dependentConfigMaps, m.dependentSecrets); err != nil {
			return err
		}
	}
	if err := waitEndpointsReady(m.ctx, m.c, m.target.GetNamespace(), m.target.PrefixedName()); err != nil {
		return fmt.Errorf("target's Service did not become ready (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
	}
	return nil
}

func (m *singleNodeMigration[Target, Agent]) finalCutover() error {
	if err := cutoverServices(m.ctx, m.c, []*corev1.Service{m.oldSvc}, m.target.SelectorLabels()); err != nil {
		return fmt.Errorf("final traffic cutover failed: %w", err)
	}
	if m.noDowntime {
		fmt.Printf("%s/%s now serves both reads and writes — read clients still on alias Service %s/%s must move off it before it's deleted, since deletion happens with no grace period\n",
			m.oldSvc.Namespace, m.oldSvc.Name, m.readsAliasSvc.Namespace, m.readsAliasSvc.Name)
		time.Sleep(WriteQuiesceGracePeriod)
		if m.opts.Yes || confirm(fmt.Sprintf("delete read alias Service %s/%s now?", m.readsAliasSvc.Namespace, m.readsAliasSvc.Name)) {
			if err := m.c.Delete(m.ctx, m.readsAliasSvc); err != nil && !k8serrors.IsNotFound(err) {
				fmt.Printf("warning: cannot delete read alias Service %s/%s, please remove it manually: %v\n", m.readsAliasSvc.Namespace, m.readsAliasSvc.Name, err)
			}
		} else {
			fmt.Printf("leaving read alias Service %s/%s running — delete it manually once all read clients have moved off it\n", m.readsAliasSvc.Namespace, m.readsAliasSvc.Name)
		}
	}
	time.Sleep(WriteQuiesceGracePeriod)
	if err := waitQueueDrained(m.ctx, m.c, m.httpClient, m.agent, m.engine.QueueMetricName); err != nil {
		return fmt.Errorf("buffer agent queue did not fully drain after final cutover — leaving it running so buffered writes aren't lost; investigate and drain it manually, or re-run this command: %w", err)
	}
	deleteBufferAgent(m.ctx, m.c, m.agent)
	return nil
}

func (m *singleNodeMigration[Target, Agent]) printCompletion() {
	fmt.Printf("migration complete — Service %s/%s now points at %s %s/%s, but it's still tracked by the Helm release %q: running `helm uninstall` "+
		"deletes it along with everything else, taking down the endpoint clients are using. Move clients to the target's own Service "+
		"(%s/%s) first, then decommission the release once nothing depends on %s/%s anymore\n",
		m.oldSvc.Namespace, m.oldSvc.Name, m.target.GetObjectKind().GroupVersionKind().Kind, m.target.GetNamespace(), m.target.GetName(), m.opts.ReleaseName,
		m.target.GetNamespace(), m.target.PrefixedName(), m.oldSvc.Namespace, m.oldSvc.Name)
	if m.noDowntime {
		fmt.Println("note: the old Deployment and its PVC were left untouched")
	}
}
