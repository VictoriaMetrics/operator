package vmagent

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/go-test/deep"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func TestCreateOrUpdateVMAgent(t *testing.T) {
	type args struct {
		cr *vmv1beta1.VMAgent
		c  *config.BaseOperatorConf
	}
	tests := []struct {
		name              string
		args              args
		validate          func(set *appsv1.StatefulSet) error
		statefulsetMode   bool
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "generate vmagent statefulset with storage",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://remote-write"},
						},
						StatefulMode:   true,
						IngestOnlyMode: true,
						StatefulStorage: &vmv1beta1.StorageSpec{
							VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
								Spec: corev1.PersistentVolumeClaimSpec{
									StorageClassName: ptr.To("embed-sc"),
									Resources: corev1.VolumeResourceRequirements{
										Requests: map[corev1.ResourceName]resource.Quantity{
											corev1.ResourceStorage: resource.MustParse("10Gi"),
										},
									},
								},
							},
						},
						ClaimTemplates: []corev1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "extraTemplate",
								},
								Spec: corev1.PersistentVolumeClaimSpec{
									StorageClassName: ptr.To("default"),
									Resources: corev1.VolumeResourceRequirements{
										Requests: map[corev1.ResourceName]resource.Quantity{
											corev1.ResourceStorage: resource.MustParse("2Gi"),
										},
									},
								},
							},
						},
					},
				},
			},
			validate: func(got *appsv1.StatefulSet) error {
				if len(got.Spec.Template.Spec.Containers) != 1 {
					return fmt.Errorf("unexpected count of container, got: %d, want: %d", len(got.Spec.Template.Spec.Containers), 1)
				}
				if len(got.Spec.VolumeClaimTemplates) != 2 {
					return fmt.Errorf("unexpected count of VolumeClaimTemplates, got: %d, want: %d", len(got.Spec.VolumeClaimTemplates), 2)
				}
				if *got.Spec.VolumeClaimTemplates[0].Spec.StorageClassName != "embed-sc" {
					return fmt.Errorf("unexpected embed VolumeClaimTemplates name, got: %s, want: %s", *got.Spec.VolumeClaimTemplates[0].Spec.StorageClassName, "embed-sc")
				}
				if diff := deep.Equal(got.Spec.VolumeClaimTemplates[0].Spec.Resources, corev1.VolumeResourceRequirements{
					Requests: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceStorage: resource.MustParse("10Gi"),
					},
				}); len(diff) != 0 {
					return fmt.Errorf("unexpected embed VolumeClaimTemplates resources, diff: %v", diff)
				}
				if *got.Spec.VolumeClaimTemplates[1].Spec.StorageClassName != "default" {
					return fmt.Errorf("unexpected extra VolumeClaimTemplates, got: %s, want: %s", *got.Spec.VolumeClaimTemplates[1].Spec.StorageClassName, "default")
				}
				if diff := deep.Equal(got.Spec.VolumeClaimTemplates[1].Spec.Resources, corev1.VolumeResourceRequirements{
					Requests: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceStorage: resource.MustParse("2Gi"),
					},
				}); len(diff) != 0 {
					return fmt.Errorf("unexpected extra VolumeClaimTemplates resources, diff: %v", diff)
				}
				return nil
			},
			statefulsetMode: true,
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmagent-example-agent", "default"),
			},
		},
		{
			name: "generate with shards vmagent",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://remote-write"},
						},
						ShardCount: func() *int { i := 2; return &i }(),
					},
				},
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmagent-example-agent-0", "default"),
				k8stools.NewReadyDeployment("vmagent-example-agent-1", "default"),
			},
		},
		{
			name: "generate vmagent with bauth-secret",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-agent-bauth",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://remote-write"},
						},
						ServiceScrapeSelector: &metav1.LabelSelector{},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmagent-example-agent-bauth", "default"),
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "bauth-secret", Namespace: "default"},
					Data:       map[string][]byte{"user": []byte(`user-name`), "password": []byte(`user-password`)},
				},
				&vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmsingle-monitor",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						Selector: metav1.LabelSelector{},
						Endpoints: []vmv1beta1.Endpoint{
							{
								Interval: "30s",
								Scheme:   "http",
								BasicAuth: &vmv1beta1.BasicAuth{
									Password: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "bauth-secret"}, Key: "password"},
									Username: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "bauth-secret"}, Key: "user"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "fail if bearer token secret is missing, without basic auth",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-agent-bearer-missing",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{
								URL:               "http://remote-write",
								BearerTokenSecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "bearer-secret"}, Key: "token"},
							},
						},
						ServiceScrapeSelector: &metav1.LabelSelector{},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "generate vmagent with tls-secret",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-agent-tls",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://remote-write"},
							{
								URL: "http://remote-write2",
								TLSConfig: &vmv1beta1.TLSConfig{
									CA: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "remote2-secret",
											},
											Key: "ca",
										},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "remote2-secret",
											},
											Key: "ca",
										},
									},
									KeySecret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "remote2-secret",
										},
										Key: "key",
									},
								},
							},
							{
								URL: "http://remote-write3",
								TLSConfig: &vmv1beta1.TLSConfig{
									CA: vmv1beta1.SecretOrConfigMap{
										ConfigMap: &corev1.ConfigMapKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "remote3-cm",
											},
											Key: "ca",
										},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										ConfigMap: &corev1.ConfigMapKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "remote3-cm",
											},
											Key: "ca",
										},
									},
									KeySecret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "remote3-secret",
										},
										Key: "key",
									},
								},
							},
							{
								URL:       "http://remote-write4",
								TLSConfig: &vmv1beta1.TLSConfig{CertFile: "/tmp/cert1", KeyFile: "/tmp/key1", CAFile: "/tmp/ca"},
							},
						},
						ServiceScrapeSelector: &metav1.LabelSelector{},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmagent-example-agent-tls", "default"),
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "default"},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "tls-scrape", Namespace: "default"},
					Data:       map[string][]byte{"cert": []byte(`cert-data`), "ca": []byte(`ca-data`), "key": []byte(`key-data`)},
				},

				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "remote2-secret", Namespace: "default"},
					Data:       map[string][]byte{"cert": []byte(`cert-data`), "ca": []byte(`ca-data`), "key": []byte(`key-data`)},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "remote3-secret", Namespace: "default"},
					Data:       map[string][]byte{"key": []byte(`key-data`)},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "remote3-cm", Namespace: "default"},
					Data:       map[string]string{"ca": "ca-data", "cert": "cert-data"},
				},
				&vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmalert-monitor",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMServiceScrapeSpec{
						Selector: metav1.LabelSelector{},
						Endpoints: []vmv1beta1.Endpoint{
							{
								Interval: "30s",
								Scheme:   "https",
								TLSConfig: &vmv1beta1.TLSConfig{
									CA: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tls-scrape"}, Key: "ca"},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tls-scrape"}, Key: "ca"},
									},
									KeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tls-scrape"}, Key: "key"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "generate vmagent with inline scrape config",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://remote-write"},
						},
						InlineScrapeConfig: strings.TrimSpace(`
- job_name: "prometheus"
  static_configs:
  - targets: ["localhost:9090"]
`),
					},
				},
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmagent-example-agent", "default"),
			},
		},
		{
			name: "generate vmagent with inline scrape config and secret scrape config",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-agent",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://remote-write"},
						},
						AdditionalScrapeConfigs: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "add-cfg"},
							Key:                  "agent.yaml",
						},
						InlineScrapeConfig: strings.TrimSpace(`
- job_name: "prometheus"
  static_configs:
  - targets: ["localhost:9090"]
`),
					},
				},
			},
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmagent-example-agent", "default"),
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "add-cfg", Namespace: "default"},
					Data: map[string][]byte{"agent.yaml": []byte(strings.TrimSpace(`
- job_name: "alertmanager"
  static_configs:
  - targets: ["localhost:9093"]
`))},
				},
			},
		},
		{
			name: "generate vmagent statefulset with serviceName when additional service is headless",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-agent-with-headless-service",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://remote-write"},
						},
						StatefulMode: true,
						ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
							EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
								Name: "my-headless-additional-service",
							},
							Spec: corev1.ServiceSpec{
								ClusterIP: corev1.ClusterIPNone,
							},
						},
					},
				},
			},
			validate: func(got *appsv1.StatefulSet) error {
				if got.Spec.ServiceName != "my-headless-additional-service" {
					return fmt.Errorf("unexpected serviceName, got: %s, want: %s", got.Spec.ServiceName, "my-headless-additional-service")
				}
				return nil
			},
			statefulsetMode: true,
			predefinedObjects: []runtime.Object{
				k8stools.NewReadyDeployment("vmagent-example-agent", "default"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			err := CreateOrUpdateVMAgent(context.TODO(), tt.args.cr, fclient, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMAgent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.statefulsetMode {
				var got appsv1.StatefulSet
				if err := fclient.Get(context.Background(), types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.PrefixedName()}, &got); (err != nil) != tt.wantErr {
					t.Fatalf("CreateOrUpdateVMAgent() error = %v, wantErr %v", err, tt.wantErr)
				}
				if err := tt.validate(&got); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func Test_loadTLSAssets(t *testing.T) {
	type args struct {
		monitors map[string]*vmv1beta1.VMServiceScrape
		pods     map[string]*vmv1beta1.VMPodScrape
		statics  map[string]*vmv1beta1.VMStaticScrape
		nodes    map[string]*vmv1beta1.VMNodeScrape
		probes   map[string]*vmv1beta1.VMProbe
		cr       *vmv1beta1.VMAgent
	}
	tests := []struct {
		name              string
		args              args
		want              map[string]string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "load tls asset from secret",
			args: args{
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{},
				},
				monitors: map[string]*vmv1beta1.VMServiceScrape{
					"vmagent-monitor": {
						ObjectMeta: metav1.ObjectMeta{Name: "vmagent-monitor", Namespace: "default"},
						Spec: vmv1beta1.VMServiceScrapeSpec{
							Endpoints: []vmv1beta1.Endpoint{
								{
									TLSConfig: &vmv1beta1.TLSConfig{
										KeySecret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "tls-secret",
											},
											Key: "cert",
										},
									},
								},
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{"cert": []byte(`cert-data`)},
				},
			},
			want: map[string]string{"default_tls-secret_cert": "cert-data"},
		},
		{
			name: "load tls asset from secret with remoteWrite tls",
			args: args{
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmagent-test-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{
								URL: "some1-url",
								TLSConfig: &vmv1beta1.TLSConfig{
									CA: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "remote1-write-spec",
											},
											Key: "ca",
										},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "remote1-write-spec",
											},
											Key: "cert",
										},
									},
									KeySecret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "remote1-write-spec",
										},
										Key: "key",
									},
								},
							},
							{
								URL: "some-url",
							},
						},
					},
				},
				monitors: map[string]*vmv1beta1.VMServiceScrape{
					"vmagent-monitor": {
						ObjectMeta: metav1.ObjectMeta{Name: "vmagent-monitor", Namespace: "default"},
						Spec: vmv1beta1.VMServiceScrapeSpec{
							Endpoints: []vmv1beta1.Endpoint{
								{TLSConfig: &vmv1beta1.TLSConfig{
									KeySecret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "tls-secret",
										},
										Key: "cert",
									},
								}},
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{"cert": []byte(`cert-data`)},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "remote1-write-spec",
						Namespace: "default",
					},
					Data: map[string][]byte{"cert": []byte(`cert-data`), "key": []byte(`cert-key`), "ca": []byte(`cert-ca`)},
				},
			},
			want: map[string]string{"default_tls-secret_cert": "cert-data", "default_remote1-write-spec_ca": "cert-ca", "default_remote1-write-spec_cert": "cert-data", "default_remote1-write-spec_key": "cert-key"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			got, err := loadTLSAssets(context.TODO(), fclient, tt.args.cr, tt.args.monitors, tt.args.pods, tt.args.probes, tt.args.nodes, tt.args.statics)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadTLSAssets() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("loadTLSAssets() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildRemoteWrites(t *testing.T) {
	type args struct {
		cr      *vmv1beta1.VMAgent
		ssCache *scrapesSecretsCache
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "test with tls config full",
			args: args{
				ssCache: &scrapesSecretsCache{},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL: "localhost:8429",

							TLSConfig: &vmv1beta1.TLSConfig{
								CA: vmv1beta1.SecretOrConfigMap{Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
									Key: "ca",
								}},
							},
						},
						{
							URL: "localhost:8429",
							TLSConfig: &vmv1beta1.TLSConfig{
								CAFile: "/path/to_ca",
								Cert: vmv1beta1.SecretOrConfigMap{Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
									Key: "ca",
								}},
							},
						},
					}},
				},
			},
			want: []string{"-remoteWrite.tlsCAFile=/etc/vmagent-tls/certs_tls-secret_ca,/path/to_ca", "-remoteWrite.tlsCertFile=,/etc/vmagent-tls/certs_tls-secret_ca", "-remoteWrite.url=localhost:8429,localhost:8429"},
		},
		{
			name: "test insecure with key only",
			args: args{
				ssCache: &scrapesSecretsCache{},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL: "localhost:8429",

							TLSConfig: &vmv1beta1.TLSConfig{
								KeySecret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
									Key: "key",
								},
								InsecureSkipVerify: true,
							},
						},
					}},
				},
			},
			want: []string{"-remoteWrite.url=localhost:8429", "-remoteWrite.tlsInsecureSkipVerify=true", "-remoteWrite.tlsKeyFile=/etc/vmagent-tls/certs_tls-secret_key"},
		},
		{
			name: "test insecure",
			args: args{
				ssCache: &scrapesSecretsCache{},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL: "localhost:8429",

							TLSConfig: &vmv1beta1.TLSConfig{
								InsecureSkipVerify: true,
							},
						},
					}},
				},
			},
			want: []string{"-remoteWrite.url=localhost:8429", "-remoteWrite.tlsInsecureSkipVerify=true"},
		},
		{
			name: "test inline relabeling",
			args: args{
				ssCache: &scrapesSecretsCache{},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{
								URL: "localhost:8429",
								TLSConfig: &vmv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								},
								InlineUrlRelabelConfig: []vmv1beta1.RelabelConfig{
									{TargetLabel: "rw-1", Replacement: "present"},
								},
							},
							{
								URL: "remote-1:8429",

								TLSConfig: &vmv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								},
							},
							{
								URL: "remote-1:8429",
								TLSConfig: &vmv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								},
								InlineUrlRelabelConfig: []vmv1beta1.RelabelConfig{
									{TargetLabel: "rw-2", Replacement: "present"},
								},
							},
						},
						InlineRelabelConfig: []vmv1beta1.RelabelConfig{
							{TargetLabel: "dst", Replacement: "ok"},
						},
					},
				},
			},
			want: []string{"-remoteWrite.url=localhost:8429,remote-1:8429,remote-1:8429", "-remoteWrite.tlsInsecureSkipVerify=true,true,true", "-remoteWrite.urlRelabelConfig=/etc/vm/relabeling/url_relabeling-0.yaml,,/etc/vm/relabeling/url_relabeling-2.yaml"},
		},
		{
			name: "test sendTimeout",
			args: args{
				ssCache: &scrapesSecretsCache{},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL: "localhost:8429",

							SendTimeout: ptr.To("10s"),
						},
						{
							URL:         "localhost:8431",
							SendTimeout: ptr.To("15s"),
						},
					}},
				},
			},
			want: []string{"-remoteWrite.url=localhost:8429,localhost:8431", "-remoteWrite.sendTimeout=10s,15s"},
		},
		{
			name: "test multi-tenant",
			args: args{
				ssCache: &scrapesSecretsCache{},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{
								URL: "http://vminsert-cluster-1:8480/insert/multitenant/prometheus/api/v1/write",

								SendTimeout: ptr.To("10s"),
							},
							{
								URL:         "http://vmagent-aggregation:8429",
								SendTimeout: ptr.To("15s"),
							},
						},
						RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
							UseMultiTenantMode: true,
						},
					},
				},
			},
			want: []string{"-remoteWrite.url=http://vminsert-cluster-1:8480/insert/multitenant/prometheus/api/v1/write,http://vmagent-aggregation:8429", "-remoteWrite.sendTimeout=10s,15s"},
		},
		{
			name: "test oauth2",
			args: args{
				ssCache: &scrapesSecretsCache{
					oauth2Secrets: map[string]*k8stools.OAuthCreds{"remoteWriteSpec/localhost:8431": {
						ClientID:     "some-id",
						ClientSecret: "some-secret",
					}},
				},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL:         "localhost:8429",
							SendTimeout: ptr.To("10s"),
						},
						{
							URL:         "localhost:8431",
							SendTimeout: ptr.To("15s"),
							OAuth2: &vmv1beta1.OAuth2{
								Scopes:       []string{"scope-1"},
								TokenURL:     "http://some-url",
								ClientSecret: &corev1.SecretKeySelector{},
								ClientID: vmv1beta1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "some-cm"},
									Key:                  "some-key",
								}},
							},
						},
					}},
				},
			},
			want: []string{"-remoteWrite.oauth2.clientID=,some-id", "-remoteWrite.oauth2.clientSecretFile=,/etc/vmagent/config/RWS_1-SECRET-OAUTH2SECRET", "-remoteWrite.oauth2.scopes=,scope-1", "-remoteWrite.oauth2.tokenUrl=,http://some-url", "-remoteWrite.url=localhost:8429,localhost:8431", "-remoteWrite.sendTimeout=10s,15s"},
		},
		{
			name: "test bearer token",
			args: args{
				ssCache: &scrapesSecretsCache{
					bearerTokens: map[string]string{
						"remoteWriteSpec/localhost:8431": "token",
					},
				},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL:         "localhost:8429",
							SendTimeout: ptr.To("10s"),
						},
						{
							URL:         "localhost:8431",
							SendTimeout: ptr.To("15s"),
							BearerTokenSecret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
								Key:                  "some-key",
							},
						},
					}},
				},
			},
			want: []string{"-remoteWrite.bearerTokenFile=\"\",\"/etc/vmagent/config/RWS_1-SECRET-BEARERTOKEN\"", "-remoteWrite.url=localhost:8429,localhost:8431", "-remoteWrite.sendTimeout=10s,15s"},
		},
		{
			name: "test with headers",
			args: args{
				ssCache: &scrapesSecretsCache{
					bearerTokens: map[string]string{
						"remoteWriteSpec/localhost:8431": "token",
					},
				},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL:         "localhost:8429",
							SendTimeout: ptr.To("10s"),
						},
						{
							URL:         "localhost:8431",
							SendTimeout: ptr.To("15s"),
							BearerTokenSecret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
								Key:                  "some-key",
							},
							Headers: []string{"key: value", "second-key: value2"},
						},
					}},
				},
			},
			want: []string{"-remoteWrite.bearerTokenFile=\"\",\"/etc/vmagent/config/RWS_1-SECRET-BEARERTOKEN\"", "-remoteWrite.headers=,key: value^^second-key: value2", "-remoteWrite.url=localhost:8429,localhost:8431", "-remoteWrite.sendTimeout=10s,15s"},
		},
		{
			name: "test with stream aggr",
			args: args{
				ssCache: &scrapesSecretsCache{},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL: "localhost:8429",
							StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
								Rules: []vmv1beta1.StreamAggrRule{
									{
										Outputs: []string{"total", "avg"},
									},
								},
								DedupInterval: "10s",
							},
						},
						{
							URL: "localhost:8431",
							StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
								Rules: []vmv1beta1.StreamAggrRule{
									{
										Outputs: []string{"histogram_bucket"},
									},
								},
								KeepInput: true,
							},
						},
					}},
				},
			},
			want: []string{
				`-remoteWrite.streamAggr.config=/etc/vm/stream-aggr/RWS_0-CM-STREAM-AGGR-CONF,/etc/vm/stream-aggr/RWS_1-CM-STREAM-AGGR-CONF`,
				`-remoteWrite.streamAggr.dedupInterval=10s,`,
				`-remoteWrite.streamAggr.keepInput=false,true`,
				`-remoteWrite.url=localhost:8429,localhost:8431`,
			},
		},
		{
			name: "test with stream aggr (one remote write)",
			args: args{
				ssCache: &scrapesSecretsCache{},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL: "localhost:8431",
							StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
								Rules: []vmv1beta1.StreamAggrRule{
									{
										Outputs: []string{"histogram_bucket"},
									},
								},
								KeepInput:     true,
								DedupInterval: "10s",
							},
						},
					}},
				},
			},
			want: []string{
				`-remoteWrite.streamAggr.config=/etc/vm/stream-aggr/RWS_0-CM-STREAM-AGGR-CONF`,
				`-remoteWrite.streamAggr.dedupInterval=10s`,
				`-remoteWrite.streamAggr.keepInput=true`,
				`-remoteWrite.url=localhost:8431`,
			},
		},
		{
			name: "test with stream aggr (one remote write with defaults)",
			args: args{
				ssCache: &scrapesSecretsCache{},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL: "localhost:8431",
							StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
								Rules: []vmv1beta1.StreamAggrRule{
									{
										Outputs: []string{"histogram_bucket"},
									},
								},
							},
						},
					}},
				},
			},
			want: []string{
				`-remoteWrite.streamAggr.config=/etc/vm/stream-aggr/RWS_0-CM-STREAM-AGGR-CONF`,
				`-remoteWrite.url=localhost:8431`,
			},
		},
		{
			name: "test with stream aggr (many remote writes)",
			args: args{
				ssCache: &scrapesSecretsCache{},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL: "localhost:8428",
						},
						{
							URL: "localhost:8429",
							StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
								Rules: []vmv1beta1.StreamAggrRule{
									{
										Interval: "1m",
										Outputs:  []string{"total", "avg"},
									},
								},
								DedupInterval: "10s",
								KeepInput:     true,
							},
						},
						{
							URL: "localhost:8430",
						},
						{
							URL: "localhost:8431",
							StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
								Rules: []vmv1beta1.StreamAggrRule{
									{
										Interval: "1m",
										Outputs:  []string{"histogram_bucket"},
									},
								},
							},
						},
						{
							URL: "localhost:8432",
						},
					}},
				},
			},
			want: []string{
				`-remoteWrite.streamAggr.config=,/etc/vm/stream-aggr/RWS_1-CM-STREAM-AGGR-CONF,,/etc/vm/stream-aggr/RWS_3-CM-STREAM-AGGR-CONF,`,
				`-remoteWrite.streamAggr.dedupInterval=,10s,,,`,
				`-remoteWrite.streamAggr.keepInput=false,true,false,false,false`,
				`-remoteWrite.url=localhost:8428,localhost:8429,localhost:8430,localhost:8431,localhost:8432`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sort.Strings(tt.want)
			got := buildRemoteWrites(tt.args.cr, tt.args.ssCache)
			sort.Strings(got)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCreateOrUpdateVMAgentService(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *vmv1beta1.VMAgent
		c   *config.BaseOperatorConf
	}
	tests := []struct {
		name                  string
		args                  args
		want                  func(svc *corev1.Service) error
		wantAdditionalService func(svc *corev1.Service) error
		wantErr               bool
		predefinedObjects     []runtime.Object
	}{
		{
			name: "base case",
			args: args{
				ctx: context.TODO(),
				c:   config.MustGetBaseConfig(),
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "base",
						Namespace: "default",
					},
				},
			},
			want: func(svc *corev1.Service) error {
				if svc.Name != "vmagent-base" {
					return fmt.Errorf("unexpected name for service: %v", svc.Name)
				}
				if len(svc.Spec.Ports) != 1 {
					return fmt.Errorf("unexpected count for service ports: %v", len(svc.Spec.Ports))
				}
				return nil
			},
		},
		{
			name: "base case with ingestPorts and extra service",
			args: args{
				ctx: context.TODO(),
				c:   config.MustGetBaseConfig(),
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "base",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						InsertPorts: &vmv1beta1.InsertPorts{
							InfluxPort: "8011",
						},
						ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
							EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "extra-svc"},
							Spec: corev1.ServiceSpec{
								Type: corev1.ServiceTypeNodePort,
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "extra-svc",
						Namespace: "default",
						Labels: map[string]string{
							"app.kubernetes.io/name":      "vmagent",
							"app.kubernetes.io/instance":  "base",
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
					},
				},
			},
			want: func(svc *corev1.Service) error {
				if svc.Name != "vmagent-base" {
					return fmt.Errorf("unexpected name for service: %v", svc.Name)
				}
				if len(svc.Spec.Ports) != 3 {
					return fmt.Errorf("unexpcted count for ports, want 3, got: %v", len(svc.Spec.Ports))
				}
				return nil
			},
		},
		{
			name: "base case with ingestPorts and extra service",
			args: args{
				ctx: context.TODO(),
				c:   config.MustGetBaseConfig(),
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "base",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						InsertPorts: &vmv1beta1.InsertPorts{
							InfluxPort: "8011",
						},
						ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
							EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "extra-svc"},
							Spec: corev1.ServiceSpec{
								Type: corev1.ServiceTypeNodePort,
								Ports: []corev1.ServicePort{
									{
										Name:     "influx-udp",
										NodePort: 8085,
										Protocol: corev1.ProtocolUDP,
									},
								},
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "extra-svc",
						Namespace: "default",
						Labels: map[string]string{
							"app.kubernetes.io/name":      "vmagent",
							"app.kubernetes.io/instance":  "base",
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
					},
				},
			},
			want: func(svc *corev1.Service) error {
				if svc.Name != "vmagent-base" {
					return fmt.Errorf("unexpected name for service: %v", svc.Name)
				}
				if len(svc.Spec.Ports) != 3 {
					return fmt.Errorf("unexpcted count for ports, want 3, got: %v", len(svc.Spec.Ports))
				}
				return nil
			},
			wantAdditionalService: func(svc *corev1.Service) error {
				if len(svc.Spec.Ports) != 1 {
					return fmt.Errorf("unexpcted count for ports, want 1, got: %v", len(svc.Spec.Ports))
				}
				if svc.Spec.Ports[0].NodePort != 8085 {
					return fmt.Errorf("unexpected port %v, want 8085", svc.Spec.Ports[0])
				}
				if svc.Spec.Ports[0].Protocol != corev1.ProtocolUDP {
					return fmt.Errorf("unexpected protocol want udp, got: %v", svc.Spec.Ports[0].Protocol)
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := CreateOrUpdateVMAgentService(tt.args.ctx, tt.args.cr, cl, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMAgentService() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err := tt.want(got); err != nil {
				t.Errorf("CreateOrUpdateVMAgentService() unexpected error: %v", err)
			}
			if tt.wantAdditionalService != nil {
				var additionalSvc corev1.Service
				if err := cl.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.Spec.ServiceSpec.NameOrDefault(tt.args.cr.Name)}, &additionalSvc); err != nil {
					t.Fatalf("unexpected error: %s", err)
				}
				if err := tt.wantAdditionalService(&additionalSvc); err != nil {
					t.Fatalf("CreateOrUpdateVMAgentService validation failed for additional service: %s", err)
				}
			}
		})
	}
}

func TestCreateOrUpdateRelabelConfigsAssets(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *vmv1beta1.VMAgent
	}
	tests := []struct {
		name              string
		args              args
		predefinedObjects []runtime.Object
		validate          func(cm *corev1.ConfigMap) error
		wantErr           bool
	}{
		{
			name: "simple relabelcfg",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						InlineRelabelConfig: []vmv1beta1.RelabelConfig{
							{
								Regex:        []string{".*"},
								Action:       "DROP",
								SourceLabels: []string{"pod"},
							},
							{},
						},
					},
				},
			},
			validate: func(cm *corev1.ConfigMap) error {
				data, ok := cm.Data[globalRelabelingName]
				if !ok {
					return fmt.Errorf("key: %s, not exists at map: %v", "global_relabeling.yaml", cm.BinaryData)
				}
				wantGlobal := `- source_labels:
  - pod
  regex: .*
  action: DROP
`
				assert.Equal(t, wantGlobal, data)
				return nil
			},
			predefinedObjects: []runtime.Object{},
		},
		{
			name: "combined relabel configs",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmag",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAgentSpec{
						InlineRelabelConfig: []vmv1beta1.RelabelConfig{
							{
								Regex:        []string{".*"},
								Action:       "DROP",
								SourceLabels: []string{"pod"},
							},
						},
						RelabelConfig: &corev1.ConfigMapKeySelector{
							Key:                  "global.yaml",
							LocalObjectReference: corev1.LocalObjectReference{Name: "relabels"},
						},
					},
				},
			},
			validate: func(cm *corev1.ConfigMap) error {
				data, ok := cm.Data[globalRelabelingName]
				if !ok {
					return fmt.Errorf("key: %s, not exists at map: %v", "global_relabeling.yaml", cm.BinaryData)
				}
				wantGlobal := strings.TrimSpace(`
- source_labels:
  - pod
  regex: .*
  action: DROP
- action: DROP
  source_labels: ["pod-1"]`)
				assert.Equal(t, wantGlobal, data)
				return nil
			},
			predefinedObjects: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "relabels", Namespace: "default"},
					Data: map[string]string{
						"global.yaml": strings.TrimSpace(`
- action: DROP
  source_labels: ["pod-1"]`),
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			if err := createOrUpdateRelabelConfigsAssets(tt.args.ctx, tt.args.cr, cl); (err != nil) != tt.wantErr {
				t.Fatalf("CreateOrUpdateRelabelConfigsAssets() error = %v, wantErr %v", err, tt.wantErr)
			}
			var createdCM corev1.ConfigMap
			if err := cl.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.RelabelingAssetName()}, &createdCM); err != nil {
				t.Fatalf("cannot fetch created cm: %v", err)
			}
			if err := tt.validate(&createdCM); err != nil {
				t.Fatalf("cannot validate created cm: %v", err)
			}
		})
	}
}

func TestCreateOrUpdateStreamAggrConfig(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *vmv1beta1.VMAgent
	}
	tests := []struct {
		name              string
		args              args
		predefinedObjects []runtime.Object
		validate          func(cm *corev1.ConfigMap) error
		wantErr           bool
	}{
		{
			name: "simple stream aggr config",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{
								URL: "localhost:8429",
								StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
									Rules: []vmv1beta1.StreamAggrRule{{
										Interval: "1m",
										Outputs:  []string{"total", "avg"},
									}},
								},
							},
						},
					},
				},
			},
			validate: func(cm *corev1.ConfigMap) error {
				data, ok := cm.Data["RWS_0-CM-STREAM-AGGR-CONF"]
				if !ok {
					return fmt.Errorf("key: %s, not exists at map: %v", "RWS_0-CM-STREAM-AGGR-CONFl", cm.BinaryData)
				}
				wantGlobal := `- interval: 1m
  outputs:
  - total
  - avg
`
				assert.Equal(t, wantGlobal, data)
				return nil
			},
			predefinedObjects: []runtime.Object{},
		},
		{
			name: "simple stream aggr config",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{
								URL: "localhost:8429",
								StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
									Rules: []vmv1beta1.StreamAggrRule{{
										Match:             []string{`{__name__="count1"}`, `{__name__="count2"}`},
										Interval:          "1m",
										StalenessInterval: "2m",
										Outputs:           []string{"total", "avg"},
										By:                []string{"job", "instance"},
										Without:           []string{"pod"},
										OutputRelabelConfigs: []vmv1beta1.RelabelConfig{{
											SourceLabels: []string{"__name__"},
											TargetLabel:  "metric",
											Regex:        []string{"(.+):.+"},
										}},
									}},
								},
							},
						},
					},
				},
			},
			validate: func(cm *corev1.ConfigMap) error {
				data, ok := cm.Data["RWS_0-CM-STREAM-AGGR-CONF"]
				if !ok {
					return fmt.Errorf("key: %s, not exists at map: %v", "RWS_0-CM-STREAM-AGGR-CONFl", cm.BinaryData)
				}
				wantGlobal := `- match:
  - '{__name__="count1"}'
  - '{__name__="count2"}'
  interval: 1m
  staleness_interval: 2m
  outputs:
  - total
  - avg
  by:
  - job
  - instance
  without:
  - pod
  output_relabel_configs:
  - regex: (.+):.+
`
				assert.Equal(t, wantGlobal, data)
				return nil
			},
			predefinedObjects: []runtime.Object{},
		},
		{
			name: "stream aggr config with multie regex",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{
								URL: "localhost:8429",
								StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
									Rules: []vmv1beta1.StreamAggrRule{{
										Match:             []string{`{__name__="count1"}`, `{__name__="count2"}`},
										Interval:          "1m",
										StalenessInterval: "2m",
										Outputs:           []string{"total", "avg"},
										By:                []string{"job", "instance"},
										Without:           []string{"pod"},
										OutputRelabelConfigs: []vmv1beta1.RelabelConfig{{
											SourceLabels: []string{"__name__"},
											TargetLabel:  "metric",
											Regex:        []string{"vmagent", "vmalert", "vmauth"},
										}},
									}},
								},
							},
						},
					},
				},
			},
			validate: func(cm *corev1.ConfigMap) error {
				data, ok := cm.Data["RWS_0-CM-STREAM-AGGR-CONF"]
				if !ok {
					return fmt.Errorf("key: %s, not exists at map: %v", "RWS_0-CM-STREAM-AGGR-CONFl", cm.BinaryData)
				}
				wantGlobal := `- match:
  - '{__name__="count1"}'
  - '{__name__="count2"}'
  interval: 1m
  staleness_interval: 2m
  outputs:
  - total
  - avg
  by:
  - job
  - instance
  without:
  - pod
  output_relabel_configs:
  - regex:
    - vmagent
    - vmalert
    - vmauth
`
				assert.Equal(t, wantGlobal, data)
				return nil
			},
			predefinedObjects: []runtime.Object{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			if err := CreateOrUpdateVMAgentStreamAggrConfig(tt.args.ctx, tt.args.cr, cl); (err != nil) != tt.wantErr {
				t.Fatalf("CreateOrUpdateVMAgentStreamAggrConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			var createdCM corev1.ConfigMap
			if err := cl.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.StreamAggrConfigName()}, &createdCM); err != nil {
				t.Fatalf("cannot fetch created cm: %v", err)
			}
			if err := tt.validate(&createdCM); err != nil {
				t.Fatalf("cannot validate created cm: %v", err)
			}
		})
	}
}

func Test_buildConfigReloaderArgs(t *testing.T) {
	type args struct {
		cr *vmv1beta1.VMAgent
		c  *config.BaseOperatorConf
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "parse ok",
			args: args{
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{Port: "8429"},
				},
				c: &config.BaseOperatorConf{},
			},
			want: []string{
				"--reload-url=http://localhost:8429/-/reload",
				"--config-file=/etc/vmagent/config/vmagent.yaml.gz",
				"--config-envsubst-file=/etc/vmagent/config_out/vmagent.env.yaml",
			},
		},
		{
			name: "ingest only",
			args: args{
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{Port: "8429", IngestOnlyMode: true},
				},
				c: &config.BaseOperatorConf{},
			},
			want: []string{
				"--reload-url=http://localhost:8429/-/reload",
			},
		},
		{
			name: "with relabel and stream",
			args: args{
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						Port:                "8429",
						IngestOnlyMode:      false,
						InlineRelabelConfig: []vmv1beta1.RelabelConfig{{TargetLabel: "test"}},
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{
								URL: "http://some",
								StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
									Rules: []vmv1beta1.StreamAggrRule{
										{
											Outputs: []string{"dst"},
										},
									},
								},
							},
						},
					},
				},
				c: &config.BaseOperatorConf{},
			},
			want: []string{
				"--reload-url=http://localhost:8429/-/reload",
				"--config-file=/etc/vmagent/config/vmagent.yaml.gz",
				"--config-envsubst-file=/etc/vmagent/config_out/vmagent.env.yaml",
				"--watched-dir=/etc/vm/relabeling",
				"--watched-dir=/etc/vm/stream-aggr",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildConfigReloaderArgs(tt.args.cr, tt.args.c)
			sort.Strings(got)
			sort.Strings(tt.want)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildRemoteWriteSettings(t *testing.T) {
	type args struct {
		cr *vmv1beta1.VMAgent
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "test with StatefulMode",
			args: args{
				cr: &vmv1beta1.VMAgent{Spec: vmv1beta1.VMAgentSpec{StatefulMode: true}},
			},
			want: []string{"-remoteWrite.maxDiskUsagePerURL=1073741824", "-remoteWrite.tmpDataPath=/vmagent_pq/vmagent-remotewrite-data"},
		},
		{
			name: "test simple ok",
			args: args{
				cr: &vmv1beta1.VMAgent{},
			},
			want: []string{"-remoteWrite.maxDiskUsagePerURL=1073741824", "-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data"},
		},
		{
			name: "test labels",
			args: args{
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
							Labels: map[string]string{
								"label-1": "value1",
								"label-2": "value2",
							},
						},
					},
				},
			},
			want: []string{"-remoteWrite.label=label-1=value1,label-2=value2", "-remoteWrite.maxDiskUsagePerURL=1073741824", "-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data"},
		},
		{
			name: "test label",
			args: args{
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
							ShowURL: ptr.To(true),
							Labels: map[string]string{
								"label-1": "value1",
							},
						},
					},
				},
			},
			want: []string{"-remoteWrite.label=label-1=value1", "-remoteWrite.showURL=true", "-remoteWrite.maxDiskUsagePerURL=1073741824", "-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data"},
		},
		{
			name: "with remoteWriteSettings",
			args: args{
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
							ShowURL:            ptr.To(true),
							TmpDataPath:        ptr.To("/tmp/my-path"),
							MaxDiskUsagePerURL: ptr.To(int64(1000)),
							UseMultiTenantMode: true,
						},
					},
				},
			},
			want: []string{"-remoteWrite.maxDiskUsagePerURL=1000", "-remoteWrite.tmpDataPath=/tmp/my-path", "-remoteWrite.showURL=true", "-enableMultitenantHandlers=true"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRemoteWriteSettings(tt.args.cr)
			sort.Strings(got)
			sort.Strings(tt.want)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildRemoteWriteSettings() = \n%v\n, want \n%v\n", got, tt.want)
			}
		})
	}
}

func TestMakeSpecForAgentOk(t *testing.T) {
	f := func(cr *vmv1beta1.VMAgent, c *config.BaseOperatorConf, sCache *scrapesSecretsCache, wantYaml string) {
		t.Helper()
		setDefaultForVMAgent(cr, c)

		// this trick allows to omit empty fields for yaml
		var wantSpec corev1.PodSpec
		if err := yaml.Unmarshal([]byte(wantYaml), &wantSpec); err != nil {
			t.Fatalf("not expected wantYaml: %q: \n%q", wantYaml, err)

			setDefaultForVMAgent(cr, c)
		}
		wantYAMLForCompare, err := yaml.Marshal(wantSpec)
		if err != nil {
			t.Fatalf("BUG: cannot parse as yaml: %q", err)
		}
		got, err := makeSpecForVMAgent(cr, c, sCache)
		if err != nil {
			t.Fatalf("not expected error=%q", err)
		}
		gotYAML, err := yaml.Marshal(got)
		if err != nil {
			t.Fatalf("cannot parse got as yaml: %q", err)
		}

		// compare yaml output instead of structs, since structs could be modified by marshal/unmarshal callbacks
		if !cmp.Equal(string(gotYAML), string(wantYAMLForCompare)) {
			diff := cmp.Diff(&wantSpec, got)
			t.Fatalf("not expected output for test, diff is %s", diff)
		}
	}
	testConfBuild := func(setup func(c *config.BaseOperatorConf)) *config.BaseOperatorConf {
		c := *config.MustGetBaseConfig()
		setup(&c)
		return &c
	}
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec:       vmv1beta1.VMAgentSpec{IngestOnlyMode: true},
	}, testConfBuild(func(c *config.BaseOperatorConf) {
		c.VMAgentDefault.UseDefaultResources = true
		c.VMAgentDefault.Resource.Limit.Cpu = "10"
		c.VMAgentDefault.Resource.Limit.Mem = "10Mi"
		c.VMAgentDefault.Resource.Request.Cpu = "10"
		c.VMAgentDefault.Resource.Request.Mem = "10Mi"
		c.VMAgentDefault.Image = "vm-repo"
		c.VMAgentDefault.Version = "v1.97.1"
		c.UseCustomConfigReloader = true
		c.VMAgentDefault.Port = "8429"
		c.CustomConfigReloaderImage = "vmcustom:config-reloader-v0.35.0"
	}), nil, `
volumes:
    - name: persistent-queue-data
      volumesource:
        emptydir:
            medium: ""
            sizelimit: null
initcontainers: []
containers:
    - name: vmagent
      image: vm-repo:v1.97.1
      args:
        - -httpListenAddr=:8429
        - -remoteWrite.maxDiskUsagePerURL=1073741824
        - -remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data
      ports:
        - name: http
          hostport: 0
          containerport: 8429
          protocol: TCP
      resources:
        limits:
            cpu:
                format: DecimalSI
            memory:
                format: BinarySI
        requests:
            cpu:
                format: DecimalSI
            memory:
                format: BinarySI
        claims: []
      volumemounts:
        - name: persistent-queue-data
          readonly: false
          mountpath: /tmp/vmagent-remotewrite-data
      livenessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    type: 0
                    intval: 8429
                    strval: ""
                scheme: HTTP
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
      readinessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8429
                scheme: HTTP
        initialdelayseconds: 0
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
      terminationmessagepolicy: FallbackToLogsOnError
      imagepullpolicy: IfNotPresent
serviceaccountname: vmagent-agent

    `)
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec:       vmv1beta1.VMAgentSpec{IngestOnlyMode: false},
	}, testConfBuild(func(c *config.BaseOperatorConf) {
		c.UseCustomConfigReloader = true
		c.VMAgentDefault.Port = "8429"
		c.CustomConfigReloaderImage = "vmcustomer:v1"
		c.VMAgentDefault.Version = "v1.97.1"
		c.VMAgentDefault.UseDefaultResources = false
	}), nil, `
volumes:
    - name: persistent-queue-data
      volumesource:
        emptydir: {}
    - name: tls-assets
      volumesource:
        secret:
            secretname: tls-assets-vmagent-agent
    - name: config-out
      volumesource:
        emptydir:
            medium: ""
            sizelimit: null
    - name: config
      volumesource:
        secret:
            secretname: vmagent-agent
initcontainers:
    - name: config-init
      image: vmcustomer:v1
      command:
        - /usr/local/bin/config-reloader
      args:
        - --reload-url=http://localhost:8429/-/reload
        - --config-envsubst-file=/etc/vmagent/config_out/vmagent.env.yaml
        - --config-secret-name=default/vmagent-agent
        - --config-secret-key=vmagent.yaml.gz
        - --only-init-config
      volumemounts:
        - name: config-out
          readonly: false
          mountpath: /etc/vmagent/config_out
containers:
    - name: config-reloader
      image: vmcustomer:v1
      command:
        - /usr/local/bin/config-reloader
      args:
        - --reload-url=http://localhost:8429/-/reload
        - --config-envsubst-file=/etc/vmagent/config_out/vmagent.env.yaml
        - --config-secret-name=default/vmagent-agent
        - --config-secret-key=vmagent.yaml.gz
      ports:
        - name: reloader-http
          hostport: 0
          containerport: 8435
          protocol: TCP
          hostip: ""
      env:
        - name: POD_NAME
          valuefrom:
            fieldref:
                fieldpath: metadata.name
      resources:
        limits: {}
        requests: {}
        claims: []
      resizepolicy: []
      volumemounts:
        - name: config-out
          readonly: false
          mountpath: /etc/vmagent/config_out
      livenessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8435
                scheme: HTTP
        initialdelayseconds: 0
        timeoutseconds: 0
        periodseconds: 0
        successthreshold: 0
        failurethreshold: 0
      readinessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8435
                scheme: HTTP
        initialdelayseconds: 5
        timeoutseconds: 0
        periodseconds: 0
        successthreshold: 0
        failurethreshold: 0
        terminationgraceperiodseconds: null
      terminationmessagepolicy: FallbackToLogsOnError
    - name: vmagent
      image: victoriametrics/vmagent:v1.97.1
      args:
        - -httpListenAddr=:8429
        - -promscrape.config=/etc/vmagent/config_out/vmagent.env.yaml
        - -remoteWrite.maxDiskUsagePerURL=1073741824
        - -remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data
      ports:
        - name: http
          containerport: 8429
          protocol: TCP
      volumemounts:
        - name: persistent-queue-data
          readonly: false
          mountpath: /tmp/vmagent-remotewrite-data
          subpath: ""
          mountpropagation: null
          subpathexpr: ""
        - name: config-out
          readonly: true
          mountpath: /etc/vmagent/config_out
          subpath: ""
          mountpropagation: null
          subpathexpr: ""
        - name: tls-assets
          readonly: true
          mountpath: /etc/vmagent-tls/certs
          subpath: ""
          mountpropagation: null
          subpathexpr: ""
        - name: config
          readonly: true
          mountpath: /etc/vmagent/config
          subpath: ""
          mountpropagation: null
          subpathexpr: ""
      livenessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8429
                scheme: HTTP
        initialdelayseconds: 0
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
        terminationgraceperiodseconds: null
      readinessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8429
                scheme: HTTP
        initialdelayseconds: 0
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
        terminationgraceperiodseconds: null
      terminationmessagepolicy: FallbackToLogsOnError
      imagepullpolicy: IfNotPresent

serviceaccountname: vmagent-agent
`)
}
