package vmagent

import (
	"context"
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func Test_generatePodScrapeConfig(t *testing.T) {
	type args struct {
		cr              vmv1beta1.VMAgent
		m               *vmv1beta1.VMPodScrape
		ep              vmv1beta1.PodMetricsEndpoint
		i               int
		apiserverConfig *vmv1beta1.APIServerConfig
		ssCache         *scrapesSecretsCache
		se              vmv1beta1.VMAgentSecurityEnforcements
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "simple test",
			args: args{
				m: &vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-1",
						Namespace: "default",
					},
				},
				ep: vmv1beta1.PodMetricsEndpoint{
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/metric",
					},
					Port: "web",
					AttachMetadata: vmv1beta1.AttachMetadata{
						Node: ptr.To(true),
					},
				},
				ssCache: &scrapesSecretsCache{},
			},
			want: `job_name: podScrape/default/test-1/0
kubernetes_sd_configs:
- role: pod
  attach_metadata:
    node: true
  namespaces:
    names:
    - default
honor_labels: false
metrics_path: /metric
relabel_configs:
- action: drop
  source_labels:
  - __meta_kubernetes_pod_phase
  regex: (Failed|Succeeded)
- action: keep
  source_labels:
  - __meta_kubernetes_pod_container_port_name
  regex: web
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_pod_container_name
  target_label: container
- source_labels:
  - __meta_kubernetes_pod_name
  target_label: pod
- target_label: job
  replacement: default/test-1
- target_label: endpoint
  replacement: web
`,
		},
		{
			name: "disabled running filter",
			args: args{
				m: &vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-1",
						Namespace: "default",
					},
				},
				ep: vmv1beta1.PodMetricsEndpoint{
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/metric",
					},
					Port: "web",
					AttachMetadata: vmv1beta1.AttachMetadata{
						Node: ptr.To(true),
					},
					FilterRunning: ptr.To(false),
				},
				ssCache: &scrapesSecretsCache{},
			},
			want: `job_name: podScrape/default/test-1/0
kubernetes_sd_configs:
- role: pod
  attach_metadata:
    node: true
  namespaces:
    names:
    - default
honor_labels: false
metrics_path: /metric
relabel_configs:
- action: keep
  source_labels:
  - __meta_kubernetes_pod_container_port_name
  regex: web
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_pod_container_name
  target_label: container
- source_labels:
  - __meta_kubernetes_pod_name
  target_label: pod
- target_label: job
  replacement: default/test-1
- target_label: endpoint
  replacement: web
`,
		},
		{
			name: "test with selector",
			args: args{
				m: &vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{
						NamespaceSelector: vmv1beta1.NamespaceSelector{
							Any: true,
						},
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"label-1": "value-1",
								"label-2": "value-2",
								"label-3": "value-3",
							},
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "some-label",
									Operator: metav1.LabelSelectorOpExists,
								},
							},
						},
					},
				},
				ep: vmv1beta1.PodMetricsEndpoint{
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/metric",
					},
					Port: "web",
				},
				ssCache: &scrapesSecretsCache{},
			},
			want: `job_name: podScrape/default/test-1/0
kubernetes_sd_configs:
- role: pod
  selectors:
  - role: pod
    label: label-1=value-1,label-2=value-2,label-3=value-3
honor_labels: false
metrics_path: /metric
relabel_configs:
- action: drop
  source_labels:
  - __meta_kubernetes_pod_phase
  regex: (Failed|Succeeded)
- action: keep
  source_labels:
  - __meta_kubernetes_pod_label_label_1
  regex: value-1
- action: keep
  source_labels:
  - __meta_kubernetes_pod_label_label_2
  regex: value-2
- action: keep
  source_labels:
  - __meta_kubernetes_pod_label_label_3
  regex: value-3
- action: keep
  source_labels:
  - __meta_kubernetes_pod_labelpresent_some_label
  regex: "true"
- action: keep
  source_labels:
  - __meta_kubernetes_pod_container_port_name
  regex: web
- source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- source_labels:
  - __meta_kubernetes_pod_container_name
  target_label: container
- source_labels:
  - __meta_kubernetes_pod_name
  target_label: pod
- target_label: job
  replacement: default/test-1
- target_label: endpoint
  replacement: web
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generatePodScrapeConfig(context.Background(), &tt.args.cr, tt.args.m, tt.args.ep, tt.args.i, tt.args.apiserverConfig, tt.args.ssCache, tt.args.se)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal PodScrapeConfig to yaml,err :%e", err)
				return
			}
			assert.Equal(t, tt.want, string(gotBytes))
		})
	}
}
