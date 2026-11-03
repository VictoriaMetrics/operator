package vtagent

import (
	"context"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

func FuzzCreateOrUpdate(f *testing.F) {
	fuzzutil.CreateOrUpdate(f, []string{
		`{}`,
		`{"spec":{"remoteWrite":[{"url":"http://vt:10428"}]}}`,
		`{"spec":{"remoteWrite":[{"url":"http://vt:10428"}],"replicaCount":2,"serviceAccountName":"sa","podDisruptionBudget":{"minAvailable":1},"vpa":{}}}`,
		`{"spec":{"remoteWrite":[{"url":"http://vt:10428"}],"replicaCount":2},"status":{"lastAppliedSpec":{"remoteWrite":[{"url":"http://old:10428"}],"replicaCount":1}}}`,
		`{"spec":{"remoteWrite":[{"url":"http://vt:10428","basicAuth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tlsConfig":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"},"serverName":"s"},"sendTimeout":"1s","headers":["a: b"],"maxDiskUsage":"1GB","proxyURL":"http://p"},{"url":"http://vt:10428","bearerTokenSecret":{"name":"ref","key":"key"},"oauth2":{"client_id":{"secret":{"name":"ref","key":"key"}},"client_secret":{"name":"ref","key":"key"},"token_url":"http://t","scopes":["s"],"endpoint_params":{"a":"b"}}},{"url":"http://vt:10428","bearerTokenPath":"/t","basicAuth":{"password_file":"/p"},"tlsConfig":{"caFile":"/ca","certFile":"/c","keyFile":"/k"}}],"remoteWriteSettings":{"maxBlockSize":"1MB","maxDiskUsagePerURL":"1GB","queues":2,"showURL":true,"tmpDataPath":"/tmp","flushInterval":"1s"},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"serviceSpec":{"metadata":{"name":"extra"},"spec":{}},"networkPolicy":{"ingress":[{}]},"podDisruptionBudget":{"minAvailable":1},"storage":{"volumeClaimTemplate":{"spec":{"resources":{"requests":{"storage":"1Gi"}}}}}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1.VTAgent { return &vmv1.VTAgent{} }, func(ctx context.Context, rclient client.Client, cr *vmv1.VTAgent) error {
		return CreateOrUpdate(ctx, cr, rclient)
	})
}
