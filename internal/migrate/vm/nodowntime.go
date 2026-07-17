// Package vm implements VictoriaMetrics-specific `helm-converter migrate` orchestration.
// Engine-agnostic pieces live in the parent internal/migrate package instead.
package vm

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// NoDowntimeSingleNode runs the NoDowntime strategy for victoria-metrics-single: never
// touches the old Deployment/PVC, provisions fresh storage from a CSI snapshot while
// buffering writes through a VMAgent proxy, then repoints traffic once caught up.
func NoDowntimeSingleNode(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1beta1.VMSingle) error {
	d, err := migrate.Discover(ctx, c, opts.Namespace, opts.ReleaseName)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}
	if d.Deployment == nil {
		return fmt.Errorf("no Deployment found for release %q in namespace %q", opts.ReleaseName, opts.Namespace)
	}
	pvc, err := d.SingleNodePVC()
	if err != nil {
		return fmt.Errorf("storage discovery failed: %w", err)
	}
	if len(d.Services) != 1 {
		return fmt.Errorf("found %d Services for release %q, expected exactly 1", len(d.Services), opts.ReleaseName)
	}
	oldSvc := &d.Services[0]

	readsAlias := target.PrefixedName() + "-migration-reads"
	fmt.Printf("plan: deploy buffer VMAgent, redirect Service %s/%s to it, snapshot PVC %s/%s, "+
		"create %s/%s from the snapshot, backfill, then cut traffic over to it — the old Deployment and PVC are never deleted. "+
		"Service %s/%s serves both reads and writes, and the buffer agent only handles writes, so queries against %[1]s/%[2]s "+
		"will fail until final cutover; point read-only clients at %s/%s in the meantime if that matters\n",
		oldSvc.Namespace, oldSvc.Name, pvc.Namespace, pvc.Name, target.GetNamespace(), target.GetName(), target.Namespace, readsAlias)

	if opts.DryRun {
		fmt.Println("dry-run: stopping before any mutation")
		return nil
	}
	if !opts.Yes {
		if !migrate.Confirm("proceed with the above plan?") {
			return fmt.Errorf("aborted by user")
		}
	}

	originalSelector := oldSvc.Spec.Selector
	readsAliasSvc, err := migrate.CreateAliasService(ctx, c, readsAlias, target.Namespace, oldSvc.Spec.Ports, originalSelector)
	if err != nil {
		return fmt.Errorf("cannot create read alias Service: %w", err)
	}
	aliasSvc, err := migrate.CreateAliasService(ctx, c, target.PrefixedName()+"-migration-source", target.Namespace, oldSvc.Spec.Ports, originalSelector)
	if err != nil {
		return fmt.Errorf("cannot create alias Service to preserve a stable path to the old storage: %w", err)
	}

	oldWriteURL, err := remoteWriteURL(aliasSvc)
	if err != nil {
		return fmt.Errorf("cannot compute old storage's write URL: %w", err)
	}
	agentName := target.PrefixedName() + "-migration-buffer"
	agent, err := newBufferAgent(agentName, target.Namespace, oldWriteURL, opts.AgentBufferSize)
	if err != nil {
		return fmt.Errorf("cannot build buffer agent spec: %w", err)
	}
	if err := c.Create(ctx, agent); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("cannot create buffer VMAgent %s/%s: %w", agent.Namespace, agent.Name, err)
		}
		var existing vmv1beta1.VMAgent
		if err := c.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, &existing); err != nil {
			return fmt.Errorf("cannot fetch existing buffer VMAgent %s/%s: %w", agent.Namespace, agent.Name, err)
		}
		if len(existing.Spec.RemoteWrite) != 1 || existing.Spec.RemoteWrite[0].URL != oldWriteURL {
			return fmt.Errorf("buffer VMAgent %s/%s already exists but isn't configured to forward to %q — "+
				"likely left over from a previous migration attempt; delete it manually before retrying", agent.Namespace, agent.Name, oldWriteURL)
		}
		agent = &existing
	}
	if err := migrate.WaitForOperational(ctx, c, agent); err != nil {
		return fmt.Errorf("buffer VMAgent did not become ready: %w", err)
	}
	if err := migrate.WaitEndpointsReady(ctx, c, agent.Namespace, agent.PrefixedName(), "http"); err != nil {
		return fmt.Errorf("buffer VMAgent's Service did not become ready: %w", err)
	}

	if err := migrate.CutoverServices(ctx, c, []*corev1.Service{oldSvc}, agent.SelectorLabels()); err != nil {
		return fmt.Errorf("cannot redirect incoming writes to the buffer agent: %w", err)
	}

	if forceMergeURL, urlErr := migrate.ForceMergeURL(aliasSvc); urlErr != nil {
		fmt.Printf("warning: cannot build force-merge URL (continuing, snapshot will still be crash-consistent): %v\n", urlErr)
	} else if err := migrate.ForceMerge(ctx, httpClient, forceMergeURL); err != nil {
		fmt.Printf("warning: force-merge on old storage failed (continuing, snapshot will still be crash-consistent): %v\n", err)
	}

	if err := c.Delete(ctx, aliasSvc); err != nil && !k8serrors.IsNotFound(err) {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("cannot delete alias Service to stop writes reaching the old storage before the snapshot: %w", err))
	}
	time.Sleep(migrate.WriteQuiesceGracePeriod)

	snapName := pvc.Name + "-migration-snapshot"
	if err := migrate.CreateVolumeSnapshot(ctx, c, pvc.Namespace, snapName, pvc.Name, opts.SnapshotClassName); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("cannot create VolumeSnapshot: %w", err))
	}
	if err := migrate.WaitVolumeSnapshotReady(ctx, c, pvc.Namespace, snapName); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, err)
	}

	newPVCName := types.NamespacedName{Name: target.PrefixedName(), Namespace: target.Namespace}
	if err := migrate.DeleteAndAwaitGone(ctx, c, &corev1.PersistentVolumeClaim{}, newPVCName); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("cannot delete stale PVC %s: %w", newPVCName, err))
	}
	newPVC := migrate.NewPVCFromSnapshot(target.PrefixedName(), target.Namespace, snapName, pvc)
	if err := c.Create(ctx, newPVC); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("cannot create PVC from snapshot: %w", err))
	}
	if err := migrate.WaitPVCBound(ctx, c, types.NamespacedName{Name: newPVC.Name, Namespace: newPVC.Namespace}); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, err)
	}

	if err := c.Create(ctx, target); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("cannot create target CR %s/%s: %w", target.Namespace, target.Name, err))
		}
		var existing vmv1beta1.VMSingle
		if err := c.Get(ctx, types.NamespacedName{Name: target.Name, Namespace: target.Namespace}, &existing); err != nil {
			return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("cannot fetch existing target CR %s/%s: %w", target.Namespace, target.Name, err))
		}
		if !reflect.DeepEqual(existing.Spec, target.Spec) {
			return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("target CR %s/%s already exists with a different spec — refusing to adopt it; delete it manually before retrying", target.Namespace, target.Name))
		}
		*target = existing
	}
	if err := migrate.WaitForOperational(ctx, c, target); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("target CR did not become ready: %w", err))
	}

	agent.Spec.RemoteWrite = []vmv1beta1.VMAgentRemoteWriteSpec{{URL: target.GetRemoteWriteURL()}}
	if err := c.Update(ctx, agent); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("cannot repoint buffer agent to new storage: %w", err))
	}
	// Past this point, oldSvc must stay pointed at the buffer agent (now targeting the new
	// storage) on any failure: reverting it to the old backend would send new writes there
	// while everything already drained through the agent lives only in the new storage,
	// silently forking the data.
	if err := migrate.WaitForOperational(ctx, c, agent); err != nil {
		return fmt.Errorf("buffer VMAgent did not roll out its new remote-write target: %w", err)
	}

	if err := migrate.WaitQueueDrained(ctx, c, httpClient, agent, vmv1beta1.VMAgentQueueMetricName, 0); err != nil {
		return err
	}

	if err := migrate.WaitEndpointsReady(ctx, c, target.Namespace, target.PrefixedName(), "http"); err != nil {
		return fmt.Errorf("target VMSingle's Service did not become ready: %w", err)
	}
	if err := migrate.CutoverServices(ctx, c, []*corev1.Service{oldSvc}, target.SelectorLabels()); err != nil {
		return fmt.Errorf("final traffic cutover failed: %w", err)
	}

	time.Sleep(migrate.WriteQuiesceGracePeriod)
	if err := migrate.WaitQueueDrained(ctx, c, httpClient, agent, vmv1beta1.VMAgentQueueMetricName, 0); err != nil {
		fmt.Printf("warning: buffer VMAgent queue did not fully drain after final cutover, some in-flight writes may need manual replay: %v\n", err)
	}
	if err := c.Delete(ctx, agent); err != nil && !k8serrors.IsNotFound(err) {
		fmt.Printf("warning: cannot delete buffer VMAgent %s/%s, please remove it manually: %v\n", agent.Namespace, agent.Name, err)
	}
	if err := c.Delete(ctx, readsAliasSvc); err != nil && !k8serrors.IsNotFound(err) {
		fmt.Printf("warning: cannot delete read alias Service %s/%s, please remove it manually: %v\n", readsAliasSvc.Namespace, readsAliasSvc.Name, err)
	}

	fmt.Println("migration complete — the old Deployment and its PVC were left untouched; decommission them (e.g. helm uninstall) whenever you're ready")
	return nil
}
