package vmauth

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
		`{"spec":{"unauthorizedUserAccessSpec":{"url_prefix":["http://backend"]}}}`,
		`{"spec":{"replicaCount":2,"serviceAccountName":"sa","podDisruptionBudget":{"minAvailable":1},"vpa":{},"hpa":{"minReplicas":1,"maxReplicas":3},"ingress":{"class_name":"nginx","host":"a.example.com"}}}`,
		`{"spec":{"httpRoute":{"parentRefs":[{"name":"gw"}],"hostnames":["a.example.com"]},"selectAllByDefault":true,"configSecret":"cfg"}}`,
		`{"spec":{"replicaCount":2},"status":{"lastAppliedSpec":{"replicaCount":1}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1beta1.VMAuth { return &vmv1beta1.VMAuth{} }, func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) error {
		return CreateOrUpdate(ctx, cr, rclient)
	})
}
