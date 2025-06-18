package vmagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func Test_generatePodScrapeConfig(t *testing.T) {
	type args struct {
		cr              vmv1beta1.VMAgent
		sc              *vmv1beta1.VMPodScrape
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
				sc: &vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-1",
						Namespace: "default",
					},
				},
				ep: vmv1beta1.PodMetricsEndpoint{
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/metric",
					},
					Port: ptr.To("web"),
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
				sc: &vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-1",
						Namespace: "default",
					},
				},
				ep: vmv1beta1.PodMetricsEndpoint{
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/metric",
					},
					Port: ptr.To("web"),
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
				cr: vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						EnableKubernetesAPISelectors: true,
					},
				},
				sc: &vmv1beta1.VMPodScrape{
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
					Port: ptr.To("web"),
				},
				ssCache: &scrapesSecretsCache{},
			},
			want: `job_name: podScrape/default/test-1/0
kubernetes_sd_configs:
- role: pod
  selectors:
  - role: pod
    label: label-1=value-1,label-2=value-2,label-3=value-3,some-label
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
			name: "with portNumber",
			args: args{
				cr: vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						EnableKubernetesAPISelectors: true,
					},
				},
				sc: &vmv1beta1.VMPodScrape{
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
							},
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "some-label",
									Operator: metav1.LabelSelectorOpExists,
								},
								{
									Key:      "some-other",
									Operator: metav1.LabelSelectorOpDoesNotExist,
								},
								{
									Key:      "bad-labe",
									Operator: metav1.LabelSelectorOpNotIn,
									Values:   []string{"one", "two"},
								},
							},
						},
					},
				},
				ep: vmv1beta1.PodMetricsEndpoint{
					EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
						Path: "/metric",
					},
					PortNumber: ptr.To[int32](8081),
				},
				ssCache: &scrapesSecretsCache{},
			},
			want: `job_name: podScrape/default/test-1/0
kubernetes_sd_configs:
- role: pod
  selectors:
  - role: pod
    label: bad-labe notin (one,two),label-1=value-1,some-label,!some-other
honor_labels: false
metrics_path: /metric
relabel_configs:
- action: drop
  source_labels:
  - __meta_kubernetes_pod_phase
  regex: (Failed|Succeeded)
- action: keep
  source_labels:
  - __meta_kubernetes_pod_container_port_number
  regex: 8081
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
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generatePodScrapeConfig(context.Background(), &tt.args.cr, tt.args.sc, tt.args.ep, tt.args.i, tt.args.apiserverConfig, tt.args.ssCache, tt.args.se)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal PodScrapeConfig to yaml,err :%e", err)
				return
			}
			assert.Equal(t, tt.want, string(gotBytes))
		})
	}
}
