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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		validator         func(*appsv1.Deployment, *corev1.Secret) error
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		ctx := context.TODO()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		err := CreateOrUpdate(ctx, opts.cr, fclient, nil)
		if err != nil {
			t.Errorf("CreateOrUpdate() error = %v", err)
			return
		}

		if opts.validator != nil {
			var generatedDeploment appsv1.Deployment
			if err := fclient.Get(ctx, types.NamespacedName{Namespace: opts.cr.Namespace, Name: opts.cr.PrefixedName()}, &generatedDeploment); err != nil {
				t.Fatalf("cannot find generated deployment=%q, err: %v", opts.cr.PrefixedName(), err)
			}
			var generatedTLSSecret corev1.Secret
			tlsSecretName := build.ResourceName(build.TLSAssetsResourceKind, opts.cr)
			if err := fclient.Get(ctx, types.NamespacedName{Namespace: opts.cr.Namespace, Name: tlsSecretName}, &generatedTLSSecret); err != nil {
				if !k8serrors.IsNotFound(err) {
					t.Fatalf("unexpected error during attempt to get tls secret=%q, err: %v", build.ResourceName(build.TLSAssetsResourceKind, opts.cr), err)
				}
			}
			if err := opts.validator(&generatedDeploment, &generatedTLSSecret); err != nil {
				t.Fatalf("unexpected error at deployment validation: %v", err)
			}
		}
	}

	// base spec gen
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

	// base spec gen with externalLabels
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

	// with remote tls
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
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "default"},
			},
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

	// with notifiers tls
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
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "default"},
			},
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
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "default"},
			},
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
	}
	f(o)
}

func TestBuildNotifiers(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		want              []string
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		ac := getAssetsCache(ctx, fclient, opts.cr)
		if got, err := buildNotifiersArgs(opts.cr, ac); err != nil {
			t.Errorf("buildNotifiersArgs(), unexpected error: %s", err)
		} else if !reflect.DeepEqual(got, opts.want) {
			t.Errorf("BuildNotifiersArgs() = \ngot \n%v, \nwant \n%v", got, opts.want)
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
		want: []string{"-notifier.config=" + notifierConfigMountPath + "/cfg.yaml"},
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
		want: []string{
			"-notifier.url=http://1,http://2",
			"-notifier.headers=key=value^^key2=value2,key3=value3^^key4=value4",
			"-notifier.bearerTokenFile=,/etc/vmalert/remote_secrets/default_bearer_bearer",
			"-notifier.oauth2.clientSecretFile=/etc/vmalert/remote_secrets/default_oauth2_client-secret,",
			"-notifier.oauth2.clientID=some-id,",
			"-notifier.oauth2.scopes=1,2,",
			"-notifier.oauth2.tokenUrl=http://some-url,",
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
	}
	f(o)
}

func TestCreateOrUpdateService(t *testing.T) {
	type opts struct {
		cr   *vmv1beta1.VMAlert
		want func(svc *corev1.Service) error
	}
	f := func(opts opts) {
		t.Helper()
		ctx := context.TODO()
		cl := k8stools.GetTestClientWithObjects(nil)
		got, err := createOrUpdateService(ctx, cl, opts.cr, nil)
		if err != nil {
			t.Errorf("createOrUpdateService() error = %v", err)
			return
		}
		if err := opts.want(got); err != nil {
			t.Errorf("createOrUpdateService() unexpected error: %v", err)
		}
	}

	// base test
	o := opts{
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
		cr      *vmv1beta1.VMAlert
		cmNames []string
		want    []string
	}
	f := func(opts opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(nil)
		ac := getAssetsCache(ctx, fclient, opts.cr)
		if got, err := buildArgs(opts.cr, opts.cmNames, ac); err != nil {
			t.Errorf("buildArgs(), unexpected error: %s", err)
		} else if !reflect.DeepEqual(got, opts.want) {
			assert.Equal(t, opts.want, got)
			t.Errorf("buildVMAlertArgs() got = \n%v\n, want \n%v\n", got, opts.want)
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
			},
		},
		cmNames: []string{
			"first-rule-cm.yaml",
		},
		want: []string{
			"-datasource.url=http://vmsingle-url",
			"-httpListenAddr=:",
			"-notifier.url=",
			"-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\"",
		},
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
			},
		},
		cmNames: []string{
			"first-rule-cm.yaml",
		},
		want: []string{
			"--datasource.headers=x-org-id:one^^x-org-tenant:5",
			"-datasource.tlsCAFile=/path/to/sa",
			"-datasource.tlsInsecureSkipVerify=true",
			"-datasource.tlsKeyFile=/path/to/key",
			"-datasource.url=http://vmsingle-url",
			"-httpListenAddr=:",
			"-notifier.url=",
			"-rule=\"/etc/vmalert/config/first-rule-cm.yaml/*.yaml\"",
		},
	}
	f(o)
}

func TestBuildConfigReloaderContainer(t *testing.T) {
	type opts struct {
		cr       *vmv1beta1.VMAlert
		cmNames  []string
		expected corev1.Container
	}
	f := func(opts opts) {
		t.Helper()
		var extraVolumes []corev1.VolumeMount
		for _, cm := range opts.cr.Spec.ConfigMaps {
			extraVolumes = append(extraVolumes, corev1.VolumeMount{
				Name:      k8stools.SanitizeVolumeName("configmap-" + cm),
				ReadOnly:  true,
				MountPath: path.Join(vmv1beta1.ConfigMapsDir, cm),
			})
		}
		got := buildConfigReloaderContainer(opts.cr, opts.cmNames, extraVolumes)
		assert.Equal(t, opts.expected, got)
	}

	// base case
	o := opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "base",
			},
		},
		cmNames: []string{"cm-0", "cm-1"},
		expected: corev1.Container{
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
		},
	}
	f(o)

	// vm config-reloader
	o = opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "base",
			},
			Spec: vmv1beta1.VMAlertSpec{
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					UseVMConfigReloader: ptr.To(true),
				},
			},
		},
		cmNames: []string{"cm-0"},
		expected: corev1.Container{
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
		},
	}
	f(o)

	// extra volumes
	o = opts{
		cr: &vmv1beta1.VMAlert{
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
		},
		cmNames: []string{"cm-0"},
		expected: corev1.Container{
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
		},
	}
	f(o)

}
