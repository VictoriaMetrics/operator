package converter

import (
	"bytes"
	"encoding/json"
	"testing"
)

func FuzzHelmValuesToCRD(f *testing.F) {
	charts := []struct {
		name string
		seed string
	}{
		{"victoria-metrics-auth", "config:\n  users:\n    - username: user\n      password: pass\n"},
		{"victoria-metrics-single", "server:\n  replicaCount: 2\n  retentionPeriod: 14d\n  image:\n    tag: v1.93.0\n"},
		{"victoria-metrics-cluster", "vmselect:\n  replicaCount: 2\nvmstorage:\n  replicaCount: 3\n"},
		{"victoria-metrics-agent", "remoteWrite:\n  - url: http://localhost:8428/api/v1/write\n"},
		{"victoria-metrics-alert", "server:\n  remoteWrite:\n    url: http://localhost:8428/api/v1/write\n"},
		{"victoria-metrics-anomaly", "server:\n  image:\n    tag: v1.0.0\n"},
		{"victoria-logs-cluster", "vlselect:\n  replicaCount: 2\n"},
		{"victoria-logs-agent", "remoteWrite:\n  - url: http://localhost:9428\n"},
		{"victoria-logs-collector", "remoteWrite:\n  - url: http://localhost:9428\n"},
		{"victoria-logs-single", "server:\n  replicaCount: 2\n"},
		{"victoria-traces-single", "server:\n  image:\n    tag: v1.0.0\n"},
		{"victoria-traces-cluster", "vtstorage:\n  replicaCount: 2\n"},
	}
	for i, chart := range charts {
		for _, seed := range []string{"{}", chart.seed} {
			values, err := UnmarshalValues([]byte(seed), chart.name)
			if err != nil {
				f.Fatalf("invalid %s seed: %v", chart.name, err)
			}
			if _, err := Convert("fuzz", "default", values); err != nil {
				f.Fatalf("unconvertible %s seed: %v", chart.name, err)
			}
			f.Add(uint8(i), []byte(seed))
		}
	}
	f.Fuzz(func(t *testing.T, kind uint8, data []byte) {
		if len(data) > 8192 {
			t.Skip()
		}
		chart := charts[int(kind)%len(charts)].name
		values, err := UnmarshalValues(data, chart)
		if err != nil {
			return
		}
		first, err := Convert("fuzz", "default", values)
		second, nextErr := Convert("fuzz", "default", values)
		if (err == nil) != (nextErr == nil) || err != nil && err.Error() != nextErr.Error() {
			t.Fatalf("non-idempotent conversion errors: %v != %v", err, nextErr)
		}
		if err != nil {
			return
		}
		a, err := json.Marshal(first)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(second)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("non-idempotent conversion: %s != %s", a, b)
		}
	})
}

func FuzzMergeValues(f *testing.F) {
	for _, seed := range [][2]string{
		{"{}", "{}"},
		{"server:\n  replicaCount: 1\n  image:\n    tag: v1\n", "server:\n  replicaCount: 2\n"},
		{"datasource:\n  headers:\n    Authorization: token\n", "datasource:\n  headers:\n    X-Env: test\n"},
		{"remoteWrite:\n  - url: http://localhost:8428\n", "remoteWrite: []\n"},
	} {
		f.Add([]byte(seed[0]), []byte(seed[1]))
	}
	f.Fuzz(func(t *testing.T, base, override []byte) {
		if len(base) > 4096 || len(override) > 4096 {
			t.Skip()
		}
		a, err := MergeValues(base, override)
		b, nextErr := MergeValues(base, override)
		if (err == nil) != (nextErr == nil) || err != nil && err.Error() != nextErr.Error() {
			t.Fatalf("non-idempotent merge errors: %v != %v", err, nextErr)
		}
		if err == nil && !bytes.Equal(a, b) {
			t.Fatalf("non-idempotent merge: %s != %s", a, b)
		}
	})
}
