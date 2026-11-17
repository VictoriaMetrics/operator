package v1alpha1

import (
	"testing"

	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

func FuzzValidators(f *testing.F) {
	fuzzutil.Validators(f,
		fuzzutil.NewValidator(func() *promv1alpha1.AlertmanagerConfig { return &promv1alpha1.AlertmanagerConfig{} }, &AlertmanagerConfigCustomValidator{},
			`{"spec":{"route":{"receiver":"r","routes":[{"receiver":"r"}]},"receivers":[{"name":"r","webhookConfigs":[{"url":"http://a"}]}]}}`,
			`{"spec":{"route":{"receiver":"missing"}}}`,
			`{"spec":{"route":{"routes":[1]}}}`),
		fuzzutil.NewValidator(func() *promv1alpha1.ScrapeConfig { return &promv1alpha1.ScrapeConfig{} }, &ScrapeConfigCustomValidator{},
			`{"spec":{"staticConfigs":[{"targets":["a:80"]}],"relabelings":[{"action":"drop","sourceLabels":["a"]}]}}`,
			`{"spec":{"kubernetesSDConfigs":[{"role":"Pod","selectors":[{"role":"Pod","label":"a"}]}]}}`,
			`{"spec":{"sampleLimit":18446744073709551615}}`),
	)
}
