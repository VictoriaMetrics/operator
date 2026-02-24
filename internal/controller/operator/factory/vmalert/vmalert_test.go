package vmalert

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
		validate          func(*appsv1.Deployment, *corev1.Secret)
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.TODO()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		err := CreateOrUpdate(ctx, o.cr, fclient, o.cmNames)
		assert.NoError(t, err)

		if o.validate != nil {
			var generatedDeploment appsv1.Deployment
			assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Namespace: o.cr.Namespace, Name: o.cr.PrefixedName()}, &generatedDeploment))
			var generatedTLSSecret corev1.Secret
			tlsSecretName := build.ResourceName(build.TLSAssetsResourceKind, o.cr)
			assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Namespace: o.cr.Namespace, Name: tlsSecretName}, &generatedTLSSecret))
			o.validate(&generatedDeploment, &generatedTLSSecret)
		}
	}

	// base-spec-gen
	f(opts{
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
	})

	// base-spec-gen with externalLabels
	f(opts{
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
		validate: func(d *appsv1.Deployment, s *corev1.Secret) {
			var foundOk bool
			for _, cnt := range d.Spec.Template.Spec.Containers {
				if cnt.Name == "vmalert" {
					args := cnt.Args
					for _, arg := range args {
						if strings.HasPrefix(arg, "-external.label") {
							foundOk = true
							kv := strings.ReplaceAll(arg, "-external.label=", "")
							assert.True(t, kv == "label1=value1" || kv == "label2=value-2")
						}
					}
				}
			}
			assert.True(t, foundOk)
		},
	})

	// with-remote-tls
	f(opts{
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
		validate: func(d *appsv1.Deployment, s *corev1.Secret) {
			assert.NotEmpty(t, s.Data["default_configmap_datasource-tls_ca"])
			assert.NotEmpty(t, s.Data["default_configmap_datasource-tls_cert"])
			assert.NotEmpty(t, s.Data["default_datasource-tls_key"])
		},
	})

	// with-notifiers-tls
	f(opts{
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
	})

	// with tlsconfig insecure true
	f(opts{
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
	})

	// with notifier config
	f(opts{
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
	})
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
		got, err := buildNotifiersArgs(o.cr, ac)
		assert.NoError(t, err)
		assert.Equal(t, o.want, got)
	}

	// ok build args
	f(opts{
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
	})

	// ok build args with config
	f(opts{
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
	})

	// with headers and oauth2
	f(opts{
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
	})
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
		assert.NoError(t, o.want(&got))
	}

	// base test
	f(opts{
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
	})
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
		assert.NoError(t, discoverNotifiersIfNeeded(ctx, fclient, o.cr))
		got, err := buildArgs(o.cr, o.ruleConfigMapNames, ac)
		assert.NoError(t, err)
		assert.Equal(t, o.want, got)
	}

	// basic args
	f(opts{
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
		want:               []string{"-datasource.url=http://vmsingle-url", "-http.shutdownDelay=50s", "-httpListenAddr=:", "-notifier.url=http://test", "-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\""},
	})

	// with tls args
	f(opts{
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
		want:               []string{"--datasource.headers=x-org-id:one^^x-org-tenant:5", "-datasource.tlsCAFile=/path/to/sa", "-datasource.tlsInsecureSkipVerify=true", "-datasource.tlsKeyFile=/path/to/key", "-datasource.url=http://vmsingle-url", "-http.shutdownDelay=50s", "-httpListenAddr=:", "-notifier.url=http://test", "-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\""},
	})

	// with static and selector notifiers
	f(opts{
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
			"-http.shutdownDelay=50s",
			"-httpListenAddr=:",
			"-notifier.tlsCAFile=,/tmp/ca.cert,,,",
			"-notifier.tlsCertFile=,/tmp/cert.pem,,,",
			"-notifier.tlsInsecureSkipVerify=false,true,false,false,false",
			"-notifier.tlsKeyFile=,/tmp/key.pem,,,",
			"-notifier.url=http://am-1,http://am-2,http://am-3,http://vmalertmanager-test-system-0.vmalertmanager-test-system.monitoring.svc:9093,http://vmalertmanager-test-am-0.vmalertmanager-test-am.monitoring.svc:9093",
		},
	})
}
