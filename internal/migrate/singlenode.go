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

// SingleNodeEngine bundles the per-engine behaviors MigrateSingleNode needs.
type SingleNodeEngine[Target singleNodeCR, Agent bufferAgentCR] struct {
	QueueMetricName string

	HasStorage   func(target Target) bool
	BuildAgent   func(name, namespace, writeURL, bufferSize string) (Agent, error)
	AgentMatches func(existing Agent, wantURL string) bool
}

// SingleNode runs a single-node chart migration, shared across engines and both strategies via
// a buffer agent (see the plan messages below for how each strategy handles the old Deployment,
// reads, and writes from connections already established before cutover). Resumable in both
// modes: NoDowntime never deletes anything, so every step just adopts what a previous attempt
// already created; WithDowntime picks up from an already-rebound PVC if the old Deployment is
// already gone.
func SingleNode[Target singleNodeCR, Agent bufferAgentCR](ctx context.Context, c client.Client, httpClient *http.Client, opts Options, target Target, engine SingleNodeEngine[Target, Agent], noDowntime bool) (err error) {
	if target.GetNamespace() != opts.Namespace {
		return fmt.Errorf("target %s/%s is in a different namespace than the Helm release %q — cross-namespace migration isn't supported: PVC snapshot data sources and Service selectors can't cross namespaces", target.GetNamespace(), target.GetName(), opts.Namespace)
	}
	targetWriteURL := target.GetRemoteWriteURL()
	if strings.HasPrefix(targetWriteURL, "https://") {
		return fmt.Errorf("target %s/%s has TLS enabled — the buffer agent has no way to be configured with its certificate automatically, so migration isn't supported for it", target.GetNamespace(), target.GetName())
	}

	d, err := Discover(ctx, c, opts.Namespace, opts.ReleaseName)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}
	targetPVCName := target.PrefixedName()

	agentName := target.PrefixedName() + "-migration-buffer"
	agent, err := engine.BuildAgent(agentName, target.GetNamespace(), targetWriteURL, opts.AgentBufferSize)
	if err != nil {
		return fmt.Errorf("cannot build buffer agent spec: %w", err)
	}
	agentSelectorLabels := agent.SelectorLabels()

	resuming := !noDowntime && d.Deployment == nil
	var pvc *corev1.PersistentVolumeClaim
	var resumeRebindPending bool
	switch {
	case resuming:
		var existingTargetPVC corev1.PersistentVolumeClaim
		getErr := c.Get(ctx, types.NamespacedName{Name: targetPVCName, Namespace: target.GetNamespace()}, &existingTargetPVC)
		switch {
		case k8serrors.IsNotFound(getErr):
			sourcePVC, pvcErr := d.SingleNodePVC()
			if pvcErr != nil {
				return fmt.Errorf("no Deployment found for release %q in namespace %q, and no rebound PVC %s/%s either — nothing safe to do automatically; the release may already be fully migrated, or something deleted the Deployment outside this tool", opts.ReleaseName, opts.Namespace, target.GetNamespace(), targetPVCName)
			}
			pvc = sourcePVC
			resumeRebindPending = true
		case getErr != nil:
			return fmt.Errorf("cannot check for an already-rebound target PVC %s/%s: %w", target.GetNamespace(), targetPVCName, getErr)
		case existingTargetPVC.Status.Phase != corev1.ClaimBound:
			return fmt.Errorf("target PVC %s/%s exists from a previous attempt but isn't Bound (phase=%q) — inspect it manually before retrying", target.GetNamespace(), targetPVCName, existingTargetPVC.Status.Phase)
		case existingTargetPVC.Annotations[reboundFromAnnotation] != opts.Namespace+"/"+opts.ReleaseName:
			return fmt.Errorf("no Deployment found for release %q in namespace %q, and PVC %s/%s exists but wasn't created by this tool's rebindPVC from this release's source PVC (annotation %q=%q, expected %q) — refusing to adopt what may be an unrelated, coincidentally-named PVC from another release in the same namespace; delete it manually first if that's intended, or investigate why the old Deployment is missing", opts.ReleaseName, opts.Namespace, target.GetNamespace(), targetPVCName, reboundFromAnnotation, existingTargetPVC.Annotations[reboundFromAnnotation], opts.Namespace+"/"+opts.ReleaseName)
		}
	case d.Deployment == nil:
		return fmt.Errorf("no Deployment found for release %q in namespace %q", opts.ReleaseName, opts.Namespace)
	default:
		if err := rejectTLSEnabled(d.Deployment.Namespace, d.Deployment.Name, d.Deployment.Spec.Template.Spec.Containers); err != nil {
			return err
		}
		pvc, err = d.SingleNodePVC()
		if err != nil {
			return fmt.Errorf("storage discovery failed: %w", err)
		}
		if pvc.Spec.VolumeName == "" || pvc.Status.Phase != corev1.ClaimBound {
			return fmt.Errorf("PVC %s/%s is not Bound (phase=%q) — cannot proceed without a bound source volume to rebind or snapshot", pvc.Namespace, pvc.Name, pvc.Status.Phase)
		}
	}
	if len(d.Services) != 1 {
		return fmt.Errorf("found %d Services for release %q, expected exactly 1", len(d.Services), opts.ReleaseName)
	}
	oldSvc := &d.Services[0]
	matchesOldPods := !resuming && selectorMatchesBase(oldSvc.Spec.Selector, d.Deployment.Spec.Selector.MatchLabels)
	matchesAgent := !selectorDiffersFromBase(oldSvc.Spec.Selector, agentSelectorLabels)
	matchesTarget := !selectorDiffersFromBase(oldSvc.Spec.Selector, target.SelectorLabels())
	switch {
	case !resuming && !matchesOldPods && !matchesAgent && !matchesTarget:
		return fmt.Errorf("service %s/%s has a selector that doesn't match the release's own pod labels — nothing to cut over; repoint it at %v manually", oldSvc.Namespace, oldSvc.Name, target.SelectorLabels())
	case !resuming && matchesTarget && noDowntime && selectorMatchesBase(target.SelectorLabels(), d.Deployment.Spec.Selector.MatchLabels):
		return fmt.Errorf("service %s/%s's selector already matches target %s/%s's future pod labels, and NoDowntime never deletes the old Deployment — cutting traffic over would route it to both the old and target pods at once, bypassing the buffer agent; use WithDowntime instead, or give the target different pod labels", oldSvc.Namespace, oldSvc.Name, target.GetNamespace(), target.GetName())
	case resuming && oldSvc.Annotations[serviceCutoverAnnotation] != "true":
		return fmt.Errorf("no Deployment found for release %q in namespace %q, and Service %s/%s wasn't previously redirected by this tool (missing %q annotation) — refusing to repoint what may be an unrelated, coincidentally-labeled Service; investigate why the old Deployment is missing, or repoint it manually", opts.ReleaseName, opts.Namespace, oldSvc.Namespace, oldSvc.Name, serviceCutoverAnnotation)
	}
	if !engine.HasStorage(target) {
		return fmt.Errorf("target %s/%s has no storage configured — it would never mount the migrated data", target.GetNamespace(), target.GetName())
	}

	var readsAlias string
	switch {
	case resumeRebindPending:
		fmt.Printf("plan: resume a previously-interrupted migration — the old Deployment is already gone but PVC %s/%s -> %s/%s (PV %s) was never finished; finish the rebind, ensure the buffer agent is forwarding to the target, create %s %s/%s, cut Service %s/%s over to it\n",
			pvc.Namespace, pvc.Name, target.GetNamespace(), targetPVCName, pvc.Spec.VolumeName,
			target.GetObjectKind().GroupVersionKind().Kind, target.GetNamespace(), target.GetName(),
			oldSvc.Namespace, oldSvc.Name)
	case resuming:
		fmt.Printf("plan: resume a previously-interrupted migration — the old Deployment is already gone and PVC %s/%s is already rebound; ensure the buffer agent is forwarding to the target, create %s %s/%s, cut Service %s/%s over to it\n",
			target.GetNamespace(), targetPVCName,
			target.GetObjectKind().GroupVersionKind().Kind, target.GetNamespace(), target.GetName(),
			oldSvc.Namespace, oldSvc.Name)
	case noDowntime:
		readsAlias = target.PrefixedName() + "-migration-reads"
		fmt.Printf("plan: deploy buffer agent pointed directly at the target's future write endpoint, redirect Service %s/%s to it, "+
			"snapshot PVC %s/%s, create %s/%s from the snapshot, then cut traffic over to it once ready — the old Deployment and PVC "+
			"are never deleted. Service %s/%s serves both reads and writes, and the buffer agent only handles writes, so queries "+
			"against %s/%s will fail as soon as writes are redirected to the buffer agent; %s/%s offers a read-only path to the old "+
			"storage for the rest of the migration — point read-only clients at the target once it's ready. A client with a "+
			"connection already established to the old Deployment before that redirect — directly, or routed through the Service, "+
			"since a selector change only affects new connections — can keep writing to it afterward; that write is captured only "+
			"if it lands before the snapshot above, an unavoidable limit of any Service-based cutover while the old workload keeps "+
			"running\n",
			oldSvc.Namespace, oldSvc.Name, pvc.Namespace, pvc.Name, target.GetNamespace(), target.GetName(),
			oldSvc.Namespace, oldSvc.Name, oldSvc.Namespace, oldSvc.Name, target.GetNamespace(), readsAlias)
	default:
		fmt.Printf("plan: deploy a buffer agent pointed at the target's future write endpoint and redirect Service %s/%s to it, delete Deployment %s/%s, "+
			"rebind PVC %s/%s -> %s/%s (PV %s), create %s %s/%s, cut Service %s/%s over to the target once ready — reads are unavailable for the "+
			"whole window since the buffer agent only handles writes and old storage is deleted rather than kept running. A client with a "+
			"connection already established to the old Deployment before that redirect — directly, or routed through the Service, since a "+
			"selector change only affects new connections — can keep writing to it afterward; unlike NoDowntime, that write isn't lost as "+
			"long as it lands before the old pod has actually terminated, since its PVC is rebound (not snapshotted) under the target — "+
			"only a write attempted after that pod is fully gone fails outright, visibly, at the client\n",
			oldSvc.Namespace, oldSvc.Name, d.Deployment.Namespace, d.Deployment.Name,
			pvc.Namespace, pvc.Name, target.GetNamespace(), targetPVCName, pvc.Spec.VolumeName,
			target.GetObjectKind().GroupVersionKind().Kind, target.GetNamespace(), target.GetName(),
			oldSvc.Namespace, oldSvc.Name)
	}

	targetAlreadyExists := false
	existsCheck := target.DeepCopyObject().(Target)
	getErr := c.Get(ctx, types.NamespacedName{Name: target.GetName(), Namespace: target.GetNamespace()}, existsCheck)
	switch {
	case getErr == nil:
		targetAlreadyExists = true
		if err := verifySameSpec(existsCheck, target); err != nil {
			return err
		}
		if noDowntime {
			if err := verifyRestoredPVC(ctx, c, types.NamespacedName{Name: target.PrefixedName(), Namespace: target.GetNamespace()}, pvc.Namespace, pvc.Name+"-migration-snapshot", pvc.Name); err != nil {
				return err
			}
		}
	case !k8serrors.IsNotFound(getErr):
		return fmt.Errorf("cannot check for an existing target CR %s/%s: %w", target.GetNamespace(), target.GetName(), getErr)
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

	var readsAliasSvc *corev1.Service
	oldSvcServesReads := selectorDiffersFromBase(oldSvc.Spec.Selector, agentSelectorLabels)
	if noDowntime {
		originalSelector := d.Deployment.Spec.Selector.MatchLabels
		readsAliasSvc, err = createAliasService(ctx, c, readsAlias, target.GetNamespace(), oldSvc, originalSelector)
		if err != nil {
			return fmt.Errorf("cannot create read alias Service: %w", err)
		}
		defer func() {
			if err == nil {
				return
			}
			if oldSvcServesReads {
				if delErr := c.Delete(ctx, readsAliasSvc); delErr != nil && !k8serrors.IsNotFound(delErr) {
					fmt.Printf("warning: migration failed and left read alias Service %s/%s behind, please remove it manually: %v\n", readsAliasSvc.Namespace, readsAliasSvc.Name, delErr)
				}
				return
			}
			fmt.Printf("warning: migration failed after redirecting %s/%s away from reads — leaving read alias Service %s/%s running as the only read path; remove it manually once you've recovered\n",
				oldSvc.Namespace, oldSvc.Name, readsAliasSvc.Namespace, readsAliasSvc.Name)
		}()
	}

	agent, err = ensureBufferAgentRunning(ctx, c, agent, targetWriteURL, engine.AgentMatches)
	if err != nil {
		return err
	}
	if err := verifyAgentPortCompatible(ctx, c, oldSvc, agent); err != nil {
		return err
	}

	if err := cutoverServices(ctx, c, []*corev1.Service{oldSvc}, agent.SelectorLabels()); err != nil {
		return fmt.Errorf("cannot redirect incoming writes to the buffer agent: %w", err)
	}
	oldSvcServesReads = false
	if noDowntime {
		time.Sleep(WriteQuiesceGracePeriod)
	}

	var dependentConfigMaps []corev1.ConfigMap
	var dependentSecrets []corev1.Secret
	switch {
	case noDowntime:
		if hasForceMergeAuthKey(d.Deployment.Spec.Template.Spec.Containers) {
			fmt.Printf("warning: %s/%s sets -forceMergeAuthKey, which this tool cannot supply — skipping force-merge (continuing, snapshot will still be crash-consistent)\n", d.Deployment.Namespace, d.Deployment.Name)
		} else if pathPrefix, prefixErr := httpPathPrefix(d.Deployment.Spec.Template.Spec.Containers); prefixErr != nil {
			fmt.Printf("warning: cannot resolve http.pathPrefix (continuing, snapshot will still be crash-consistent): %v\n", prefixErr)
		} else if forceMergeURL, urlErr := forceMergeURL(readsAliasSvc, pathPrefix); urlErr != nil {
			fmt.Printf("warning: cannot build force-merge URL (continuing, snapshot will still be crash-consistent): %v\n", urlErr)
		} else if err := forceMerge(ctx, httpClient, forceMergeURL); err != nil {
			fmt.Printf("warning: force-merge on old storage failed (continuing, snapshot will still be crash-consistent): %v\n", err)
		}
	case resumeRebindPending:
		if _, err := rebindPVC(ctx, c, pvc, targetPVCName, target.GetNamespace(), nil); err != nil {
			return fmt.Errorf("storage rebind failed (writes are safely buffered on disk): %w", err)
		}
	case resuming:
		if err := ensureReclaimPolicyRestored(ctx, c, types.NamespacedName{Name: targetPVCName, Namespace: target.GetNamespace()}); err != nil {
			return err
		}
	case !resuming:
		dependentConfigMaps, dependentSecrets = dependentConfigsOf(&d.Deployment.Spec.Template.Spec, d.ConfigMaps, d.Secrets)
		if err := c.Delete(ctx, d.Deployment); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("cannot delete old Deployment %s/%s (writes are safely buffered on disk): %w", d.Deployment.Namespace, d.Deployment.Name, err)
		}
		if err := waitPodsGone(ctx, c, d.Deployment.Namespace, d.Deployment.Spec.Selector.MatchLabels); err != nil {
			return err
		}
		if _, err := rebindPVC(ctx, c, pvc, targetPVCName, target.GetNamespace(), nil); err != nil {
			return fmt.Errorf("storage rebind failed (writes are safely buffered on disk): %w", err)
		}
	}

	if noDowntime {
		snapName := pvc.Name + "-migration-snapshot"
		newPVCName := types.NamespacedName{Name: target.PrefixedName(), Namespace: target.GetNamespace()}
		existingTarget := target.DeepCopyObject().(Target)
		getErr := c.Get(ctx, types.NamespacedName{Name: target.GetName(), Namespace: target.GetNamespace()}, existingTarget)
		switch {
		case getErr == nil:
			target = existingTarget
		case k8serrors.IsNotFound(getErr):
			if err := deleteStaleRestoredPVC(ctx, c, newPVCName, pvc.Namespace, snapName, pvc.Name); err != nil {
				return err
			}
			if err := createVolumeSnapshot(ctx, c, pvc.Namespace, snapName, pvc.Name, opts.SnapshotClassName); err != nil {
				return fmt.Errorf("cannot create VolumeSnapshot (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
			}
			if err := waitVolumeSnapshotReady(ctx, c, pvc.Namespace, snapName); err != nil {
				return err
			}
			newPVC := newPVCFromSnapshot(target.PrefixedName(), target.GetNamespace(), snapName, pvc)
			if err := c.Create(ctx, newPVC); err != nil {
				return fmt.Errorf("cannot create PVC from snapshot (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
			}
			if err := c.Create(ctx, target); err != nil {
				if !k8serrors.IsAlreadyExists(err) {
					return fmt.Errorf("cannot create target CR %s/%s (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", target.GetNamespace(), target.GetName(), err)
				}
				existing := target.DeepCopyObject().(Target)
				if err := c.Get(ctx, types.NamespacedName{Name: target.GetName(), Namespace: target.GetNamespace()}, existing); err != nil {
					return fmt.Errorf("cannot fetch existing target CR %s/%s: %w", target.GetNamespace(), target.GetName(), err)
				}
				if err := verifySameSpec(existing, target); err != nil {
					return err
				}
				target = existing
			}
		default:
			return fmt.Errorf("cannot check for existing target CR %s/%s: %w", target.GetNamespace(), target.GetName(), getErr)
		}
	} else if !targetAlreadyExists {
		if err := c.Create(ctx, target); err != nil {
			return fmt.Errorf("cannot create target CR %s/%s (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", target.GetNamespace(), target.GetName(), err)
		}
	}
	if err := waitForOperational(ctx, c, target); err != nil {
		return fmt.Errorf("target CR did not become ready (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
	}

	if len(dependentConfigMaps) > 0 || len(dependentSecrets) > 0 {
		if err := deleteDependentConfigs(ctx, c, target, dependentConfigMaps, dependentSecrets); err != nil {
			return err
		}
	}

	if err := waitEndpointsReady(ctx, c, target.GetNamespace(), target.PrefixedName()); err != nil {
		return fmt.Errorf("target's Service did not become ready (writes are safely buffered on disk; fix the issue and re-run this command to resume): %w", err)
	}

	if err := cutoverServices(ctx, c, []*corev1.Service{oldSvc}, target.SelectorLabels()); err != nil {
		return fmt.Errorf("final traffic cutover failed: %w", err)
	}
	if noDowntime {
		fmt.Printf("%s/%s now serves both reads and writes — read clients still on alias Service %s/%s must move off it before it's deleted, since deletion happens with no grace period\n",
			oldSvc.Namespace, oldSvc.Name, readsAliasSvc.Namespace, readsAliasSvc.Name)
		time.Sleep(WriteQuiesceGracePeriod)
		if opts.Yes || confirm(fmt.Sprintf("delete read alias Service %s/%s now?", readsAliasSvc.Namespace, readsAliasSvc.Name)) {
			if err := c.Delete(ctx, readsAliasSvc); err != nil && !k8serrors.IsNotFound(err) {
				fmt.Printf("warning: cannot delete read alias Service %s/%s, please remove it manually: %v\n", readsAliasSvc.Namespace, readsAliasSvc.Name, err)
			}
		} else {
			fmt.Printf("leaving read alias Service %s/%s running — delete it manually once all read clients have moved off it\n", readsAliasSvc.Namespace, readsAliasSvc.Name)
		}
	}

	time.Sleep(WriteQuiesceGracePeriod)
	if err := waitQueueDrained(ctx, c, httpClient, agent, engine.QueueMetricName); err != nil {
		return fmt.Errorf("buffer agent queue did not fully drain after final cutover — leaving it running so buffered writes aren't lost; investigate and drain it manually, or re-run this command: %w", err)
	}
	deleteBufferAgent(ctx, c, agent)

	fmt.Printf("migration complete — Service %s/%s now points at %s %s/%s, but it's still tracked by the Helm release %q: running `helm uninstall` "+
		"deletes it along with everything else, taking down the endpoint clients are using. Move clients to the target's own Service "+
		"(%s/%s) first, then decommission the release once nothing depends on %s/%s anymore\n",
		oldSvc.Namespace, oldSvc.Name, target.GetObjectKind().GroupVersionKind().Kind, target.GetNamespace(), target.GetName(), opts.ReleaseName,
		target.GetNamespace(), target.PrefixedName(), oldSvc.Namespace, oldSvc.Name)
	if noDowntime {
		fmt.Println("note: the old Deployment and its PVC were left untouched")
	}
	return nil
}
