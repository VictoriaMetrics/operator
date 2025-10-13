package vmdistributedcluster

import (
	"context"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateOrUpdate - handles VM deployment reconciliation.
func CreateOrUpdate(ctx context.Context, cr *vmv1alpha1.VMDistributedCluster, rclient client.Client) error {

	var prevCR *vmv1alpha1.VMDistributedCluster
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}

	return nil
}
