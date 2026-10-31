package build

import (
	"encoding/json"
	"testing"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

func FuzzVMServiceScrapeBuilder(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"service":{"metadata":{"name":"single","namespace":"default","labels":{"app":"single"}},"spec":{"ports":[{"name":"http","port":8428}]}}}`,
		`{"cr":{"spec":{"extraArgs":{"metricsAuthKey":"secret","http.pathPrefix":"/vm","tls":"true"}}},"service":{"spec":{"ports":[{"name":"http","port":8428},{"name":"reloader","port":8430},{"name":"other","port":9000}]}},"additionalPorts":["reloader"]}`,
		`{"cr":{"spec":{"serviceScrapeSpec":{"selector":{"matchLabels":{"custom":"yes"}},"endpoints":[{"port":"http","path":"/custom"},{"port":"extra","path":"/extra"}]}}},"service":{"spec":{"ports":[{"name":"http","port":8428}]}}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			CR              vmv1beta1.VMSingle `json:"cr"`
			Service         corev1.Service     `json:"service"`
			AdditionalPorts []string           `json:"additionalPorts"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fuzz(&input)
		}
		input.CR.Name, input.CR.Namespace = "single", "default"
		assertStableBuilder(t, func() (any, error) {
			return VMServiceScrape(&input.Service, &input.CR, input.AdditionalPorts...), nil
		})
	})
}

func FuzzVMPodScrapeBuilder(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"port":"http","additionalPorts":["reloader","backup"]}`,
		`{"cr":{"spec":{"extraArgs":{"metricsAuthKey":"secret","http.pathPrefix":"/vm","tls":"true"}}},"port":"http","additionalPorts":["reloader"]}`,
		`{"cr":{"spec":{"serviceScrapeSpec":{"sampleLimit":100,"podTargetLabels":["app"],"endpoints":[{"port":"http","path":"/custom"},{"port":"extra","path":"/extra"}]}}},"port":"http"}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			CR              vmv1beta1.VMSingle `json:"cr"`
			Port            string             `json:"port"`
			AdditionalPorts []string           `json:"additionalPorts"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fuzz(&input)
		}
		input.CR.Name, input.CR.Namespace = "single", "default"
		assertStableBuilder(t, func() (any, error) {
			return VMPodScrape(&input.CR, input.Port, input.AdditionalPorts...), nil
		})
	})
}
