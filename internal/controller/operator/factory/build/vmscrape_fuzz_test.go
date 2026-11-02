package build

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func FuzzVMServiceScrapeBuilder(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"service":{"metadata":{"name":"single","namespace":"default","labels":{"app":"single"}},"spec":{"ports":[{"name":"http","port":8428}]}}}`,
		`{"service":{"spec":{"ports":[{"name":"other","port":9000}]}}}`,
		`{"cr":{"spec":{"extraArgs":{"metricsAuthKey":"secret","http.pathPrefix":"/vm","tls":"true"}}},"service":{"spec":{"ports":[{"name":"http","port":8428},{"name":"reloader","port":8430},{"name":"other","port":9000}]}},"additionalPorts":["reloader"]}`,
		`{"service":{"spec":{"ports":[{"name":"reloader","port":8430}]}},"additionalPorts":["reloader"]}`,
		`{"service":{"spec":{"ports":[{"name":"http","port":8428}]}},"additionalPorts":["missing"]}`,
		`{"cr":{"spec":{"serviceScrapeSpec":{"selector":{"matchLabels":{"custom":"yes"}},"endpoints":[{"port":"http","path":"/custom"},{"port":"extra","path":"/extra"}]}}},"service":{"spec":{"ports":[{"name":"http","port":8428}]}}}`,
		`{"cr":{"spec":{"extraArgs":{"http.pathPrefix":"/vm"},"serviceScrapeSpec":{"endpoints":[{"port":"http"}]}}},"service":{"spec":{"ports":[{"name":"http","port":8428}]}}}`,
		`{"cr":{"spec":{"serviceScrapeSpec":{"endpoints":[{"port":"http","relabelConfigs":[{"targetLabel":"victoriametrics_app","replacement":"false"}]}]}}},"service":{"spec":{"ports":[{"name":"http","port":8428}]}}}`,
		`{"service":{"spec":{"ports":[{"name":"http","port":8428},{"name":"http","port":8429}]}}}`,
		`{"cr":{"spec":{"serviceScrapeSpec":{"selector":{"matchExpressions":[{"key":"app","operator":"Exists"}]}}}},"service":{"spec":{"ports":[{"name":"http","port":8428}]}}}`,
		`{"service":{"metadata":{"name":"svc","namespace":"ns","annotations":{"a":"b"},"ownerReferences":[{"apiVersion":"v1","kind":"VMSingle","name":"single","uid":"1"}]},"spec":{"ports":[{"name":"http","port":8428}]}}}`,
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
			fuzztest.New(data).Fill(&input)
		}
		input.CR.Name, input.CR.Namespace = "single", "default"

		VMServiceScrape(&input.Service, &input.CR, input.AdditionalPorts...)
	})
}

func FuzzVMPodScrapeBuilder(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"port":"http","additionalPorts":["reloader","backup"]}`,
		`{"cr":{"spec":{"extraArgs":{"metricsAuthKey":"secret","http.pathPrefix":"/vm","tls":"true"}}},"port":"http","additionalPorts":["reloader"]}`,
		`{"cr":{"spec":{"serviceScrapeSpec":{"sampleLimit":100,"seriesLimit":50,"podTargetLabels":["app"],"attachMetadata":{"node":true},"endpoints":[{"port":"http","path":"/custom"},{"port":"extra","path":"/extra"}]}}},"port":"http"}`,
		`{"cr":{"spec":{"serviceScrapeSpec":{"endpoints":[{"port":"reloader","path":"/custom","interval":"10s"}]}}},"port":"http","additionalPorts":["reloader"]}`,
		`{"cr":{"spec":{"serviceScrapeSpec":{"endpoints":[{"port":"http","relabelConfigs":[{"targetLabel":"victoriametrics_app","replacement":"false"}]}]}}},"port":"http"}`,
		`{"cr":{"spec":{"useLegacyNaming":true}},"port":"http"}`,
		`{"additionalPorts":["reloader"]}`,
		`{"port":"http","additionalPorts":["http"]}`,
		`{"cr":{"spec":{"serviceScrapeSpec":{"endpoints":[{"port":"extra"},{"port":"extra"}]}}},"port":"http"}`,
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
			fuzztest.New(data).Fill(&input)
		}
		input.CR.Name, input.CR.Namespace = "single", "default"

		VMPodScrape(&input.CR, input.Port, input.AdditionalPorts...)
	})
}
