package config

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name              string
		cr                *vmv1.VMAnomaly
		wantErr           bool
		predefinedObjects []runtime.Object
		expected          string
	}{
		{
			name: "no custom readers and writers",
			cr: &vmv1.VMAnomaly{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-anomaly",
					Namespace:   "monitoring",
					Annotations: map[string]string{"not": "touch"},
					Labels:      map[string]string{"main": "system"},
				},
				Spec: vmv1.VMAnomalySpec{
					License: &vmv1beta1.License{
						Key: ptr.To("test"),
					},
					ConfigRawYaml: `
models:
  model_univariate_1:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['query_alias2']
schedulers:
  scheduler_periodic_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: 1m
    fit_every: 2m
    fit_window: 3h
reader:
  class: vm
  datasource_url: "http://test.com"
  sampling_period: 1m
  queries:
    query_alias2:
      expr: vm_metric
writer:
  class: vm
  datasource_url: "http://test.com"
`,
				},
			},
			expected: ``,
			wantErr:  true,
		},
		{
			name: "with reader, writer, monitoring and settings",
			cr: &vmv1.VMAnomaly{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-anomaly",
					Namespace:   "monitoring",
					Annotations: map[string]string{"not": "touch"},
					Labels:      map[string]string{"main": "system"},
				},
				Spec: vmv1.VMAnomalySpec{
					License: &vmv1beta1.License{
						Key: ptr.To("test"),
					},
					ConfigRawYaml: `
reader:
  class: vm
  datasource_url: "http://test.com"
  sampling_period: 1m
  queries:
    test:
      expr: vm_metric
      data_range: [0, inf]
writer:
  class: vm
  datasource_url: "http://test.com"
models:
  model_univariate_1:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['test']
schedulers:
  scheduler_periodic_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: 1m
    fit_every: 2m
    fit_window: 3h
settings:
  restore_state: true
`,
					Monitoring: &vmv1.VMAnomalyMonitoringSpec{
						Pull: &vmv1.VMAnomalyMonitoringPullSpec{
							Port: "8888",
						},
						Push: &vmv1.VMAnomalyMonitoringPushSpec{
							URL:           "http://monitoring",
							PushFrequency: "20s",
							ExtraLabels: map[string]string{
								"label1": "value1",
							},
							VMAnomalyHTTPClientSpec: vmv1.VMAnomalyHTTPClientSpec{
								TenantID: "0:3",
								TLSConfig: &vmv1beta1.TLSConfig{
									CA: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "tls",
											},
											Key: "remote-ca",
										},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "tls",
											},
											Key: "remote-cert",
										},
									},
									KeySecret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "tls",
										},
										Key: "remote-key",
									},
								},
							},
						},
					},
					Writer: &vmv1.VMAnomalyWritersSpec{
						DatasourceURL: "http://write.endpoint",
						VMAnomalyHTTPClientSpec: vmv1.VMAnomalyHTTPClientSpec{
							TenantID: "0:2",
							TLSConfig: &vmv1beta1.TLSConfig{
								CA: vmv1beta1.SecretOrConfigMap{
									Secret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "tls",
										},
										Key: "remote-ca",
									},
								},
								Cert: vmv1beta1.SecretOrConfigMap{
									Secret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "tls",
										},
										Key: "remote-cert",
									},
								},
								KeySecret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls",
									},
									Key: "remote-key",
								},
							},
						},
					},
					Reader: &vmv1.VMAnomalyReadersSpec{
						DatasourceURL:  "http://custom.ds",
						QueryRangePath: "/api/v1/query_range",
						SamplingPeriod: "10s",
						VMAnomalyHTTPClientSpec: vmv1.VMAnomalyHTTPClientSpec{
							TenantID: "0:1",
							TLSConfig: &vmv1beta1.TLSConfig{
								CA: vmv1beta1.SecretOrConfigMap{
									Secret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "tls",
										},
										Key: "remote-ca",
									},
								},
								Cert: vmv1beta1.SecretOrConfigMap{
									Secret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "tls",
										},
										Key: "remote-cert",
									},
								},
								KeySecret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls",
									},
									Key: "remote-key",
								},
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls",
						Namespace: "monitoring",
					},
					Data: map[string][]byte{
						"remote-ca":   []byte("ca"),
						"remote-cert": []byte("cert"),
						"remote-key":  []byte("key"),
					},
				},
			},
			expected: `
models:
  model_univariate_1:
    class: zscore
    queries:
    - test
    z_threshold: 2.5
schedulers:
  scheduler_periodic_1m:
    class: scheduler.periodic.PeriodicScheduler
    fit_every: 2m
    fit_window: 3h
    infer_every: 1m
reader:
  class: vm
  datasource_url: http://custom.ds
  sampling_period: 10s
  query_range_path: /api/v1/query_range
  queries:
    test:
      expr: vm_metric
      data_range:
      - "0"
      - inf
  tenant_id: "0:1"
  verify_tls: true
  tls_cert_file: /test/monitoring_tls_remote-cert
  tls_key_file: /test/monitoring_tls_remote-key
writer:
  class: vm
  datasource_url: http://write.endpoint
  tenant_id: "0:2"
  verify_tls: true
  tls_cert_file: /test/monitoring_tls_remote-cert
  tls_key_file: /test/monitoring_tls_remote-key
monitoring:
  pull:
    port: "8888"
  push:
    url: http://monitoring
    tenant_id: "0:3"
    verify_tls: true
    tls_cert_file: /test/monitoring_tls_remote-cert
    tls_key_file: /test/monitoring_tls_remote-key
    push_frequency: 20s
    extra_labels:
      label1: value1
settings:
  restore_state: true
`,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			build.AddDefaults(fclient.Scheme())
			fclient.Scheme().Default(tt.cr)
			ctx := context.TODO()

			cfg := map[build.ResourceKind]*build.ResourceCfg{
				build.TLSAssetsResourceKind: {
					MountDir:   "/test",
					SecretName: build.ResourceName(build.TLSAssetsResourceKind, tt.cr),
				},
			}
			ac := build.NewAssetsCache(ctx, fclient, cfg)
			loaded, err := Load(tt.cr, ac)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
			expected := strings.TrimSpace(tt.expected)
			got := strings.TrimSpace(string(loaded))
			if got != expected {
				t.Fatalf("unexpected config produced by Load(): \nexpected:\n%s\ngot:\n%s", expected, got)
			}
		})
	}
}
