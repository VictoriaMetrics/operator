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
		fuzztest.New(data).Fill(&src)
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
	const (
		meta     = `"metadata":{"labels":{"keep":"1","drop/x":"2"},"annotations":{"drop/x":"2"}}`
		secret   = `{"name":"s","key":"k"}`
		scm      = `{"secret":` + secret + `}`
		selector = `{"matchLabels":{"app":"api"},"matchExpressions":[]}`
		relabel  = `[{"action":"keep","sourceLabels":["a"],"regex":"b"},{"action":"drop"}]`
		auth     = `"basicAuth":{"username":` + secret + `,"password":` + secret + `},"bearerTokenSecret":` + secret + `,"authorization":{"type":"Bearer"},` +
			`"oauth2":{"clientId":` + scm + `,"clientSecret":` + secret + `,"tokenUrl":"http://t"},"tlsConfig":{"ca":` + scm + `,"cert":` + scm + `,"insecureSkipVerify":true,"serverName":"sn"}`
		endpoint = `{"scheme":"https","bearerTokenFile":"/etc/prometheus/secrets/s/t",` + auth + `,"relabelings":` + relabel + `,"metricRelabelings":` + relabel + `}`
		sampling = `"sampleLimit":10,"attachMetadata":{"node":true}`
	)
	for _, seed := range []struct {
		kind uint8
		data string
	}{
		{promRuleKind, `{` + meta + `,"spec":{"groups":[{"name":"g","interval":"1m","query_offset":"5s","limit":10,"rules":[{"alert":"a","expr":"up == 0","for":"5m","keep_firing_for":"10m"}]}]}}`},
		{serviceMonitorKind, `{` + meta + `,"spec":{"selector":` + selector + `,"namespaceSelector":{"any":true},` + sampling + `,"serviceDiscoveryRole":"EndpointSlice","endpoints":[` + endpoint + `,{"bearerTokenFile":"/etc/prometheus/configmaps/c/t","bearerTokenSecret":{}}]}}`},
		{podMonitorKind, `{}`},
		{podMonitorKind, `{` + meta + `,"spec":{"selector":` + selector + `,` + sampling + `,"podMetricsEndpoints":[{"filterRunning":true,` + auth + `,"relabelings":` + relabel + `}]}}`},
		{probeKind, `{}`},
		{probeKind, `{` + meta + `,"spec":{"sampleLimit":10,"prober":{"url":"bb:9115","proxyUrl":"http://p"},"targets":{"staticConfig":{"static":["a"],"relabelingConfigs":` + relabel + `},"ingress":{"selector":` + selector + `}},` + auth + `}}`},
	} {
		f.Add(seed.kind, []byte(seed.data))
	}
	f.Fuzz(func(t *testing.T, kind uint8, data []byte) {
		ctx := context.Background()
		conf := &config.BaseOperatorConf{
			EnabledPrometheusConverterOwnerReferences:     true,
			PrometheusConverterAddArgoCDIgnoreAnnotations: true,
			FilterPrometheusConverterLabelPrefixes:        []string{"drop/"},
			FilterPrometheusConverterAnnotationPrefixes:   []string{"drop/"},
		}
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
