package vm

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// NoDowntimeCluster runs the NoDowntime strategy for victoria-metrics-cluster. Only vminsert
// (writes) needs buffering; vmselect (reads) just cuts over once ready; vmstorage has no
// client-facing Service, so its per-ordinal PVCs are just CSI-snapshotted.
func NoDowntimeCluster(ctx context.Context, c client.Client, httpClient *http.Client, opts migrate.Options, target *vmv1beta1.VMCluster) error {
	dInsert, err := migrate.DiscoverComponent(ctx, c, opts.Namespace, opts.ReleaseName, "vminsert")
	if err != nil {
		return fmt.Errorf("discovery failed for vminsert: %w", err)
	}
	if dInsert.Deployment == nil {
		return fmt.Errorf("no Deployment found for vminsert component in namespace %q", opts.Namespace)
	}
	if len(dInsert.Services) != 1 {
		return fmt.Errorf("found %d Services for vminsert component, expected exactly 1", len(dInsert.Services))
	}
	oldInsertSvc := &dInsert.Services[0]

	dSelect, err := migrate.DiscoverComponent(ctx, c, opts.Namespace, opts.ReleaseName, "vmselect")
	if err != nil {
		return fmt.Errorf("discovery failed for vmselect: %w", err)
	}
	if len(dSelect.Services) != 1 {
		return fmt.Errorf("found %d Services for vmselect component, expected exactly 1", len(dSelect.Services))
	}
	oldSelectSvc := &dSelect.Services[0]

	dStorage, err := migrate.DiscoverComponent(ctx, c, opts.Namespace, opts.ReleaseName, "vmstorage")
	if err != nil {
		return fmt.Errorf("discovery failed for vmstorage: %w", err)
	}
	if _, err := dStorage.SingleStatefulSet(); err != nil {
		return fmt.Errorf("vmstorage: %w", err)
	}
	if len(dStorage.Services) != 1 {
		return fmt.Errorf("found %d Services for vmstorage component, expected exactly 1", len(dStorage.Services))
	}
	oldStorageSvc := &dStorage.Services[0]
	if len(dStorage.PVCs) == 0 {
		return fmt.Errorf("no PersistentVolumeClaims found for vmstorage component in namespace %q", opts.Namespace)
	}

	storageWorkloadName := target.PrefixedName(vmv1beta1.ClusterComponentStorage)
	var storagePVCTemplateName string
	if target.Spec.VMStorage != nil && target.Spec.VMStorage.Storage != nil {
		storagePVCTemplateName = target.Spec.VMStorage.GetStorageVolumeName()
	}
	if storagePVCTemplateName == "" {
		return fmt.Errorf("vmstorage has %d existing PVC(s) but the target CR has no storage configured for it — refusing to proceed and orphan data", len(dStorage.PVCs))
	}

	fmt.Printf("plan: deploy buffer VMAgent pointed at old vminsert, redirect vminsert's Service to it, force-merge + snapshot vmstorage's %d PVC(s), "+
		"create %s/%s from the snapshots, backfill, then cut vminsert/vmselect traffic over to it — the old workloads and PVCs are never deleted\n",
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

	// See vm/nodowntime.go for why the alias must be created before oldInsertSvc's selector
	// is redirected below.
	originalInsertSelector := oldInsertSvc.Spec.Selector
	aliasSvc, err := migrate.CreateAliasService(ctx, c, target.PrefixedName(vmv1beta1.ClusterComponentInsert)+"-migration-source", target.Namespace, oldInsertSvc.Spec.Ports, originalInsertSelector)
	if err != nil {
		return fmt.Errorf("cannot create alias Service to preserve a stable path to the old vminsert: %w", err)
	}

	oldWriteURL, err := insertRemoteWriteURL(aliasSvc)
	if err != nil {
		return fmt.Errorf("cannot compute old vminsert's write URL: %w", err)
	}
	agentName := target.PrefixedName(vmv1beta1.ClusterComponentInsert) + "-migration-buffer"
	agent, err := newBufferAgent(agentName, target.Namespace, oldWriteURL, opts.AgentBufferSize)
	if err != nil {
		return fmt.Errorf("cannot build buffer agent spec: %w", err)
	}
	if err := c.Create(ctx, agent); err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("cannot create buffer VMAgent %s/%s: %w", agent.Namespace, agent.Name, err)
	}

	if err := migrate.CutoverServices(ctx, c, []*corev1.Service{oldInsertSvc}, agent.SelectorLabels()); err != nil {
		return fmt.Errorf("cannot redirect incoming writes to the buffer agent: %w", err)
	}

	migrate.ForceMergeComponentPods(ctx, c, httpClient, opts.Namespace, oldStorageSvc.Name, "http")

	if err := migrate.SnapshotClusterPVCs(ctx, c, dStorage.PVCs, storagePVCTemplateName, storageWorkloadName, target.Namespace, opts.SnapshotClassName); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldInsertSvc, originalInsertSelector, fmt.Errorf("vmstorage snapshot failed: %w", err))
	}

	if err := c.Create(ctx, target); err != nil && !k8serrors.IsAlreadyExists(err) {
		return migrate.RevertSelectorAndFail(ctx, c, oldInsertSvc, originalInsertSelector, fmt.Errorf("cannot create target CR %s/%s: %w", target.Namespace, target.Name, err))
	}
	if err := migrate.WaitForOperational(ctx, c, target); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldInsertSvc, originalInsertSelector, fmt.Errorf("target CR did not become ready: %w", err))
	}

	agent.Spec.RemoteWrite = []vmv1beta1.VMAgentRemoteWriteSpec{{URL: target.GetRemoteWriteURL()}}
	if err := c.Update(ctx, agent); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldInsertSvc, originalInsertSelector, fmt.Errorf("cannot repoint buffer agent to new storage: %w", err))
	}

	if err := migrate.WaitQueueDrained(ctx, c, httpClient, agent, queueMetricName, 0); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldInsertSvc, originalInsertSelector, err)
	}

	if err := migrate.CutoverServices(ctx, c, []*corev1.Service{oldInsertSvc}, target.SelectorLabels(vmv1beta1.ClusterComponentInsert)); err != nil {
		return migrate.RevertSelectorAndFail(ctx, c, oldInsertSvc, originalInsertSelector, fmt.Errorf("final vminsert traffic cutover failed: %w", err))
	}
	if err := migrate.CutoverServices(ctx, c, []*corev1.Service{oldSelectSvc}, target.SelectorLabels(vmv1beta1.ClusterComponentSelect)); err != nil {
		return fmt.Errorf("vminsert already cut over to the target, but vmselect traffic cutover failed — fix its Service selector manually: %w", err)
	}

	if err := c.Delete(ctx, agent); err != nil && !k8serrors.IsNotFound(err) {
		fmt.Printf("warning: cannot delete buffer VMAgent %s/%s, please remove it manually: %v\n", agent.Namespace, agent.Name, err)
	}
	if err := c.Delete(ctx, aliasSvc); err != nil && !k8serrors.IsNotFound(err) {
		fmt.Printf("warning: cannot delete alias Service %s/%s, please remove it manually: %v\n", aliasSvc.Namespace, aliasSvc.Name, err)
	}

	fmt.Println("migration complete — the old workloads and PVCs were left untouched; decommission them (e.g. helm uninstall) whenever you're ready")
	return nil
}
