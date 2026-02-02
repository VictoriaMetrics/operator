package vmalert

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		cmNames           []string
		predefinedObjects []runtime.Object
		validator         func(*appsv1.Deployment, *corev1.Secret) error
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.TODO()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		err := CreateOrUpdate(ctx, o.cr, fclient, o.cmNames)
		if err != nil {
			t.Errorf("CreateOrUpdate() error = %v", err)
			return
		}

		if o.validator != nil {
			var generatedDeploment appsv1.Deployment
			if err := fclient.Get(ctx, types.NamespacedName{Namespace: o.cr.Namespace, Name: o.cr.PrefixedName()}, &generatedDeploment); err != nil {
				t.Fatalf("cannot find generated deployment=%q, err: %v", o.cr.PrefixedName(), err)
			}
			var generatedTLSSecret corev1.Secret
			tlsSecretName := build.ResourceName(build.TLSAssetsResourceKind, o.cr)
			if err := fclient.Get(ctx, types.NamespacedName{Namespace: o.cr.Namespace, Name: tlsSecretName}, &generatedTLSSecret); err != nil {
				if !k8serrors.IsNotFound(err) {
					t.Fatalf("unexpected error during attempt to get tls secret=%q, err: %v", build.ResourceName(build.TLSAssetsResourceKind, o.cr), err)
				}
			}
			if err := o.validator(&generatedDeploment, &generatedTLSSecret); err != nil {
				t.Fatalf("unexpected error at deployment validation: %v", err)
			}
		}
	}

	// base-spec-gen
	o := opts{
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
	}
	f(o)

	// base-spec-gen with externalLabels
	o = opts{
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
	}
	f(o)

	// with-remote-tls
	o = opts{
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
	}
	f(o)

	// with-notifiers-tls
	o = opts{
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
	}
	f(o)

	// with tlsconfig insecure true
	o = opts{
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
	}
	f(o)

	// with notifier config
	o = opts{
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
				Data: map[string][]byte{"cfg.yaml": []byte("static: []")},
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
	}
	f(o)
}

func TestBuildNotifiers(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		predefinedObjects []runtime.Object
		want              []string
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ac := getAssetsCache(ctx, fclient, o.cr)
		if got, err := buildNotifiersArgs(o.cr, ac); err != nil {
			t.Errorf("buildNotifiersArgs(), unexpected error: %s", err)
		} else if !reflect.DeepEqual(got, o.want) {
			t.Errorf("BuildNotifiersArgs() = \ngot \n%v, \nwant \n%v", got, o.want)
		}
	}

	// ok build args
	o := opts{
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
	}
	f(o)

	// ok build args with config
	o = opts{
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
	}
	f(o)

	// with headers and oauth2
	o = opts{
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
	}
	f(o)
}

func TestCreateOrUpdateService(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		c                 *config.BaseOperatorConf
		want              func(svc *corev1.Service) error
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.TODO()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		assert.NoError(t, createOrUpdateService(ctx, cl, o.cr, nil))
		svc := build.Service(o.cr, o.cr.Spec.Port, nil)
		var got corev1.Service
		nsn := types.NamespacedName{
			Name:      svc.Name,
			Namespace: svc.Namespace,
		}
		assert.NoError(t, cl.Get(ctx, nsn, &got))
		if err := o.want(&got); err != nil {
			t.Errorf("createOrUpdateService() unexpected error: %v", err)
		}
	}

	// base test
	o := opts{
		c: config.MustGetBaseConfig(),
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
	}
	f(o)
}

func Test_buildVMAlertArgs(t *testing.T) {
	type opts struct {
		cr                 *vmv1beta1.VMAlert
		predefinedObjects  []runtime.Object
		ruleConfigMapNames []string
		want               []string
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ac := getAssetsCache(ctx, fclient, o.cr)
		if err := discoverNotifiersIfNeeded(ctx, fclient, o.cr); err != nil {
			t.Errorf("discoverNotifiersIfNeeded(): %s", err)
		}
		if got, err := buildArgs(o.cr, o.ruleConfigMapNames, ac); err != nil {
			t.Errorf("buildArgs(), unexpected error: %s", err)
		} else if !reflect.DeepEqual(got, o.want) {
			assert.Equal(t, o.want, got)
			t.Errorf("buildVMAlertArgs() got = \n%v\n, want \n%v\n", got, o.want)
		}
	}

	// basic args
	o := opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "base",
			},
			Spec: vmv1beta1.VMAlertSpec{
				Datasource: vmv1beta1.VMAlertDatasourceSpec{
					URL: "http://vmsingle-url",
				},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ExtraArgs: map[string]string{
						"notifier.url": "http://test",
					},
				},
			},
		},
		ruleConfigMapNames: []string{"first-rule-cm.yaml"},
		want:               []string{"-datasource.url=http://vmsingle-url", "-httpListenAddr=:", "-notifier.url=http://test", "-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\""},
	}
	f(o)

	// with tls args
	o = opts{
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
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ExtraArgs: map[string]string{
						"notifier.url": "http://test",
					},
				},
			},
		},
		ruleConfigMapNames: []string{"first-rule-cm.yaml"},
		want:               []string{"--datasource.headers=x-org-id:one^^x-org-tenant:5", "-datasource.tlsCAFile=/path/to/sa", "-datasource.tlsInsecureSkipVerify=true", "-datasource.tlsKeyFile=/path/to/key", "-datasource.url=http://vmsingle-url", "-httpListenAddr=:", "-notifier.url=http://test", "-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\""},
	}
	f(o)

	// with static and selector notifiers
	o = opts{
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
						Selector: &vmv1beta1.DiscoverySelector{
							Labels: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"main": "system",
								},
							},
						},
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
				Datasource: vmv1beta1.VMAlertDatasourceSpec{
					URL: "http://some-vm-datasource",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAlertmanager{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-am",
					Namespace:   "monitoring",
					Annotations: map[string]string{"not": "touch"},
					Labels:      map[string]string{"main": "system"},
				},
				Spec: vmv1beta1.VMAlertmanagerSpec{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			},
			&vmv1beta1.VMAlertmanager{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-system",
					Namespace:   "monitoring",
					Annotations: map[string]string{"not": "touch"},
					Labels:      map[string]string{"main": "system"},
				},
				Spec: vmv1beta1.VMAlertmanagerSpec{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			},
		},
		want: []string{
			"-datasource.url=http://some-vm-datasource",
			"-httpListenAddr=:",
			"-notifier.tlsCAFile=,/tmp/ca.cert,,,",
			"-notifier.tlsCertFile=,/tmp/cert.pem,,,",
			"-notifier.tlsInsecureSkipVerify=false,true,false,false,false",
			"-notifier.tlsKeyFile=,/tmp/key.pem,,,",
			"-notifier.url=http://am-1,http://am-2,http://am-3,http://vmalertmanager-test-system-0.vmalertmanager-test-system.monitoring.svc:9093,http://vmalertmanager-test-am-0.vmalertmanager-test-am.monitoring.svc:9093",
		},
	}
	f(o)
}
