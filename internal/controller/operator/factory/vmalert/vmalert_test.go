package vmalert

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	tests := []struct {
		name              string
		cr                *vmv1beta1.VMAlert
		cmNames           []string
		wantErr           bool
		predefinedObjects []runtime.Object
		validator         func(*appsv1.Deployment, *corev1.Secret) error
	}{
		{
			name: "base-spec-gen",
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
			validator: func(d *appsv1.Deployment, s *corev1.Secret) error {
				var foundOk bool
				for _, cnt := range d.Spec.Template.Spec.Containers {
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
									Secret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "datasource-tls",
										},
										Key: "ca",
									},
								},
								Cert: vmv1beta1.SecretOrConfigMap{
									Secret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "datasource-tls",
										},
										Key: "ca",
									},
								},
								KeySecret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "datasource-tls",
									},
									Key: "key",
								},
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
									ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "cert"},
								},
								KeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "datasource-tls"}, Key: "key"},
							},
						},
					},
				},
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
			validator: func(d *appsv1.Deployment, s *corev1.Secret) error {
				if _, ok := s.Data["default_configmap_datasource-tls_ca"]; !ok {
					return fmt.Errorf("failed to find expected TLS CA")
				}
				if _, ok := s.Data["default_configmap_datasource-tls_cert"]; !ok {
					return fmt.Errorf("failed to find expected TLS cert")
				}
				if _, ok := s.Data["default_datasource-tls_key"]; !ok {
					return fmt.Errorf("failed to find TLS key")
				}
				return nil
			},
		},
		{
			name: "with-notifiers-tls",
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
			ctx := context.TODO()
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			err := CreateOrUpdate(ctx, tt.cr, fclient, tt.cmNames)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.validator != nil {
				var generatedDeploment appsv1.Deployment
				if err := fclient.Get(ctx, types.NamespacedName{Namespace: tt.cr.Namespace, Name: tt.cr.PrefixedName()}, &generatedDeploment); err != nil {
					t.Fatalf("cannot find generated deployment=%q, err: %v", tt.cr.PrefixedName(), err)
				}
				var generatedTLSSecret corev1.Secret
				tlsSecretName := build.ResourceName(build.TLSAssetsResourceKind, tt.cr)
				if err := fclient.Get(ctx, types.NamespacedName{Namespace: tt.cr.Namespace, Name: tlsSecretName}, &generatedTLSSecret); err != nil {
					if !errors.IsNotFound(err) {
						t.Fatalf("unexpected error during attempt to get tls secret=%q, err: %v", build.ResourceName(build.TLSAssetsResourceKind, tt.cr), err)
					}
				}
				if err := tt.validator(&generatedDeploment, &generatedTLSSecret); err != nil {
					t.Fatalf("unexpected error at deployment validation: %v", err)
				}
			}
		})
	}
}

func TestBuildNotifiers(t *testing.T) {
	tests := []struct {
		name              string
		cr                *vmv1beta1.VMAlert
		predefinedObjects []runtime.Object
		want              []string
	}{
		{
			name: "ok build args",
			cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "basic-vmalert",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertSpec{
					Notifiers: []vmv1beta1.VMAlertNotifierSpec{
						{
							URL: "http://am-1",
						},
						{
							URL: "http://am-2",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									CAFile:             "/tmp/ca.cert",
									InsecureSkipVerify: true,
									KeyFile:            "/tmp/key.pem",
									CertFile:           "/tmp/cert.pem",
								},
							},
						},
						{
							URL: "http://am-3",
						},
					},
				},
			},
			want: []string{
				"-notifier.url=http://am-1,http://am-2,http://am-3",
				"-notifier.tlsKeyFile=,/tmp/key.pem,",
				"-notifier.tlsCertFile=,/tmp/cert.pem,",
				"-notifier.tlsCAFile=,/tmp/ca.cert,",
				"-notifier.tlsInsecureSkipVerify=false,true,false",
			},
		},
		{
			name: "ok build args with config",
			cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "basic-vmalert",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertSpec{
					NotifierConfigRef: &corev1.SecretKeySelector{
						Key: "cfg.yaml",
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "datasource-tls",
						},
					},
				},
			},
			want:              []string{"-notifier.config=" + notifierConfigMountPath + "/cfg.yaml"},
			predefinedObjects: []runtime.Object{},
		},
		{
			name: "with headers and oauth2",
			cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "basic-vmalert",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertSpec{
					Notifiers: []vmv1beta1.VMAlertNotifierSpec{
						{
							URL: "http://1",
							HTTPAuth: vmv1beta1.HTTPAuth{
								Headers: []string{"key=value", "key2=value2"},
								OAuth2: &vmv1beta1.OAuth2{
									Scopes:   []string{"1", "2"},
									TokenURL: "http://some-url",
									ClientSecret: &corev1.SecretKeySelector{
										Key: "client-secret",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "oauth2",
										},
									},
									ClientID: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											Key: "client-id",
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "oauth2",
											},
										},
									},
								},
							},
						},
						{
							URL: "http://2",
							HTTPAuth: vmv1beta1.HTTPAuth{
								Headers: []string{"key3=value3", "key4=value4"},
								BearerAuth: &vmv1beta1.BearerAuth{
									TokenSecret: &corev1.SecretKeySelector{
										Key: "bearer",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "bearer",
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
						Name:      "oauth2",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"client-id":     []byte("some-id"),
						"client-secret": []byte("some-secret"),
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bearer",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"bearer": []byte("some-v"),
					},
				},
			},
			want: []string{"-notifier.url=http://1,http://2", "-notifier.headers=key=value^^key2=value2,key3=value3^^key4=value4", "-notifier.bearerTokenFile=,/etc/vmalert/remote_secrets/default_bearer_bearer", "-notifier.oauth2.clientSecretFile=/etc/vmalert/remote_secrets/default_oauth2_client-secret,", "-notifier.oauth2.clientID=some-id,", "-notifier.oauth2.scopes=1,2,", "-notifier.oauth2.tokenUrl=http://some-url,"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			ac := getAssetsCache(ctx, fclient, tt.cr)
			if got, err := buildNotifiersArgs(tt.cr, ac); err != nil {
				t.Errorf("buildNotifiersArgs(), unexpected error: %s", err)
			} else if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildNotifiersArgs() = \ngot \n%v, \nwant \n%v", got, tt.want)
			}
		})
	}
}

func TestCreateOrUpdateService(t *testing.T) {
	tests := []struct {
		name              string
		cr                *vmv1beta1.VMAlert
		c                 *config.BaseOperatorConf
		want              func(svc *corev1.Service) error
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "base test",
			c:    config.MustGetBaseConfig(),
			cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "base",
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
			ctx := context.TODO()
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := createOrUpdateService(ctx, cl, tt.cr, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("createOrUpdateService() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err := tt.want(got); err != nil {
				t.Errorf("createOrUpdateService() unexpected error: %v", err)
			}
		})
	}
}

func Test_buildVMAlertArgs(t *testing.T) {
	tests := []struct {
		name               string
		cr                 *vmv1beta1.VMAlert
		ruleConfigMapNames []string
		want               []string
	}{
		{
			name: "basic args",
			cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "base",
				},
				Spec: vmv1beta1.VMAlertSpec{
					Datasource: vmv1beta1.VMAlertDatasourceSpec{
						URL: "http://vmsingle-url",
					},
				},
			},
			ruleConfigMapNames: []string{"first-rule-cm.yaml"},
			want:               []string{"-datasource.url=http://vmsingle-url", "-httpListenAddr=:", "-notifier.url=", "-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\""},
		},
		{
			name: "with tls args",
			cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "base",
				},
				Spec: vmv1beta1.VMAlertSpec{
					Datasource: vmv1beta1.VMAlertDatasourceSpec{
						URL: "http://vmsingle-url",
						HTTPAuth: vmv1beta1.HTTPAuth{
							Headers: []string{"x-org-id:one", "x-org-tenant:5"},
							TLSConfig: &vmv1beta1.TLSConfig{
								InsecureSkipVerify: true,
								KeyFile:            "/path/to/key",
								CAFile:             "/path/to/sa",
							},
						},
					},
				},
			},
			ruleConfigMapNames: []string{"first-rule-cm.yaml"},
			want:               []string{"--datasource.headers=x-org-id:one^^x-org-tenant:5", "-datasource.tlsCAFile=/path/to/sa", "-datasource.tlsInsecureSkipVerify=true", "-datasource.tlsKeyFile=/path/to/key", "-datasource.url=http://vmsingle-url", "-httpListenAddr=:", "-notifier.url=", "-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fclient := k8stools.GetTestClientWithObjects(nil)
			ac := getAssetsCache(ctx, fclient, tt.cr)
			if got, err := buildArgs(tt.cr, tt.ruleConfigMapNames, ac); err != nil {
				t.Errorf("buildArgs(), unexpected error: %s", err)
			} else if !reflect.DeepEqual(got, tt.want) {
				assert.Equal(t, tt.want, got)
				t.Errorf("buildVMAlertArgs() got = \n%v\n, want \n%v\n", got, tt.want)
			}
		})
	}
}

func TestBuildConfigReloaderContainer(t *testing.T) {
	f := func(cr *vmv1beta1.VMAlert, cmNames []string, expectedContainer corev1.Container) {
		t.Helper()
		var extraVolumes []corev1.VolumeMount
		for _, cm := range cr.Spec.ConfigMaps {
			extraVolumes = append(extraVolumes, corev1.VolumeMount{
				Name:      k8stools.SanitizeVolumeName("configmap-" + cm),
				ReadOnly:  true,
				MountPath: path.Join(vmv1beta1.ConfigMapsDir, cm),
			})
		}
		got := buildConfigReloaderContainer(cr, cmNames, extraVolumes)
		assert.Equal(t, expectedContainer, got)
	}

	// base case
	cr := &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "base",
		},
	}
	cmNames := []string{"cm-0", "cm-1"}
	expected := corev1.Container{
		Name: "config-reloader",
		Args: []string{
			"-webhook-url=http://localhost:/-/reload",
			"-volume-dir=/etc/vmalert/config/cm-0",
			"-volume-dir=/etc/vmalert/config/cm-1",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "cm-0",
				MountPath: "/etc/vmalert/config/cm-0",
			},
			{
				Name:      "cm-1",
				MountPath: "/etc/vmalert/config/cm-1",
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	f(cr, cmNames, expected)

	// vm config-reloader
	cr = &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "base",
		},
		Spec: vmv1beta1.VMAlertSpec{
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				UseVMConfigReloader: ptr.To(true),
			},
		},
	}
	cmNames = []string{"cm-0"}
	expected = corev1.Container{
		Name: "config-reloader",
		Args: []string{
			"--reload-url=http://localhost:/-/reload",
			"--watched-dir=/etc/vmalert/config/cm-0",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "cm-0",
				MountPath: "/etc/vmalert/config/cm-0",
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		Ports: []corev1.ContainerPort{
			{
				Name:          "reloader-http",
				Protocol:      corev1.ProtocolTCP,
				ContainerPort: 8435,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   intstr.FromInt32(8435),
					Scheme: "HTTP",
				},
			},
			TimeoutSeconds:   1,
			PeriodSeconds:    10,
			SuccessThreshold: 1,
			FailureThreshold: 3,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   intstr.FromInt32(8435),
					Scheme: "HTTP",
				},
			},
			InitialDelaySeconds: 5,
			TimeoutSeconds:      1,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			FailureThreshold:    3,
		},
	}
	f(cr, cmNames, expected)

	// extra volumes
	cr = &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "base",
		},
		Spec: vmv1beta1.VMAlertSpec{
			CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
				UseVMConfigReloader: ptr.To(true),
			},
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ConfigMaps: []string{"extra-template-1", "extra-template-2"},
			},
		},
	}
	cmNames = []string{"cm-0"}
	expected = corev1.Container{
		Name: "config-reloader",
		Args: []string{
			"--reload-url=http://localhost:/-/reload",
			"--watched-dir=/etc/vmalert/config/cm-0",
			"--watched-dir=/etc/vm/configs/extra-template-1",
			"--watched-dir=/etc/vm/configs/extra-template-2",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "cm-0",
				MountPath: "/etc/vmalert/config/cm-0",
			},
			{
				Name:      "configmap-extra-template-1",
				ReadOnly:  true,
				MountPath: "/etc/vm/configs/extra-template-1",
			},
			{
				Name:      "configmap-extra-template-2",
				ReadOnly:  true,
				MountPath: "/etc/vm/configs/extra-template-2",
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		Ports: []corev1.ContainerPort{
			{
				Name:          "reloader-http",
				Protocol:      corev1.ProtocolTCP,
				ContainerPort: 8435,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   intstr.FromInt32(8435),
					Scheme: "HTTP",
				},
			},
			TimeoutSeconds:   1,
			PeriodSeconds:    10,
			SuccessThreshold: 1,
			FailureThreshold: 3,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/health",
					Port:   intstr.FromInt32(8435),
					Scheme: "HTTP",
				},
			},
			InitialDelaySeconds: 5,
			TimeoutSeconds:      1,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			FailureThreshold:    3,
		},
	}
	f(cr, cmNames, expected)

}
