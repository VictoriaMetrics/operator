package v1

import (
	"testing"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

const (
	selector = `"selector":{"matchLabels":{"app":"a"}}`
	badSel   = `{"spec":{"selector":{"matchExpressions":[{"key":"a","operator":"bad"}]}}}`
)

func FuzzValidators(f *testing.F) {
	fuzzutil.Validators(f,
		fuzzutil.NewValidator(func() *promv1.PodMonitor { return &promv1.PodMonitor{} }, &PodMonitorCustomValidator{},
			`{"spec":{`+selector+`,"podMetricsEndpoints":[{"port":"http","relabelings":[{"action":"drop","sourceLabels":["a"]}]}]}}`, badSel),
		fuzzutil.NewValidator(func() *promv1.Probe { return &promv1.Probe{} }, &ProbeCustomValidator{},
			`{"spec":{"prober":{"url":"blackbox:9115"},"targets":{"staticConfig":{"static":["http://a"]}}}}`,
			`{"spec":{"targets":{"ingress":{"selector":{"matchExpressions":[{"key":"a","operator":"bad"}]}}}}}`),
		fuzzutil.NewValidator(func() *promv1.PrometheusRule { return &promv1.PrometheusRule{} }, &PrometheusRuleCustomValidator{},
			`{"spec":{"groups":[{"name":"g","rules":[{"alert":"a","expr":"up == 0","for":"1m"},{"record":"r","expr":"sum(up)"}]}]}}`,
			`{"spec":{"groups":[{"name":"g","rules":[{"alert":"a","expr":"up(("}]},{"name":"g"}]}}`),
		fuzzutil.NewValidator(func() *promv1.ServiceMonitor { return &promv1.ServiceMonitor{} }, &ServiceMonitorCustomValidator{},
			`{"spec":{`+selector+`,"endpoints":[{"port":"http","metricRelabelings":[{"action":"keep","sourceLabels":["a"],"regex":"b"}]}]}}`, badSel),
	)
}
