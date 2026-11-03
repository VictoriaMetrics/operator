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
		`{"spec":{"replicaCount":3,"configRawYaml":"global:\n  resolve_timeout: 5m\nroute:\n  receiver: r\n  routes:\n  - receiver: r\nreceivers:\n- name: r\n  webhook_configs:\n  - url: http://w\ntemplates:\n- /t/*.tmpl\n","templates":[{"name":"ref","key":"key"}],"additionalPeers":["p:9094"],"clusterAdvertiseAddress":"1.2.3.4:9094","externalURL":"http://am","routePrefix":"/am","listenLocal":true,"retention":"24h","enforcedTopRouteMatchers":["a=\"b\""],"disableNamespaceMatcher":true,"enforcedNamespaceLabel":"ns","webConfig":{"tls_server_config":{"client_ca_secret_ref":{"name":"ref","key":"key"},"client_auth_type":"RequireAndVerifyClientCert","min_version":"TLS12","cipher_suites":["TLS_AES_128_GCM_SHA256"],"cert_secret_ref":{"name":"ref","key":"key"},"key_secret_ref":{"name":"ref","key":"key"}},"http_server_config":{"http2":true,"headers":{"a":"b"}},"basic_auth_users":{"u":"p"}},"gossipConfig":{"tls_server_config":{"client_ca_file":"/ca","cert_file":"/c","key_file":"/k"},"tls_client_config":{"ca_secret_ref":{"name":"ref","key":"key"},"cert_secret_ref":{"name":"ref","key":"key"},"key_secret_ref":{"name":"ref","key":"key"},"insecure_skip_verify":true,"server_name":"s"}},"tracingConfig":{"client_type":"grpc","endpoint":"t:4317","sampling_fraction":"0.5","insecure":true,"tls_config":{"ca_secret_ref":{"name":"ref","key":"key"},"cert_secret_ref":{"name":"ref","key":"key"},"key_secret_ref":{"name":"ref","key":"key"}},"headers":{"a":"b"},"compression":"gzip","timeout":"5s"},"serviceSpec":{"metadata":{"name":"extra"},"spec":{"type":"NodePort"}},"networkPolicy":{"ingress":[{}]},"podDisruptionBudget":{"minAvailable":1},"vpa":{"updatePolicy":{"updateMode":"Auto"},"resourcePolicy":{"containerPolicies":[{"containerName":"*"}]}},"claimTemplates":[{"metadata":{"name":"c"}}],"persistentVolumeClaimRetentionPolicy":{"whenDeleted":"Delete"}}}`,
		`{"spec":{"configSecret":"ref"}}`,
		`{"spec":{"configRawYaml":"bad: [","tracingConfig":{"endpoint":"::bad"}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1beta1.VMAlertmanager { return &vmv1beta1.VMAlertmanager{} }, func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlertmanager) error {
		if err := CreateOrUpdateConfig(ctx, rclient, cr, nil); err != nil {
			return err
		}
		return CreateOrUpdateAlertManager(ctx, cr, rclient)
	})
}
