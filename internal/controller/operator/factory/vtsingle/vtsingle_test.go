package vtsingle

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1.VTSingle
		cfgMutator        func(*config.BaseOperatorConf)
		validate          func(ctx context.Context, rclient client.Client, cr *vmv1.VTSingle)
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
		synctest.Test(t, func(t *testing.T) {
			err := CreateOrUpdate(ctx, fclient, o.cr)
			if o.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if o.validate != nil {
				o.validate(ctx, fclient, o.cr)
			}
		})
	}

	// base gen
	f(opts{
		cr: &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VTSingleSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "vtsingle-0",
					Labels: map[string]string{
						"app.kubernetes.io/component": "monitoring",
						"app.kubernetes.io/name":      "vtsingle",
						"app.kubernetes.io/instance":  "base",
						"managed-by":                  "vm-operator",
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vtsingle-base", "default"),
		},
	})

	// base with specific port
	f(opts{
		cr: &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VTSingleSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
					Port:         "10435",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "vtsingle-0",
					Labels: map[string]string{
						"app.kubernetes.io/component": "monitoring",
						"app.kubernetes.io/name":      "vtsingle",
						"app.kubernetes.io/instance":  "base",
						"managed-by":                  "vm-operator",
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vtsingle-base", "default"),
		},
	})

	// with syslog tls config
	f(opts{
		cr: &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VTSingleSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
					Port:         "10435",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "vtsingle-0",
					Labels: map[string]string{
						"app.kubernetes.io/component": "monitoring",
						"app.kubernetes.io/name":      "vtsingle",
						"app.kubernetes.io/instance":  "base",
						"managed-by":                  "vm-operator",
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vtsingle-base", "default"),
		},
	})

	// managed metadata
	f(opts{
		cr: &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VTSingleSpec{
				ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{
					Labels:      map[string]string{"env": "prod"},
					Annotations: map[string]string{"controller": "true"},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VTSingle) {
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got))
			assert.Equal(t, got.Labels, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vtsingle",
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
		cr: &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VTSingleSpec{
				ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{
					Annotations: map[string]string{
						"env": "base",
					},
				},
				StorageMetadata: vmv1beta1.EmbeddedObjectMetadata{
					Labels: map[string]string{
						"env": "test",
					},
					Annotations: map[string]string{
						"env": "test",
					},
				},
				Storage: &corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("5Gi"),
						},
					},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VTSingle) {
			nsn := types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Equal(t, got.Labels, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vtsingle",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			})
			assert.Equal(t, got.Annotations, map[string]string{
				"env":        "base",
				"controller": "true",
			})
			var pvc corev1.PersistentVolumeClaim
			assert.NoError(t, rclient.Get(ctx, nsn, &pvc))
			assert.Equal(t, pvc.Labels, map[string]string{
				"env":                         "test",
				"app.kubernetes.io/name":      "vtsingle",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			})
			assert.Equal(t, pvc.Annotations, map[string]string{
				"controller": "true",
				"env":        "test",
			})
		},
	})
}

func TestMakePodSpec_GRPC(t *testing.T) {
	cr := &vmv1.VTSingle{
		ObjectMeta: metav1.ObjectMeta{Name: "traces-1", Namespace: "default"},
		Spec: vmv1.VTSingleSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{Port: "10428"},
			GRPCSpec: &vmv1.OTLPGRPCSpec{
				ListenPort: 4317,
				TLSConfig: &vmv1.TLSServerConfig{
					CertSecret: &corev1.SecretKeySelector{
						Key:                  "CERT",
						LocalObjectReference: corev1.LocalObjectReference{Name: "tls"},
					},
					KeySecret: &corev1.SecretKeySelector{
						Key:                  "KEY",
						LocalObjectReference: corev1.LocalObjectReference{Name: "tls"},
					},
				},
			},
		},
	}
	spec, err := makePodSpec(cr)
	require.NoError(t, err)
	require.Len(t, spec.Spec.Containers, 1)
	c := spec.Spec.Containers[0]
	assert.Contains(t, c.Args, "-otlpGRPCListenAddr=:4317")
	assert.Contains(t, c.Args, "-otlpGRPC.tls=true")
	assert.Contains(t, c.Args, "-otlpGRPC.tlsCertFile=/etc/vm/tls-server-secrets/tls/CERT")
	assert.Contains(t, c.Args, "-otlpGRPC.tlsKeyFile=/etc/vm/tls-server-secrets/tls/KEY")
	assert.Contains(t, c.Ports, corev1.ContainerPort{Name: "otlp-grpc", Protocol: corev1.ProtocolTCP, ContainerPort: 4317})

	var found bool
	for _, m := range c.VolumeMounts {
		if m.Name == "secret-tls-tls" {
			found = true
			assert.Equal(t, "/etc/vm/tls-server-secrets/tls", m.MountPath)
		}
	}
	assert.True(t, found, "expected secret-tls-tls volume mount")
}

func TestCreateOrUpdateService(t *testing.T) {
	type opts struct {
		cr                *vmv1.VTSingle
		validate          func(ctx context.Context, rclient client.Client, cr *vmv1.VTSingle)
		wantErr           bool
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		err := createOrUpdateService(ctx, fclient, o.cr, nil)
		if o.wantErr {
			assert.Error(t, err)
			return
		}
		assert.NoError(t, err)
		if o.validate != nil {
			o.validate(ctx, fclient, o.cr)
		}
	}

	// base service test
	f(opts{
		cr: &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "traces-1",
				Namespace: "default",
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VTSingle) {
			var got corev1.Service
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: cr.Namespace}, &got))
			assert.Equal(t, got.Name, "vtsingle-traces-1")
			assert.Equal(t, got.Namespace, "default")
			assert.Len(t, got.Spec.Ports, 1)
			assert.Equal(t, got.Labels, map[string]string{
				"app.kubernetes.io/name":      "vtsingle",
				"app.kubernetes.io/instance":  "traces-1",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			})
		},
	})

	// with extra service nodePort
	f(opts{
		cr: &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "traces-1",
				Namespace: "default",
			},
			Spec: vmv1.VTSingleSpec{
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "additional-service"},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeNodePort,
					},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VTSingle) {
			var got corev1.Service
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: cr.Namespace}, &got))
			assert.Equal(t, got.Name, "vtsingle-traces-1")
			assert.Equal(t, got.Namespace, "default")
			assert.Len(t, got.Spec.Ports, 1)
			assert.Equal(t, got.Labels, map[string]string{
				"app.kubernetes.io/name":      "vtsingle",
				"app.kubernetes.io/instance":  "traces-1",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			})
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-svc",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vtsingle",
						"app.kubernetes.io/instance":  "traces-1",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
				Spec: corev1.ServiceSpec{},
			},
		},
	})

	// with grpc spec
	f(opts{
		cr: &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "traces-1",
				Namespace: "default",
			},
			Spec: vmv1.VTSingleSpec{
				GRPCSpec: &vmv1.OTLPGRPCSpec{ListenPort: 4317},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VTSingle) {
			var got corev1.Service
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: cr.Namespace}, &got))
			require.Len(t, got.Spec.Ports, 2)
			assert.Equal(t, "otlp-grpc", got.Spec.Ports[1].Name)
			assert.Equal(t, int32(4317), got.Spec.Ports[1].Port)
		},
	})
}
