package build

import (
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVMServiceScrapeForServiceWithSpec(t *testing.T) {
	type args struct {
		service                   *v1.Service
		serviceScrapeSpecTemplate *victoriametricsv1beta1.VMServiceScrapeSpec
		metricPath                string
		filterPortNames           []string
	}
	tests := []struct {
		name                  string
		args                  args
		wantServiceScrapeSpec victoriametricsv1beta1.VMServiceScrapeSpec
		wantErr               bool
	}{
		{
			name: "custom selector",
			args: args{
				metricPath:      "/metrics",
				filterPortNames: []string{"http"},
				service: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "vmagent-svc",
						Labels: map[string]string{"my-label": "value"},
					},
					Spec: v1.ServiceSpec{
						Ports: []v1.ServicePort{
							{
								Name: "http",
							},
						},
					},
				},
				serviceScrapeSpecTemplate: &victoriametricsv1beta1.VMServiceScrapeSpec{
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{"my-label": "value"}},
				},
			},
			wantServiceScrapeSpec: victoriametricsv1beta1.VMServiceScrapeSpec{
				Endpoints: []victoriametricsv1beta1.Endpoint{
					{
						Path: "/metrics",
						Port: "http",
					},
				},
				Selector: metav1.LabelSelector{MatchLabels: map[string]string{"my-label": "value"}},
			},
		},
		{
			name: "multiple ports with filter",
			args: args{
				metricPath:      "/metrics",
				filterPortNames: []string{"http"},
				service: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vmagent-svc",
					},
					Spec: v1.ServiceSpec{
						Ports: []v1.ServicePort{
							{
								Name: "http",
							},
							{
								Name: "opentsdb-http",
							},
						},
					},
				},
			},
			wantServiceScrapeSpec: victoriametricsv1beta1.VMServiceScrapeSpec{
				Endpoints: []victoriametricsv1beta1.Endpoint{
					{
						Path: "/metrics",
						Port: "http",
					},
				},
				Selector: metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: victoriametricsv1beta1.AdditionalServiceLabel, Operator: metav1.LabelSelectorOpDoesNotExist}}},
			},
		},
		{
			name: "with extra metric labels",
			args: args{
				metricPath:      "/metrics",
				filterPortNames: []string{"http"},
				service: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vmagent-svc",
						Labels: map[string]string{
							"key": "value",
						},
					},
					Spec: v1.ServiceSpec{
						Ports: []v1.ServicePort{
							{
								Name: "http",
							},
						},
					},
				},
				serviceScrapeSpecTemplate: &victoriametricsv1beta1.VMServiceScrapeSpec{
					TargetLabels: []string{"key"},
				},
			},
			wantServiceScrapeSpec: victoriametricsv1beta1.VMServiceScrapeSpec{
				Endpoints: []victoriametricsv1beta1.Endpoint{
					{
						Path: "/metrics",
						Port: "http",
					},
				},
				TargetLabels: []string{"key"},
				Selector:     metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: victoriametricsv1beta1.AdditionalServiceLabel, Operator: metav1.LabelSelectorOpDoesNotExist}}},
			},
		},
		{
			name: "with extra endpoints",
			args: args{
				metricPath:      "/metrics",
				filterPortNames: []string{"http"},
				service: &v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vmagent-svc",
						Labels: map[string]string{
							"key": "value",
						},
					},
					Spec: v1.ServiceSpec{
						Ports: []v1.ServicePort{
							{
								Name: "http",
							},
						},
					},
				},
				serviceScrapeSpecTemplate: &victoriametricsv1beta1.VMServiceScrapeSpec{
					TargetLabels: []string{"key"},
					Endpoints: []victoriametricsv1beta1.Endpoint{
						{
							Path: "/metrics",
							Port: "sidecar",
						},
						{
							Path:           "/metrics",
							Port:           "http",
							ScrapeInterval: "30s",
							ScrapeTimeout:  "10s",
						},
					},
				},
			},
			wantServiceScrapeSpec: victoriametricsv1beta1.VMServiceScrapeSpec{
				Endpoints: []victoriametricsv1beta1.Endpoint{
					{
						Path: "/metrics",
						Port: "sidecar",
					},
					{
						Path:           "/metrics",
						Port:           "http",
						ScrapeInterval: "30s",
						ScrapeTimeout:  "10s",
					},
				},
				TargetLabels: []string{"key"},
				Selector:     metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: victoriametricsv1beta1.AdditionalServiceLabel, Operator: metav1.LabelSelectorOpDoesNotExist}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotServiceScrape := VMServiceScrapeForServiceWithSpec(tt.args.service, tt.args.serviceScrapeSpecTemplate, tt.args.metricPath, tt.args.filterPortNames...)
			assert.Equal(t, tt.wantServiceScrapeSpec, gotServiceScrape.Spec)
		})
	}
}
