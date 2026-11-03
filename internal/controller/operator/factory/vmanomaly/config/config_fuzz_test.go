package config

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

func FuzzVMAnomalyConfigLoad(f *testing.F) {
	const validConfig = `reader:
  queries:
    up:
      expr: up
  sampling_period: 1m
models:
  baseline:
    class: zscore
    queries: [up]
schedulers:
  every_minute:
    class: periodic
    infer_every: 1m
    fit_every: 2m
    fit_window: 3h
`
	const validSpec = `{"reader":{"datasourceURL":"http://reader:8428","samplingPeriod":"1m"},"writer":{"datasourceURL":"http://writer:8428"}}`
	const validChild = `models:
  child:
    class: zscore
    queries: [child_up]
queries:
  child_up:
    expr: up
`
	for _, seed := range []struct {
		config string
		spec   string
		child  string
	}{
		{`preset: ui`, `{}`, ``},
		{`preset: UI`, `{"server":{"port":"8080"},"monitoring":{"pull":{"port":"8429"}}}`, ``},
		{validConfig, validSpec, ``},
		{validConfig, validSpec, validChild},
		{`models:
  auto:
    class: auto
    tuned_class_name: zscore
    optimization_params: {anomaly_percentage: 0.1, n_splits: 2, n_trials: 3, train_val_ratio: 0.5, validation_scheme: leaky, beta: 1, timeout: 5, n_jobs: -1, frozen_params: {z_threshold: 3}}
  prophet: {class: prophet, args: {interval_width: 0.9}, tz_seasonalities: [dow, {name: hod, fourier_order: 4}]}
  zscore: {class: zscore_online, z_threshold: 3, decay: 0.9, history_strength: 1, min_n_samples_seen: 5}
  hw: {class: holtwinters, seasonality: 1d, frequency: 1h}
  mad: {class: mad_online, threshold: 2, decay: 1, compression: 10}
  rq: {class: rolling_quantile, quantile: 0.9, window_steps: 10}
  oq: {class: quantile_online, quantiles: [0.1, 0.5, 0.9], seasonal_interval: 1h, decay: 0.5}
  std: {class: std, z_threshold: 2, period: 3, min_n_samples_seen: 2}
  if: {class: isolation_forest, contamination: auto, seasonal_features: [hod]}
  ifm: {class: isolation_forest_multivariate, contamination: 0.01, groupby: [job]}
  te: {class: temporal_envelope, quantiles: [0.1, 0.9], iqr_threshold: 1, alpha: 0.5, loss_reactivity: 1, min_n_samples_seen: 3, changepoint_window: 2, seasonalities: [hod_smooth, dow_spiky]}
  tem: {class: temporal_envelope_multivariate, groupby: [job], dependency_rank: 1, score_aggregation: l2, max_channels: 4, recommended_max_channels: 2}
  peer: {class: peer_outlier, min_peer_count: 3, epsilon_quantile: 0.5, tolerance: 1, decay: 0.99, min_n_samples_seen: 20, groupby: [job]}
schedulers:
  p: {class: periodic, fit_every: 1h, fit_window: 1d, infer_every: 1m, tz: UTC}
  o: {class: oneoff, infer_start_s: 10, infer_end_s: 20, fit_start_iso: 2024-01-01T00:00:00Z, fit_end_s: 1800000000}
  b: {class: backtesting, fit_window: 1h, from_s: 1, to_iso: 2024-01-01T00:00:00Z, fit_every: 1h, n_jobs: 2}
reader:
  queries:
    q: {expr: up, step: 1m, data_range: ["0", "1"], detection_direction: above_expected, tz: UTC, tenant_id: "1:0", offset: 1m}
settings:
  n_workers: 2
  native_threads_per_worker: 1
  anomaly_score_outside_data_range: 0
  restore_state: true
  retention: {ttl: 1d, check_interval: 1h}
  logger_levels: {reader: debug}
server:
  max_concurrent_tasks: -1
`, `{"reader":{"class":"vm","datasourceURL":"http://r","samplingPeriod":"1m","queryRangePath":"/q","extraFilters":["a"],"fetchTimeout":"10s","processingTimeout":"1m","queryFromLastSeenTimestamp":true,"queryLastSeenMaxLookback":"1h","tenantID":"0:0","healthPath":"/h","timeout":"5s","basicAuth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tlsConfig":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}},"writer":{"datasourceURL":"http://w","metricFormat":{"__name__":"$VAR","for":"$QUERY_KEY","extraLabels":{"a":"b"}},"connectionRetryAttempts":2,"batchMaxSeries":10,"batchMaxBytes":100,"metricPrefixCacheMaxEntries":5,"bearer":{"bearerTokenSecret":{"name":"ref","key":"key"},"bearerTokenFile":"/t"},"tlsConfig":{"insecureSkipVerify":true}},"monitoring":{"pull":{"port":"8080"},"push":{"url":"http://p","pushFrequency":"15s","extraLabels":{"a":"b"},"basicAuth":{"username":{"name":"ref","key":"key"}},"tlsConfig":{"ca":{"secret":{"name":"ref","key":"key"}}}}},"server":{"addr":"0.0.0.0","port":"8490","pathPrefix":"/p","maxConcurrentTasks":2,"uiDefaultState":"x","useReaderConnectionSettings":true}}`, validChild},
		{validConfig, `{"reader":{"class":"vm","datasourceURL":"http://r","samplingPeriod":"1m","queryRangePath":"/q","extraFilters":["a"],"fetchTimeout":"10s","processingTimeout":"1m","queryFromLastSeenTimestamp":true,"queryLastSeenMaxLookback":"1h","tenantID":"0:0","healthPath":"/h","timeout":"5s","basicAuth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tlsConfig":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}},"writer":{"datasourceURL":"http://w","metricFormat":{"__name__":"$VAR","for":"$QUERY_KEY","extraLabels":{"a":"b"}},"connectionRetryAttempts":2,"batchMaxSeries":10,"batchMaxBytes":100,"metricPrefixCacheMaxEntries":5,"bearer":{"bearerTokenSecret":{"name":"ref","key":"key"},"bearerTokenFile":"/t"},"tlsConfig":{"insecureSkipVerify":true}},"monitoring":{"pull":{"port":"8080"},"push":{"url":"http://p","pushFrequency":"15s","extraLabels":{"a":"b"},"basicAuth":{"username":{"name":"ref","key":"key"}},"tlsConfig":{"ca":{"secret":{"name":"ref","key":"key"}}}}},"server":{"addr":"0.0.0.0","port":"8490","pathPrefix":"/p","maxConcurrentTasks":2,"uiDefaultState":"x","useReaderConnectionSettings":true}}`, ``},
		{`models: [`, `{}`, ``},
		{`preset: ui
settings:
  n_workers: -1
`, `{}`, ``},
		// several empty schedulers: error must not depend on map order
		{"reader:\n  queries:\n   B9:\nmodels:\n x:\n    class: zscore\nschedulers:\n 8:\n Y:", `{"reAder":{},"writer":{"":"0"}}`, ``},
	} {
		f.Add([]byte(seed.config), []byte(seed.spec), []byte(seed.child))
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		f.Fatal(err)
	}
	fclient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(fuzzutil.RefObjects()...).Build()
	ctx := context.Background()
	f.Fuzz(func(t *testing.T, raw, spec, childRaw []byte) {
		var input struct {
			Reader     *vmv1.VMAnomalyReadersSpec    `json:"reader"`
			Writer     *vmv1.VMAnomalyWritersSpec    `json:"writer"`
			Monitoring *vmv1.VMAnomalyMonitoringSpec `json:"monitoring"`
			Server     *vmv1.VMAnomalyServerSpec     `json:"server"`
		}
		if err := json.Unmarshal(spec, &input); err != nil {
			fuzztest.New(spec).Fill(&input)
		}
		cr := &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{Name: "fuzz", Namespace: "default"},
			Spec: vmv1.VMAnomalySpec{
				ConfigRawYaml: string(raw),
				Reader:        input.Reader,
				Writer:        input.Writer,
				Monitoring:    input.Monitoring,
				Server:        input.Server,
			},
		}
		var configs []*vmv1.VMAnomalyConfig
		var names []string
		if len(childRaw) > 0 {
			configs = []*vmv1.VMAnomalyConfig{{
				ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "default"},
				Spec:       runtime.RawExtension{Raw: childRaw},
			}}
			names = []string{"default/child"}
		}
		pos := &ParsedObjects{configs: build.NewChildObjects("vmanomalyconfigs", configs, names)}
		ac := build.NewAssetsCache(ctx, fclient, map[build.ResourceKind]*build.ResourceCfg{
			build.TLSAssetsResourceKind: {MountDir: "/test", SecretName: build.ResourceName(build.TLSAssetsResourceKind, cr)},
		})
		first, err := pos.Load(cr, ac)
		if string(raw) == validConfig && string(spec) == validSpec && (len(childRaw) == 0 || string(childRaw) == validChild) && err != nil {
			t.Fatalf("valid config seed failed to load: %v", err)
		}
		second, nextErr := pos.Load(cr, ac)
		if (err == nil) != (nextErr == nil) || err != nil && err.Error() != nextErr.Error() {
			t.Fatalf("non-idempotent config errors: %v != %v", err, nextErr)
		}
		if err == nil && !bytes.Equal(first, second) {
			t.Fatalf("non-idempotent config: %s != %s", first, second)
		}
	})
}

func FuzzPartialConfigValidate(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`models:
  baseline:
    class: zscore
schedulers:
  timer:
    class: noop
`,
		`models:
  broken: null
schedulers:
  broken: null
`,
		`queries:
  up:
    expr: up
`,
		`models: [`,
		`models:
  auto:
    class: auto
    tuned_class_name: zscore
    optimization_params: {anomaly_percentage: 0.1, n_splits: 2, n_trials: 3, train_val_ratio: 0.5, validation_scheme: leaky, beta: 1, timeout: 5, n_jobs: -1, frozen_params: {z_threshold: 3}}
  prophet: {class: prophet, args: {interval_width: 0.9}, tz_seasonalities: [dow, {name: hod, fourier_order: 4}]}
  zscore: {class: zscore_online, z_threshold: 3, decay: 0.9, history_strength: 1, min_n_samples_seen: 5}
  hw: {class: holtwinters, seasonality: 1d, frequency: 1h}
  mad: {class: mad_online, threshold: 2, decay: 1, compression: 10}
  rq: {class: rolling_quantile, quantile: 0.9, window_steps: 10}
  oq: {class: quantile_online, quantiles: [0.1, 0.5, 0.9], seasonal_interval: 1h, decay: 0.5}
  std: {class: std, z_threshold: 2, period: 3, min_n_samples_seen: 2}
  if: {class: isolation_forest, contamination: auto, seasonal_features: [hod]}
  ifm: {class: isolation_forest_multivariate, contamination: 0.01, groupby: [job]}
  te: {class: temporal_envelope, quantiles: [0.1, 0.9], iqr_threshold: 1, alpha: 0.5, loss_reactivity: 1, min_n_samples_seen: 3, changepoint_window: 2, seasonalities: [hod_smooth, dow_spiky]}
  tem: {class: temporal_envelope_multivariate, groupby: [job], dependency_rank: 1, score_aggregation: l2, max_channels: 4, recommended_max_channels: 2}
  peer: {class: peer_outlier, min_peer_count: 3, epsilon_quantile: 0.5, tolerance: 1, decay: 0.99, min_n_samples_seen: 20, groupby: [job]}
schedulers:
  p: {class: periodic, fit_every: 1h, fit_window: 1d, infer_every: 1m, tz: UTC}
  o: {class: oneoff, infer_start_s: 10, infer_end_s: 20, fit_start_iso: 2024-01-01T00:00:00Z, fit_end_s: 1800000000}
  b: {class: backtesting, fit_window: 1h, from_s: 1, to_iso: 2024-01-01T00:00:00Z, fit_every: 1h, n_jobs: 2}
`,
		`models:
  m: {class: peer_outlier, args: {a: 1}}
`,
		`models:
  m: {class: peer_outlier, min_peer_count: 1}
`,
		`models:
  m: {class: peer_outlier, epsilon_quantile: 2}
`,
		`models:
  m: {class: peer_outlier, tolerance: 0}
`,
		`models:
  m: {class: peer_outlier, decay: 0.5}
`,
		`models:
  m: {class: peer_outlier, decay: 0.5, min_n_samples_seen: 100}
`,
		`models:
  m: {class: auto}
`,
		`models:
  m: {class: auto, optimization_params: {anomaly_percentage: 0.1, validation_scheme: x, frozen_params: {class: a}}}
`,
		`models:
  m: {class: zscore_online, decay: 2}
`,
		`models:
  m: {class: temporal_envelope, quantiles: [0.9, 0.1]}
`,
		`models:
  m: {class: temporal_envelope, seasonalities: [hod_smooth, hod_smooth]}
`,
		`models:
  m: {class: temporal_envelope_multivariate, score_aggregation: x, max_channels: 1, recommended_max_channels: 2}
`,
		`models:
  m: {class: unknown}
`,
		`schedulers:
  s: {class: oneoff, infer_start_s: 2, infer_end_s: 1}
`,
		`schedulers:
  s: {class: backtesting, from_s: 1, to_s: 2, n_jobs: -1}
`,
		`schedulers:
  s: {class: periodic}
`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var cfg PartialConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return
		}
		_ = cfg.Validate()
	})
}
