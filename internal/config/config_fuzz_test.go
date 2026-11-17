package config

import (
	"encoding/json"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/caarlos0/env/v11"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
)

func FuzzEnvConfig(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"WATCH_NAMESPACE":"a,b","VM_ENABLEDPROMETHEUSCONVERTER_PODMONITOR":"true"}`,
		`{"WATCH_NAMESPACE":"Bad_NS"}`,
		`{"VM_OPENSHIFT_COMPATIBILITY":"enabled","VM_FORCERESYNCINTERVAL":"60s"}`,
		`{"VM_OPENSHIFT_COMPATIBILITY":"bad"}`,
		`{"VM_FORCERESYNCINTERVAL":"1ns"}`,
		`{"VM_FORCERESYNCINTERVAL":"bad"}`,
		`{"VM_CONFIG_RELOADER_LIMIT_MEMORY":"bad"}`,
		`{"VM_CONFIG_RELOADER_LIMIT_CPU":"bad"}`,
		`{"VM_CONFIG_RELOADER_REQUEST_MEMORY":"bad"}`,
		`{"VM_CONFIG_RELOADER_REQUEST_CPU":"bad"}`,
		`{"VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_MEM":"1Gi","VM_VMSINGLEDEFAULT_RESOURCE_REQUEST_CPU":"x"}`,
		`{"VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_EPHEMERAL_STORAGE":"x"}`,
		`{"VM_VMSINGLEDEFAULT_RESOURCE_REQUEST_EPHEMERAL_STORAGE":"x"}`,
		`{"VM_VMSINGLEDEFAULT_RESOURCE_LIMIT_CPU":"x"}`,
		`{"VM_VMSINGLEDEFAULT_RESOURCE_REQUEST_MEM":"x"}`,
		`{"VM_VLOGSDEFAULT_RESOURCE_LIMIT_MEM":"x"}`,
		`{"VM_VMAGENTDEFAULT_RESOURCE_LIMIT_MEM":"unlimited","VM_VMALERTDEFAULT_RESOURCE_LIMIT_MEM":"x"}`,
		`{"VM_METRICS_VERSION":"v1.0.0","VM_CONFIG_RELOADER_IMAGE":"img"}`,
		`{"VM_CONFIG_DATA_BUDGET_BYTES":"-1","VM_ENABLETCP6":"maybe"}`,
		`{"VM_CUSTOMCONFIGRELOADERIMAGE":"img:tag","VM_LOOPBACK":"::1"}`,
	} {
		for _, format := range []string{"table", "list", "json", "yaml", "markdown", "bad"} {
			f.Add(format, []byte(seed))
		}
	}
	f.Fuzz(func(t *testing.T, format string, data []byte) {
		var vars map[string]string
		if err := json.Unmarshal(data, &vars); err != nil {
			fuzztest.New(data).Fill(&vars)
		}

		// parse fuzzed env on top of defaults, like MustGetBaseConfig
		opts := getEnvOpts()
		maps.Copy(opts.Environment, vars)
		for k, v := range vars {
			// cyclic ${VAR} expansion overflows stack in caarlos0/env
			// Fixed in 11.4.2
			if strings.Contains(v, "$") {
				delete(opts.Environment, k)
			}
		}
		c, err := env.ParseAsWithOptions[BaseOperatorConf](opts)
		if err != nil {
			return
		}
		_ = c.validate()
		_ = c.ResyncAfterDuration()
		ConfigAsMetrics(prometheus.NewRegistry(), &c)

		// PrintDefaults writes to stdout, silence it
		stdout := os.Stdout
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = devNull
		t.Cleanup(func() {
			os.Stdout = stdout
			devNull.Close()
		})
		_ = c.PrintDefaults(format)
	})
}
