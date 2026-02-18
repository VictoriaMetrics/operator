package vmdistributed

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

const (
	vmAgentQueueMetricName         = "vm_persistentqueue_bytes_pending"
	httpTimeout                    = 10 * time.Second
	defaultEndpointsUpdateInterval = 5 * time.Second
	defaultMetricsCheckInterval    = 50 * time.Second
	defaultStatusCheckInterval     = 5 * time.Second
)

// CreateOrUpdate handles VM deployment reconciliation.
func CreateOrUpdate(ctx context.Context, cr *vmv1alpha1.VMDistributed, rclient client.Client) error {
	// No actions performed if CR is paused
	if cr.Paused() {
		return nil
	}

	if !build.MustSkipRuntimeValidation() {
		if err := cr.Validate(); err != nil {
			return err
		}
	}
	nsn := types.NamespacedName{
		Name:      cr.Name,
		Namespace: cr.Namespace,
	}

	logger.WithContext(ctx).Info("starting reconciliation", "name", nsn)

	// build VMDistributed desired zones state
	zs, err := getZones(ctx, rclient, cr)
	if err != nil {
		return fmt.Errorf("failed to build distributed zones VMDistributed=%s: %w", nsn, err)
	}

	// Apply changes to VMClusters one by one if new spec needs to be applied
	lastZoneIdx := max(0, len(cr.Spec.Zones)-1)
	for i := range cr.Spec.Zones {
		if err := zs.upgrade(ctx, rclient, cr, i); err != nil {
			return err
		}

		// Sleep for zoneUpdatePause time between VMClusters updates (unless its the last one)
		if i != lastZoneIdx && zs.hasChanges[i] {
			item := fmt.Sprintf("%d/%d", i+1, len(cr.Spec.Zones))
			logger.WithContext(ctx).Info("pausing between zone updates", "name", nsn, "zone", item, "updatePause", cr.Spec.ZoneCommon.UpdatePause.Duration)
			select {
			case <-time.After(cr.Spec.ZoneCommon.UpdatePause.Duration):
			case <-ctx.Done():
				return fmt.Errorf("update of VMDistributed=%s was canceled by controller", nsn)
			}
		}
	}

	logger.WithContext(ctx).Info("reconciliation completed", "name", nsn)
	return nil
}
