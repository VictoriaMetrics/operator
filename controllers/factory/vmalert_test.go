package factory

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func Test_loadVMAlertRemoteSecrets(t *testing.T) {
	type args struct {
		cr          *victoriametricsv1beta1.VMAlert
		SecretsInNS *corev1.SecretList
	}
	tests := []struct {
		name    string
		args    args
		want    map[string]BasicAuthCredentials
		wantErr bool
	}{
		{
			name: "test ok, secret found",
			args: args{
				cr: &victoriametricsv1beta1.VMAlert{
					Spec: victoriametricsv1beta1.VMAlertSpec{RemoteWrite: &victoriametricsv1beta1.VMAlertRemoteWriteSpec{
						HTTPAuth: victoriametricsv1beta1.HTTPAuth{
							BasicAuth: &victoriametricsv1beta1.BasicAuth{
								Password: corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "secret-1",
									},
									Key: "password",
								},
								Username: corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "secret-1",
									},
									Key: "username",
								},
							},
						}}},
				},
				SecretsInNS: &corev1.SecretList{
					Items: []corev1.Secret{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "secret-1"},
							Data:       map[string][]byte{"password": []byte("pass"), "username": []byte("user")},
						},
					},
				},
			},
			want: map[string]BasicAuthCredentials{
				"remoteWrite": {password: "pass", username: "user"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var predefinedObjets []runtime.Object
			for i := range tt.args.SecretsInNS.Items {
				predefinedObjets = append(predefinedObjets, &tt.args.SecretsInNS.Items[i])
			}
			testClient := k8stools.GetTestClientWithObjects(predefinedObjets)
			got, err := loadVMAlertRemoteSecrets(context.TODO(), testClient, tt.args.cr)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadVMAlertRemoteSecrets() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("loadVMAlertRemoteSecrets() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_loadTLSAssetsForVMAlert(t *testing.T) {
	type args struct {
		cr *victoriametricsv1beta1.VMAlert
	}
	tests := []struct {
		name              string
		args              args
		want              map[string]string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "base vmalert gen",
			args: args{
				cr: &victoriametricsv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAlertSpec{
						Notifier: &victoriametricsv1beta1.VMAlertNotifierSpec{
							URL: "http://some-alertmanager",
						},
						Datasource: victoriametricsv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-vm-datasource",
						},
					},
				},
			},
			want: map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := loadTLSAssetsForVMAlert(context.TODO(), fclient, tt.args.cr)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadTLSAssetsForVMAlert() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("loadTLSAssetsForVMAlert() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateOrUpdateVMAlert(t *testing.T) {
	type args struct {
		cr      *victoriametricsv1beta1.VMAlert
		c       *config.BaseOperatorConf
		cmNames []string
	}
	tests := []struct {
		name              string
		args              args
		want              reconcile.Result
		wantErr           bool
		predefinedObjects []runtime.Object
		validator         func(vma *v1.Deployment) error
	}{
		{
			name: "base-spec-gen",
			args: args{
				cr: &victoriametricsv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAlertSpec{
						Notifier: &victoriametricsv1beta1.VMAlertNotifierSpec{
							URL: "http://some-alertmanager",
						},
						Datasource: victoriametricsv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-vm-datasource",
						},
					},
				},
				c: config.MustGetBaseConfig(),
			},
		},
		{
			name: "base-spec-gen with externalLabels",
			args: args{
				cr: &victoriametricsv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAlertSpec{
						Notifier: &victoriametricsv1beta1.VMAlertNotifierSpec{
							URL: "http://some-alertmanager",
							TLSConfig: &victoriametricsv1beta1.TLSConfig{
								InsecureSkipVerify: true,
							},
						},
						Datasource: victoriametricsv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-vm-datasource",
							HTTPAuth: victoriametricsv1beta1.HTTPAuth{
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								}},
						},
						ExternalLabels: map[string]string{"label1": "value1", "label2": "value-2"},
					},
				},
				c: config.MustGetBaseConfig(),
			},
			validator: func(vma *v1.Deployment) error {
				var foundOk bool
				for _, cnt := range vma.Spec.Template.Spec.Containers {
					if cnt.Name == "vmalert" {
						args := cnt.Args
						for _, arg := range args {
							if strings.HasPrefix(arg, "-external.label") {
								foundOk = true
								kv := strings.ReplaceAll(arg, "-external.label=", "")
								if kv != "label1=value1" && kv != "label2=value-2" {
									return fmt.Errorf("unexepcted value for external.label arg: %s", kv)
								}
							}
						}
					}
				}
				if !foundOk {
					return fmt.Errorf("expected to found arg: -external.label at vmalert container")
				}
				return nil
			},
		},
		{
			name: "with-remote-tls",
			args: args{
				cr: &victoriametricsv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAlertSpec{
						Notifier: &victoriametricsv1beta1.VMAlertNotifierSpec{
							URL: "http://some-alertmanager",
							TLSConfig: &victoriametricsv1beta1.TLSConfig{
								CAFile:   "/tmp/ca",
								CertFile: "/tmp/cert",
								KeyFile:  "/tmp/key",
							},
						},
						Notifiers: []victoriametricsv1beta1.VMAlertNotifierSpec{
							{
								URL: "http://another-alertmanager",
							},
						},
						Datasource: victoriametricsv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-vm-datasource",
							HTTPAuth: victoriametricsv1beta1.HTTPAuth{
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									Cert: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									KeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "key"},
								},
							}},
						RemoteWrite: &victoriametricsv1beta1.VMAlertRemoteWriteSpec{
							URL: "http://vm-insert-url",
							HTTPAuth: victoriametricsv1beta1.HTTPAuth{
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									Cert: victoriametricsv1beta1.SecretOrConfigMap{
										ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									KeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "key"},
								},
							}},
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "datasource-tls",
						Namespace: "default",
					},
					Data: map[string][]byte{"ca": []byte(`sa`), "cert": []byte(`cert-data`), "key": []byte(`"key-data"`)},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "datasource-tls",
						Namespace: "default",
					},
					Data: map[string]string{"ca": "ca-data", "cert": "cert-data"},
				},
			},
		},
		{
			name: "with-notifiers-tls",
			args: args{
				cr: &victoriametricsv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAlertSpec{
						Notifiers: []victoriametricsv1beta1.VMAlertNotifierSpec{
							{
								URL: "http://another-alertmanager",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CAFile:   "/tmp/ca",
									CertFile: "/tmp/cert",
									KeyFile:  "/tmp/key",
								},
							},
						},
						Datasource: victoriametricsv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-vm-datasource",
							HTTPAuth: victoriametricsv1beta1.HTTPAuth{
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									Cert: victoriametricsv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									KeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "key"},
								},
							},
						},
						RemoteWrite: &victoriametricsv1beta1.VMAlertRemoteWriteSpec{
							URL: "http://vm-insert-url",
							HTTPAuth: victoriametricsv1beta1.HTTPAuth{
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									CA: victoriametricsv1beta1.SecretOrConfigMap{
										ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									Cert: victoriametricsv1beta1.SecretOrConfigMap{
										ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									KeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "key"},
								},
							}},
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "datasource-tls",
						Namespace: "default",
					},
					Data: map[string][]byte{"ca": []byte(`sa`), "cert": []byte(`cert-data`), "key": []byte(`"key-data"`)},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "datasource-tls",
						Namespace: "default",
					},
					Data: map[string]string{"ca": "ca-data", "cert": "cert-data"},
				},
			},
		},
		{
			name: "with tlsconfig insecure true",
			args: args{
				cr: &victoriametricsv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAlertSpec{
						Notifiers: []victoriametricsv1beta1.VMAlertNotifierSpec{
							{
								URL: "http://another-alertmanager",
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								},
							},
						},
						Datasource: victoriametricsv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-vm-datasource",
							HTTPAuth: victoriametricsv1beta1.HTTPAuth{
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								}},
						},
						RemoteWrite: &victoriametricsv1beta1.VMAlertRemoteWriteSpec{
							URL: "http://vm-insert-url",
							HTTPAuth: victoriametricsv1beta1.HTTPAuth{
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								}},
						},
						RemoteRead: &victoriametricsv1beta1.VMAlertRemoteReadSpec{
							URL: "http://vm-insert-url",
							HTTPAuth: victoriametricsv1beta1.HTTPAuth{
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								},
							},
						},
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "datasource-tls",
						Namespace: "default",
					},
					Data: map[string][]byte{"ca": []byte(`sa`), "cert": []byte(`cert-data`), "key": []byte(`"key-data"`)},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "datasource-tls",
						Namespace: "default",
					},
					Data: map[string]string{"ca": "ca-data", "cert": "cert-data"},
				},
			},
		},
		{
			name: "with notifier config",
			args: args{
				cr: &victoriametricsv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAlertSpec{
						NotifierConfigRef: &corev1.SecretKeySelector{
							Key: "cfg.yaml",
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "notifier-cfg",
							},
						},
						Datasource: victoriametricsv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-vm-datasource",
						},
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "notifier-cfg",
						Namespace: "default",
					},
					StringData: map[string]string{"cfg.yaml": "static: []"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := CreateOrUpdateVMAlert(context.TODO(), tt.args.cr, fclient, tt.args.c, tt.args.cmNames)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMAlert() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CreateOrUpdateVMAlert() got = %v, want %v", got, tt.want)
			}
			if tt.validator != nil {
				var generatedDeploment v1.Deployment
				if err := fclient.Get(context.TODO(), types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.PrefixedName()}, &generatedDeploment); err != nil {
					t.Fatalf("cannot find generated deployment: %v, err: %v", tt.args.cr.PrefixedName(), err)
				}
				if err := tt.validator(&generatedDeploment); err != nil {
					t.Fatalf("unexpetected error at deployment validation: %v", err)
				}
			}
		})
	}
}

func TestBuildNotifiers(t *testing.T) {
	type args struct {
		cr          *victoriametricsv1beta1.VMAlert
		ntBasicAuth map[string]BasicAuthCredentials
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "ok build args",
			args: args{
				cr: &victoriametricsv1beta1.VMAlert{
					Spec: victoriametricsv1beta1.VMAlertSpec{Notifiers: []victoriametricsv1beta1.VMAlertNotifierSpec{
						{
							URL: "http://am-1",
						},
						{
							URL: "http://am-2",
							TLSConfig: &victoriametricsv1beta1.TLSConfig{
								CAFile:             "/tmp/ca.cert",
								InsecureSkipVerify: true,
								KeyFile:            "/tmp/key.pem",
								CertFile:           "/tmp/cert.pem",
							},
						},
						{
							URL: "http://am-3",
						},
					}},
				},
			},
			want: []string{"-notifier.url=http://am-1,http://am-2,http://am-3", "-notifier.tlsKeyFile=,/tmp/key.pem,", "-notifier.tlsCertFile=,/tmp/cert.pem,", "-notifier.tlsCAFile=,/tmp/ca.cert,", "-notifier.tlsInsecureSkipVerify=false,true,false"},
		},
		{
			name: "ok build args with config",
			args: args{
				cr: &victoriametricsv1beta1.VMAlert{
					Spec: victoriametricsv1beta1.VMAlertSpec{
						NotifierConfigRef: &corev1.SecretKeySelector{
							Key: "cfg.yaml",
						},
					},
				},
			},
			want: []string{"-notifier.config=" + notifierConfigMountPath + "/cfg.yaml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildNotifiersArgs(tt.args.cr, tt.args.ntBasicAuth); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildNotifiersArgs() = \ngot \n%v, \nwant \n%v", got, tt.want)
			}
		})
	}
}

func TestCreateOrUpdateVMAlertService(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *victoriametricsv1beta1.VMAlert
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
			name: "base test",
			args: args{
				ctx: context.TODO(),
				c:   config.MustGetBaseConfig(),
				cr: &victoriametricsv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "base",
					},
				},
			},
			want: func(svc *corev1.Service) error {
				if svc.Name != "vmalert-base" {
					return fmt.Errorf("unexpected name for vmalert service: %v", svc.Name)
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := CreateOrUpdateVMAlertService(tt.args.ctx, tt.args.cr, cl, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateVMAlertService() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err := tt.want(got); err != nil {
				t.Errorf("CreateOrUpdateVMAlertService() unexpected error: %v", err)
			}
		})
	}
}

func Test_buildVMAlertArgs(t *testing.T) {
	type args struct {
		cr                 *victoriametricsv1beta1.VMAlert
		ruleConfigMapNames []string
		remoteSecrets      map[string]BasicAuthCredentials
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "basic args",
			args: args{
				cr: &victoriametricsv1beta1.VMAlert{
					Spec: victoriametricsv1beta1.VMAlertSpec{
						Datasource: victoriametricsv1beta1.VMAlertDatasourceSpec{
							URL: "http://vmsingle-url",
						},
					},
				},
				ruleConfigMapNames: []string{"first-rule-cm.yaml"},
				remoteSecrets:      map[string]BasicAuthCredentials{},
			},
			want: []string{"-datasource.url=http://vmsingle-url", "-httpListenAddr=:", "-notifier.url=", "-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\""},
		},
		{
			name: "with tls args",
			args: args{
				cr: &victoriametricsv1beta1.VMAlert{
					Spec: victoriametricsv1beta1.VMAlertSpec{
						Datasource: victoriametricsv1beta1.VMAlertDatasourceSpec{
							URL: "http://vmsingle-url",
							HTTPAuth: victoriametricsv1beta1.HTTPAuth{
								TLSConfig: &victoriametricsv1beta1.TLSConfig{
									InsecureSkipVerify: true,
									KeyFile:            "/path/to/key",
									CAFile:             "/path/to/sa",
								}},
							Headers: []string{"x-org-id:one", "x-org-tenant:5"},
						},
					},
				},
				ruleConfigMapNames: []string{"first-rule-cm.yaml"},
				remoteSecrets:      map[string]BasicAuthCredentials{},
			},
			want: []string{"--datasource.headers=x-org-id:one^^x-org-tenant:5", "-datasource.tlsCAFile=/path/to/sa", "-datasource.tlsInsecureSkipVerify=true", "-datasource.tlsKeyFile=/path/to/key", "-datasource.url=http://vmsingle-url", "-httpListenAddr=:", "-notifier.url=", "-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildVMAlertArgs(tt.args.cr, tt.args.ruleConfigMapNames, tt.args.remoteSecrets); !reflect.DeepEqual(got, tt.want) {
				assert.Equal(t, tt.want, got)
				t.Errorf("buildVMAlertArgs() got = \n%v\n, want \n%v\n", got, tt.want)
			}
		})
	}
}
