package vmalert

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		cfgMutator        func(*config.BaseOperatorConf)
		cmNames           []string
		predefinedObjects []runtime.Object
		validate          func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert)
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.TODO()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		cfg := config.MustGetBaseConfig()
		if o.cfgMutator != nil {
			defaultCfg := *cfg
			o.cfgMutator(cfg)
			defer func() {
				*config.MustGetBaseConfig() = defaultCfg
			}()
		}
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(o.cr)
		err := CreateOrUpdate(ctx, o.cr, fclient, o.cmNames)
		assert.NoError(t, err)

		if o.validate != nil {
			o.validate(ctx, fclient, o.cr)
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
			k8stools.NewReadyDeployment("vmalert-basic-vmalert", "default"),
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
			k8stools.NewReadyDeployment("vmalert-basic-vmalert", "default"),
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) {
			var d appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &d))
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
			k8stools.NewReadyDeployment("vmalert-basic-vmalert", "default"),
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) {
			var s corev1.Secret
			tlsSecretName := build.ResourceName(build.TLSAssetsResourceKind, cr)
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: tlsSecretName}, &s))
			assert.NotEmpty(t, s.Data["default_configmap_datasource-tls_ca"])
			assert.NotEmpty(t, s.Data["default_configmap_datasource-tls_cert"])
			assert.NotEmpty(t, s.Data["default_datasource-tls_key"])
			assert.Equal(t, map[string]string{
				"app.kubernetes.io/name":      "vmalert",
				"app.kubernetes.io/instance":  "basic-vmalert",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, s.Labels)
			var svc corev1.Service
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &svc))
			assert.Equal(t, map[string]string{
				"app.kubernetes.io/name":      "vmalert",
				"app.kubernetes.io/instance":  "basic-vmalert",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, svc.Labels)
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
			k8stools.NewReadyDeployment("vmalert-basic-vmalert", "default"),
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
			k8stools.NewReadyDeployment("vmalert-basic-vmalert", "default"),
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
			k8stools.NewReadyDeployment("vmalert-basic-vmalert", "default"),
		},
	})

	// managed metadata
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAlertSpec{
				Notifier: &vmv1beta1.VMAlertNotifierSpec{
					URL: "http://notifier",
				},
				Datasource: vmv1beta1.VMAlertDatasourceSpec{
					URL: "http://datasource",
				},
				ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{
					Labels:      map[string]string{"env": "prod"},
					Annotations: map[string]string{"controller": "true"},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) {
			var set appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &set))
			assert.Equal(t, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vmalert",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, set.Labels)
			assert.Equal(t, map[string]string{"controller": "true"}, set.Annotations)
			var svc corev1.Service
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &svc))
			assert.Equal(t, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vmalert",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, svc.Labels)
		},
	})

	// common labels
	f(opts{
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.CommonLabels = map[string]string{"env": "prod"}
			c.CommonAnnotations = map[string]string{"controller": "true"}
		},
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAlertSpec{
				Notifier: &vmv1beta1.VMAlertNotifierSpec{
					URL: "http://notifier",
				},
				Datasource: vmv1beta1.VMAlertDatasourceSpec{
					URL: "http://datasource",
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) {
			var set appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &set))
			assert.Equal(t, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vmalert",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, set.Labels)
			assert.Equal(t, map[string]string{"controller": "true"}, set.Annotations)
			var svc corev1.Service
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &svc))
			assert.Equal(t, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vmalert",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, svc.Labels)
		}})
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
		validate          func(svc *corev1.Service)
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
		if o.validate != nil {
			o.validate(&got)
		}
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
		validate: func(svc *corev1.Service) {
			assert.Equal(t, "vmalert-base", svc.Name)
			assert.Equal(t, map[string]string{
				"app.kubernetes.io/name":      "vmalert",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, svc.Labels)
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
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(o.cr)
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
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ExtraArgs: map[string]string{
						"notifier.url": "http://test",
					},
				},
			},
		},
		ruleConfigMapNames: []string{"first-rule-cm.yaml"},
		want:               []string{"-datasource.url=http://vmsingle-url", "-httpListenAddr=:8080", "-notifier.url=http://test", "-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\""},
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
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ExtraArgs: map[string]string{
						"notifier.url": "http://test",
					},
				},
			},
		},
		ruleConfigMapNames: []string{"first-rule-cm.yaml"},
		want:               []string{"--datasource.headers=x-org-id:one^^x-org-tenant:5", "-datasource.tlsCAFile=/path/to/sa", "-datasource.tlsInsecureSkipVerify=true", "-datasource.tlsKeyFile=/path/to/key", "-datasource.url=http://vmsingle-url", "-httpListenAddr=:8080", "-notifier.url=http://test", "-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\""},
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
					CommonAppsParams: vmv1beta1.CommonAppsParams{
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
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			},
		},
		want: []string{
			"-datasource.url=http://some-vm-datasource",
			"-httpListenAddr=:8080",
			"-notifier.tlsCAFile=,/tmp/ca.cert,,,",
			"-notifier.tlsCertFile=,/tmp/cert.pem,,,",
			"-notifier.tlsInsecureSkipVerify=false,true,false,false,false",
			"-notifier.tlsKeyFile=,/tmp/key.pem,,,",
			"-notifier.url=http://am-1,http://am-2,http://am-3,http://vmalertmanager-test-system-0.vmalertmanager-test-system.monitoring.svc:9093,http://vmalertmanager-test-am-0.vmalertmanager-test-am.monitoring.svc:9093",
		},
	})
}
