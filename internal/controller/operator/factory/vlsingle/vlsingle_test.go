package vlsingle

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdateVLSingle(t *testing.T) {
	type opts struct {
		cr                *vmv1.VLSingle
		cfgMutator        func(c *config.BaseOperatorConf)
		validate          func(ctx context.Context, rclient client.Client, cr *vmv1.VLSingle)
		wantErr           bool
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(o.cr)
		cfg := config.MustGetBaseConfig()
		if o.cfgMutator != nil {
			defaultCfg := *cfg
			o.cfgMutator(cfg)
			defer func() {
				*config.MustGetBaseConfig() = defaultCfg
			}()
		}
		ctx := context.TODO()
		err := CreateOrUpdate(ctx, fclient, o.cr)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		if o.validate != nil {
			o.validate(ctx, fclient, o.cr)
		}
	}

	// base gen
	f(opts{
		cr: &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VLSingleSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "vlsingle-0",
					Labels: map[string]string{
						"app.kubernetes.io/component": "monitoring",
						"app.kubernetes.io/name":      "vlsingle",
						"app.kubernetes.io/instance":  "base",
						"managed-by":                  "vm-operator",
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vlsingle-base", "default"),
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VLSingle) {
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got))
			assert.Equal(t, "vlsingle-base", got.Name)
			assert.Equal(t, "default", got.Namespace)
		},
	})

	// base with specific port
	f(opts{
		cr: &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VLSingleSpec{

				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
					Port:         "8435",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "vlsingle-0",
					Labels: map[string]string{
						"app.kubernetes.io/component": "monitoring",
						"app.kubernetes.io/name":      "vlsingle",
						"app.kubernetes.io/instance":  "base",
						"managed-by":                  "vm-operator",
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vlsingle-base", "default"),
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VLSingle) {
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got))
			assert.Equal(t, "vlsingle-base", got.Name)
			assert.Equal(t, "default", got.Namespace)
		},
	})

	// with syslog tls config
	f(opts{
		cr: &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VLSingleSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
					Port:         "8435",
				},
				SyslogSpec: &vmv1.SyslogServerSpec{
					TCPListeners: []*vmv1.SyslogTCPListener{
						{
							ListenPort:       3001,
							DecolorizeFields: vmv1.FieldsListString(`["log","level"]`),
						},
						{
							ListenPort:   3002,
							StreamFields: vmv1.FieldsListString(`["job","instance"]`),
							IgnoreFields: vmv1.FieldsListString(`["ip"]`),
							TLSConfig: &vmv1.TLSServerConfig{
								KeySecret: &corev1.SecretKeySelector{
									Key: "tls-key",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "syslog-tls-server",
									},
								},
								CertSecret: &corev1.SecretKeySelector{
									Key: "tls-cert",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "syslog-tls-server",
									},
								},
							},
						},
					},
					UDPListeners: []*vmv1.SyslogUDPListener{
						{
							ListenPort:     3001,
							CompressMethod: "zstd",
							StreamFields:   vmv1.FieldsListString(`["job","instance"]`),
							IgnoreFields:   vmv1.FieldsListString(`["ip"]`),
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "vlsingle-0",
					Labels: map[string]string{
						"app.kubernetes.io/component": "monitoring",
						"app.kubernetes.io/name":      "vlsingle",
						"app.kubernetes.io/instance":  "base",
						"managed-by":                  "vm-operator",
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vlsingle-base", "default"),
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VLSingle) {
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got))
			assert.Equal(t, "vlsingle-base", got.Name)
			assert.Equal(t, "default", got.Namespace)
		},
	})

	// managed metadata
	f(opts{
		cr: &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VLSingleSpec{
				ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{
					Labels:      map[string]string{"env": "prod"},
					Annotations: map[string]string{"controller": "true"},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VLSingle) {
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got))
			assert.Equal(t, got.Labels, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vlsingle",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			})
			assert.Equal(t, got.Annotations, map[string]string{"controller": "true"})
		},
	})

	// common labels
	f(opts{
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.CommonLabels = map[string]string{"env": "prod"}
			c.CommonAnnotations = map[string]string{"controller": "true"}
		},
		cr: &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VLSingle) {
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got))
			assert.Equal(t, got.Labels, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vlsingle",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			})
			assert.Equal(t, got.Annotations, map[string]string{"controller": "true"})
		}})
}

func TestCreateOrUpdateVLSingle_Paused(t *testing.T) {
	cr := &vmv1.VLSingle{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "base",
			Namespace: "default",
		},
		Spec: vmv1.VLSingleSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				Paused: true,
			},
		},
	}
	fclient := k8stools.GetTestClientWithObjects(nil)
	err := CreateOrUpdate(context.TODO(), fclient, cr)
	assert.NoError(t, err)

	// check that no deployment was created
	var deploy appsv1.Deployment
	err = fclient.Get(context.TODO(), types.NamespacedName{Name: "vlsingle-base", Namespace: "default"}, &deploy)
	assert.Error(t, err)
	assert.True(t, k8serrors.IsNotFound(err))
}

func TestCreateOrUpdateVLSingleService(t *testing.T) {
	type opts struct {
		cr                *vmv1.VLSingle
		c                 *config.BaseOperatorConf
		validate          func(*corev1.Service)
		wantErr           bool
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		err := createOrUpdateService(ctx, fclient, o.cr, nil)
		if o.wantErr {
			assert.Error(t, err)
			return
		}
		assert.NoError(t, err)
		svc := build.Service(o.cr, o.cr.Spec.Port, nil)
		var got corev1.Service
		nsn := types.NamespacedName{
			Name:      svc.Name,
			Namespace: svc.Namespace,
		}
		assert.NoError(t, fclient.Get(ctx, nsn, &got))
		if o.validate != nil {
			o.validate(&got)
		}
	}

	// base service test
	f(opts{
		c: config.MustGetBaseConfig(),
		cr: &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "logs-1",
				Namespace: "default",
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, "vlsingle-logs-1", svc.Name)
			assert.Equal(t, "default", svc.Namespace)
			assert.Len(t, svc.Spec.Ports, 1)

			assert.Equal(t, map[string]string{
				"app.kubernetes.io/name":      "vlsingle",
				"app.kubernetes.io/instance":  "logs-1",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, svc.Labels)
		},
	})

	// with extra service nodePort
	f(opts{
		c: config.MustGetBaseConfig(),
		cr: &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "logs-1",
				Namespace: "default",
			},
			Spec: vmv1.VLSingleSpec{
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "additional-service"},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeNodePort,
					},
				},
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, "vlsingle-logs-1", svc.Name)
			assert.Equal(t, "default", svc.Namespace)
			assert.Len(t, svc.Spec.Ports, 1)
			// verify labels exist and include core operator metadata
			assert.Equal(t, map[string]string{
				"app.kubernetes.io/name":      "vlsingle",
				"app.kubernetes.io/instance":  "logs-1",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, svc.Labels)
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-svc",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vlsingle",
						"app.kubernetes.io/instance":  "logs-1",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
				Spec: corev1.ServiceSpec{},
			},
		},
	})
}
