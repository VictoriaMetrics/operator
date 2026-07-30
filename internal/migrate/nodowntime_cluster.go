package migrate

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/podutil"
)

// ForceMergeComponentPods best-effort force-merges every ready pod behind a StatefulSet
// component's Service, since each pod holds an independent shard. Failures are only warned
// about: CSI snapshots are crash-consistent regardless.
func ForceMergeComponentPods(ctx context.Context, c client.Client, httpClient *http.Client, namespace, serviceName, portName string) {
	addrs, err := podutil.DiscoverEndpointAddrs(ctx, c, namespace, serviceName, portName, "http", "/internal/force_merge")
	if err != nil {
		fmt.Printf("warning: cannot discover %s pods for force-merge (continuing, snapshots will still be crash-consistent): %v\n", serviceName, err)
		return
	}
	for addr := range addrs {
		if err := ForceMerge(ctx, httpClient, addr); err != nil {
			fmt.Printf("warning: force-merge failed for %s (continuing, snapshot will still be crash-consistent): %v\n", addr, err)
		}
	}
}

// SnapshotClusterPVCs snapshots each of a StatefulSet component's per-ordinal PVCs and
// provisions a corresponding new PVC named <targetPVCTemplateName>-<targetWorkloadName>-
// <ordinal>, since NoDowntime never touches the old PVCs directly. Requires contiguous
// ordinals from 0, same as rebindClusterPVCs.
func SnapshotClusterPVCs(ctx context.Context, c client.Client, pvcs []corev1.PersistentVolumeClaim, targetPVCTemplateName, targetWorkloadName, targetNamespace, snapshotClassName string) error {
	byOrdinal, err := pvcsByOrdinal(pvcs)
	if err != nil {
		return err
	}
	for i := 0; i < len(byOrdinal); i++ {
		pvc, ok := byOrdinal[i]
		if !ok {
			return fmt.Errorf("PVC ordinals are not contiguous from 0 (missing ordinal %d among %d discovered PVCs) — refusing to guess a partial snapshot", i, len(pvcs))
		}
		snapName := pvc.Name + "-migration-snapshot"
		if err := CreateVolumeSnapshot(ctx, c, pvc.Namespace, snapName, pvc.Name, snapshotClassName); err != nil && !k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("cannot create VolumeSnapshot for PVC %q: %w", pvc.Name, err)
		}
		if err := WaitVolumeSnapshotReady(ctx, c, pvc.Namespace, snapName); err != nil {
			return fmt.Errorf("PVC %q: %w", pvc.Name, err)
		}
		targetName := fmt.Sprintf("%s-%s-%d", targetPVCTemplateName, targetWorkloadName, i)
		newPVC := NewPVCFromSnapshot(targetName, targetNamespace, snapName, pvc)
		if err := c.Create(ctx, newPVC); err != nil && !k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("cannot create PVC %q from snapshot: %w", targetName, err)
		}
		if err := WaitPVCBound(ctx, c, types.NamespacedName{Name: targetName, Namespace: targetNamespace}); err != nil {
			return fmt.Errorf("PVC %q: %w", targetName, err)
		}
	}
	return nil
}
