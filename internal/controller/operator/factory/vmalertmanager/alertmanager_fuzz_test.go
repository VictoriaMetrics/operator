package vmalertmanager

import (
	"context"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

func FuzzCreateOrUpdate(f *testing.F) {
	fuzzutil.CreateOrUpdate(f, []string{
		`{}`,
		`{"spec":{"replicaCount":1}}`,
		`{"spec":{"replicaCount":2,"configRawYaml":"route:\n  receiver: r\nreceivers:\n- name: r\n","serviceAccountName":"sa","podDisruptionBudget":{"minAvailable":1},"vpa":{}}}`,
		`{"spec":{"selectAllByDefault":true,"configSecret":"cfg","storage":{"volumeClaimTemplate":{"spec":{"resources":{"requests":{"storage":"1Gi"}}}}}}}`,
		`{"spec":{"replicaCount":2},"status":{"lastAppliedSpec":{"replicaCount":1}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1beta1.VMAlertmanager { return &vmv1beta1.VMAlertmanager{} }, func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlertmanager) error {
		return CreateOrUpdateAlertManager(ctx, cr, rclient)
	})
}
