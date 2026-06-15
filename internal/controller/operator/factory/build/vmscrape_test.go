package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type testScrapeObject struct {
	serviceScrapeSpecTemplate *vmv1beta1.VMServiceScrapeSpec
	extraArgs                 map[string]string
}

func (tb *testScrapeObject) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return tb.serviceScrapeSpecTemplate
}

func (tb *testScrapeObject) GetMetricsPath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(tb.extraArgs, "/metrics")
}

func (tb *testScrapeObject) UseTLS() bool {
	return vmv1beta1.UseTLS(tb.extraArgs)
}

func (tb *testScrapeObject) GetExtraArgs() map[string]string {
	return tb.extraArgs
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
		service               *corev1.Service
		filterPortNames       []string
		wantServiceScrapeSpec vmv1beta1.VMServiceScrapeSpec
	}

	f := func(o opts) {
		t.Helper()
		gotServiceScrape := VMServiceScrape(o.service, &o.spec, o.filterPortNames...)
		assert.Equal(t, o.wantServiceScrapeSpec, gotServiceScrape.Spec)
	}

	// custom selector
	f(opts{
		filterPortNames: []string{"http-2"},
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

	// multiple ports with filter
	f(opts{
		filterPortNames: []string{"http-5"},
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

	// multiple ports with vmbackup filter
	f(opts{
		filterPortNames: []string{"vmbackup"},
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
						Name: "vmbackup",
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
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/metrics",
					},
					Port: "vmbackup",
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
}

func TestVMServiceScrapeAddsVictoriaMetricsAppLabel(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
			{Name: "http"},
			{Name: "extra"},
		}},
	}
	spec := testScrapeObject{serviceScrapeSpecTemplate: &vmv1beta1.VMServiceScrapeSpec{
		Endpoints: []vmv1beta1.Endpoint{
			{Port: "http"},
			{Port: "custom"},
		},
	}}

	scrape := VMServiceScrape(service, &spec, "extra")

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

	podScrape := VMPodScrape(&spec, "http")

	assert.Len(t, podScrape.Spec.PodMetricsEndpoints, 2)
	assert.Equal(t, "/custom", podScrape.Spec.PodMetricsEndpoints[0].Path)
	for i := range podScrape.Spec.PodMetricsEndpoints {
		assert.Contains(t, podScrape.Spec.PodMetricsEndpoints[i].RelabelConfigs, victoriaMetricsAppRelabelConfig())
	}
}

func TestVMServiceScrapeObjectsAddVictoriaMetricsAppLabel(t *testing.T) {
	objectMeta := metav1.ObjectMeta{Name: "test", Namespace: "default"}

	f := func(name string, builder scrapeBuilder) {
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
	f("VMSingle", &vmv1beta1.VMSingle{ObjectMeta: objectMeta})
	f("VMAlert", &vmv1beta1.VMAlert{ObjectMeta: objectMeta})
	f("VMAuth", &vmv1beta1.VMAuth{ObjectMeta: objectMeta})
	f("VMSelect", &vmv1beta1.VMSelect{})
	f("VMInsert", &vmv1beta1.VMInsert{})
	f("VMStorage", &vmv1beta1.VMStorage{})
	f("VLSingle", &vmv1.VLSingle{ObjectMeta: objectMeta})
	f("VLSelect", &vmv1.VLSelect{})
	f("VLInsert", &vmv1.VLInsert{})
	f("VLStorage", &vmv1.VLStorage{})
	f("VTSingle", &vmv1.VTSingle{ObjectMeta: objectMeta})
	f("VTSelect", &vmv1.VTSelect{})
	f("VTInsert", &vmv1.VTInsert{})
	f("VTStorage", &vmv1.VTStorage{})
}

func TestVMPodScrapeObjectsAddVictoriaMetricsAppLabel(t *testing.T) {
	objectMeta := metav1.ObjectMeta{Name: "test", Namespace: "default"}

	f := func(builder podScrapeBuilder, port string) {
		scrape := VMPodScrape(builder, port)

		assert.Len(t, scrape.Spec.PodMetricsEndpoints, 1)
		assert.Contains(t, scrape.Spec.PodMetricsEndpoints[0].RelabelConfigs, victoriaMetricsAppRelabelConfig())
	}
	f(&vmv1beta1.VMAgent{ObjectMeta: objectMeta}, "http")
	f(&vmv1.VLAgent{ObjectMeta: objectMeta}, "http")
	f(&vmv1.VMAnomaly{ObjectMeta: objectMeta}, "monitoring-http")
}
