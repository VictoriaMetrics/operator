package factory

import (
	"context"
	"reflect"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/conf"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestCreateOrUpdateVMAgent(t *testing.T) {
	type args struct {
		cr *victoriametricsv1beta1.VMAgent
		c  *conf.BaseOperatorConf
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
				c: conf.MustGetBaseConfig(),
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
			name: "generate vmagent with bauth-secret",
			args: args{
				c: conf.MustGetBaseConfig(),
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
				c: conf.MustGetBaseConfig(),
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
			obj := []runtime.Object{}
			obj = append(obj, tt.predefinedObjects...)
			fclient := fake.NewFakeClientWithScheme(testGetScheme(), obj...)

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
			fclient := fake.NewFakeClientWithScheme(testGetScheme(), obj...)

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
			obj := []runtime.Object{}
			obj = append(obj, tt.predefinedObjects...)
			fclient := fake.NewFakeClientWithScheme(testGetScheme(), obj...)

			got, err := loadTLSAssets(context.TODO(), fclient, tt.args.cr, tt.args.monitors)
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
