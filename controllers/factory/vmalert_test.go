package factory

import (
	"context"
	"reflect"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
					}},
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
			got, err := loadVMAlertRemoteSecrets(tt.args.cr, tt.args.SecretsInNS)
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
						RemoteWrite: &victoriametricsv1beta1.VMAlertRemoteWriteSpec{
							URL: "http://vm-insert-url",
							TLSConfig: &victoriametricsv1beta1.TLSConfig{
								CA: victoriametricsv1beta1.SecretOrConfigMap{
									ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
								},
								Cert: victoriametricsv1beta1.SecretOrConfigMap{
									ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
								},
								KeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "key"},
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
						RemoteWrite: &victoriametricsv1beta1.VMAlertRemoteWriteSpec{
							URL: "http://vm-insert-url",
							TLSConfig: &victoriametricsv1beta1.TLSConfig{
								CA: victoriametricsv1beta1.SecretOrConfigMap{
									ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
								},
								Cert: victoriametricsv1beta1.SecretOrConfigMap{
									ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
								},
								KeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "key"},
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
			want: []string{"-notifier.url=http://am-1,http://am-2,http://am-3", "-notifier.tlsKeyFile=,/tmp/key.pem,", "-notifier.tlsCertFile=,/tmp/cert.pem,", "-notifier.tlsCAFile=,/tmp/ca.cert,", "-notifier.tlsInsecureSkipVerify=true"},
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
