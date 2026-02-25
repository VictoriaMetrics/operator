package vmscrapes

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_generatePodScrapeConfig(t *testing.T) {
	type opts struct {
		cr   *vmv1beta1.VMAgent
		sc   *vmv1beta1.VMPodScrape
		ep   vmv1beta1.PodMetricsEndpoint
		want string
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(nil)
		ac := getAssetsCache(ctx, fclient)
		pos := &ParsedObjects{Namespace: o.cr.Namespace}
		sp := &o.cr.Spec.CommonScrapeParams
		got, err := generatePodScrapeConfig(ctx, sp, pos, o.sc, o.ep, 0, ac)
		if assert.NoError(t, err, "cannot generate PodScrapeConfig") {
			gotBytes, err := yaml.Marshal(got)
			if assert.NoError(t, err, "cannot marshal PodScrapeConfig to yaml") {
				assert.Equal(t, o.want, string(gotBytes))
			}
		}
	}

	// simple test
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
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
	})

	// disabled running filter
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
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
	})

	// test with selector
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					EnableKubernetesAPISelectors: true,
				},
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
	})

	// with portNumber
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					EnableKubernetesAPISelectors: true,
				},
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
	})
}
