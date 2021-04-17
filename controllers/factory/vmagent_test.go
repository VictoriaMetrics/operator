package factory

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestCreateOrUpdateVMAgent(t *testing.T) {
	type args struct {
		cr *victoriametricsv1beta1.VMAgent
		c  *config.BaseOperatorConf
	}
	tests := []struct {
		name              string
		args              args
		want              reconcile.Result
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "generate base vmagent",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-agent",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						RemoteWrite: []victoriametricsv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://remote-write"},
						},
					},
				},
			},
		},
		{
			name: "generate with shards vmagent",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-agent",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						RemoteWrite: []victoriametricsv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://remote-write"},
						},
						ShardCount: func() *int { i := 2; return &i }(),
					},
				},
			},
		},
		{
			name: "generate vmagent with bauth-secret",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-agent-bauth",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						RemoteWrite: []victoriametricsv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://remote-write"},
						},
						ServiceScrapeSelector: &metav1.LabelSelector{},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "bauth-secret", Namespace: "default"},
					Data:       map[string][]byte{"user": []byte(`user-name`), "password": []byte(`user-password`)},
				},
				&victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmsingle-monitor",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
						Selector: metav1.LabelSelector{},
						Endpoints: []victoriametricsv1beta1.Endpoint{
							{
								Interval: "30s",
								Scheme:   "http",
								BasicAuth: &victoriametricsv1beta1.BasicAuth{
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
			name: "generate vmagent with tls-secret",
			args: args{
				c: config.MustGetBaseConfig(),
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "example-agent-tls",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						RemoteWrite: []victoriametricsv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://remote-write"},
							{URL: "http://remote-write2",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "remote2-secret",
											},
											Key: "ca",
										},
									},
									Cert: victoriametricsv1beta1.SecretOrConfigMap{
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
							{URL: "http://remote-write3",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										ConfigMap: &corev1.ConfigMapKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "remote3-cm",
											},
											Key: "ca",
										},
									},
									Cert: victoriametricsv1beta1.SecretOrConfigMap{
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
								}},
							{URL: "http://remote-write4",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{CertFile: "/tmp/cert1", KeyFile: "/tmp/key1", CAFile: "/tmp/ca"}},
						},
						ServiceScrapeSelector: &metav1.LabelSelector{},
					},
				},
			},
			predefinedObjects: []runtime.Object{
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
				&victoriametricsv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmalert-monitor",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
						Selector: metav1.LabelSelector{},
						Endpoints: []victoriametricsv1beta1.Endpoint{
							{
								Interval: "30s",
								Scheme:   "https",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tls-scrape"}, Key: "ca"},
									},
									Cert: victoriametricsv1beta1.SecretOrConfigMap{
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			got, err := CreateOrUpdateVMAgent(context.TODO(), tt.args.cr, fclient, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMAgent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CreateOrUpdateVMAgent() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_addAddtionalScrapeConfigOwnership(t *testing.T) {
	type args struct {
		cr *victoriametricsv1beta1.VMAgent
		l  logr.Logger
	}
	tests := []struct {
		name              string
		args              args
		wantErr           bool
		predefinedObjects *corev1.SecretList
	}{
		{
			name: "append ownership to secret",
			args: args{
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmagent-1",
						Namespace: "ns-1",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						AdditionalScrapeConfigs: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "secret-1"}}},
				},
				l: logf.Log.WithName("test"),
			},
			predefinedObjects: &corev1.SecretList{
				Items: []corev1.Secret{
					{ObjectMeta: metav1.ObjectMeta{Name: "secret-1", Namespace: "ns-1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "secret-2", Namespace: "ns-2"}},
				},
			},
		},
		{
			name: "empty scrape config - nothing todo",
			args: args{
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmagent-1",
						Namespace: "ns-1",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{},
				},
				l: logf.Log.WithName("test"),
			},
			predefinedObjects: &corev1.SecretList{
				Items: []corev1.Secret{
					{ObjectMeta: metav1.ObjectMeta{Name: "secret-1", Namespace: "ns-1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "secret-2", Namespace: "ns-2"}},
				},
			},
		},
		{
			name: "ownership exists - nothing todo",
			args: args{
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmagent-2",
						Namespace: "ns-2",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						AdditionalScrapeConfigs: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "secret-2"}}},
				},
				l: logf.Log.WithName("test"),
			},
			predefinedObjects: &corev1.SecretList{
				Items: []corev1.Secret{
					{ObjectMeta: metav1.ObjectMeta{Name: "secret-1", Namespace: "ns-1"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "secret-2", Namespace: "ns-2", OwnerReferences: []metav1.OwnerReference{
						{Name: "vmagent-2"},
					}}},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			obj := []runtime.Object{}
			for _, secret := range tt.predefinedObjects.Items {
				localSecret := secret
				obj = append(obj, &localSecret)
			}
			fclient := k8stools.GetTestClientWithObjects(obj)

			if err := addAddtionalScrapeConfigOwnership(tt.args.cr, fclient, tt.args.l); (err != nil) != tt.wantErr {
				t.Errorf("addAddtionalScrapeConfigOwnership() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.args.cr.Spec.AdditionalScrapeConfigs != nil {
				secret := &corev1.Secret{}
				var refFound bool
				err := fclient.Get(context.TODO(), types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.Spec.AdditionalScrapeConfigs.Name}, secret)
				if err != nil {
					t.Errorf("cannot find secret for scrape config")
				}
				for _, ownerRef := range secret.OwnerReferences {
					if ownerRef.Name == tt.args.cr.Name {
						refFound = true
					}
				}
				if !refFound {
					t.Errorf("cannot find secret ownership for vmagent: %s,secret name: %v", tt.args.cr.Name, tt.args.cr.Spec.AdditionalScrapeConfigs.Name)
				}
			}

		})
	}
}

func Test_loadTLSAssets(t *testing.T) {
	type args struct {
		monitors map[string]*victoriametricsv1beta1.VMServiceScrape
		pods     map[string]*victoriametricsv1beta1.VMPodScrape
		cr       *victoriametricsv1beta1.VMAgent
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
				cr: &victoriametricsv1beta1.VMAgent{
					Spec: victoriametricsv1beta1.VMAgentSpec{},
				},
				monitors: map[string]*victoriametricsv1beta1.VMServiceScrape{
					"vmagent-monitor": {
						ObjectMeta: metav1.ObjectMeta{Name: "vmagent-monitor", Namespace: "default"},
						Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
							Endpoints: []victoriametricsv1beta1.Endpoint{
								{
									TLSConfig: &victoriametricsv1beta1.TLSConfig{
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
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmagent-test-1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						RemoteWrite: []victoriametricsv1beta1.VMAgentRemoteWriteSpec{
							{
								URL: "some1-url",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "remote1-write-spec",
											},
											Key: "ca",
										},
									},
									Cert: victoriametricsv1beta1.SecretOrConfigMap{
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
				monitors: map[string]*victoriametricsv1beta1.VMServiceScrape{
					"vmagent-monitor": {
						ObjectMeta: metav1.ObjectMeta{Name: "vmagent-monitor", Namespace: "default"},
						Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
							Endpoints: []victoriametricsv1beta1.Endpoint{
								{TLSConfig: &victoriametricsv1beta1.TLSConfig{
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

			got, err := loadTLSAssets(context.TODO(), fclient, tt.args.cr, tt.args.monitors, tt.args.pods)
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
		cr           *victoriametricsv1beta1.VMAgent
		rwsBasicAuth map[string]BasicAuthCredentials
		rwsTokens    map[string]BearerToken
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "test labels",
			args: args{
				cr: &victoriametricsv1beta1.VMAgent{
					Spec: victoriametricsv1beta1.VMAgentSpec{RemoteWrite: []victoriametricsv1beta1.VMAgentRemoteWriteSpec{
						{
							URL:    "localhost:8429",
							Labels: map[string]string{"label1": "value1", "label2": "value2"},
						},
					}},
				},
			},
			want: []string{"-remoteWrite.url=localhost:8429", "-remoteWrite.label=label1=value1,label2=value2"},
		},
		{
			name: "test with tls config full",
			args: args{
				cr: &victoriametricsv1beta1.VMAgent{
					Spec: victoriametricsv1beta1.VMAgentSpec{RemoteWrite: []victoriametricsv1beta1.VMAgentRemoteWriteSpec{
						{
							URL:    "localhost:8429",
							Labels: map[string]string{"label1": "value1", "label2": "value2"},
							TLSConfig: &victoriametricsv1beta1.TLSConfig{
								CA: victoriametricsv1beta1.SecretOrConfigMap{Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
									Key: "ca",
								}},
							},
						},
						{
							URL: "localhost:8429",
							TLSConfig: &victoriametricsv1beta1.TLSConfig{
								CAFile: "/path/to_ca",
								Cert: victoriametricsv1beta1.SecretOrConfigMap{Secret: &corev1.SecretKeySelector{
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
			want: []string{"-remoteWrite.label=label1=value1,label2=value2", "-remoteWrite.tlsCAFile=/etc/vmagent-tls/certs_tls-secret_ca,/path/to_ca", "-remoteWrite.tlsCertFile=,/etc/vmagent-tls/certs_tls-secret_ca", "-remoteWrite.url=localhost:8429,localhost:8429"},
		},
		{
			name: "test insecure with key only",
			args: args{
				cr: &victoriametricsv1beta1.VMAgent{
					Spec: victoriametricsv1beta1.VMAgentSpec{RemoteWrite: []victoriametricsv1beta1.VMAgentRemoteWriteSpec{
						{
							URL:    "localhost:8429",
							Labels: map[string]string{"label1": "value1", "label2": "value2"},
							TLSConfig: &victoriametricsv1beta1.TLSConfig{
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
			want: []string{"-remoteWrite.url=localhost:8429", "-remoteWrite.tlsInsecureSkipVerify=true", "-remoteWrite.tlsKeyFile=/etc/vmagent-tls/certs_tls-secret_key", "-remoteWrite.label=label1=value1,label2=value2"},
		},
		{
			name: "test insecure",
			args: args{
				cr: &victoriametricsv1beta1.VMAgent{
					Spec: victoriametricsv1beta1.VMAgentSpec{RemoteWrite: []victoriametricsv1beta1.VMAgentRemoteWriteSpec{
						{
							URL:    "localhost:8429",
							Labels: map[string]string{"label1": "value1", "label2": "value2"},
							TLSConfig: &victoriametricsv1beta1.TLSConfig{
								InsecureSkipVerify: true,
							},
						},
					}},
				},
			},
			want: []string{"-remoteWrite.url=localhost:8429", "-remoteWrite.tlsInsecureSkipVerify=true", "-remoteWrite.label=label1=value1,label2=value2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sort.Strings(tt.want)
			got := BuildRemoteWrites(tt.args.cr, tt.args.rwsBasicAuth, tt.args.rwsTokens)
			sort.Strings(got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildRemoteWrites() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateOrUpdateVMAgentService(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *victoriametricsv1beta1.VMAgent
		c   *config.BaseOperatorConf
	}
	tests := []struct {
		name              string
		args              args
		want              func(svc *corev1.Service) error
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "base case",
			args: args{
				ctx: context.TODO(),
				c:   config.MustGetBaseConfig(),
				cr: &victoriametricsv1beta1.VMAgent{
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
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "base",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						InsertPorts: &victoriametricsv1beta1.InsertPorts{
							InfluxPort: "8011",
						},
						ServiceSpec: &victoriametricsv1beta1.ServiceSpec{
							EmbeddedObjectMetadata: victoriametricsv1beta1.EmbeddedObjectMetadata{Name: "extra-svc"},
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
		})
	}
}

func TestCreateOrUpdateRelabelConfigsAssets(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *victoriametricsv1beta1.VMAgent
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
				cr: &victoriametricsv1beta1.VMAgent{
					Spec: victoriametricsv1beta1.VMAgentSpec{
						InlineRelabelConfig: []victoriametricsv1beta1.RelabelConfig{
							{
								Regex:        ".*",
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
				cr: &victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmag",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAgentSpec{
						InlineRelabelConfig: []victoriametricsv1beta1.RelabelConfig{
							{
								Regex:        ".*",
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
			if err := CreateOrUpdateRelabelConfigsAssets(tt.args.ctx, tt.args.cr, cl); (err != nil) != tt.wantErr {
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
