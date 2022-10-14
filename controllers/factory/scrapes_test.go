package factory

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"reflect"
	"sort"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestSelectServiceMonitors(t *testing.T) {
	type args struct {
		p *victoriametricsv1beta1.VMAgent
		l logr.Logger
	}
	tests := []struct {
		name              string
		args              args
		want              []string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "select service scrape inside vmagent namespace",
			args: args{
				p: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						ServiceScrapeSelector: &metav1.LabelSelector{},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "default-monitor"},
					Spec:       victoriametricsv1beta1.VMServiceScrapeSpec{},
				},
			},
			want:    []string{"default/default-monitor"},
			wantErr: false,
		},
		{
			name: "select service scrape from namespace with filter",
			args: args{
				p: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-agent",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						ServiceScrapeNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "stage"}},
						ServiceScrapeSelector:          &metav1.LabelSelector{},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "default-monitor"},
					Spec:       victoriametricsv1beta1.VMServiceScrapeSpec{},
				},
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "stg", Labels: map[string]string{"name": "stage"}}},
				&victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Namespace: "stg", Name: "default-monitor"},
					Spec:       victoriametricsv1beta1.VMServiceScrapeSpec{},
				},
			},
			want:    []string{"stg/default-monitor"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := SelectServiceScrapes(context.TODO(), tt.args.p, fclient)
			if (err != nil) != tt.wantErr {
				t.Errorf("SelectServiceScrapes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			gotNames := []string{}
			for monitorName := range got {
				gotNames = append(gotNames, monitorName)
			}
			sort.Strings(gotNames)
			if !reflect.DeepEqual(gotNames, tt.want) {
				t.Errorf("SelectServiceScrapes() got = %v, want %v", gotNames, tt.want)
			}
		})
	}
}

func TestSelectPodMonitors(t *testing.T) {
	type args struct {
		p *victoriametricsv1beta1.VMAgent
		l logr.Logger
	}
	tests := []struct {
		name              string
		args              args
		want              []string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "selector pod scrape at vmagent ns",
			args: args{
				p: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-vmagent",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						PodScrapeSelector: &metav1.LabelSelector{},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMPodScrapeSpec{},
				},
			},
			wantErr: false,
			want:    []string{"default/pod1"},
		},
		{
			name: "selector pod scrape at different ns with ns selector",
			args: args{
				p: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-vmagent",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						PodScrapeNamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"name": "monitoring"},
						},
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitor", Labels: map[string]string{"name": "monitoring"}}},
				&victoriametricsv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "monitor",
					},
					Spec: victoriametricsv1beta1.VMPodScrapeSpec{},
				},
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&victoriametricsv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMPodScrapeSpec{},
				},
			},
			wantErr: false,
			want:    []string{"monitor/pod2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := SelectPodScrapes(context.TODO(), tt.args.p, fclient)
			if (err != nil) != tt.wantErr {
				t.Errorf("SelectPodScrapes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			gotNames := []string{}
			for podName := range got {
				gotNames = append(gotNames, podName)
			}
			sort.Strings(gotNames)
			if !reflect.DeepEqual(gotNames, tt.want) {
				t.Errorf("SelectPodScrapes() got = %v, want %v", gotNames, tt.want)
			}
		})
	}
}

func Test_getCredFromConfigMap(t *testing.T) {
	type args struct {
		ns       string
		sel      v1.ConfigMapKeySelector
		cacheKey string
		cache    map[string]*v1.ConfigMap
	}
	tests := []struct {
		name              string
		args              args
		want              string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "extract key from cm",
			args: args{
				ns:    "default",
				sel:   v1.ConfigMapKeySelector{Key: "tls-conf", LocalObjectReference: v1.LocalObjectReference{Name: "tls-cm"}},
				cache: map[string]*v1.ConfigMap{},
			},
			predefinedObjects: []runtime.Object{
				&v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "tls-cm", Namespace: "default"},
					Data:       map[string]string{"tls-conf": "secret-data"},
				},
			},
			want:    "secret-data",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			got, err := getCredFromConfigMap(context.TODO(), fclient, tt.args.ns, tt.args.sel, tt.args.cacheKey, tt.args.cache)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCredFromConfigMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getCredFromConfigMap() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getCredFromSecret(t *testing.T) {
	type args struct {
		ns       string
		sel      v1.SecretKeySelector
		cacheKey string
		cache    map[string]*v1.Secret
	}
	tests := []struct {
		name              string
		args              args
		want              string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "extract tls key data from secret",
			args: args{
				ns: "default",
				sel: v1.SecretKeySelector{LocalObjectReference: v1.LocalObjectReference{
					Name: "tls-secret"},
					Key: "key.pem"},
				cacheKey: "tls-secret",
				cache:    map[string]*v1.Secret{},
			},
			want:    "tls-key-data",
			wantErr: false,
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{"ca.crt": []byte(`ca-data`), "key.pem": []byte(`tls-key-data`)},
				},
			},
		},
		{
			name: "fail extract missing tls cert data from secret",
			args: args{
				ns: "default",
				sel: v1.SecretKeySelector{LocalObjectReference: v1.LocalObjectReference{
					Name: "tls-secret"},
					Key: "cert.pem"},
				cacheKey: "tls-secret",
				cache:    map[string]*v1.Secret{},
			},
			want:    "",
			wantErr: true,
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{"ca.crt": []byte(`ca-data`), "key.pem": []byte(`tls-key-data`)},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			got, err := getCredFromSecret(context.TODO(), fclient, tt.args.ns, &tt.args.sel, tt.args.cacheKey, tt.args.cache)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCredFromSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getCredFromSecret() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectVMProbes(t *testing.T) {
	type args struct {
		cr *victoriametricsv1beta1.VMAgent
	}
	tests := []struct {
		name              string
		args              args
		want              []string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "select vmProbe with static conf",
			args: args{
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-vmagent",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						ProbeSelector: &metav1.LabelSelector{},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMProbe{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "static-probe",
					},
					Spec: victoriametricsv1beta1.VMProbeSpec{Targets: victoriametricsv1beta1.VMProbeTargets{StaticConfig: &victoriametricsv1beta1.VMProbeTargetStaticConfig{Targets: []string{"host-1"}}}},
				},
			},
			want: []string{"default/static-probe"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := SelectVMProbes(context.TODO(), tt.args.cr, fclient)
			if (err != nil) != tt.wantErr {
				t.Errorf("SelectVMProbes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			var result []string
			for k := range got {
				result = append(result, k)
			}
			sort.Strings(result)
			if !reflect.DeepEqual(result, tt.want) {
				t.Errorf("SelectVMProbes() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateOrUpdateConfigurationSecret(t *testing.T) {
	type args struct {
		cr *victoriametricsv1beta1.VMAgent
		c  *config.BaseOperatorConf
	}
	tests := []struct {
		name              string
		args              args
		predefinedObjects []runtime.Object
		wantConfig        string
		wantErr           bool
	}{
		{
			name: "complete test",
			args: args{
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						ServiceScrapeNamespaceSelector: &metav1.LabelSelector{},
						ServiceScrapeSelector:          &metav1.LabelSelector{},
						PodScrapeSelector:              &metav1.LabelSelector{},
						PodScrapeNamespaceSelector:     &metav1.LabelSelector{},
						NodeScrapeNamespaceSelector:    &metav1.LabelSelector{},
						NodeScrapeSelector:             &metav1.LabelSelector{},
						StaticScrapeNamespaceSelector:  &metav1.LabelSelector{},
						StaticScrapeSelector:           &metav1.LabelSelector{},
						ProbeNamespaceSelector:         &metav1.LabelSelector{},
						ProbeSelector:                  &metav1.LabelSelector{},
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				&v1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "default",
					},
				},
				&v1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kube-system",
					},
				},
				&victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vms",
					},
					Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
						Selector:          metav1.LabelSelector{},
						JobLabel:          "app",
						NamespaceSelector: victoriametricsv1beta1.NamespaceSelector{},
						Endpoints: []victoriametricsv1beta1.Endpoint{
							{
								Path: "/metrics",
								Port: "8085",
								BearerTokenSecret: &v1.SecretKeySelector{
									Key: "bearer",
									LocalObjectReference: v1.LocalObjectReference{
										Name: "access-creds",
									},
								},
							},
							{
								Path: "/metrics-2",
								Port: "8083",
							},
						},
					},
				},
				&victoriametricsv1beta1.VMProbe{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "kube-system",
						Name:      "test-vmp",
					},
					Spec: victoriametricsv1beta1.VMProbeSpec{
						Targets: victoriametricsv1beta1.VMProbeTargets{
							StaticConfig: &victoriametricsv1beta1.VMProbeTargetStaticConfig{
								Targets: []string{"localhost:8428"},
							},
						},
						VMProberSpec: victoriametricsv1beta1.VMProberSpec{URL: "http://blackbox"},
					},
				},
				&victoriametricsv1beta1.VMPodScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vps",
					},
					Spec: victoriametricsv1beta1.VMPodScrapeSpec{
						JobLabel:          "app",
						NamespaceSelector: victoriametricsv1beta1.NamespaceSelector{},
						Selector: metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"prod"},
								},
							},
						},
						SampleLimit: 10,
						PodMetricsEndpoints: []victoriametricsv1beta1.PodMetricsEndpoint{
							{
								Path: "/metrics-3",
								Port: "805",
								VMScrapeParams: &victoriametricsv1beta1.VMScrapeParams{
									StreamParse: pointer.Bool(true),
									ProxyClientConfig: &victoriametricsv1beta1.ProxyAuth{
										TLSConfig: &victoriametricsv1beta1.TLSConfig{
											InsecureSkipVerify: true,
											KeySecret: &v1.SecretKeySelector{
												Key: "key",
												LocalObjectReference: v1.LocalObjectReference{
													Name: "access-creds",
												},
											},
											Cert: victoriametricsv1beta1.SecretOrConfigMap{Secret: &v1.SecretKeySelector{
												Key: "cert",
												LocalObjectReference: v1.LocalObjectReference{
													Name: "access-creds",
												},
											}},
											CA: victoriametricsv1beta1.SecretOrConfigMap{Secret: &v1.SecretKeySelector{
												Key: "ca",
												LocalObjectReference: v1.LocalObjectReference{
													Name: "access-creds",
												},
											},
											},
										},
									},
								},
							},
							{
								Port: "801",
								Path: "/metrics-5",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									InsecureSkipVerify: true,
									KeySecret: &v1.SecretKeySelector{
										Key: "key",
										LocalObjectReference: v1.LocalObjectReference{
											Name: "access-creds",
										},
									},
									Cert: victoriametricsv1beta1.SecretOrConfigMap{Secret: &v1.SecretKeySelector{
										Key: "cert",
										LocalObjectReference: v1.LocalObjectReference{
											Name: "access-creds",
										},
									}},
									CA: victoriametricsv1beta1.SecretOrConfigMap{Secret: &v1.SecretKeySelector{
										Key: "ca",
										LocalObjectReference: v1.LocalObjectReference{
											Name: "access-creds",
										},
									},
									},
								},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMNodeScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vms",
					},
					Spec: victoriametricsv1beta1.VMNodeScrapeSpec{
						BasicAuth: &victoriametricsv1beta1.BasicAuth{
							Username: v1.SecretKeySelector{
								Key: "username",
								LocalObjectReference: v1.LocalObjectReference{
									Name: "access-creds",
								},
							},
							Password: v1.SecretKeySelector{
								Key: "password",
								LocalObjectReference: v1.LocalObjectReference{
									Name: "access-creds",
								},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMStaticScrape{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "test-vmstatic",
					},
					Spec: victoriametricsv1beta1.VMStaticScrapeSpec{
						TargetEndpoints: []*victoriametricsv1beta1.TargetEndpoint{
							{
								Path:     "/metrics-3",
								Port:     "3031",
								Scheme:   "https",
								ProxyURL: pointer.String("https://some-proxy-1"),
								OAuth2: &victoriametricsv1beta1.OAuth2{
									TokenURL: "https://some-tr",
									ClientSecret: &v1.SecretKeySelector{
										Key: "cs",
										LocalObjectReference: v1.LocalObjectReference{
											Name: "access-creds",
										},
									},
									ClientID: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &v1.SecretKeySelector{
											Key: "cid",
											LocalObjectReference: v1.LocalObjectReference{
												Name: "access-creds",
											},
										},
									},
								},
							},
						},
					},
				},
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "access-creds",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"cid":      []byte(`some-client-id`),
						"cs":       []byte(`some-client-secret`),
						"username": []byte(`some-username`),
						"password": []byte(`some-password`),
						"ca":       []byte(`some-ca-cert`),
						"cert":     []byte(`some-cert`),
						"key":      []byte(`some-key`),
						"bearer":   []byte(`some-bearer`),
					},
				},
			},
			wantConfig: `global:
  scrape_interval: 30s
  external_labels:
    prometheus: default/test
scrape_configs:
- job_name: serviceScrape/default/test-vms/0
  honor_labels: false
  kubernetes_sd_configs:
  - role: endpoints
    namespaces:
      names:
      - default
  metrics_path: /metrics
  bearer_token: some-bearer
  relabel_configs:
  - action: keep
    source_labels:
    - __meta_kubernetes_endpoint_port_name
    regex: "8085"
  - source_labels:
    - __meta_kubernetes_endpoint_address_target_kind
    - __meta_kubernetes_endpoint_address_target_name
    separator: ;
    regex: Node;(.*)
    replacement: ${1}
    target_label: node
  - source_labels:
    - __meta_kubernetes_endpoint_address_target_kind
    - __meta_kubernetes_endpoint_address_target_name
    separator: ;
    regex: Pod;(.*)
    replacement: ${1}
    target_label: pod
  - source_labels:
    - __meta_kubernetes_pod_name
    target_label: pod
  - source_labels:
    - __meta_kubernetes_pod_container_name
    target_label: container
  - source_labels:
    - __meta_kubernetes_namespace
    target_label: namespace
  - source_labels:
    - __meta_kubernetes_service_name
    target_label: service
  - source_labels:
    - __meta_kubernetes_service_name
    target_label: job
    replacement: ${1}
  - source_labels:
    - __meta_kubernetes_service_label_app
    target_label: job
    regex: (.+)
    replacement: ${1}
  - target_label: endpoint
    replacement: "8085"
- job_name: serviceScrape/default/test-vms/1
  honor_labels: false
  kubernetes_sd_configs:
  - role: endpoints
    namespaces:
      names:
      - default
  metrics_path: /metrics-2
  relabel_configs:
  - action: keep
    source_labels:
    - __meta_kubernetes_endpoint_port_name
    regex: "8083"
  - source_labels:
    - __meta_kubernetes_endpoint_address_target_kind
    - __meta_kubernetes_endpoint_address_target_name
    separator: ;
    regex: Node;(.*)
    replacement: ${1}
    target_label: node
  - source_labels:
    - __meta_kubernetes_endpoint_address_target_kind
    - __meta_kubernetes_endpoint_address_target_name
    separator: ;
    regex: Pod;(.*)
    replacement: ${1}
    target_label: pod
  - source_labels:
    - __meta_kubernetes_pod_name
    target_label: pod
  - source_labels:
    - __meta_kubernetes_pod_container_name
    target_label: container
  - source_labels:
    - __meta_kubernetes_namespace
    target_label: namespace
  - source_labels:
    - __meta_kubernetes_service_name
    target_label: service
  - source_labels:
    - __meta_kubernetes_service_name
    target_label: job
    replacement: ${1}
  - source_labels:
    - __meta_kubernetes_service_label_app
    target_label: job
    regex: (.+)
    replacement: ${1}
  - target_label: endpoint
    replacement: "8083"
- job_name: podScrape/default/test-vps/0
  honor_labels: false
  kubernetes_sd_configs:
  - role: pod
    namespaces:
      names:
      - default
  metrics_path: /metrics-3
  relabel_configs:
  - action: keep
    source_labels:
    - __meta_kubernetes_pod_label_app
    regex: prod
  - action: keep
    source_labels:
    - __meta_kubernetes_pod_container_port_name
    regex: "805"
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
    replacement: default/test-vps
  - source_labels:
    - __meta_kubernetes_pod_label_app
    target_label: job
    regex: (.+)
    replacement: ${1}
  - target_label: endpoint
    replacement: "805"
  sample_limit: 10
  stream_parse: true
  proxy_tls_config:
    insecure_skip_verify: true
    ca_file: /etc/vmagent-tls/certs/default_access-creds_ca
    cert_file: /etc/vmagent-tls/certs/default_access-creds_cert
    key_file: /etc/vmagent-tls/certs/default_access-creds_key
- job_name: podScrape/default/test-vps/1
  honor_labels: false
  kubernetes_sd_configs:
  - role: pod
    namespaces:
      names:
      - default
  metrics_path: /metrics-5
  tls_config:
    insecure_skip_verify: true
    ca_file: /etc/vmagent-tls/certs/default_access-creds_ca
    cert_file: /etc/vmagent-tls/certs/default_access-creds_cert
    key_file: /etc/vmagent-tls/certs/default_access-creds_key
  relabel_configs:
  - action: keep
    source_labels:
    - __meta_kubernetes_pod_label_app
    regex: prod
  - action: keep
    source_labels:
    - __meta_kubernetes_pod_container_port_name
    regex: "801"
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
    replacement: default/test-vps
  - source_labels:
    - __meta_kubernetes_pod_label_app
    target_label: job
    regex: (.+)
    replacement: ${1}
  - target_label: endpoint
    replacement: "801"
  sample_limit: 10
- job_name: probe/kube-system/test-vmp/0
  params:
    module:
    - ""
  metrics_path: /probe
  static_configs:
  - targets:
    - localhost:8428
  relabel_configs:
  - source_labels:
    - __address__
    target_label: __param_target
  - source_labels:
    - __param_target
    target_label: instance
  - target_label: __address__
    replacement: http://blackbox
- job_name: nodeScrape/default/test-vms/0
  honor_labels: false
  kubernetes_sd_configs:
  - role: node
  basic_auth:
    username: some-username
    password: some-password
  relabel_configs:
  - source_labels:
    - __meta_kubernetes_node_name
    target_label: node
  - target_label: job
    replacement: default/test-vms
- job_name: staticScrape/default/test-vmstatic/0
  honor_labels: false
  static_configs:
  - targets: []
  metrics_path: /metrics-3
  proxy_url: https://some-proxy-1
  scheme: https
  relabel_configs: []
  oauth2:
    client_id: some-client-id
    client_secret: some-client-secret
    token_url: https://some-tr
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			if err := CreateOrUpdateConfigurationSecret(context.TODO(), tt.args.cr, testClient, tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateConfigurationSecret() error = %v, wantErr %v", err, tt.wantErr)
			}
			var expectSecret v1.Secret
			if err := testClient.Get(context.TODO(), types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.PrefixedName()}, &expectSecret); err != nil {
				t.Fatalf("cannot get vmagent config secret: %s", err)
			}
			gotCfg := expectSecret.Data[configFilename]
			cfgB := bytes.NewBuffer(gotCfg)
			gr, err := gzip.NewReader(cfgB)
			if err != nil {
				t.Fatalf("er: %s", err)
			}
			data, err := io.ReadAll(gr)
			if err != nil {
				t.Fatalf("cannot read cfg: %s", err)
			}
			gr.Close()

			assert.Equal(t, tt.wantConfig, string(data))

		})
	}
}

func TestCreateVMServiceScrapeFromService(t *testing.T) {
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
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient := k8stools.GetTestClientWithObjects(nil)
			err := CreateVMServiceScrapeFromService(context.Background(), testClient, tt.args.service, tt.args.serviceScrapeSpecTemplate, tt.args.metricPath, tt.args.filterPortNames...)
			if err != nil && !tt.wantErr {
				t.Fatalf("unexpected error: %s", err)
			}
			var gotServiceScrape victoriametricsv1beta1.VMServiceScrape
			if err := testClient.Get(context.Background(), types.NamespacedName{Name: tt.args.service.Name}, &gotServiceScrape); err != nil {
				t.Fatalf("unexpected error at retriving created object: %s", err)
			}
			assert.Equal(t, tt.wantServiceScrapeSpec, gotServiceScrape.Spec)
		})
	}
}
