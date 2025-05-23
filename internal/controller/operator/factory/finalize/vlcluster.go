package finalize

import (
	"context"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func OnVClusterDelete(ctx context.Context, rclient client.Client, crd *vmv1.VLCluster) error {

	return nil
}
