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
		fuzztest.New(data).Fill(&src)
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
	const (
		meta     = `"metadata":{"name":"n","namespace":"default","uid":"u","labels":{"keep":"1","drop/x":"2"},"annotations":{"keep":"1","drop/x":"2"}}`
		secret   = `{"name":"s","key":"k"}`
		httpCfg  = `"httpConfig":{"basicAuth":{"username":` + secret + `,"password":` + secret + `},"authorization":{"type":"Bearer","credentials":` + secret + `},"followRedirects":true,"proxyURL":"http://proxy:3128","tlsConfig":{"insecureSkipVerify":true}}`
		receiver = `{"name":"r","emailConfigs":[{"to":"a@b.c","from":"d@e.f","smarthost":"smtp:25","authPassword":` + secret + `,"headers":[{"key":"k","value":"v"}],"requireTLS":true,` + httpCfg + `}],` +
			`"pagerdutyConfigs":[{"routingKey":` + secret + `,"details":[{"key":"k","value":"v"}],"pagerDutyImageConfigs":[{"href":"h","src":"s","alt":"a"}],"pagerDutyLinkConfigs":[{"href":"h","text":"t"}]}],` +
			`"pushoverConfigs":[{"userKey":` + secret + `,"token":` + secret + `,"priority":"1","retry":"1m","expire":"1h","html":true}],` +
			`"slackConfigs":[{"apiURL":` + secret + `,"channel":"c","fields":[{"title":"t","value":"v","short":true}],"actions":[{"type":"button","text":"t","confirm":{"text":"t","okText":"ok"}}],"mrkdwnIn":["text"]}],` +
			`"opsgenieConfigs":[{"apiKey":` + secret + `,"details":[{"key":"k","value":"v"}],"responders":[{"id":"i","type":"team"}],"tags":"a,b"}],` +
			`"webhookConfigs":[{"url":"https://example.org","maxAlerts":5,"timeout":"10s",` + httpCfg + `},{"urlSecret":` + secret + `}],` +
			`"victoropsConfigs":[{"apiKey":` + secret + `,"routingKey":"rk","customFields":[{"key":"k","value":"v"}]}],` +
			`"wechatConfigs":[{"apiSecret":` + secret + `,"corpID":"c","agentID":"a"}],` +
			`"telegramConfigs":[{"botToken":` + secret + `,"chatID":1,"messageThreadID":2}],` +
			`"msteamsConfigs":[{"webhookUrl":` + secret + `,"title":"t"}],` +
			`"msteamsv2Configs":[{"webhookURL":` + secret + `,"title":"t"}],` +
			`"discordConfigs":[{"apiURL":` + secret + `,"title":"t"}],` +
			`"snsConfigs":[{"topicARN":"arn","sigv4":{"region":"r","accessKey":` + secret + `,"secretKey":` + secret + `},"attributes":{"k":"v"}}],` +
			`"webexConfigs":[{"apiURL":"https://webex","roomID":"r"}]}`
		route = `"route":{"receiver":"r","groupBy":["a"],"groupWait":"30s","groupInterval":"5m","repeatInterval":"4h","matchers":[{"name":"a","value":"b","matchType":"="},{"name":"c","value":"d","matchType":"=~"}],"continue":true,"muteTimeIntervals":["m"],"activeTimeIntervals":["m"],"routes":[{"receiver":"r","routes":[{"receiver":"r"}]}]}`
		am    = `{` + meta + `,"spec":{` + route + `,"receivers":[` + receiver + `],` +
			`"inhibitRules":[{"targetMatch":[{"name":"a","value":"b","matchType":"="}],"sourceMatch":[{"name":"c","value":"d","matchType":"!="}],"equal":["x"]}],` +
			`"muteTimeIntervals":[{"name":"m","timeIntervals":[{"times":[{"startTime":"00:00","endTime":"12:00"}],"weekdays":["monday:friday"],"daysOfMonth":[{"start":1,"end":15}],"months":["january"],"years":["2025"]}]}]}}`
		sc = `{` + meta + `,"spec":{"jobName":"j","metricsPath":"/m","scrapeInterval":"30s","enableCompression":false,"scheme":"HTTPS","params":{"a":["b"]},` +
			`"staticConfigs":[{"targets":["localhost:8428"],"labels":{"a":"b"}}],` +
			`"kubernetesSDConfigs":[{"role":"Pod","selectors":[{"role":"Node","label":"a=b"}]},{"role":"Endpoints"}],` +
			`"fileSDConfigs":[{"files":["/f.yaml"]}],"httpSDConfigs":[{"url":"http://sd"}],` +
			`"relabelings":[{"action":"replace","sourceLabels":["a"],"targetLabel":"job","replacement":"j"},{"action":"keep","sourceLabels":["a"],"regex":"b"}],` +
			`"metricRelabelings":[{"action":"labeldrop","regex":"x"}],` +
			`"basicAuth":{"username":` + secret + `,"password":` + secret + `},"tlsConfig":{"insecureSkipVerify":true},"sampleLimit":10}}`
	)
	const minimal = `{"spec":{"route":{"receiver":"r","matchers":[{"name":"a"},{"name":"b","value":"c"}],"routes":[{"receiver":"r"}]},` +
		`"receivers":[{"name":"r","pagerdutyConfigs":[{}],"snsConfigs":[{}],"webexConfigs":[{}],"slackConfigs":[{"actions":[{"type":"b","text":"t"}]}]}]}}`
	for _, seed := range []struct {
		kind uint8
		data string
	}{
		{alertmanagerConfigKind, `{}`},
		{alertmanagerConfigKind, `{"spec":{"route":{"routes":[{"routes":[1]}]}}}`},
		{alertmanagerConfigKind, minimal},
		{alertmanagerConfigKind, am},
		{scrapeConfigKind, sc},
	} {
		f.Add(seed.kind, []byte(seed.data))
	}
	f.Fuzz(func(t *testing.T, kind uint8, data []byte) {
		conf := &config.BaseOperatorConf{
			EnabledPrometheusConverterOwnerReferences:     true,
			PrometheusConverterAddArgoCDIgnoreAnnotations: true,
			FilterPrometheusConverterLabelPrefixes:        []string{"drop/"},
			FilterPrometheusConverterAnnotationPrefixes:   []string{"drop/"},
		}
		if kind%promAlphaKindCount == alertmanagerConfigKind {
			fuzzPromConversion(t, data, func(src *promv1alpha1.AlertmanagerConfig) (any, error) { return AlertmanagerConfig(src, conf) })
		} else {
			fuzzPromConversion(t, data, func(src *promv1alpha1.ScrapeConfig) (any, error) {
				return ScrapeConfig(context.Background(), src, conf)
			})
		}
	})
}
