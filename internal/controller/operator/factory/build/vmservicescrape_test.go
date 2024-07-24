package build

import (
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVMServiceScrapeForServiceWithSpec(t *testing.T) {
	type args struct {
		service                   *corev1.Service
		serviceScrapeSpecTemplate *vmv1beta1.VMServiceScrapeSpec
		metricPath                string
		filterPortNames           []string
	}
	tests := []struct {
		name                  string
		args                  args
		wantServiceScrapeSpec vmv1beta1.VMServiceScrapeSpec
		wantErr               bool
	}{
		{
			name: "custom selector",
			args: args{
				metricPath:      "/metrics",
				filterPortNames: []string{"http"},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "vmagent-svc",
						Labels: map[string]string{"my-label": "value"},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name: "http",
							},
						},
					},
				},
				serviceScrapeSpecTemplate: &vmv1beta1.VMServiceScrapeSpec{
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{"my-label": "value"}},
				},
			},
			wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
				Endpoints: []vmv1beta1.Endpoint{
					{
						EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
							Path: "/metrics",
						},
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
			},
			wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
				Endpoints: []vmv1beta1.Endpoint{
					{
						EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
							Path: "/metrics",
						},
						Port: "http",
					},
				},
				Selector: metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: vmv1beta1.AdditionalServiceLabel, Operator: metav1.LabelSelectorOpDoesNotExist}}},
			},
		},
		{
			name: "with extra metric labels",
			args: args{
				metricPath:      "/metrics",
				filterPortNames: []string{"http"},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vmagent-svc",
						Labels: map[string]string{
							"key": "value",
						},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name: "http",
							},
						},
					},
				},
				serviceScrapeSpecTemplate: &vmv1beta1.VMServiceScrapeSpec{
					TargetLabels: []string{"key"},
				},
			},
			wantServiceScrapeSpec: vmv1beta1.VMServiceScrapeSpec{
				Endpoints: []vmv1beta1.Endpoint{
					{
						EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
							Path: "/metrics",
						},
						Port: "http",
					},
				},
				TargetLabels: []string{"key"},
				Selector:     metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: vmv1beta1.AdditionalServiceLabel, Operator: metav1.LabelSelectorOpDoesNotExist}}},
			},
		},
		{
			name: "with extra endpoints",
			args: args{
				metricPath:      "/metrics",
				filterPortNames: []string{"http"},
				service: &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vmagent-svc",
						Labels: map[string]string{
							"key": "value",
						},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name: "http",
							},
						},
					},
				},
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
				Selector:     metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: vmv1beta1.AdditionalServiceLabel, Operator: metav1.LabelSelectorOpDoesNotExist}}},
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
