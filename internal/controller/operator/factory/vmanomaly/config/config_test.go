package config

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
	type opts struct {
		cr                *vmv1.VMAnomaly
		wantErr           bool
		predefinedObjects []runtime.Object
		expected          string
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(o.cr)
		ctx := context.TODO()

		cfg := map[build.ResourceKind]*build.ResourceCfg{
			build.TLSAssetsResourceKind: {
				MountDir:   "/test",
				SecretName: build.ResourceName(build.TLSAssetsResourceKind, o.cr),
			},
		}
		ac := build.NewAssetsCache(ctx, fclient, cfg)
		pos, err := NewParsedObjects(ctx, fclient, o.cr)
		assert.NoError(t, err)
		loaded, err := pos.Load(o.cr, ac)
		if o.wantErr {
			assert.Error(t, err)
			return
		}
		assert.NoError(t, err)
		expected := strings.TrimSpace(o.expected)
		got := strings.TrimSpace(string(loaded))
		assert.Equal(t, got, expected)
	}

	// no custom readers and writers
	f(opts{
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
		wantErr: true,
	})

	// with reader, writer, monitoring and settings
	f(opts{
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
					MetricFormat: vmv1.VMAnomalyVMWriterMetricFormatSpec{
						Name: "metrics_$VAR",
						For:  "custom_$QUERY_KEY",
						ExtraLabels: map[string]string{
							"label1": "value1",
							"label2": "value2",
						},
					},
					ConnectionRetryAttempts: 3,
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
					Offset:         "5m",
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
  offset: 5m
  queries:
    test:
      expr: vm_metric
      data_range:
      - "0"
      - inf
  tenant_id: "0:1"
  verify_tls: /test/monitoring_tls_remote-ca
  tls_cert_file: /test/monitoring_tls_remote-cert
  tls_key_file: /test/monitoring_tls_remote-key
writer:
  class: vm
  datasource_url: http://write.endpoint
  metric_format:
    __name__: metrics_$VAR
    for: custom_$QUERY_KEY
    label1: value1
    label2: value2
  connection_retry_attempts: 3
  tenant_id: "0:2"
  verify_tls: /test/monitoring_tls_remote-ca
  tls_cert_file: /test/monitoring_tls_remote-cert
  tls_key_file: /test/monitoring_tls_remote-key
monitoring:
  pull:
    port: "8888"
  push:
    url: http://monitoring
    tenant_id: "0:3"
    verify_tls: /test/monitoring_tls_remote-ca
    tls_cert_file: /test/monitoring_tls_remote-cert
    tls_key_file: /test/monitoring_tls_remote-key
    push_frequency: 20s
    extra_labels:
      label1: value1
settings:
  restore_state: true
server:
  port: "8490"
`,
	})

	// TLS without a CA bundle and InsecureSkipVerify=false => verify_tls: true
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly",
				Namespace: "monitoring",
			},
			Spec: vmv1.VMAnomalySpec{
				License: &vmv1beta1.License{
					Key: ptr.To("test"),
				},
				ConfigRawYaml: `
models:
  model_zscore:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['test_query']
schedulers:
  scheduler_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: 1m
    fit_every: 2m
    fit_window: 3h
reader:
  queries:
    test_query:
      expr: vm_metric
writer:
  datasource_url: "http://test.com"
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://reader.test",
					SamplingPeriod: "30s",
					VMAnomalyHTTPClientSpec: vmv1.VMAnomalyHTTPClientSpec{
						TLSConfig: &vmv1beta1.TLSConfig{
							Cert: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "tls"},
									Key:                  "cert",
								},
							},
							KeySecret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "tls"},
								Key:                  "key",
							},
						},
					},
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://writer.test",
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
					"cert": []byte("cert"),
					"key":  []byte("key"),
				},
			},
		},
		expected: `
models:
  model_zscore:
    class: zscore
    queries:
    - test_query
    z_threshold: 2.5
schedulers:
  scheduler_1m:
    class: scheduler.periodic.PeriodicScheduler
    fit_every: 2m
    fit_window: 3h
    infer_every: 1m
reader:
  class: vm
  datasource_url: http://reader.test
  sampling_period: 30s
  queries:
    test_query:
      expr: vm_metric
  verify_tls: true
  tls_cert_file: /test/monitoring_tls_cert
  tls_key_file: /test/monitoring_tls_key
writer:
  class: vm
  datasource_url: http://writer.test
monitoring:
  pull:
    port: "8080"
server:
  port: "8490"
`,
	})

	// InsecureSkipVerify=true takes precedence over a provided CA => verify_tls: false
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly",
				Namespace: "monitoring",
			},
			Spec: vmv1.VMAnomalySpec{
				License: &vmv1beta1.License{
					Key: ptr.To("test"),
				},
				ConfigRawYaml: `
models:
  model_zscore:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['test_query']
schedulers:
  scheduler_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: 1m
    fit_every: 2m
    fit_window: 3h
reader:
  queries:
    test_query:
      expr: vm_metric
writer:
  datasource_url: "http://test.com"
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://reader.test",
					SamplingPeriod: "30s",
					VMAnomalyHTTPClientSpec: vmv1.VMAnomalyHTTPClientSpec{
						TLSConfig: &vmv1beta1.TLSConfig{
							InsecureSkipVerify: true,
							CA: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "tls"},
									Key:                  "ca",
								},
							},
							Cert: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "tls"},
									Key:                  "cert",
								},
							},
							KeySecret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "tls"},
								Key:                  "key",
							},
						},
					},
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://writer.test",
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
					"ca":   []byte("ca"),
					"cert": []byte("cert"),
					"key":  []byte("key"),
				},
			},
		},
		expected: `
models:
  model_zscore:
    class: zscore
    queries:
    - test_query
    z_threshold: 2.5
schedulers:
  scheduler_1m:
    class: scheduler.periodic.PeriodicScheduler
    fit_every: 2m
    fit_window: 3h
    infer_every: 1m
reader:
  class: vm
  datasource_url: http://reader.test
  sampling_period: 30s
  queries:
    test_query:
      expr: vm_metric
  verify_tls: false
  tls_cert_file: /test/monitoring_tls_cert
  tls_key_file: /test/monitoring_tls_key
writer:
  class: vm
  datasource_url: http://writer.test
monitoring:
  pull:
    port: "8080"
server:
  port: "8490"
`,
	})

	// with settings including retention
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly",
				Namespace: "monitoring",
			},
			Spec: vmv1.VMAnomalySpec{
				License: &vmv1beta1.License{
					Key: ptr.To("test"),
				},
				ConfigRawYaml: `
models:
  model_zscore:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['test_query']
schedulers:
  scheduler_backtesting:
    class: "backtesting"
    fit_window: 3h
    fit_every: 1h
    from_s: 1000
    to_s: 2000
    exact: true
    infer_every: 5m
reader:
  queries:
    test_query:
      expr: vm_metric
writer:
  datasource_url: "http://test.com"
settings:
  restore_state: true
  retention:
    ttl: 24h
    check_interval: 30m
  logger_levels:
    root: DEBUG
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://reader.test",
					SamplingPeriod: "30s",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://writer.test",
				},
			},
		},
		expected: `
models:
  model_zscore:
    class: zscore
    queries:
    - test_query
    z_threshold: 2.5
schedulers:
  scheduler_backtesting:
    class: backtesting
    fit_window: 3h
    from_iso: 0001-01-01T00:00:00Z
    from_s: 1000
    to_iso: 0001-01-01T00:00:00Z
    to_s: 2000
    fit_every: 1h
    exact: true
    infer_every: 5m
reader:
  class: vm
  datasource_url: http://reader.test
  sampling_period: 30s
  queries:
    test_query:
      expr: vm_metric
writer:
  class: vm
  datasource_url: http://writer.test
monitoring:
  pull:
    port: "8080"
settings:
  restore_state: true
  retention:
    ttl: 24h
    check_interval: 30m
  logger_levels:
    root: DEBUG
server:
  port: "8490"
`,
	})

	// with server section
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly",
				Namespace: "monitoring",
			},
			Spec: vmv1.VMAnomalySpec{
				License: &vmv1beta1.License{
					Key: ptr.To("test"),
				},
				ConfigRawYaml: `
models:
  model_zscore:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['test_query']
schedulers:
  scheduler_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: 1m
    fit_every: 2m
    fit_window: 3h
reader:
  queries:
    test_query:
      expr: vm_metric
writer:
  datasource_url: "http://test.com"
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://reader.test",
					SamplingPeriod: "30s",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://writer.test",
				},
				Server: &vmv1.VMAnomalyServerSpec{
					Addr:                        "127.0.0.1",
					Port:                        "9090",
					PathPrefix:                  "my-anomaly",
					MaxConcurrentTasks:          10,
					UseReaderConnectionSettings: true,
				},
			},
		},
		expected: `
models:
  model_zscore:
    class: zscore
    queries:
    - test_query
    z_threshold: 2.5
schedulers:
  scheduler_1m:
    class: scheduler.periodic.PeriodicScheduler
    fit_every: 2m
    fit_window: 3h
    infer_every: 1m
reader:
  class: vm
  datasource_url: http://reader.test
  sampling_period: 30s
  queries:
    test_query:
      expr: vm_metric
writer:
  class: vm
  datasource_url: http://writer.test
monitoring:
  pull:
    port: "8080"
server:
  addr: 127.0.0.1
  port: "9090"
  path_prefix: my-anomaly
  max_concurrent_tasks: 10
  use_reader_connection_settings: true
`,
	})

	// prophet model with scale and args fields
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly",
				Namespace: "monitoring",
			},
			Spec: vmv1.VMAnomalySpec{
				License: &vmv1beta1.License{
					Key: ptr.To("test"),
				},
				ConfigRawYaml: `
models:
  prophet_model:
    class: 'prophet'
    queries: ['test_query']
    scale: [0.5, 1.5]
    args:
      changepoint_prior_scale: 0.05
      seasonality_mode: multiplicative
schedulers:
  scheduler_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: 1m
    fit_every: 2m
    fit_window: 3h
reader:
  queries:
    test_query:
      expr: vm_metric
writer:
  datasource_url: "http://test.com"
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://reader.test",
					SamplingPeriod: "30s",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://writer.test",
				},
			},
		},
		expected: `
models:
  prophet_model:
    class: prophet
    queries:
    - test_query
    scale:
    - 0.5
    - 1.5
    args:
      changepoint_prior_scale: 0.05
      seasonality_mode: multiplicative
schedulers:
  scheduler_1m:
    class: scheduler.periodic.PeriodicScheduler
    fit_every: 2m
    fit_window: 3h
    infer_every: 1m
reader:
  class: vm
  datasource_url: http://reader.test
  sampling_period: 30s
  queries:
    test_query:
      expr: vm_metric
writer:
  class: vm
  datasource_url: http://writer.test
monitoring:
  pull:
    port: "8080"
server:
  port: "8490"
`,
	})

	// online quantile model with scale field
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly",
				Namespace: "monitoring",
			},
			Spec: vmv1.VMAnomalySpec{
				License: &vmv1beta1.License{
					Key: ptr.To("test"),
				},
				ConfigRawYaml: `
models:
  quantile_model:
    class: 'quantile_online'
    queries: ['test_query']
    scale: [0.5, 1.5]
    min_subseason: hourly
    decay: 0.5
    global_smoothing: 0.5
schedulers:
  scheduler_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: 1m
    fit_every: 2m
    fit_window: 3h
reader:
  queries:
    test_query:
      expr: vm_metric
      offset: 1m
writer:
  datasource_url: "http://test.com"
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://reader.test",
					SamplingPeriod: "30s",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://writer.test",
				},
			},
		},
		expected: `
models:
  quantile_model:
    class: quantile_online
    queries:
    - test_query
    scale:
    - 0.5
    - 1.5
    decay: 0.5
    min_subseason: hourly
    global_smoothing: 0.5
schedulers:
  scheduler_1m:
    class: scheduler.periodic.PeriodicScheduler
    fit_every: 2m
    fit_window: 3h
    infer_every: 1m
reader:
  class: vm
  datasource_url: http://reader.test
  sampling_period: 30s
  queries:
    test_query:
      expr: vm_metric
      offset: 1m
writer:
  class: vm
  datasource_url: http://writer.test
monitoring:
  pull:
    port: "8080"
server:
  port: "8490"
`,
	})

	// ui preset with nil monitoring - must not panic
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly-ui",
				Namespace: "monitoring",
			},
			Spec: vmv1.VMAnomalySpec{
				License: &vmv1beta1.License{
					Key: ptr.To("test"),
				},
				ConfigRawYaml: `preset: ui`,
				Server: &vmv1.VMAnomalyServerSpec{
					PathPrefix: "/",
				},
				// Monitoring intentionally nil to reproduce the panic
			},
		},
		expected: `
models:
  placeholder:
    class: zscore
    schedulers:
    - noop
schedulers:
  noop:
    class: noop
reader:
  class: noop
  datasource_url: ""
  sampling_period: null
writer:
  class: noop
  datasource_url: ""
monitoring:
  pull:
    addr: 0.0.0.0
    port: "8080"
server:
  port: "8490"
  path_prefix: /
preset: ui
`,
	})

	// ui preset with explicit monitoring pull port
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly-ui-monitoring",
				Namespace: "monitoring",
			},
			Spec: vmv1.VMAnomalySpec{
				License: &vmv1beta1.License{
					Key: ptr.To("test"),
				},
				ConfigRawYaml: `preset: ui`,
				Monitoring: &vmv1.VMAnomalyMonitoringSpec{
					Pull: &vmv1.VMAnomalyMonitoringPullSpec{
						Port: "9999",
					},
				},
			},
		},
		expected: `
models:
  placeholder:
    class: zscore
    schedulers:
    - noop
schedulers:
  noop:
    class: noop
reader:
  class: noop
  datasource_url: ""
  sampling_period: null
writer:
  class: noop
  datasource_url: ""
monitoring:
  pull:
    addr: 0.0.0.0
    port: "9999"
server:
  port: "8490"
preset: ui
`,
	})

	// server section validation error - maxConcurrentTasks must be a positive integer
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly",
				Namespace: "monitoring",
			},
			Spec: vmv1.VMAnomalySpec{
				License: &vmv1beta1.License{
					Key: ptr.To("test"),
				},
				ConfigRawYaml: `
models:
  model_zscore:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['test_query']
schedulers:
  scheduler_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: 1m
    fit_every: 2m
    fit_window: 3h
reader:
  queries:
    test_query:
      expr: vm_metric
writer:
  datasource_url: "http://test.com"
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://reader.test",
					SamplingPeriod: "30s",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://writer.test",
				},
				Server: &vmv1.VMAnomalyServerSpec{
					MaxConcurrentTasks: -1, // negative is invalid; vmanomaly imposes no upper bound
				},
			},
		},
		wantErr: true,
	})

	// with external models
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly",
				Namespace: "default",
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
				ConfigSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "app",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"prod"},
						},
					},
				},
				ConfigNamespaceSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "env",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"prod"},
						},
					},
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://write.endpoint",
					MetricFormat: vmv1.VMAnomalyVMWriterMetricFormatSpec{
						Name: "metrics_$VAR",
						For:  "custom_$QUERY_KEY",
						ExtraLabels: map[string]string{
							"label1": "value1",
							"label2": "value2",
						},
					},
					VMAnomalyHTTPClientSpec: vmv1.VMAnomalyHTTPClientSpec{
						TenantID: "0:2",
					},
				},
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://custom.ds",
					QueryRangePath: "/api/v1/query_range",
					SamplingPeriod: "10s",
					VMAnomalyHTTPClientSpec: vmv1.VMAnomalyHTTPClientSpec{
						TenantID: "0:1",
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
					Labels: map[string]string{
						"env": "prod",
					},
				},
			},
			&vmv1.VMAnomalyConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "anomaly",
					Labels: map[string]string{
						"app": "prod",
					},
					Namespace: "default",
				},
				Spec: runtime.RawExtension{
					Raw: []byte(`{
  "models": {
    "test": {
      "class": "zscore",
      "queries": ["test"],
      "z_threshold": 2.5
    }
  },
  "schedulers": {
    "test": {
      "class": "periodic",
      "fit_every": "12m",
      "fit_window": "13h",
      "infer_every": "11m"
    }
  },
  "queries": {
    "test": {
      "expr": "vm_metric"
    }
  }
}`),
				},
			},
		},
		expected: `
models:
  default-anomaly-test:
    class: zscore
    queries:
    - default-anomaly-test
    z_threshold: 2.5
  model_univariate_1:
    class: zscore
    queries:
    - test
    z_threshold: 2.5
schedulers:
  default-anomaly-test:
    class: periodic
    fit_every: 12m
    fit_window: 13h
    infer_every: 11m
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
    default-anomaly-test:
      expr: vm_metric
    test:
      expr: vm_metric
      data_range:
      - "0"
      - inf
  tenant_id: "0:1"
writer:
  class: vm
  datasource_url: http://write.endpoint
  metric_format:
    __name__: metrics_$VAR
    for: custom_$QUERY_KEY
    label1: value1
    label2: value2
  tenant_id: "0:2"
monitoring:
  pull:
    port: "8080"
settings:
  restore_state: true
server:
  port: "8490"
`,
	})

	// tz is serialized as a string (reader/query/scheduler), an explicit zero
	// anomaly_score_outside_data_range survives marshalling, and an unset decay is omitted
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly",
				Namespace: "monitoring",
			},
			Spec: vmv1.VMAnomalySpec{
				License: &vmv1beta1.License{Key: ptr.To("test")},
				ConfigRawYaml: `
settings:
  anomaly_score_outside_data_range: 0
models:
  m_online:
    class: zscore_online
    queries: ['q1']
schedulers:
  s1:
    class: periodic
    infer_every: 1m
    fit_window: 1h
    tz: "Europe/Kyiv"
reader:
  queries:
    q1:
      expr: up
      tz: "America/New_York"
writer: {}
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://reader.test",
					SamplingPeriod: "30s",
					Timezone:       "UTC",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://writer.test",
				},
			},
		},
		expected: `
models:
  m_online:
    class: zscore_online
    queries:
    - q1
schedulers:
  s1:
    class: periodic
    fit_window: 1h
    infer_every: 1m
    tz: Europe/Kyiv
reader:
  class: vm
  datasource_url: http://reader.test
  sampling_period: 30s
  tz: UTC
  queries:
    q1:
      expr: up
      tz: America/New_York
writer:
  class: vm
  datasource_url: http://writer.test
monitoring:
  pull:
    port: "8080"
settings:
  anomaly_score_outside_data_range: 0
server:
  port: "8490"
`,
	})

	// contamination accepts a float
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly",
				Namespace: "monitoring",
			},
			Spec: vmv1.VMAnomalySpec{
				License: &vmv1beta1.License{Key: ptr.To("test")},
				ConfigRawYaml: `
models:
  m_iforest:
    class: isolation_forest
    queries: ['q1']
    contamination: 0.05
schedulers:
  s1:
    class: periodic
    infer_every: 1m
    fit_window: 1h
reader:
  queries:
    q1:
      expr: up
writer: {}
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://reader.test",
					SamplingPeriod: "30s",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://writer.test",
				},
				Server: &vmv1.VMAnomalyServerSpec{
					MaxConcurrentTasks: 50, // no upper bound
				},
			},
		},
		expected: `
models:
  m_iforest:
    class: isolation_forest
    queries:
    - q1
    contamination: 0.05
schedulers:
  s1:
    class: periodic
    fit_window: 1h
    infer_every: 1m
reader:
  class: vm
  datasource_url: http://reader.test
  sampling_period: 30s
  queries:
    q1:
      expr: up
writer:
  class: vm
  datasource_url: http://writer.test
monitoring:
  pull:
    port: "8080"
server:
  port: "8490"
  max_concurrent_tasks: 50
`,
	})

	// contamination accepts the string "auto"
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly",
				Namespace: "monitoring",
			},
			Spec: vmv1.VMAnomalySpec{
				License: &vmv1beta1.License{Key: ptr.To("test")},
				ConfigRawYaml: `
models:
  m_iforest:
    class: isolation_forest
    queries: ['q1']
    contamination: auto
schedulers:
  s1:
    class: periodic
    infer_every: 1m
    fit_window: 1h
reader:
  queries:
    q1:
      expr: up
writer: {}
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://reader.test",
					SamplingPeriod: "30s",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://writer.test",
				},
			},
		},
		expected: `
models:
  m_iforest:
    class: isolation_forest
    queries:
    - q1
    contamination: auto
schedulers:
  s1:
    class: periodic
    fit_window: 1h
    infer_every: 1m
reader:
  class: vm
  datasource_url: http://reader.test
  sampling_period: 30s
  queries:
    q1:
      expr: up
writer:
  class: vm
  datasource_url: http://writer.test
monitoring:
  pull:
    port: "8080"
server:
  port: "8490"
`,
	})

	// periodic scheduler with scatter_infer_jobs — regression test for #2328
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-anomaly",
				Namespace: "default",
			},
			Spec: vmv1.VMAnomalySpec{
				License: &vmv1beta1.License{
					Key: ptr.To("test"),
				},
				ConfigRawYaml: `
models:
  zscore:
    class: zscore
    queries: [q1]
schedulers:
  periodic_online:
    class: periodic
    infer_every: 5m
    scatter_infer_jobs: true
    fit_every: 10000d
    fit_window: 3d
reader:
  queries:
    q1:
      expr: up
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://vm:8428",
					SamplingPeriod: "1m",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://vm:8428",
				},
			},
		},
		expected: `
models:
  zscore:
    class: zscore
    queries:
    - q1
schedulers:
  periodic_online:
    class: periodic
    fit_every: 10000d
    fit_window: 3d
    infer_every: 5m
    scatter_infer_jobs: true
reader:
  class: vm
  datasource_url: http://vm:8428
  sampling_period: 1m
  queries:
    q1:
      expr: up
writer:
  class: vm
  datasource_url: http://vm:8428
monitoring:
  pull:
    port: "8080"
server:
  port: "8490"
`,
	})

}
