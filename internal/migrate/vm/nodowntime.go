// Package vm implements VictoriaMetrics-specific `helm-converter migrate` orchestration.
// Engine-agnostic pieces live in the parent internal/migrate package instead.
package vm

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// queueMetricName is VMAgent's persistent-queue gauge (see
// internal/controller/operator/factory/vmdistributed/vmdistributed.go).
const queueMetricName = "vm_persistentqueue_bytes_pending"

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

	fmt.Printf("plan: deploy buffer VMAgent, redirect Service %s/%s to it, snapshot PVC %s/%s, "+
		"create %s/%s from the snapshot, backfill, then cut traffic over to it — the old Deployment and PVC are never deleted\n",
		oldSvc.Namespace, oldSvc.Name, pvc.Namespace, pvc.Name, target.GetNamespace(), target.GetName())

	if opts.DryRun {
		fmt.Println("dry-run: stopping before any mutation")
		return nil
	}
	if !opts.Yes {
		if !migrate.Confirm("proceed with the above plan?") {
			return fmt.Errorf("aborted by user")
		}
	}

	// The alias must be created, and its URL used for the agent, before oldSvc's own
	// selector is redirected below — see CreateAliasService's doc comment for why.
	originalSelector := oldSvc.Spec.Selector
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
	if err := c.Create(ctx, agent); err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("cannot create buffer VMAgent %s/%s: %w", agent.Namespace, agent.Name, err)
	}

	if err := migrate.CutoverServices(ctx, c, []*corev1.Service{oldSvc}, agent.SelectorLabels()); err != nil {
		return fmt.Errorf("cannot redirect incoming writes to the buffer agent: %w", err)
	}

	if forceMergeURL, urlErr := migrate.ForceMergeURL(aliasSvc); urlErr != nil {
		fmt.Printf("warning: cannot build force-merge URL (continuing, snapshot will still be crash-consistent): %v\n", urlErr)
	} else if err := migrate.ForceMerge(ctx, httpClient, forceMergeURL); err != nil {
		fmt.Printf("warning: force-merge on old storage failed (continuing, snapshot will still be crash-consistent): %v\n", err)
	}

	snapName := pvc.Name + "-migration-snapshot"
	if err := migrate.CreateVolumeSnapshot(ctx, c, pvc.Namespace, snapName, pvc.Name, opts.SnapshotClassName); err != nil && !k8serrors.IsAlreadyExists(err) {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("cannot create VolumeSnapshot: %w", err))
	}
	if err := migrate.WaitVolumeSnapshotReady(ctx, c, pvc.Namespace, snapName); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, err)
	}

	newPVC := migrate.NewPVCFromSnapshot(target.PrefixedName(), target.Namespace, snapName, pvc)
	if err := c.Create(ctx, newPVC); err != nil && !k8serrors.IsAlreadyExists(err) {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("cannot create PVC from snapshot: %w", err))
	}
	if err := migrate.WaitPVCBound(ctx, c, types.NamespacedName{Name: newPVC.Name, Namespace: newPVC.Namespace}); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, err)
	}

	if err := c.Create(ctx, target); err != nil && !k8serrors.IsAlreadyExists(err) {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("cannot create target CR %s/%s: %w", target.Namespace, target.Name, err))
	}
	if err := migrate.WaitForOperational(ctx, c, target); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("target CR did not become ready: %w", err))
	}

	agent.Spec.RemoteWrite = []vmv1beta1.VMAgentRemoteWriteSpec{{URL: target.GetRemoteWriteURL()}}
	if err := c.Update(ctx, agent); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("cannot repoint buffer agent to new storage: %w", err))
	}

	if err := migrate.WaitQueueDrained(ctx, c, httpClient, agent, queueMetricName, 0); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, err)
	}

	if err := migrate.CutoverServices(ctx, c, []*corev1.Service{oldSvc}, target.SelectorLabels()); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldSvc, originalSelector, fmt.Errorf("final traffic cutover failed: %w", err))
	}

	if err := c.Delete(ctx, agent); err != nil && !k8serrors.IsNotFound(err) {
		fmt.Printf("warning: cannot delete buffer VMAgent %s/%s, please remove it manually: %v\n", agent.Namespace, agent.Name, err)
	}
	if err := c.Delete(ctx, aliasSvc); err != nil && !k8serrors.IsNotFound(err) {
		fmt.Printf("warning: cannot delete alias Service %s/%s, please remove it manually: %v\n", aliasSvc.Namespace, aliasSvc.Name, err)
	}

	fmt.Println("migration complete — the old Deployment and its PVC were left untouched; decommission them (e.g. helm uninstall) whenever you're ready")
	return nil
}
