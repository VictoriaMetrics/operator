package converter

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	"github.com/VictoriaMetrics/operator/internal/config"
)

const (
	promRuleKind uint8 = iota
	serviceMonitorKind
	podMonitorKind
	probeKind
	promKindCount
)

// fuzzPromConversion checks the same source twice without resetting it, so mutation is observable.
func fuzzPromConversion[T any](t *testing.T, data []byte, convert func(*T) any) {
	t.Helper()
	var src T
	if err := json.Unmarshal(data, &src); err != nil {
		fuzztest.New(data).Fuzz(&src)
	}
	first, err := json.Marshal(convert(&src))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(convert(&src))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("non-idempotent conversion: %s != %s", first, second)
	}
}

func FuzzPrometheusConversion(f *testing.F) {
	for _, seed := range []struct {
		kind uint8
		data string
	}{
		{promRuleKind, `{}`},
		{promRuleKind, `{"metadata":{"name":"rules","namespace":"default"},"spec":{"groups":[{"name":"recording","rules":[{"record":"up:sum","expr":"sum(up)","labels":{"team":"ops"}}]}]}}`},
		{serviceMonitorKind, `{}`},
		{serviceMonitorKind, `{"metadata":{"name":"service","namespace":"default"},"spec":{"selector":{"matchLabels":{"app":"api"}},"endpoints":[{"port":"http","path":"/metrics","relabelings":[{"action":"keep","sourceLabels":["job"],"regex":"api"}]}]}}`},
		{podMonitorKind, `{}`},
		{podMonitorKind, `{"metadata":{"name":"pod","namespace":"default"},"spec":{"selector":{"matchLabels":{"app":"api"}},"podMetricsEndpoints":[{"port":"http","path":"/metrics"}]}}`},
		{probeKind, `{}`},
		{probeKind, `{"metadata":{"name":"probe","namespace":"default"},"spec":{"prober":{"url":"blackbox:9115"},"targets":{"staticConfig":{"static":["example.org"]}}}}`},
		{probeKind, `{"metadata":{"name":"probe","namespace":"default"},"spec":{"targets":{"ingress":{"selector":{"matchLabels":{"app":"api"}}}}}}`},
	} {
		f.Add(seed.kind, []byte(seed.data))
	}
	f.Fuzz(func(t *testing.T, kind uint8, data []byte) {
		ctx := context.Background()
		conf := &config.BaseOperatorConf{}
		switch kind % promKindCount {
		case promRuleKind:
			fuzzPromConversion(t, data, func(src *promv1.PrometheusRule) any { return PrometheusRule(ctx, src, conf) })
		case serviceMonitorKind:
			fuzzPromConversion(t, data, func(src *promv1.ServiceMonitor) any { return ServiceMonitor(ctx, src, conf) })
		case podMonitorKind:
			fuzzPromConversion(t, data, func(src *promv1.PodMonitor) any { return PodMonitor(ctx, src, conf) })
		case probeKind:
			fuzzPromConversion(t, data, func(src *promv1.Probe) any { return Probe(ctx, src, conf) })
		}
	})
}
