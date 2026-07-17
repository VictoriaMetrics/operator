package vl

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// NoDowntimeCluster runs the NoDowntime strategy for victoria-logs-cluster. Only vlinsert
// (writes) needs buffering; vlselect (reads) just cuts over once ready; vlstorage has no
// client-facing Service, so its per-ordinal PVCs are just CSI-snapshotted.
func NoDowntimeCluster(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1.VLCluster) error {
	dInsert, err := migrate.DiscoverComponent(ctx, c, opts.Namespace, opts.ReleaseName, "vlinsert")
	if err != nil {
		return fmt.Errorf("discovery failed for vlinsert: %w", err)
	}
	if dInsert.Deployment == nil {
		return fmt.Errorf("no Deployment found for vlinsert component in namespace %q", opts.Namespace)
	}
	if len(dInsert.Services) != 1 {
		return fmt.Errorf("found %d Services for vlinsert component, expected exactly 1", len(dInsert.Services))
	}
	oldInsertSvc := &dInsert.Services[0]

	dSelect, err := migrate.DiscoverComponent(ctx, c, opts.Namespace, opts.ReleaseName, "vlselect")
	if err != nil {
		return fmt.Errorf("discovery failed for vlselect: %w", err)
	}
	if len(dSelect.Services) != 1 {
		return fmt.Errorf("found %d Services for vlselect component, expected exactly 1", len(dSelect.Services))
	}
	oldSelectSvc := &dSelect.Services[0]

	dStorage, err := migrate.DiscoverComponent(ctx, c, opts.Namespace, opts.ReleaseName, "vlstorage")
	if err != nil {
		return fmt.Errorf("discovery failed for vlstorage: %w", err)
	}
	if _, err := dStorage.SingleStatefulSet(); err != nil {
		return fmt.Errorf("vlstorage: %w", err)
	}
	if len(dStorage.Services) != 1 {
		return fmt.Errorf("found %d Services for vlstorage component, expected exactly 1", len(dStorage.Services))
	}
	oldStorageSvc := &dStorage.Services[0]
	if len(dStorage.PVCs) == 0 {
		return fmt.Errorf("no PersistentVolumeClaims found for vlstorage component in namespace %q", opts.Namespace)
	}

	storageWorkloadName := target.PrefixedName(vmv1beta1.ClusterComponentStorage)
	var storagePVCTemplateName string
	if target.Spec.VLStorage != nil && target.Spec.VLStorage.Storage != nil {
		storagePVCTemplateName = target.Spec.VLStorage.GetStorageVolumeName()
	}
	if storagePVCTemplateName == "" {
		return fmt.Errorf("vlstorage has %d existing PVC(s) but the target CR has no storage configured for it — refusing to proceed and orphan data", len(dStorage.PVCs))
	}

	fmt.Printf("plan: deploy buffer VLAgent pointed at old vlinsert, redirect vlinsert's Service to it, force-merge + snapshot vlstorage's %d PVC(s), "+
		"create %s/%s from the snapshots, backfill, then cut vlinsert/vlselect traffic over to it — the old workloads and PVCs are never deleted\n",
		len(dStorage.PVCs), target.GetNamespace(), target.GetName())

	if opts.DryRun {
		fmt.Println("dry-run: stopping before any mutation")
		return nil
	}
	if !opts.Yes {
		if !migrate.Confirm("proceed with the above plan?") {
			return fmt.Errorf("aborted by user")
		}
	}

	// See vl/nodowntime.go for why the alias must be created before oldInsertSvc's selector
	// is redirected below.
	originalInsertSelector := oldInsertSvc.Spec.Selector
	aliasSvc, err := migrate.CreateAliasService(ctx, c, target.PrefixedName(vmv1beta1.ClusterComponentInsert)+"-migration-source", target.Namespace, oldInsertSvc.Spec.Ports, originalInsertSelector)
	if err != nil {
		return fmt.Errorf("cannot create alias Service to preserve a stable path to the old vlinsert: %w", err)
	}

	oldWriteURL, err := remoteWriteURL(aliasSvc)
	if err != nil {
		return fmt.Errorf("cannot compute old vlinsert's write URL: %w", err)
	}
	agentName := target.PrefixedName(vmv1beta1.ClusterComponentInsert) + "-migration-buffer"
	agent, err := newBufferAgent(agentName, target.Namespace, oldWriteURL, opts.AgentBufferSize)
	if err != nil {
		return fmt.Errorf("cannot build buffer agent spec: %w", err)
	}
	if err := c.Create(ctx, agent); err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("cannot create buffer VLAgent %s/%s: %w", agent.Namespace, agent.Name, err)
	}

	if err := migrate.CutoverServices(ctx, c, []*corev1.Service{oldInsertSvc}, agent.SelectorLabels()); err != nil {
		return fmt.Errorf("cannot redirect incoming writes to the buffer agent: %w", err)
	}

	migrate.ForceMergeComponentPods(ctx, c, httpClient, opts.Namespace, oldStorageSvc.Name, "http")

	if err := migrate.SnapshotClusterPVCs(ctx, c, dStorage.PVCs, storagePVCTemplateName, storageWorkloadName, target.Namespace, opts.SnapshotClassName); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldInsertSvc, originalInsertSelector, fmt.Errorf("vlstorage snapshot failed: %w", err))
	}

	if err := c.Create(ctx, target); err != nil && !k8serrors.IsAlreadyExists(err) {
		return migrate.RevertSelectorAndFail(ctx, c, oldInsertSvc, originalInsertSelector, fmt.Errorf("cannot create target CR %s/%s: %w", target.Namespace, target.Name, err))
	}
	if err := migrate.WaitForOperational(ctx, c, target); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldInsertSvc, originalInsertSelector, fmt.Errorf("target CR did not become ready: %w", err))
	}

	agent.Spec.RemoteWrite = []vmv1.VLAgentRemoteWriteSpec{{URL: target.AsURL(vmv1beta1.ClusterComponentInsert, false) + "/"}}
	if err := c.Update(ctx, agent); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldInsertSvc, originalInsertSelector, fmt.Errorf("cannot repoint buffer agent to new storage: %w", err))
	}

	if err := migrate.WaitQueueDrained(ctx, c, httpClient, agent, queueMetricName, 0); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldInsertSvc, originalInsertSelector, err)
	}

	if err := migrate.CutoverServices(ctx, c, []*corev1.Service{oldInsertSvc}, target.SelectorLabels(vmv1beta1.ClusterComponentInsert)); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldInsertSvc, originalInsertSelector, fmt.Errorf("final vlinsert traffic cutover failed: %w", err))
	}
	if err := migrate.CutoverServices(ctx, c, []*corev1.Service{oldSelectSvc}, target.SelectorLabels(vmv1beta1.ClusterComponentSelect)); err != nil {
		return fmt.Errorf("vlinsert already cut over to the target, but vlselect traffic cutover failed — fix its Service selector manually: %w", err)
	}

	if err := c.Delete(ctx, agent); err != nil && !k8serrors.IsNotFound(err) {
		fmt.Printf("warning: cannot delete buffer VLAgent %s/%s, please remove it manually: %v\n", agent.Namespace, agent.Name, err)
	}
	if err := c.Delete(ctx, aliasSvc); err != nil && !k8serrors.IsNotFound(err) {
		fmt.Printf("warning: cannot delete alias Service %s/%s, please remove it manually: %v\n", aliasSvc.Namespace, aliasSvc.Name, err)
	}

	fmt.Println("migration complete — the old workloads and PVCs were left untouched; decommission them (e.g. helm uninstall) whenever you're ready")
	return nil
}
