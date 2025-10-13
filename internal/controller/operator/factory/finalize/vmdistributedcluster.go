package finalize

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
)

// OnVMDistributedClusterDelete removes all objects related to vmdistributedcluster component
func OnVMDistributedClusterDelete(ctx context.Context, rclient client.Client, obj *vmv1alpha1.VMDistributedCluster) error {
	// TODO[vrutkovs]: No actions required?
	return nil
}
