package factory

import (
	"context"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/conf"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
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
		// TODO: Add test cases.
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
						Notifier: victoriametricsv1beta1.VMAlertNotifierSpec{
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
			obj := []runtime.Object{}
			obj = append(obj, tt.predefinedObjects...)
			fclient := fake.NewFakeClientWithScheme(testGetScheme(), obj...)
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
		c       *conf.BaseOperatorConf
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
						Notifier: victoriametricsv1beta1.VMAlertNotifierSpec{
							URL: "http://some-alertmanager",
						},
						Datasource: victoriametricsv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-vm-datasource",
						},
					},
				},
				c: conf.MustGetBaseConfig(),
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
						Notifier: victoriametricsv1beta1.VMAlertNotifierSpec{
							URL: "http://some-alertmanager",
							TLSConfig: &victoriametricsv1beta1.TLSConfig{
								CAFile:   "/tmp/ca",
								CertFile: "/tmp/cert",
								KeyFile:  "/tmp/key",
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
				c: conf.MustGetBaseConfig(),
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
			obj := []runtime.Object{}
			obj = append(obj, tt.predefinedObjects...)
			fclient := fake.NewFakeClientWithScheme(testGetScheme(), obj...)
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
