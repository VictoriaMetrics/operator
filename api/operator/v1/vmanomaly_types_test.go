package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestVMAnomaly_DefaultStatusFields(t *testing.T) {
	f := func(cr *VMAnomaly, wantShards int32) {
		t.Helper()
		cr.Name = "test"
		var status VMAnomalyStatus
		cr.DefaultStatusFields(&status)
		assert.Equal(t, wantShards, status.Shards, "shards")
		assert.Equal(t,
			"app.kubernetes.io/component=monitoring,app.kubernetes.io/instance=test,app.kubernetes.io/name=vmanomaly,managed-by=vm-operator",
			status.Selector, "selector")
	}

	// no shardCount set: must report 1 so VPA scale subresource gets a non-zero statusReplicasPath
	f(&VMAnomaly{Spec: VMAnomalySpec{}}, 1)

	// shardCount explicitly set
	f(&VMAnomaly{Spec: VMAnomalySpec{ShardCount: ptr.To(int32(3))}}, 3)

	// shardCount=1 explicitly
	f(&VMAnomaly{Spec: VMAnomalySpec{ShardCount: ptr.To(int32(1))}}, 1)
}

func TestVMAnomaly_ServerTLS(t *testing.T) {
	f := func(server *VMAnomalyServerSpec, monitoring *VMAnomalyMonitoringSpec, wantErr bool, wantURL string) {
		t.Helper()
		cr := &VMAnomaly{Spec: VMAnomalySpec{
			License:    &vmv1beta1.License{Key: ptr.To("test")},
			Server:     server,
			Monitoring: monitoring,
		}}
		cr.Name = "test"
		cr.Namespace = "default"
		if wantErr {
			assert.Error(t, cr.Validate())
			return
		}
		assert.NoError(t, cr.Validate())
		assert.Equal(t, wantURL, cr.AsURL(false))
	}
	files := func() *vmv1beta1.TLSConfig {
		return &vmv1beta1.TLSConfig{CertFile: "/tls/tls.crt", KeyFile: "/tls/tls.key"}
	}

	// no TLS
	f(&VMAnomalyServerSpec{}, nil, false, "http://vmanomaly-test.default.svc:8490")

	// TLS from files
	f(&VMAnomalyServerSpec{TLSConfig: files()}, nil, false, "https://vmanomaly-test.default.svc:8490")

	// mTLS with certificate, key and CA from a secret
	secret := func(key string) *corev1.SecretKeySelector {
		return &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tls"}, Key: key}
	}
	f(&VMAnomalyServerSpec{TLSConfig: &vmv1beta1.TLSConfig{
		CA:        vmv1beta1.SecretOrConfigMap{Secret: secret("ca.crt")},
		Cert:      vmv1beta1.SecretOrConfigMap{Secret: secret("tls.crt")},
		KeySecret: secret("tls.key"),
	}}, nil, false, "https://vmanomaly-test.default.svc:8490")

	// missing key
	f(&VMAnomalyServerSpec{TLSConfig: &vmv1beta1.TLSConfig{CertFile: "/tls/tls.crt"}}, nil, true, "")

	// missing certificate
	f(&VMAnomalyServerSpec{TLSConfig: &vmv1beta1.TLSConfig{KeyFile: "/tls/tls.key"}}, nil, true, "")

	// client-side options are rejected
	insecure := files()
	insecure.InsecureSkipVerify = true
	f(&VMAnomalyServerSpec{TLSConfig: insecure}, nil, true, "")

	serverName := files()
	serverName.ServerName = "example.com"
	f(&VMAnomalyServerSpec{TLSConfig: serverName}, nil, true, "")

	// Reject ambiguous credential sources rather than silently choosing one.
	configMap := &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tls"}, Key: "cert"}
	for _, tc := range []struct {
		name    string
		mutate  func(*vmv1beta1.TLSConfig)
		wantErr string
	}{
		{"certificate file and secret", func(c *vmv1beta1.TLSConfig) { c.Cert.Secret = secret("tls.crt") }, "CertFile and Cert"},
		{"key file and secret", func(c *vmv1beta1.TLSConfig) { c.KeySecret = secret("tls.key") }, "KeyFile and KeySecret"},
		{"CA file and configmap", func(c *vmv1beta1.TLSConfig) { c.CAFile = "/tls/ca.crt"; c.CA.ConfigMap = configMap }, "CAFile and CA"},
		{"certificate secret and configmap", func(c *vmv1beta1.TLSConfig) {
			c.CertFile = ""
			c.Cert = vmv1beta1.SecretOrConfigMap{Secret: secret("tls.crt"), ConfigMap: configMap}
		}, "both Secret and ConfigMap"},
		{"CA secret and configmap", func(c *vmv1beta1.TLSConfig) {
			c.CA = vmv1beta1.SecretOrConfigMap{Secret: secret("ca.crt"), ConfigMap: configMap}
		}, "both Secret and ConfigMap"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			credentials := files()
			tc.mutate(credentials)
			server := &VMAnomalyServerSpec{TLSConfig: credentials}
			assert.ErrorContains(t, server.validateTLS("8490", "8080"), tc.wantErr)
		})
	}

	// probes would hit the TLS listener
	f(&VMAnomalyServerSpec{Port: "8080", TLSConfig: files()}, nil, true, "")
	f(&VMAnomalyServerSpec{TLSConfig: files()}, &VMAnomalyMonitoringSpec{Pull: &VMAnomalyMonitoringPullSpec{Port: "8490"}}, true, "")
}
