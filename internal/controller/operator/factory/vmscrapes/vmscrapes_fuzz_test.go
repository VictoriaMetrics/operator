package vmscrapes

import (
	"bytes"
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func FuzzRelabelConfig(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"sourceLabels":["job"],"targetLabel":"service","regex":"(.*)"}`,
		`{"source_labels":["__name__"],"target_label":"metric","action":"replace"}`,
		`{"sourceLabels":["a","b"],"separator":";","targetLabel":"t","regex":["x","y"],"modulus":4,"replacement":"$1","action":"hashmod"}`,
		`{"action":"keep_if_equal","if":["{a=\"b\"}"],"match":"m","labels":{"b":"2","a":"1"}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var rc vmv1beta1.RelabelConfig
		if err := json.Unmarshal(data, &rc); err != nil {
			fuzztest.New(data).Fill(&rc)
		}
		a, err := yaml.Marshal(generateRelabelConfig(&rc))
		if err != nil {
			t.Fatal(err)
		}
		b, err := yaml.Marshal(generateRelabelConfig(&rc))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("non-idempotent relabel config: %q != %q", a, b)
		}
	})
}

type staticScrapeInput struct {
	Params   vmv1beta1.CommonScrapeParams `json:"params"`
	Spec     vmv1beta1.VMStaticScrapeSpec `json:"spec"`
	Endpoint vmv1beta1.TargetEndpoint     `json:"endpoint"`
}

func FuzzStaticScrapeConfig(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"endpoint":{"targets":["localhost:8428"]}}`,
		`{"endpoint":{"targets":["host:9100"],"labels":{"job":"node"},"relabelConfigs":[{"targetLabel":"instance","replacement":"node"}]}}`,
		`{"endpoint":{"targets":["localhost:8428"],"metricRelabelConfigs":[null]}}`,
		`{"endpoint":{"targets":["localhost:8428"],"relabelConfigs":[null]}}`,
		`{"endpoint":{"targets":["h:1"],"path":"/m","scheme":"HTTPS","params":{"b":["1"],"a":["2"]},"follow_redirects":true,"sampleLimit":5,"seriesLimit":6,"interval":"30s","scrapeTimeout":"10s","proxyURL":"http://p","honorLabels":true,"honorTimestamps":false,"max_scrape_size":"1MB","bearerTokenFile":"/t","tlsConfig":{"caFile":"/ca","certFile":"/c","keyFile":"/k","serverName":"s","insecureSkipVerify":true},"vm_scrape_params":{"disable_compression":true,"disable_keep_alive":true,"no_stale_markers":true,"stream_parse":true,"scrape_align_interval":"1m","scrape_offset":"5s","headers":["h:v"]},"metricRelabelConfigs":[{"targetLabel":"ns"},{"targetLabel":"drop"}]}}`,
		`{"params":{"enforcedNamespaceLabel":"ns","overrideHonorLabels":true,"overrideHonorTimestamps":true,"scrapeTimeout":"5s","minScrapeInterval":"1m","staticScrapeRelabelTemplate":[{"targetLabel":"tpl","replacement":"v"}]},"spec":{"jobName":"j","sampleLimit":10,"seriesLimit":20},"endpoint":{"targets":["h:1"],"interval":"5s","honorLabels":true,"metricRelabelConfigs":[{"targetLabel":"ns"}]}}`,
		`{"params":{"staticScrapeRelabelTemplate":[null]},"endpoint":{"targets":["h:1"]}}`,
		`{"params":{"maxScrapeInterval":"1m"},"endpoint":{"targets":["h:1"],"interval":"5m"}}`,
		`{"params":{"minScrapeInterval":"x"},"endpoint":{"targets":["h:1"],"interval":"5s"}}`,
		`{"params":{"minScrapeInterval":"1m","maxScrapeInterval":"x"},"endpoint":{"targets":["h:1"],"interval":"5m"}}`,
		`{"params":{"scrapeClasses":[{"name":"c","default":true,"tlsConfig":{"caFile":"/ca"},"authorization":{"credentialsFile":"/c"},"relabelConfigs":[{"targetLabel":"cls"}],"metricRelabelConfigs":[{"targetLabel":"m"}]}]},"spec":{"scrapeClass":"c"},"endpoint":{"targets":["h:1"],"basicAuth":{"username_file":"/u","password_file":"/p"}}}`,
		`{"params":{"scrapeClasses":[{"name":"c","tlsConfig":{"caFile":"/ca","certFile":"/c","keyFile":"/k","serverName":"s"},"authorization":{"credentialsFile":"/c"},"basicAuth":{"username":{"name":"s","key":"u"},"password":{"name":"s","key":"p"}},"oauth2":{"client_id":{"secret":{"name":"s","key":"i"}},"client_secret":{"name":"s","key":"k"},"token_url":"http://t","scopes":["a"],"endpoint_params":{"a":"b"},"proxy_url":"http://p","tlsConfig":{"caFile":"/ca"}},"bearerTokenFile":"/b"}]},"spec":{"scrapeClass":"c"},"endpoint":{"targets":["h:1"],"tlsConfig":{"insecureSkipVerify":true},"authorization":{"type":"Bearer"},"basicAuth":{"username":{"name":"x"}},"oauth2":{"token_url":"http://u","tlsConfig":{"serverName":"x"}}}}`,
		`{"endpoint":{"targets":["h:1"],"bearerTokenSecret":{"name":"s","key":"k"}}}`,
		`{"endpoint":{"targets":["h:1"],"basicAuth":{"username":{"name":"s","key":"u"},"password":{"name":"s","key":"p"}}}}`,
		`{"endpoint":{"targets":["h:1"],"oauth2":{"client_id":{"configMap":{"name":"c","key":"k"}},"client_secret":{"name":"s","key":"k"},"token_url":"http://t"}}}`,
		`{"endpoint":{"targets":["h:1"],"vm_scrape_params":{"proxy_client_config":{"basic_auth":{"username":{"name":"s","key":"u"}}}}}}`,
	} {
		f.Add([]byte(seed))
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		f.Fatal(err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	f.Fuzz(func(t *testing.T, data []byte) {
		var in staticScrapeInput
		if err := json.Unmarshal(data, &in); err != nil {
			fuzztest.New(data).Fill(&in)
		}
		render := func() ([]byte, error) {
			params := in.Params.DeepCopy()
			sc := &vmv1beta1.VMStaticScrape{Spec: *in.Spec.DeepCopy()}
			cfg, err := generateStaticScrapeConfig(t.Context(), params, sc, in.Endpoint.DeepCopy(), 0, build.NewAssetsCache(t.Context(), client, nil))
			if err != nil {
				return nil, err
			}
			return yaml.Marshal(cfg)
		}
		a, err := render()
		b, err2 := render()
		if (err == nil) != (err2 == nil) || !bytes.Equal(a, b) {
			t.Fatalf("non-idempotent static scrape: %q (%v) != %q (%v)", a, err, b, err2)
		}
	})
}
