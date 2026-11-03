package vmalert

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
		`{"spec":{"datasource":{"url":"http://datasource"},"notifier":{"url":"http://notifier"}}}`,
		`{"spec":{"datasource":{"url":"http://datasource"},"notifiers":[{"url":"http://n1"},{"url":"http://n2"}],"replicaCount":2,"serviceAccountName":"sa","podDisruptionBudget":{"minAvailable":1},"vpa":{}}}`,
		`{"spec":{"datasource":{"url":"http://datasource"},"notifier":{"selector":{}},"selectAllByDefault":true}}`,
		`{"spec":{"datasource":{"url":"http://datasource"},"notifier":{"url":"http://notifier"},"replicaCount":2},"status":{"lastAppliedSpec":{"datasource":{"url":"http://old"},"notifier":{"url":"http://notifier"},"replicaCount":1}}}`,
		`{"spec":{"datasource":{"url":"http://d","basicAuth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tlsConfig":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"},"serverName":"s","insecureSkipVerify":true},"headers":["a:b","c:d"]},"remoteWrite":{"url":"http://w","concurrency":2,"flushInterval":"1s","maxBatchSize":10,"maxQueueSize":100,"bearerTokenSecret":{"name":"ref","key":"key"},"oauth2":{"client_id":{"secret":{"name":"ref","key":"key"}},"client_secret":{"name":"ref","key":"key"},"token_url":"http://t","scopes":["a","b"]}},"remoteRead":{"url":"http://r","lookback":"1h","bearerTokenFile":"/f","basicAuth":{"username":{"name":"ref","key":"key"},"password_file":"/p"},"oauth2":{"client_id":{"configMap":{"name":"ref","key":"key"}},"client_secret_file":"/s","token_url":"http://t"},"tlsConfig":{"caFile":"/ca","certFile":"/c","keyFile":"/k"}},"notifiers":[{"url":"http://n1","basicAuth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tlsConfig":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"},"serverName":"s","insecureSkipVerify":true},"headers":["a:b","c:d"]},{"url":"http://n2","bearerTokenSecret":{"name":"ref","key":"key"},"oauth2":{"client_id":{"secret":{"name":"ref","key":"key"}},"client_secret":{"name":"ref","key":"key"},"token_url":"http://t","scopes":["a","b"]}},{"url":"http://n3","bearerTokenFile":"/f","basicAuth":{"username":{"name":"ref","key":"key"},"password_file":"/p"},"oauth2":{"client_id":{"configMap":{"name":"ref","key":"key"}},"client_secret_file":"/s","token_url":"http://t"},"tlsConfig":{"caFile":"/ca","certFile":"/c","keyFile":"/k"}}],"externalLabels":{"a":"b"},"rulePath":["/r"],"evaluationInterval":"1m","enforcedNamespaceLabel":"ns","selectAllByDefault":true,"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}},"networkPolicy":{"ingress":[{}]},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"extraArgs":{"a":"b"}}}`,
		`{"spec":{"datasource":{"url":"http://d"},"notifierConfigRef":{"name":"ref","key":"key"}}}`,
		`{"spec":{"datasource":{"url":"http://d"},"notifiers":[{"url":"http://n"}],"extraArgs":{"notifier.blackhole":"true"}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1beta1.VMAlert { return &vmv1beta1.VMAlert{} }, func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) error {
		maps, err := CreateOrUpdateRuleConfigMaps(ctx, rclient, cr, nil, cr.HasNotifiersConfigured())
		if err != nil {
			return err
		}
		return CreateOrUpdate(ctx, cr, rclient, maps)
	})
}
