package v1alpha1

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	"github.com/VictoriaMetrics/operator/internal/config"
)

const (
	alertmanagerConfigKind uint8 = iota
	scrapeConfigKind
	promAlphaKindCount
)

func fuzzPromConversion[T any](t *testing.T, data []byte, convert func(*T) (any, error)) {
	t.Helper()
	var src T
	if err := json.Unmarshal(data, &src); err != nil {
		fuzztest.New(data).Fuzz(&src)
	}
	first, err := convert(&src)
	second, nextErr := convert(&src)
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
}

func FuzzPrometheusAlphaConversion(f *testing.F) {
	for _, seed := range []struct {
		kind uint8
		data string
	}{
		{alertmanagerConfigKind, `{}`},
		{alertmanagerConfigKind, `{"metadata":{"name":"am","namespace":"default"},"spec":{"route":{"receiver":"webhook","routes":[{"receiver":"webhook"}]},"receivers":[{"name":"webhook","webhookConfigs":[{"url":"https://example.org"}]}]}}`},
		{alertmanagerConfigKind, `{"spec":{"route":{"receiver":"unknown","routes":[{"invalid":true}]}}}`},
		{scrapeConfigKind, `{}`},
		{scrapeConfigKind, `{"metadata":{"name":"scrape","namespace":"default"},"spec":{"jobName":"scrape","staticConfigs":[{"targets":["localhost:8428"]}],"relabelings":[{"action":"replace","targetLabel":"job","replacement":"scrape"}]}}`},
	} {
		f.Add(seed.kind, []byte(seed.data))
	}
	f.Fuzz(func(t *testing.T, kind uint8, data []byte) {
		conf := &config.BaseOperatorConf{}
		if kind%promAlphaKindCount == alertmanagerConfigKind {
			fuzzPromConversion(t, data, func(src *promv1alpha1.AlertmanagerConfig) (any, error) { return AlertmanagerConfig(src, conf) })
		} else {
			fuzzPromConversion(t, data, func(src *promv1alpha1.ScrapeConfig) (any, error) {
				return ScrapeConfig(context.Background(), src, conf)
			})
		}
	})
}
