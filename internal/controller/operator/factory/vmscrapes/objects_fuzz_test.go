package vmscrapes

import (
	"encoding/json"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

type scrapeObjectsInput struct {
	Params               vmv1beta1.CommonScrapeParams    `json:"params"`
	ExternalLabels       map[string]string               `json:"externalLabels"`
	MustUseNodeSelector  bool                            `json:"mustUseNodeSelector"`
	HasClusterWideAccess bool                            `json:"hasClusterWideAccess"`
	IgnoreNSSelectors    bool                            `json:"ignoreNamespaceSelectors"`
	ServiceScrapes       []vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapes"`
	PodScrapes           []vmv1beta1.VMPodScrapeSpec     `json:"podScrapes"`
	Probes               []vmv1beta1.VMProbeSpec         `json:"probes"`
	NodeScrapes          []vmv1beta1.VMNodeScrapeSpec    `json:"nodeScrapes"`
	StaticScrapes        []vmv1beta1.VMStaticScrapeSpec  `json:"staticScrapes"`
	ScrapeConfigs        []vmv1beta1.VMScrapeConfigSpec  `json:"scrapeConfigs"`
}

// objects turns specs into namespaced scrape objects.
func (in *scrapeObjectsInput) objects() []runtime.Object {
	objs := fuzzutil.RefObjects()
	add := func(o client.Object) {
		o.SetName(fmt.Sprintf("s%d", len(objs)))
		o.SetNamespace("default")
		o.SetLabels(map[string]string{"app": "fuzz"})
		objs = append(objs, o.(runtime.Object))
	}
	for _, s := range in.ServiceScrapes {
		add(&vmv1beta1.VMServiceScrape{Spec: s})
	}
	for _, s := range in.PodScrapes {
		add(&vmv1beta1.VMPodScrape{Spec: s})
	}
	for _, s := range in.Probes {
		add(&vmv1beta1.VMProbe{Spec: s})
	}
	for _, s := range in.NodeScrapes {
		add(&vmv1beta1.VMNodeScrape{Spec: s})
	}
	for _, s := range in.StaticScrapes {
		add(&vmv1beta1.VMStaticScrape{Spec: s})
	}
	for _, s := range in.ScrapeConfigs {
		add(&vmv1beta1.VMScrapeConfig{Spec: s})
	}
	return objs
}

func FuzzScrapeObjects(f *testing.F) {
	const (
		auth     = `"basicAuth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tlsConfig":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"},"serverName":"s"}`
		bearer   = `"bearerTokenSecret":{"name":"ref","key":"key"},"oauth2":{"client_id":{"secret":{"name":"ref","key":"key"}},"client_secret":{"name":"ref","key":"key"},"token_url":"http://t"}`
		relabel  = `"relabelConfigs":[{"sourceLabels":["a"],"targetLabel":"b"},null],"metricRelabelConfigs":[{"action":"drop","regex":"x"},null]`
		params   = `"interval":"10s","scrape_interval":"5m","scrapeTimeout":"5s","honorLabels":true,"honorTimestamps":false,"sampleLimit":10,"seriesLimit":5,"params":{"a":["b"]},"vm_scrape_params":{"stream_parse":true,"scrape_align_interval":"1s","proxy_client_config":{"bearer_token":{"name":"ref","key":"key"}},"headers":["a:b"]},"proxyURL":"http://p"`
		endpoint = `{"port":"http","path":"/m","scheme":"https",` + auth + `,` + relabel + `,` + params + `,"attach_metadata":{"node":true}}`
		common   = `"selectAllByDefault":true,"scrapeInterval":"30s","scrapeTimeout":"20s","minScrapeInterval":"15s","maxScrapeInterval":"1m","vmAgentExternalLabelName":"prom","externalLabels":{"a":"b"},"enforcedNamespaceLabel":"ns","ignoreNamespaceSelectors":false,"arbitraryFSAccessThroughSMs":{"deny":true},"overrideHonorLabels":true,"overrideHonorTimestamps":true,"scrapeClasses":[{"name":"c","default":true,"tlsConfig":{"caFile":"/ca"},"authorization":{"credentialsFile":"/c"},"relabelConfigs":[{"targetLabel":"x","replacement":"y"}],"metricRelabelConfigs":[{"action":"drop"}],"attachMetadata":{"node":true}},{"name":"d"}],"inlineScrapeConfig":"- job_name: inline\n","globalScrapeRelabelConfigs":[{"targetLabel":"g"}],"globalScrapeMetricRelabelConfigs":[{"action":"drop"}]`
		full     = `{"params":{` + common + `},"externalLabels":{"e":"f"},"hasClusterWideAccess":true,` +
			`"serviceScrapes":[{"scrapeClass":"c","discoveryRole":"endpointslices","jobLabel":"j","targetLabels":["t"],"podTargetLabels":["p"],"sampleLimit":1,"selector":{"matchLabels":{"a":"b"}},"namespaceSelector":{"any":true},"attach_metadata":{"node":true},"endpoints":[` + endpoint + `,{"targetPort":8080,"follow_redirects":true}]},{"discoveryRole":"service","namespaceSelector":{"matchNames":["default","other"]},"endpoints":[{"port":"web"}]}],` +
			`"podScrapes":[{"jobLabel":"j","podTargetLabels":["p"],"selector":{"matchExpressions":[{"key":"a","operator":"Exists"}]},"podMetricsEndpoints":[{"port":"http","portNumber":9100,"filterRunning":false,"scheme":"http",` + bearer + `,` + relabel + `,"attach_metadata":{"node":true}},{"targetPort":"web"}]}],` +
			`"probes":[{"jobName":"probe","module":"http_2xx","vmProberSpec":{"url":"prober:9115","scheme":"https","path":"/probe"},"targets":{"staticConfig":{"targets":["http://a"],"labels":{"l":"v"},"relabelingConfigs":[{"targetLabel":"r"},null]},"ingress":{"role":"ingress","selector":{"matchLabels":{"a":"b"}},"namespaceSelector":{"any":true},"relabelingConfigs":[{"targetLabel":"i"},null]},"kubernetes":[{"role":"service"}]},` + auth + `,"metricRelabelConfigs":[{"action":"drop"},null],` + params + `}],` +
			`"nodeScrapes":[{"jobLabel":"j","targetLabels":["t"],"port":"9100","path":"/m","scheme":"http","selector":{"matchLabels":{"n":"1"}},` + auth + `,` + relabel + `,` + params + `}],` +
			`"staticScrapes":[{"jobName":"st","targetEndpoints":[{"targets":["a:1"],"labels":{"l":"v"},` + auth + `,` + relabel + `}]}],` +
			`"scrapeConfigs":[{"staticConfigs":[{"targets":["a:1"],"labels":{"l":"v"}}],"fileSDConfigs":[{"files":["/f"]}],"httpSDConfigs":[{"url":"http://sd",` + auth + `}],"kubernetesSDConfigs":[{"role":"pod","apiServer":"http://api","namespaces":{"names":["a"]},"selectors":[{"role":"pod","label":"a=b"}],"attach_metadata":{"node":true},` + auth + `}],"consulSDConfigs":[{"server":"c:8500","tokenRef":{"name":"ref","key":"key"},"services":["s"]}],"dnsSDConfigs":[{"names":["a"],"type":"SRV","port":53}],"ec2SDConfigs":[{"region":"r","accessKey":{"name":"ref","key":"key"},"secretKey":{"name":"ref","key":"key"}}],"azureSDConfigs":[{"subscriptionID":"s","clientSecret":{"name":"ref","key":"key"}}],"gceSDConfigs":[{"project":"p","zone":"z"}],"openstackSDConfigs":[{"role":"instance","region":"r","password":{"name":"ref","key":"key"}}],"digitalOceanSDConfigs":[{"authorization":{"credentials":{"name":"ref","key":"key"}}}],"nomadSDConfigs":[{"server":"n"}],"path":"/m","scrapeClass":"c",` + relabel + `,` + params + `,` + auth + `}]}`
	)
	for _, seed := range []string{
		`{}`,
		`{"params":{"selectAllByDefault":true},"serviceScrapes":[{"endpoints":[{"port":"http"}]}],"podScrapes":[{"podMetricsEndpoints":[{"port":"http"}]}],"probes":[{"targets":{"staticConfig":{"targets":["a"]}}}],"nodeScrapes":[{}],"staticScrapes":[{"targetEndpoints":[{"targets":["a:1"]}]}],"scrapeConfigs":[{"staticConfigs":[{"targets":["a:1"]}]}]}`,
		full,
		`{"params":{"selectAllByDefault":true,"ignoreNamespaceSelectors":true,"scrapeClasses":[{"name":"c"}]},"ignoreNamespaceSelectors":true,"serviceScrapes":[{"scrapeClass":"missing","endpoints":[{"port":"a","bearerTokenFile":"/f"}]}],"podScrapes":[{"podMetricsEndpoints":[{"tlsConfig":{"caFile":"/f"}}]}],"nodeScrapes":[{"bearerTokenFile":"/f"}],"scrapeConfigs":[{"kubernetesSDConfigs":[{"role":"node"}]}]}`,
		`{"params":{"serviceScrapeSelector":{"matchLabels":{"app":"fuzz"}},"serviceScrapeNamespaceSelector":{},"podScrapeSelector":{"matchLabels":{"app":"other"}},"probeSelector":{},"nodeScrapeSelector":{},"staticScrapeSelector":{},"scrapeConfigSelector":{}},"serviceScrapes":[{"endpoints":[{"port":"a"}]}],"probes":[{}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var in scrapeObjectsInput
		if err := json.Unmarshal(data, &in); err != nil {
			fuzztest.New(data).Fill(&in)
		}
		ctx := t.Context()
		rclient := k8stools.GetTestClientWithObjects(in.objects())
		pos := &ParsedObjects{
			Namespace:                "default",
			ExternalLabels:           in.ExternalLabels,
			MustUseNodeSelector:      in.MustUseNodeSelector,
			HasClusterWideAccess:     in.HasClusterWideAccess,
			IgnoreNamespaceSelectors: in.IgnoreNSSelectors,
		}
		sp := &in.Params
		if err := pos.Init(ctx, rclient, sp); err != nil {
			return
		}
		pos.ValidateObjects(sp)
		_, _ = pos.GenerateConfig(ctx, sp, getAssetsCache(ctx, rclient))
		_ = pos.UpdateStatusesForScrapeObjects(ctx, rclient, "fuzz.default.vmagent", &vmv1beta1.VMAgent{ObjectMeta: metav1.ObjectMeta{Name: "fuzz", Namespace: "default"}})
	})
}
