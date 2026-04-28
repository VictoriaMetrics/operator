package vmsingle

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMSingle
		cfgMutator        func(*config.BaseOperatorConf)
		validate          func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle)
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		t.Helper()
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
		ctx := context.TODO()
		assert.NoError(t, CreateOrUpdate(ctx, o.cr, fclient))
		if o.validate != nil {
			o.validate(ctx, fclient, o.cr)
		}
	}

	// base-vmsingle-gen
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmsingle-base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1))},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "vmsingle-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmsingle", "app.kubernetes.io/instance": "vmsingle-base", "managed-by": "vm-operator"}},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vmsingle-vmsingle-base", "default"),
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got))
			assert.Equal(t, "vmsingle-vmsingle-base", got.Name)
		},
	})

	// base-vmsingle-with-ports
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmsingle-base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				InsertPorts: &vmv1beta1.InsertPorts{
					InfluxPort:       "8051",
					OpenTSDBHTTPPort: "8052",
					GraphitePort:     "8053",
					OpenTSDBPort:     "8054",
				},
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1))},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "vmsingle-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmsingle", "app.kubernetes.io/instance": "vmsingle-base", "managed-by": "vm-operator"}},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vmsingle-vmsingle-base", "default"),
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got))
			assert.Equal(t, "vmsingle-vmsingle-base", got.Name)
		},
	})

	// managed metadata
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{
					Labels:      map[string]string{"env": "prod"},
					Annotations: map[string]string{"controller": "true"},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got))
			assert.Equal(t, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vmsingle",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, got.Labels)
			assert.Equal(t, map[string]string{"controller": "true"}, got.Annotations)
		},
	})

	// common labels
	f(opts{
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.CommonLabels = map[string]string{"env": "prod"}
			c.CommonAnnotations = map[string]string{"controller": "true"}
		},
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got))
			assert.Equal(t, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vmsingle",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, got.Labels)
			assert.Equal(t, map[string]string{"controller": "true"}, got.Annotations)
		}})

	// common labels cannot overwrite standard labels
	f(opts{
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.CommonLabels = map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "hacked",
				"app.kubernetes.io/instance":  "hacked",
				"app.kubernetes.io/component": "hacked",
				"managed-by":                  "hacked",
			}
		},
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got))
			assert.Equal(t, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vmsingle",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, got.Labels)
		}})
}

func TestCreateOrUpdateService(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMSingle
		validate          func(*corev1.Service)
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		build.AddDefaults(fclient.Scheme())
		ctx := context.TODO()
		assert.NoError(t, createOrUpdateService(ctx, fclient, o.cr, nil))
		svc := build.Service(o.cr, o.cr.Spec.Port, nil)
		var got corev1.Service
		nsn := types.NamespacedName{
			Name:      svc.Name,
			Namespace: svc.Namespace,
		}
		assert.NoError(t, fclient.Get(ctx, nsn, &got))
		if o.validate != nil {
			o.validate(&got)
		} else {
			assert.Equal(t, got.Name, svc.Name)
		}
	}

	// base service test
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "single-1",
				Namespace: "default",
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, "vmsingle-single-1", svc.Name)
			assert.Equal(t, "default", svc.Namespace)

			expectedPorts := []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   "TCP",
					TargetPort: intstr.FromInt32(0),
				},
				{
					Name:       "http-alias",
					Port:       8428,
					Protocol:   "TCP",
					TargetPort: intstr.FromInt32(8428),
				},
			}
			assert.Equal(t, expectedPorts, svc.Spec.Ports)
			assert.Equal(t, map[string]string{
				"app.kubernetes.io/name":      "vmsingle",
				"app.kubernetes.io/instance":  "single-1",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, svc.Labels)
		},
	})

	// base service test-with ports
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "single-1",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				InsertPorts: &vmv1beta1.InsertPorts{
					InfluxPort:       "8051",
					OpenTSDBHTTPPort: "8052",
					GraphitePort:     "8053",
					OpenTSDBPort:     "8054",
				},
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, "vmsingle-single-1", svc.Name)
			assert.Equal(t, "default", svc.Namespace)
			// sanity-check ports count and a couple of representative ports
			assert.Len(t, svc.Spec.Ports, 9)
			// check graphite tcp present
			foundGraphite := false
			for _, p := range svc.Spec.Ports {
				if p.Name == "graphite-tcp" && p.Port == 8053 {
					foundGraphite = true
					break
				}
			}
			assert.True(t, foundGraphite)

			assert.Equal(t, map[string]string{
				"app.kubernetes.io/name":      "vmsingle",
				"app.kubernetes.io/instance":  "single-1",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, svc.Labels)
		},
	})

	// with extra service nodePort
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "single-1",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "additional-service"},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeNodePort,
					},
				},
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, "vmsingle-single-1", svc.Name)
			assert.Equal(t, "default", svc.Namespace)
			assert.Len(t, svc.Spec.Ports, 2)
			assert.Equal(t, map[string]string{
				"app.kubernetes.io/name":      "vmsingle",
				"app.kubernetes.io/instance":  "single-1",
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
						"app.kubernetes.io/name":      "vmsingle",
						"app.kubernetes.io/instance":  "single-1",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
				Spec: corev1.ServiceSpec{},
			},
		},
	})
}
