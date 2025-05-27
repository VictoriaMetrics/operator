package vmalert

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func Test_loadVMAlertRemoteSecrets(t *testing.T) {
	type args struct {
		cr          *vmv1beta1.VMAlert
		SecretsInNS *corev1.SecretList
	}
	tests := []struct {
		name    string
		args    args
		want    map[string]*authSecret
		wantErr bool
	}{
		{
			name: "test ok, secret found",
			args: args{
				cr: &vmv1beta1.VMAlert{
					Spec: vmv1beta1.VMAlertSpec{RemoteWrite: &vmv1beta1.VMAlertRemoteWriteSpec{
						HTTPAuth: vmv1beta1.HTTPAuth{
							BasicAuth: &vmv1beta1.BasicAuth{
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
			want: map[string]*authSecret{
				"remoteWrite": {BasicAuthCredentials: &k8stools.BasicAuthCredentials{Password: "pass", Username: "user"}},
				"datasource":  {},
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

func Test_loadTLSAssets(t *testing.T) {
	type args struct {
		cr *vmv1beta1.VMAlert
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
				cr: &vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAlertSpec{
						Notifier: &vmv1beta1.VMAlertNotifierSpec{
							URL: "http://some-alertmanager",
						},
						Datasource: vmv1beta1.VMAlertDatasourceSpec{
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
			got, err := loadTLSAssets(context.TODO(), fclient, tt.args.cr)
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

func TestCreateOrUpdate(t *testing.T) {
	type args struct {
		cr *vmv1beta1.VMAlert
		c  *config.BaseOperatorConf
	}
	tests := []struct {
		name              string
		args              args
		want              reconcile.Result
		wantErr           bool
		predefinedObjects []runtime.Object
		validator         func(vma *appsv1.Deployment) error
	}{
		{
			name: "base-spec-gen",
			args: args{
				cr: &vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAlertSpec{
						Notifier: &vmv1beta1.VMAlertNotifierSpec{
							URL: "http://some-alertmanager",
						},
						Datasource: vmv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-vm-datasource",
						},
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "vmalert-basic-vmalert",
					},
					Status: appsv1.DeploymentStatus{
						Conditions: []appsv1.DeploymentCondition{
							{
								Reason: "NewReplicaSetAvailable",
								Type:   appsv1.DeploymentProgressing,
								Status: "True",
							},
						},
					},
				},
			},
		},
		{
			name: "base-spec-gen with externalLabels",
			args: args{
				cr: &vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAlertSpec{
						Notifier: &vmv1beta1.VMAlertNotifierSpec{
							URL: "http://some-alertmanager",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								},
							},
						},
						Datasource: vmv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-vm-datasource",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								},
							},
						},
						ExternalLabels: map[string]string{"label1": "value1", "label2": "value-2"},
					},
				},
				c: config.MustGetBaseConfig(),
			},
			predefinedObjects: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "vmalert-basic-vmalert",
					},
					Status: appsv1.DeploymentStatus{
						Conditions: []appsv1.DeploymentCondition{
							{
								Reason: "NewReplicaSetAvailable",
								Type:   appsv1.DeploymentProgressing,
								Status: "True",
							},
						},
					},
				},
			},

			validator: func(vma *appsv1.Deployment) error {
				var foundOk bool
				for _, cnt := range vma.Spec.Template.Spec.Containers {
					if cnt.Name == "vmalert" {
						args := cnt.Args
						for _, arg := range args {
							if strings.HasPrefix(arg, "-external.label") {
								foundOk = true
								kv := strings.ReplaceAll(arg, "-external.label=", "")
								if kv != "label1=value1" && kv != "label2=value-2" {
									return fmt.Errorf("unexpected value for external.label arg: %s", kv)
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
				cr: &vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAlertSpec{
						Notifier: &vmv1beta1.VMAlertNotifierSpec{
							URL: "http://some-alertmanager",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									CAFile:   "/tmp/ca",
									CertFile: "/tmp/cert",
									KeyFile:  "/tmp/key",
								},
							},
						},
						Notifiers: []vmv1beta1.VMAlertNotifierSpec{
							{
								URL: "http://another-alertmanager",
							},
						},
						Datasource: vmv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-vm-datasource",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									CA: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									KeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "key"},
								},
							},
						},
						RemoteWrite: &vmv1beta1.VMAlertRemoteWriteSpec{
							URL: "http://vm-insert-url",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									CA: vmv1beta1.SecretOrConfigMap{
										ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									KeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "key"},
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
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "vmalert-basic-vmalert",
					},
					Status: appsv1.DeploymentStatus{
						Conditions: []appsv1.DeploymentCondition{
							{
								Reason: "NewReplicaSetAvailable",
								Type:   appsv1.DeploymentProgressing,
								Status: "True",
							},
						},
					},
				},
			},
		},
		{
			name: "with-notifiers-tls",
			args: args{
				cr: &vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAlertSpec{
						Notifiers: []vmv1beta1.VMAlertNotifierSpec{
							{
								URL: "http://another-alertmanager",
								HTTPAuth: vmv1beta1.HTTPAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										CAFile:   "/tmp/ca",
										CertFile: "/tmp/cert",
										KeyFile:  "/tmp/key",
									},
								},
							},
						},
						Datasource: vmv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-vm-datasource",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									CA: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									KeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "key"},
								},
							},
						},
						RemoteWrite: &vmv1beta1.VMAlertRemoteWriteSpec{
							URL: "http://vm-insert-url",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									CA: vmv1beta1.SecretOrConfigMap{
										ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "ca"},
									},
									KeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "key"},
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
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "vmalert-basic-vmalert",
					},
					Status: appsv1.DeploymentStatus{
						Conditions: []appsv1.DeploymentCondition{
							{
								Reason: "NewReplicaSetAvailable",
								Type:   appsv1.DeploymentProgressing,
								Status: "True",
							},
						},
					},
				},
			},
		},
		{
			name: "with tlsconfig insecure true",
			args: args{
				cr: &vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAlertSpec{
						Notifiers: []vmv1beta1.VMAlertNotifierSpec{
							{
								URL: "http://another-alertmanager",
								HTTPAuth: vmv1beta1.HTTPAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										InsecureSkipVerify: true,
									},
								},
							},
						},
						Datasource: vmv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-vm-datasource",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								},
							},
						},
						RemoteWrite: &vmv1beta1.VMAlertRemoteWriteSpec{
							URL: "http://vm-insert-url",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									InsecureSkipVerify: true,
								},
							},
						},
						RemoteRead: &vmv1beta1.VMAlertRemoteReadSpec{
							URL: "http://vm-insert-url",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
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
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "vmalert-basic-vmalert",
					},
					Status: appsv1.DeploymentStatus{
						Conditions: []appsv1.DeploymentCondition{
							{
								Reason: "NewReplicaSetAvailable",
								Type:   appsv1.DeploymentProgressing,
								Status: "True",
							},
						},
					},
				},
			},
		},
		{
			name: "with notifier config",
			args: args{
				cr: &vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-vmalert",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAlertSpec{
						NotifierConfigRef: &corev1.SecretKeySelector{
							Key: "cfg.yaml",
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "notifier-cfg",
							},
						},
						Datasource: vmv1beta1.VMAlertDatasourceSpec{
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
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "vmalert-basic-vmalert",
					},
					Status: appsv1.DeploymentStatus{
						Conditions: []appsv1.DeploymentCondition{
							{
								Reason: "NewReplicaSetAvailable",
								Type:   appsv1.DeploymentProgressing,
								Status: "True",
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
			err := CreateOrUpdate(context.TODO(), tt.args.cr, fclient)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.validator != nil {
				var generatedDeploment appsv1.Deployment
				if err := fclient.Get(context.TODO(), types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.PrefixedName()}, &generatedDeploment); err != nil {
					t.Fatalf("cannot find generated deployment: %v, err: %v", tt.args.cr.PrefixedName(), err)
				}
				if err := tt.validator(&generatedDeploment); err != nil {
					t.Fatalf("unexpected error at deployment validation: %v", err)
				}
			}
		})
	}
}

func TestCreateOrUpdateService(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *vmv1beta1.VMAlert
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
				cr: &vmv1beta1.VMAlert{
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
			got, err := createOrUpdateService(tt.args.ctx, cl, tt.args.cr, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateService() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err := tt.want(got); err != nil {
				t.Errorf("CreateOrUpdateService() unexpected error: %v", err)
			}
		})
	}
}
