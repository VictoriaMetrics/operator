package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type testScrapeObject struct {
	serviceScrapeSpecTemplate *vmv1beta1.VMServiceScrapeSpec
	extraArgs                 map[string]string
	listeners                 []vmv1beta1.HTTPListener
	primaryPortName           string
}

func (tb *testScrapeObject) PrimaryPortName() string {
	if tb.primaryPortName != "" {
		return tb.primaryPortName
	}
	return "http"
}

func (tb *testScrapeObject) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return tb.serviceScrapeSpecTemplate
}

func (tb *testScrapeObject) GetMetricsPath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(tb.extraArgs, "/metrics")
}

func (tb *testScrapeObject) Params(vmv1beta1.ParamsKind) *vmv1beta1.StandardAppsParams {
	listeners := tb.listeners
	if listeners == nil {
		listeners = []vmv1beta1.HTTPListener{{Name: tb.PrimaryPortName()}}
	}
	return &vmv1beta1.StandardAppsParams{
		CommonAppsParams: vmv1beta1.CommonAppsParams{
			ExtraArgs: tb.extraArgs,
		},
		HTTPListeners: listeners,
	}
}

func (tb *testScrapeObject) GetNamespace() string {
	return "default"
}

func (tb *testScrapeObject) PrefixedName() string {
	return "test"
}

func (tb *testScrapeObject) SelectorLabels() map[string]string {
	return map[string]string{"app": "test"}
}

func (tb *testScrapeObject) AsOwner() metav1.OwnerReference {
	return metav1.OwnerReference{Name: "test"}
}

func TestVMServiceScrapeForServiceWithSpec(t *testing.T) {
	vmAppRelabel := []*vmv1beta1.RelabelConfig{victoriaMetricsAppRelabelConfig()}
	type opts struct {
		spec                  testScrapeObject
		sidecars              []testScrapeObject
		service               *corev1.Service
		wantServiceScrapeSpec vmv1beta1.VMServiceScrapeSpec
	}

	f := func(o opts) {
		t.Helper()
		sidecars := make([]ScrapeBuilder, len(o.sidecars))
		for i := range o.sidecars {
			sidecars[i] = &o.sidecars[i]
		}
		gotServiceScrape := VMServiceScrape(o.service, &o.spec, sidecars...)
		assert.Equal(t, o.wantServiceScrapeSpec, gotServiceScrape.Spec)
	}

	// custom selector
	f(opts{
		service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "vmagent-svc",
				Labels: map[string]string{"my-label": "value"},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name: "http",
				}},
			},
		},
		spec: testScrapeObject{
			serviceScrapeSpecTemplate: &vmv1beta1.VMServiceScrapeSpec{
				Selector: metav1.LabelSelector{MatchLabels: map[string]string{"my-label": "value"}},
			},
		},
		wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{{
				EndpointRelabelings: vmv1beta1.EndpointRelabelings{
					RelabelConfigs: vmAppRelabel,
				},
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					Path: "/metrics",
				},
				Port: "http",
			}},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"my-label": "value",
				},
			},
		},
	})

	// multiple ports, only the primary's own listener name matches
	f(opts{
		service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "vmagent-svc",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name: "http",
					},
					{
						Name: "opentsdb-http",
					},
				},
			},
		},
		spec: testScrapeObject{},
		wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{{
				EndpointRelabelings: vmv1beta1.EndpointRelabelings{
					RelabelConfigs: vmAppRelabel,
				},
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					Path: "/metrics",
				},
				Port: "http",
			}},
			Selector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      vmv1beta1.AdditionalServiceLabel,
					Operator: metav1.LabelSelectorOpDoesNotExist,
				}},
			},
		},
	})

	// a sidecar (vmbackupmanager-style) contributes its own TargetPort-addressed endpoint,
	// regardless of whether the Service happens to declare a matching named port
	f(opts{
		sidecars: []testScrapeObject{{
			listeners: []vmv1beta1.HTTPListener{{Name: "vmbackup", Addr: ":9000"}},
		}},
		service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "vmagent-svc",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name: "http",
					},
				},
			},
		},
		spec: testScrapeObject{},
		wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{
				{
					EndpointRelabelings: vmv1beta1.EndpointRelabelings{
						RelabelConfigs: vmAppRelabel,
					},
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/metrics",
					},
					Port: "http",
				},
				{
					TargetPort: ptr.To(intstr.Parse("9000")),
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/metrics",
					},
					EndpointRelabelings: vmv1beta1.EndpointRelabelings{
						RelabelConfigs: []*vmv1beta1.RelabelConfig{{
							SourceLabels: []string{"job"},
							TargetLabel:  "job",
							Regex:        vmv1beta1.StringOrArray{"(.+)"},
							Replacement:  ptr.To("${1}-vmbackup"),
						}, victoriaMetricsAppRelabelConfig()},
					},
				},
			},
			Selector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      vmv1beta1.AdditionalServiceLabel,
					Operator: metav1.LabelSelectorOpDoesNotExist,
				}},
			},
		},
	})

	// with extra metric labels
	f(opts{
		service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "vmagent-svc",
				Labels: map[string]string{
					"key": "value",
				},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name: "http",
				}},
			},
		},
		spec: testScrapeObject{
			serviceScrapeSpecTemplate: &vmv1beta1.VMServiceScrapeSpec{
				TargetLabels: []string{"key"},
			},
		},
		wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{{
				EndpointRelabelings: vmv1beta1.EndpointRelabelings{
					RelabelConfigs: vmAppRelabel,
				},
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					Path: "/metrics",
				},
				Port: "http",
			}},
			TargetLabels: []string{"key"},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"key": "value",
				},
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      vmv1beta1.AdditionalServiceLabel,
					Operator: metav1.LabelSelectorOpDoesNotExist,
				}},
			},
		},
	})

	// with extra endpoints
	f(opts{
		service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "vmagent-svc",
				Labels: map[string]string{
					"key": "value",
				},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name: "http",
				}},
			},
		},
		spec: testScrapeObject{
			serviceScrapeSpecTemplate: &vmv1beta1.VMServiceScrapeSpec{
				TargetLabels: []string{"key"},
				Endpoints: []vmv1beta1.Endpoint{
					{
						EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
							Path: "/metrics",
						},
						Port: "sidecar",
					},
					{
						EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
							Path:           "/metrics",
							ScrapeInterval: "30s",
							ScrapeTimeout:  "10s",
						},
						Port: "http",
					},
				},
			},
		},
		wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{
				{
					EndpointRelabelings: vmv1beta1.EndpointRelabelings{
						RelabelConfigs: vmAppRelabel,
					},
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/metrics",
					},
					Port: "sidecar",
				},
				{
					EndpointRelabelings: vmv1beta1.EndpointRelabelings{
						RelabelConfigs: vmAppRelabel,
					},
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path:           "/metrics",
						ScrapeInterval: "30s",
						ScrapeTimeout:  "10s",
					},
					Port: "http",
				},
			},
			TargetLabels: []string{"key"},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"key": "value",
				},
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      vmv1beta1.AdditionalServiceLabel,
					Operator: metav1.LabelSelectorOpDoesNotExist,
				}},
			},
		},
	})

	// with a custom http.pathPrefix: the primary port uses it, but a sidecar (config-reloader
	// style) always scrapes its own literal /metrics path via TargetPort, unaffected by the
	// app's own path prefix and needing no matching named Service port
	f(opts{
		sidecars: []testScrapeObject{{
			listeners: []vmv1beta1.HTTPListener{{Name: "reloader-http", Addr: ":8435"}},
		}},
		service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "vmagent-svc",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "http"},
				},
			},
		},
		spec: testScrapeObject{
			extraArgs: map[string]string{"http.pathPrefix": "/prefix"},
		},
		wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{
				{
					EndpointRelabelings: vmv1beta1.EndpointRelabelings{
						RelabelConfigs: vmAppRelabel,
					},
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/prefix/metrics",
					},
					Port: "http",
				},
				{
					TargetPort: ptr.To(intstr.Parse("8435")),
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/metrics",
					},
					EndpointRelabelings: vmv1beta1.EndpointRelabelings{
						RelabelConfigs: []*vmv1beta1.RelabelConfig{{
							SourceLabels: []string{"job"},
							TargetLabel:  "job",
							Regex:        vmv1beta1.StringOrArray{"(.+)"},
							Replacement:  ptr.To("${1}-reloader-http"),
						}, victoriaMetricsAppRelabelConfig()},
					},
				},
			},
			Selector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      vmv1beta1.AdditionalServiceLabel,
					Operator: metav1.LabelSelectorOpDoesNotExist,
				}},
			},
		},
	})

	// with authKey and tls
	f(opts{
		service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "vmagent-svc",
				Labels: map[string]string{
					"key": "value",
				},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name: "http",
				}},
			},
		},
		spec: testScrapeObject{
			extraArgs: map[string]string{
				"tls":            "true",
				"metricsAuthKey": "some-access-key",
			},
		},
		wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{{
				EndpointRelabelings: vmv1beta1.EndpointRelabelings{
					RelabelConfigs: vmAppRelabel,
				},
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					Path:   "/metrics",
					Params: map[string][]string{"authKey": {"some-access-key"}},
					Scheme: "https",
					EndpointAuth: vmv1beta1.EndpointAuth{
						TLSConfig: &vmv1beta1.TLSConfig{InsecureSkipVerify: true},
					},
				},
				Port: "http",
			}},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"key": "value",
				},
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      vmv1beta1.AdditionalServiceLabel,
					Operator: metav1.LabelSelectorOpDoesNotExist,
				}},
			},
		},
	})

	// multiple HTTPListeners: every plain-HTTP one gets its own endpoint, a
	// PROXY-protocol one is skipped, and a sidecar still contributes its own endpoint
	f(opts{
		sidecars: []testScrapeObject{{
			listeners: []vmv1beta1.HTTPListener{{Name: "vmbackup", Addr: ":9000"}},
		}},
		service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "vmagent-svc"},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "public"},
					{Name: "internal"},
				},
			},
		},
		spec: testScrapeObject{
			listeners: []vmv1beta1.HTTPListener{
				{Name: "public", Addr: ":8427", Primary: true, UseProxyProtocol: ptr.To(true)},
				{Name: "internal", Addr: ":8428"},
			},
		},
		wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{
				{
					EndpointRelabelings: vmv1beta1.EndpointRelabelings{
						RelabelConfigs: vmAppRelabel,
					},
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/metrics",
					},
					Port: "internal",
				},
				{
					TargetPort: ptr.To(intstr.Parse("9000")),
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/metrics",
					},
					EndpointRelabelings: vmv1beta1.EndpointRelabelings{
						RelabelConfigs: []*vmv1beta1.RelabelConfig{{
							SourceLabels: []string{"job"},
							TargetLabel:  "job",
							Regex:        vmv1beta1.StringOrArray{"(.+)"},
							Replacement:  ptr.To("${1}-vmbackup"),
						}, victoriaMetricsAppRelabelConfig()},
					},
				},
			},
			Selector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      vmv1beta1.AdditionalServiceLabel,
					Operator: metav1.LabelSelectorOpDoesNotExist,
				}},
			},
		},
	})
}

func TestVMServiceScrapeAddsVictoriaMetricsAppLabel(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
			{Name: "http"},
		}},
	}
	spec := testScrapeObject{serviceScrapeSpecTemplate: &vmv1beta1.VMServiceScrapeSpec{
		Endpoints: []vmv1beta1.Endpoint{
			{Port: "http"},
			{Port: "custom"},
		},
	}}
	sidecar := testScrapeObject{listeners: []vmv1beta1.HTTPListener{{Name: "extra", Addr: ":1234"}}}

	scrape := VMServiceScrape(service, &spec, &sidecar)

	assert.Len(t, scrape.Spec.Endpoints, 3)
	for i := range scrape.Spec.Endpoints {
		assert.Contains(t, scrape.Spec.Endpoints[i].RelabelConfigs, victoriaMetricsAppRelabelConfig())
	}

}

func TestVMPodScrapeAddsVictoriaMetricsAppLabel(t *testing.T) {
	spec := testScrapeObject{serviceScrapeSpecTemplate: &vmv1beta1.VMServiceScrapeSpec{
		Endpoints: []vmv1beta1.Endpoint{
			{
				Port: "http",
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					Path: "/custom",
				},
			},
			{Port: "extra"},
		},
	}}

	podScrape := VMPodScrape(&spec)

	assert.Len(t, podScrape.Spec.PodMetricsEndpoints, 2)
	assert.Equal(t, "/custom", podScrape.Spec.PodMetricsEndpoints[0].Path)
	for i := range podScrape.Spec.PodMetricsEndpoints {
		assert.Contains(t, podScrape.Spec.PodMetricsEndpoints[i].RelabelConfigs, victoriaMetricsAppRelabelConfig())
	}
}

func TestVMServiceScrapeObjectsAddVictoriaMetricsAppLabel(t *testing.T) {
	objectMeta := metav1.ObjectMeta{Name: "test", Namespace: "default"}
	sap := vmv1beta1.StandardAppsParams{HTTPListeners: []vmv1beta1.HTTPListener{{Name: "http"}}}

	f := func(name string, builder ScrapeBuilder) {
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
				{Name: "http"},
			}},
		}

		scrape := VMServiceScrape(service, builder)

		assert.Len(t, scrape.Spec.Endpoints, 1)
		assert.Contains(t, scrape.Spec.Endpoints[0].RelabelConfigs, victoriaMetricsAppRelabelConfig())
	}
	f("VMSingle", &vmv1beta1.VMSingle{ObjectMeta: objectMeta, Spec: vmv1beta1.VMSingleSpec{StandardAppsParams: sap}})
	f("VMAlert", &vmv1beta1.VMAlert{ObjectMeta: objectMeta, Spec: vmv1beta1.VMAlertSpec{StandardAppsParams: sap}})
	f("VMAuth", &vmv1beta1.VMAuth{ObjectMeta: objectMeta, Spec: vmv1beta1.VMAuthSpec{StandardAppsParams: sap}})
	f("VMSelect", &vmv1beta1.VMSelect{StandardAppsParams: sap})
	f("VMInsert", &vmv1beta1.VMInsert{StandardAppsParams: sap})
	f("VMStorage", &vmv1beta1.VMStorage{StandardAppsParams: sap})
	f("VLSingle", &vmv1.VLSingle{ObjectMeta: objectMeta, Spec: vmv1.VLSingleSpec{StandardAppsParams: sap}})
	f("VLSelect", &vmv1.VLSelect{StandardAppsParams: sap})
	f("VLInsert", &vmv1.VLInsert{StandardAppsParams: sap})
	f("VLStorage", &vmv1.VLStorage{StandardAppsParams: sap})
	f("VTSingle", &vmv1.VTSingle{ObjectMeta: objectMeta, Spec: vmv1.VTSingleSpec{StandardAppsParams: sap}})
	f("VTSelect", &vmv1.VTSelect{StandardAppsParams: sap})
	f("VTInsert", &vmv1.VTInsert{StandardAppsParams: sap})
	f("VTStorage", &vmv1.VTStorage{StandardAppsParams: sap})
}

func TestVMPodScrapeObjectsAddVictoriaMetricsAppLabel(t *testing.T) {
	objectMeta := metav1.ObjectMeta{Name: "test", Namespace: "default"}
	sap := vmv1beta1.StandardAppsParams{HTTPListeners: []vmv1beta1.HTTPListener{{Name: "http"}}}

	f := func(builder podScrapeBuilder) {
		scrape := VMPodScrape(builder)

		assert.Len(t, scrape.Spec.PodMetricsEndpoints, 1)
		assert.Contains(t, scrape.Spec.PodMetricsEndpoints[0].RelabelConfigs, victoriaMetricsAppRelabelConfig())
	}
	f(&vmv1beta1.VMAgent{ObjectMeta: objectMeta, Spec: vmv1beta1.VMAgentSpec{StandardAppsParams: sap}})
	f(&vmv1.VLAgent{ObjectMeta: objectMeta, Spec: vmv1.VLAgentSpec{StandardAppsParams: sap}})
	f(&vmv1.VMAnomaly{ObjectMeta: objectMeta})
}
