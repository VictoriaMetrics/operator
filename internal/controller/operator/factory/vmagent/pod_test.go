package vmagent

import (
	"path"
	"reflect"
	"sort"
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

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
								InlineUrlRelabelConfig: []*vmv1beta1.RelabelConfig{
									{TargetLabel: "rw-1", Replacement: ptr.To("present")},
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
								InlineUrlRelabelConfig: []*vmv1beta1.RelabelConfig{
									{TargetLabel: "rw-2", Replacement: ptr.To("present")},
								},
							},
						},
						InlineRelabelConfig: []*vmv1beta1.RelabelConfig{
							{TargetLabel: "dst", Replacement: ptr.To("ok")},
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
			name: "test maxDiskUsage",
			args: args{
				ssCache: &scrapesSecretsCache{},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL:          "localhost:8429",
							MaxDiskUsage: ptr.To(vmv1beta1.BytesString("1500MB")),
						},
						{
							URL:          "localhost:8431",
							MaxDiskUsage: ptr.To(vmv1beta1.BytesString("500MB")),
						},
						{
							URL: "localhost:8432",
						},
					}},
				},
			},
			want: []string{"-remoteWrite.url=localhost:8429,localhost:8431,localhost:8432", "-remoteWrite.maxDiskUsagePerURL=1500MB,500MB,1073741824"},
		},
		{
			name: "test forceVMProto",
			args: args{
				ssCache: &scrapesSecretsCache{},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL:          "localhost:8429",
							ForceVMProto: true,
						},
						{
							URL: "localhost:8431",
						},
						{
							URL:          "localhost:8432",
							ForceVMProto: true,
						},
					}},
				},
			},
			want: []string{"-remoteWrite.url=localhost:8429,localhost:8431,localhost:8432", "-remoteWrite.forceVMProto=true,false,true"},
		},
		{
			name: "test oauth2",
			args: args{
				ssCache: &scrapesSecretsCache{
					oauth2Secrets: map[string]*k8stools.OAuthCreds{func() string {
						rws := vmv1beta1.VMAgentRemoteWriteSpec{
							URL: "localhost:8431",
						}
						return rws.AsMapKey()
					}(): {
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
								Scopes:   []string{"scope-1"},
								TokenURL: "http://some-url",
								ClientSecret: &corev1.SecretKeySelector{
									Key: "some-secret",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "some-cm",
									},
								},
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
		{
			name: "test with proxyURL (one remote write with defaults)",
			args: args{
				ssCache: &scrapesSecretsCache{},
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{
								URL: "http://localhost:8431",
							},
							{
								URL:      "http://localhost:8432",
								ProxyURL: ptr.To("http://proxy.example.com"),
							},
							{
								URL: "http://localhost:8433",
							},
						},
					},
				},
			},
			want: []string{
				`-remoteWrite.proxyURL=,http://proxy.example.com,`,
				`-remoteWrite.url=http://localhost:8431,http://localhost:8432,http://localhost:8433`,
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

func Test_buildConfigReloaderArgs(t *testing.T) {
	type args struct {
		cr *vmv1beta1.VMAgent
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
					Spec: vmv1beta1.VMAgentSpec{
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{Port: "8429"},
					},
				},
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
					Spec: vmv1beta1.VMAgentSpec{
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{Port: "8429"},

						IngestOnlyMode: true},
				},
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
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{Port: "8429"},
						IngestOnlyMode:          false,
						InlineRelabelConfig:     []*vmv1beta1.RelabelConfig{{TargetLabel: "test"}},
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
			},
			want: []string{
				"--reload-url=http://localhost:8429/-/reload",
				"--config-file=/etc/vmagent/config/vmagent.yaml.gz",
				"--config-envsubst-file=/etc/vmagent/config_out/vmagent.env.yaml",
				"--watched-dir=/etc/vm/relabeling",
				"--watched-dir=/etc/vm/stream-aggr",
			},
		},
		{
			name: "with configMaps mount",
			args: args{
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{Port: "8429"},
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ConfigMaps: []string{"cm-0", "cm-1"},
						},
						IngestOnlyMode:      false,
						InlineRelabelConfig: []*vmv1beta1.RelabelConfig{{TargetLabel: "test"}},
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
			},
			want: []string{
				"--reload-url=http://localhost:8429/-/reload",
				"--config-file=/etc/vmagent/config/vmagent.yaml.gz",
				"--config-envsubst-file=/etc/vmagent/config_out/vmagent.env.yaml",
				"--watched-dir=/etc/vm/configs/cm-0",
				"--watched-dir=/etc/vm/configs/cm-1",
				"--watched-dir=/etc/vm/relabeling",
				"--watched-dir=/etc/vm/stream-aggr",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var exvms []corev1.VolumeMount
			for _, cm := range tt.args.cr.Spec.ConfigMaps {
				exvms = append(exvms, corev1.VolumeMount{
					Name:      k8stools.SanitizeVolumeName("configmap-" + cm),
					ReadOnly:  true,
					MountPath: path.Join(vmv1beta1.ConfigMapsDir, cm),
				})
			}
			got := buildConfigReloaderArgs(tt.args.cr, exvms)
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
							MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("1000")),
							UseMultiTenantMode: true,
						},
					},
				},
			},
			want: []string{"-remoteWrite.maxDiskUsagePerURL=1000", "-remoteWrite.tmpDataPath=/tmp/my-path", "-remoteWrite.showURL=true", "-enableMultitenantHandlers=true"},
		},
		{
			name: "maxDiskUsage already set in RemoteWriteSpec",
			args: args{
				cr: &vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{
								URL:          "localhost:8431",
								MaxDiskUsage: ptr.To(vmv1beta1.BytesString("500MB")),
							},
						},
						RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
							MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("1000")),
						},
					},
				},
			},
			want: []string{"-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data"},
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
	f := func(cr *vmv1beta1.VMAgent, sCache *scrapesSecretsCache, wantYaml string) {
		t.Helper()

		scheme := k8stools.GetTestClientWithObjects(nil).Scheme()
		build.AddDefaults(scheme)
		scheme.Default(cr)
		// this trick allows to omit empty fields for yaml
		var wantSpec corev1.PodSpec
		if err := yaml.Unmarshal([]byte(wantYaml), &wantSpec); err != nil {
			t.Fatalf("not expected wantYaml: %q: \n%q", wantYaml, err)
		}
		wantYAMLForCompare, err := yaml.Marshal(wantSpec)
		if err != nil {
			t.Fatalf("BUG: cannot parse as yaml: %q", err)
		}
		got, err := newPodSpec(cr, sCache)
		if err != nil {
			t.Fatalf("not expected error=%q", err)
		}
		gotYAML, err := yaml.Marshal(got)
		if err != nil {
			t.Fatalf("cannot parse got as yaml: %q", err)
		}

		assert.Equal(t, string(wantYAMLForCompare), string(gotYAML))
	}
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: vmv1beta1.VMAgentSpec{
			IngestOnlyMode: true,
			CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
				Image: vmv1beta1.Image{
					Repository: "vm-repo",
					Tag:        "v1.97.1",
				},
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10m"),
						corev1.ResourceMemory: resource.MustParse("10Mi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10m"),
						corev1.ResourceMemory: resource.MustParse("10Mi"),
					},
				},
				Port: "8425",
			},
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				UseVMConfigReloader:    ptr.To(true),
				ConfigReloaderImageTag: "vmcustom:config-reloader-v0.35.0",
			},
		},
	}, nil, `
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
        - -httpListenAddr=:8425
        - -remoteWrite.maxDiskUsagePerURL=1073741824
        - -remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data
      ports:
        - name: http
          hostport: 0
          containerport: 8425
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
                    intval: 8425
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
                    intval: 8425
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
		Spec: vmv1beta1.VMAgentSpec{
			IngestOnlyMode: false,
			CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
				Image: vmv1beta1.Image{
					Tag: "v1.97.1",
				},
				UseDefaultResources: ptr.To(false),
				Port:                "8429",
			},
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				UseVMConfigReloader:    ptr.To(true),
				ConfigReloaderImageTag: "vmcustomer:v1",
			},
		},
	}, nil, `
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
        timeoutseconds: 1
        periodseconds: 10
        successthreshold: 1
        failurethreshold: 3
      readinessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8435
                scheme: HTTP
        initialdelayseconds: 5
        timeoutseconds: 1
        periodseconds: 10
        successthreshold: 1
        failurethreshold: 3
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

	// test maxDiskUsage and empty remoteWriteSettings
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: vmv1beta1.VMAgentSpec{
			IngestOnlyMode: true,
			CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
				Image: vmv1beta1.Image{
					Tag: "v1.97.1",
				},
				UseDefaultResources: ptr.To(false),
				Port:                "8425",
			},
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				UseVMConfigReloader:    ptr.To(true),
				ConfigReloaderImageTag: "vmcustom:config-reloader-v0.35.0",
			},
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL:          "http://some-url/api/v1/write",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
				},
				{
					URL:          "http://some-url-2/api/v1/write",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
				},
				{
					URL: "http://some-url-3/api/v1/write",
				},
			},
		},
	}, nil, `
volumes:
    - name: persistent-queue-data
      volumesource:
        emptydir:
            medium: ""
            sizelimit: null
initcontainers: []
containers:
    - name: vmagent
      image: victoriametrics/vmagent:v1.97.1
      args:
        - -httpListenAddr=:8425
        - -remoteWrite.maxDiskUsagePerURL=10GB,10GB,1073741824
        - -remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data
        - -remoteWrite.url=http://some-url/api/v1/write,http://some-url-2/api/v1/write,http://some-url-3/api/v1/write
      ports:
        - name: http
          hostport: 0
          containerport: 8425
          protocol: TCP
      resources:
        limits: {}
        requests: {}
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
                    intval: 8425
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
                    intval: 8425
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

	// test MaxDiskUsage with RemoteWriteSettings
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: vmv1beta1.VMAgentSpec{
			IngestOnlyMode: true,
			CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
				Image: vmv1beta1.Image{
					Tag: "v1.97.1",
				},
				UseDefaultResources: ptr.To(false),
				Port:                "8425",
			},
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				UseVMConfigReloader:    ptr.To(true),
				ConfigReloaderImageTag: "vmcustom:config-reloader-v0.35.0",
			},
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL:          "http://some-url/api/v1/write",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
				},
				{
					URL: "http://some-url-2/api/v1/write",
				},
				{
					URL:          "http://some-url-3/api/v1/write",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
				},
			},
			RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
				MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("20MB")),
			},
		},
	}, nil, `
volumes:
    - name: persistent-queue-data
      volumesource:
        emptydir:
            medium: ""
            sizelimit: null
initcontainers: []
containers:
    - name: vmagent
      image: victoriametrics/vmagent:v1.97.1
      args:
        - -httpListenAddr=:8425
        - -remoteWrite.maxDiskUsagePerURL=10GB,20MB,10GB
        - -remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data
        - -remoteWrite.url=http://some-url/api/v1/write,http://some-url-2/api/v1/write,http://some-url-3/api/v1/write
      ports:
        - name: http
          hostport: 0
          containerport: 8425
          protocol: TCP
      resources:
        limits: {}
        requests: {}
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
                    intval: 8425
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
                    intval: 8425
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
	// test MaxDiskUsage with RemoteWriteSettings and extraArgs overwrite
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: vmv1beta1.VMAgentSpec{
			IngestOnlyMode: true,
			CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
				Image: vmv1beta1.Image{
					Tag: "v1.97.1",
				},
				UseDefaultResources: ptr.To(false),
				Port:                "8425",
			},
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				UseVMConfigReloader:    ptr.To(true),
				ConfigReloaderImageTag: "vmcustom:config-reloader-v0.35.0",
			},
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ExtraArgs: map[string]string{
					"remoteWrite.maxDiskUsagePerURL": "35GiB",
					"remoteWrite.forceVMProto":       "false",
				},
			},
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL:          "http://some-url/api/v1/write",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
				},
				{
					URL:          "http://some-url-2/api/v1/write",
					ForceVMProto: true,
				},
				{
					URL:          "http://some-url-3/api/v1/write",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
				},
			},
			RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
				MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("20MB")),
			},
		},
	}, nil, `
volumes:
    - name: persistent-queue-data
      volumesource:
        emptydir:
            medium: ""
            sizelimit: null
initcontainers: []
containers:
    - name: vmagent
      image: victoriametrics/vmagent:v1.97.1
      args:
        - -httpListenAddr=:8425
        - -remoteWrite.forceVMProto=false
        - -remoteWrite.maxDiskUsagePerURL=35GiB
        - -remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data
        - -remoteWrite.url=http://some-url/api/v1/write,http://some-url-2/api/v1/write,http://some-url-3/api/v1/write
      ports:
        - name: http
          hostport: 0
          containerport: 8425
          protocol: TCP
      resources:
        limits: {}
        requests: {}
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
                    intval: 8425
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
                    intval: 8425
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

}
