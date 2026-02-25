package vmanomaly

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1.VMAnomaly
		validate          func(set *appsv1.StatefulSet)
		wantErr           bool
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.TODO()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(o.cr)
		err := CreateOrUpdate(ctx, o.cr, fclient)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		if o.validate != nil {
			var got appsv1.StatefulSet
			assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Namespace: o.cr.Namespace, Name: o.cr.PrefixedName()}, &got))
			o.validate(&got)
		}
	}

	// simple vmanomaly
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-anomaly",
				Namespace:   "monitoring",
				Annotations: map[string]string{"not": "touch"},
				Labels:      map[string]string{"main": "system"},
			},
			Spec: vmv1.VMAnomalySpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
				License: &vmv1beta1.License{
					Key: ptr.To("test"),
				},
				ConfigRawYaml: `
reader:
  queries:
    query_alias2:
      expr: vm_metric
writer:
  datasource_url: "http://test.com"
models:
  model_univariate_1:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['query_alias2']
schedulers:
  scheduler_periodic_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: 1m
    fit_every: 2m
    fit_window: 3h
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://test.com",
					SamplingPeriod: "1m",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://write.endpoint",
				},
			},
		},
		validate: func(set *appsv1.StatefulSet) {
			assert.Equal(t, set.Name, "vmanomaly-test-anomaly")
			assert.Equal(t, set.Spec.Template.Spec.Containers[0].Resources, corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("500Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("200Mi"),
				},
			})
			assert.Equal(t, set.Labels, map[string]string{
				"app.kubernetes.io/component": "monitoring",
				"app.kubernetes.io/instance":  "test-anomaly",
				"app.kubernetes.io/name":      "vmanomaly",
				"managed-by":                  "vm-operator",
			})
		},
	})

	// vmanomaly with embedded probe
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-anomaly",
				Namespace:   "monitoring",
				Annotations: map[string]string{"not": "touch"},
				Labels:      map[string]string{"main": "system"},
			},
			Spec: vmv1.VMAnomalySpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
				License: &vmv1beta1.License{
					Key: ptr.To("test"),
				},
				ConfigRawYaml: `
reader:
  queries:
    query_alias2:
      expr: vm_metric
models:
  model_univariate_1:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['query_alias2']
schedulers:
  scheduler_periodic_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: 1m
    fit_every: 2m
    fit_window: 3h
`,
				EmbeddedProbes: &vmv1beta1.EmbeddedProbes{
					LivenessProbe: &corev1.Probe{
						TimeoutSeconds: 20,
					},
				},
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://test.com",
					SamplingPeriod: "1m",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://write.endpoint",
				},
			},
		},
		validate: func(set *appsv1.StatefulSet) {
			assert.Len(t, set.Spec.Template.Spec.Containers, 1)
			container := set.Spec.Template.Spec.Containers[0]
			assert.Equal(t, container.Name, "vmanomaly")
			assert.Equal(t, container.LivenessProbe.TimeoutSeconds, int32(20))
			assert.Equal(t, container.LivenessProbe.HTTPGet.Path, "/health")
			assert.Equal(t, container.ReadinessProbe.HTTPGet.Path, "/health")
		},
	})

	// vmanomaly with server PathPrefix - probes should use prefixed path
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-anomaly",
				Namespace:   "monitoring",
				Annotations: map[string]string{"not": "touch"},
				Labels:      map[string]string{"main": "system"},
			},
			Spec: vmv1.VMAnomalySpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
				License: &vmv1beta1.License{
					Key: ptr.To("test"),
				},
				ConfigRawYaml: `
reader:
  queries:
    query_alias2:
      expr: vm_metric
models:
  model_univariate_1:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['query_alias2']
schedulers:
  scheduler_periodic_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: 1m
    fit_every: 2m
    fit_window: 3h
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://test.com",
					SamplingPeriod: "1m",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://write.endpoint",
				},
				Server: &vmv1.VMAnomalyServerSpec{
					PathPrefix: "custom-prefix",
				},
			},
		},
		validate: func(set *appsv1.StatefulSet) {
			assert.Len(t, set.Spec.Template.Spec.Containers, 1)
			container := set.Spec.Template.Spec.Containers[0]
			expectedPath := "/custom-prefix/health"
			assert.Equal(t, container.LivenessProbe.HTTPGet.Path, expectedPath)
			assert.Equal(t, container.ReadinessProbe.HTTPGet.Path, expectedPath)
		},
	})
}

func Test_createDefaultConfig(t *testing.T) {
	type opts struct {
		cr                  *vmv1.VMAnomaly
		wantErr             bool
		predefinedObjects   []runtime.Object
		secretMustBeMissing bool
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		cfg := map[build.ResourceKind]*build.ResourceCfg{
			build.TLSAssetsResourceKind: {
				MountDir:   tlsAssetsDir,
				SecretName: build.ResourceName(build.TLSAssetsResourceKind, o.cr),
			},
		}
		ctx := context.TODO()
		ac := build.NewAssetsCache(ctx, fclient, cfg)
		_, err := createOrUpdateConfig(ctx, fclient, o.cr, nil, ac)
		if o.wantErr {
			assert.Error(t, err)
			return
		}
		assert.NoError(t, err)
		var createdSecret corev1.Secret
		secretName := build.ResourceName(build.SecretConfigResourceKind, o.cr)

		err = fclient.Get(ctx, types.NamespacedName{Namespace: o.cr.Namespace, Name: secretName}, &createdSecret)
		if err != nil {
			if k8serrors.IsNotFound(err) && o.secretMustBeMissing {
				return
			}
			if !assert.NoError(t, err, "config for vmanomaly not exist") {
				return
			}
		}
	}

	// create vmanomaly config
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-anomaly",
			},
			Spec: vmv1.VMAnomalySpec{
				ConfigRawYaml: `
reader:
  queries:
    query_alias2:
      expr: vm_metric
writer:
  datasource_url: "http://test.com"
models:
  model_univariate_1:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['query_alias2']
schedulers:
  scheduler_periodic_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: 1m
    fit_every: 2m
    fit_window: 3h
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://test",
					QueryRangePath: "/api/v1/query_range",
					SamplingPeriod: "10s",
					VMAnomalyHTTPClientSpec: vmv1.VMAnomalyHTTPClientSpec{
						TenantID: "0",
						TLSConfig: &vmv1beta1.TLSConfig{
							CA: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls",
									},
									Key: "remote-ca",
								},
							},
							Cert: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls",
									},
									Key: "remote-cert",
								},
							},
							KeySecret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "tls",
								},
								Key: "remote-key",
							},
						},
					},
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://test",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "tls",
				},
				Data: map[string][]byte{
					"remote-ca":   []byte("ca"),
					"remote-cert": []byte("cert"),
					"remote-key":  []byte("key"),
				},
			},
		},
	})

	// with raw config
	f(opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-anomaly",
			},
			Spec: vmv1.VMAnomalySpec{
				ConfigRawYaml: "some-bad-yaml",
			},
		},
		predefinedObjects: []runtime.Object{},
		wantErr:           true,
	})
}
