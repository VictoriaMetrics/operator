package scrape

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_selectPodScrapes(t *testing.T) {
	tests := []struct {
		name              string
		cr                scraping
		want              []string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "selector pod scrape at vmagent ns",
			cr: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-vmagent",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAgentSpec{
					CommonScrapeParams: vmv1beta1.CommonScrapeParams{
						PodScrapeSelector: &metav1.LabelSelector{},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{},
				},
			},
			wantErr: false,
			want:    []string{"default/pod1"},
		},
		{
			name: "selector pod scrape at different ns with ns selector",
			cr: &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-vmagent",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAgentSpec{
					CommonScrapeParams: vmv1beta1.CommonScrapeParams{
						PodScrapeNamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"name": "monitoring"},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitor", Labels: map[string]string{"name": "monitoring"}}},
				&vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "monitor",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{},
				},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&vmv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMPodScrapeSpec{},
				},
			},
			wantErr: false,
			want:    []string{"monitor/pod2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := selectPodScrapes(context.TODO(), fclient, tt.cr)
			if (err != nil) != tt.wantErr {
				t.Errorf("SelectPodScrapes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			var gotNames []string

			for _, k := range got {
				gotNames = append(gotNames, fmt.Sprintf("%s/%s", k.Namespace, k.Name))
			}
			sort.Strings(gotNames)
			if !reflect.DeepEqual(gotNames, tt.want) {
				t.Errorf("SelectPodScrapes() got = %v, want %v", gotNames, tt.want)
			}
		})
	}
}

func Test_generatePodScrapeConfig(t *testing.T) {
	tests := []struct {
		name              string
		cr                scraping
		sc                *vmv1beta1.VMPodScrape
		ep                vmv1beta1.PodMetricsEndpoint
		i                 int
		predefinedObjects []runtime.Object
		want              string
	}{
		{
			name: "simple test",
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
		},
		{
			name: "disabled running filter",
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
		},
		{
			name: "test with selector",
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
		},
		{
			name: "with portNumber",
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			ac := getAssetsCache(ctx, fclient)
			got, err := generatePodScrapeConfig(ctx, tt.sc, tt.ep, tt.i, tt.cr, ac)
			if err != nil {
				t.Errorf("cannot generate PodScrapeConfig, err: %e", err)
				return
			}
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Errorf("cannot marshal PodScrapeConfig to yaml,err :%e", err)
				return
			}
			assert.Equal(t, tt.want, string(gotBytes))
		})
	}
}
