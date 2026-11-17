package converter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"sigs.k8s.io/randfill"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
)

var fuzzCharts = []string{
	"victoria-metrics-auth",
	"victoria-metrics-single",
	"victoria-metrics-cluster",
	"victoria-metrics-agent",
	"victoria-metrics-alert",
	"victoria-metrics-anomaly",
	"victoria-logs-cluster",
	"victoria-logs-agent",
	"victoria-logs-collector",
	"victoria-logs-single",
	"victoria-traces-single",
	"victoria-traces-cluster",
	"unknown",
}

// fillAny fills untyped helm values with strings
func fillAny(v *any, c randfill.Continue) {
	*v = c.String(8)
}

func FuzzConvert(f *testing.F) {
	seeds := []string{
		``,
		`{}`,
		`[]`,
		"replicaCount: 1\nheaders:\n  a: b\n",
		"server:\n  replicaCount: 2\n  retentionPeriod: 14d\n  resources:\n    limits:\n      memory: \"1Gi\"\n  extraArgs:\n    a: b\n  securityContext:\n    runAsUser: 1000\n    seLinuxOptions:\n      level: s0\n",
		"config:\n  users:\n    - username: u\n      password: p\n      url_prefix: http://a\n    - bearer_token: t\n      name: n\n      url_map:\n        - src_paths: [\"/a\"]\n          url_prefix: [\"http://b\"]\n    - {}\n",
		"server:\n  datasource:\n    url: http://ds\n    headers:\n      a: b\n  notifier:\n    alertmanager:\n      url: http://am\n  remote:\n    write:\n      url: http://rw\n  config:\n    alerts:\n      groups:\n        - name: g\n          rules:\n            - alert: a\n              expr: up\n",
		"vmstorage:\n  replicaCount: 2\n  retentionPeriod: 1\n  persistentVolume:\n    storageClassName: sc\n    size: 10Gi\nvmselect:\n  replicaCount: 2\nvminsert:\n  replicaCount: 2\n",
		"remoteWrite:\n  - url: http://a\n    tls:\n      ca: c\nextraArgs:\n  a: b\n",
		"server: [1]\n",
	}
	for chart := range fuzzCharts {
		for _, seed := range seeds {
			f.Add(uint8(chart), false, []byte(seed), []byte(seeds[3]))
		}
		f.Add(uint8(chart), true, []byte(strings.Repeat("fuzz\x01\xff", 200)), []byte(nil))
	}
	f.Fuzz(func(t *testing.T, chart uint8, filled bool, base, override []byte) {
		name := fuzzCharts[int(chart)%len(fuzzCharts)]
		data := base
		if merged, err := MergeValues(base, override); err == nil {
			data = merged
		}

		// filled mode builds typed values directly to reach deep fields
		values, err := UnmarshalValues(data, name)
		if filled {
			values, err = UnmarshalValues([]byte(`{}`), name)
			if err == nil {
				fuzztest.New(base).Funcs(fillAny).Fill(values)
			}
		}
		if err != nil {
			return
		}

		stable(t, func() (any, error) { return Convert("fuzz", "default", values) })
		if v, ok := values.(*VMAuthHelmValues); ok {
			stable(t, func() (any, error) { return ConvertVMAuthUsers("fuzz", "default", v) })
		}
		if v, ok := values.(*VMAlertHelmValues); ok {
			stable(t, func() (any, error) {
				alert, secrets, err := ConvertVMAlert("fuzz", "default", v)
				return []any{alert, secrets}, err
			})
			stable(t, func() (any, error) { return ConvertVMAlertRules("fuzz", "default", v) })
		}
	})
}

// stable runs conv twice; differing results mean nondeterminism or input mutation
func stable(t *testing.T, conv func() (any, error)) {
	t.Helper()
	a, errA := conv()
	b, errB := conv()
	if fmt.Sprint(errA) != fmt.Sprint(errB) {
		t.Fatalf("conversion errors differ: %v != %v", errA, errB)
	}

	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	if fmt.Sprint(errA) != fmt.Sprint(errB) || !bytes.Equal(ja, jb) {
		t.Fatalf("conversion outputs differ:\n%s\n%s", ja, jb)
	}
}
