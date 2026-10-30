package config

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v2"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
		{`models: [`, `{}`, ``},
		{`preset: ui
settings:
  n_workers: -1
`, `{}`, ``},
	} {
		f.Add([]byte(seed.config), []byte(seed.spec), []byte(seed.child))
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		f.Fatal(err)
	}
	fclient := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()
	f.Fuzz(func(t *testing.T, raw, spec, childRaw []byte) {
		if len(raw) > 4096 || len(spec) > 4096 || len(childRaw) > 4096 {
			t.Skip()
		}
		var input struct {
			Reader     *vmv1.VMAnomalyReadersSpec    `json:"reader"`
			Writer     *vmv1.VMAnomalyWritersSpec    `json:"writer"`
			Monitoring *vmv1.VMAnomalyMonitoringSpec `json:"monitoring"`
			Server     *vmv1.VMAnomalyServerSpec     `json:"server"`
		}
		if err := json.Unmarshal(spec, &input); err != nil {
			return
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
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		var cfg PartialConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return
		}
		_ = cfg.Validate()
	})
}
