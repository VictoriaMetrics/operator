package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type testVMServiceScrapeForServiceWithSpecArgs struct {
	serviceScrapeSpecTemplate *vmv1beta1.VMServiceScrapeSpec
	metricPath                string
	extraArgs                 map[string]string
}

func (tb *testVMServiceScrapeForServiceWithSpecArgs) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return tb.serviceScrapeSpecTemplate
}

func (tb *testVMServiceScrapeForServiceWithSpecArgs) GetMetricPath() string {
	return tb.metricPath
}

func (tb *testVMServiceScrapeForServiceWithSpecArgs) GetExtraArgs() map[string]string {
	return tb.extraArgs
}

func TestVMServiceScrapeForServiceWithSpec(t *testing.T) {
	type opts struct {
		spec                  testVMServiceScrapeForServiceWithSpecArgs
		service               *corev1.Service
		filterPortNames       []string
		wantServiceScrapeSpec vmv1beta1.VMServiceScrapeSpec
	}

	f := func(o opts) {
		t.Helper()
		gotServiceScrape := VMServiceScrapeForServiceWithSpec(o.service, &o.spec, o.filterPortNames...)
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
		spec: testVMServiceScrapeForServiceWithSpecArgs{
			metricPath: "/metrics",
			serviceScrapeSpecTemplate: &vmv1beta1.VMServiceScrapeSpec{
				Selector: metav1.LabelSelector{MatchLabels: map[string]string{"my-label": "value"}},
			},
		},
		wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{{
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
		spec: testVMServiceScrapeForServiceWithSpecArgs{
			metricPath: "/metrics",
		},
		wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{{
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
		spec: testVMServiceScrapeForServiceWithSpecArgs{
			metricPath: "/metrics",
		},
		wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{
				{
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
						}},
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
		spec: testVMServiceScrapeForServiceWithSpecArgs{
			metricPath: "/metrics",
			serviceScrapeSpecTemplate: &vmv1beta1.VMServiceScrapeSpec{
				TargetLabels: []string{"key"},
			},
		},
		wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{{
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
		spec: testVMServiceScrapeForServiceWithSpecArgs{
			metricPath: "/metrics",
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
		spec: testVMServiceScrapeForServiceWithSpecArgs{
			metricPath: "/metrics",
			extraArgs: map[string]string{
				"tls":            "true",
				"metricsAuthKey": "some-access-key",
			},
		},
		wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
			Endpoints: []vmv1beta1.Endpoint{{
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					Path:   "/metrics",
					Params: map[string][]string{"authKey": {"some-access-key"}},
					Scheme: "https",
				},
				EndpointAuth: vmv1beta1.EndpointAuth{
					TLSConfig: &vmv1beta1.TLSConfig{InsecureSkipVerify: true},
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
