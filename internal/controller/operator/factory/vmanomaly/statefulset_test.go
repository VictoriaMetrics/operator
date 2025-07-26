package vmanomaly

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-test/deep"
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
		cr       *vmv1.VMAnomaly
		validate func(set *appsv1.StatefulSet) error
	}
	f := func(opts opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(nil)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(opts.cr)
		ctx, cancel := context.WithTimeout(context.TODO(), time.Second*20)
		defer cancel()

		go func() {
			tc := time.NewTicker(time.Millisecond * 100)
			for {
				select {
				case <-ctx.Done():
					return
				case <-tc.C:
					var got appsv1.StatefulSet
					if err := fclient.Get(ctx, types.NamespacedName{Namespace: opts.cr.Namespace, Name: opts.cr.PrefixedName()}, &got); err != nil {
						if !k8serrors.IsNotFound(err) {
							t.Errorf("cannot get statefulset for vmanomaly: %s", err)
							return
						}
						continue
					}
					got.Status.ReadyReplicas = *opts.cr.Spec.ReplicaCount
					got.Status.UpdatedReplicas = *opts.cr.Spec.ReplicaCount

					if err := fclient.Status().Update(ctx, &got); err != nil {
						t.Errorf("cannot update status statefulset for vmanomaly: %s", err)
					}
					return
				}
			}
		}()
		err := CreateOrUpdate(ctx, opts.cr, fclient)
		if err != nil {
			t.Fatalf("CreateOrUpdate() error = %v", err)
		}
		// TODO add client.Default
		var got appsv1.StatefulSet
		if err := fclient.Get(ctx, types.NamespacedName{Namespace: opts.cr.Namespace, Name: opts.cr.PrefixedName()}, &got); err != nil {
			t.Fatalf("CreateOrUpdate() error = %v", err)
		}
		if err := opts.validate(&got); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// simple vmanomaly
	o := opts{
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
		validate: func(set *appsv1.StatefulSet) error {
			if set.Name != "vmanomaly-test-anomaly" {
				return fmt.Errorf("unexpected name, got: %s, want: %s", set.Name, "vmanomaly-test-anomaly")
			}
			if diff := deep.Equal(set.Spec.Template.Spec.Containers[0].Resources, corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("500Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("200Mi"),
				},
			}); len(diff) > 0 {
				return fmt.Errorf("unexpected diff with resources: %v", diff)
			}
			if diff := deep.Equal(set.Labels, map[string]string{
				"app.kubernetes.io/component": "monitoring",
				"app.kubernetes.io/instance":  "test-anomaly",
				"app.kubernetes.io/name":      "vmanomaly",
				"managed-by":                  "vm-operator",
			}); len(diff) > 0 {
				return fmt.Errorf("unexpected diff with labels: %v", diff)
			}
			return nil
		},
	}
	f(o)

	// vmanomaly with embedded probe
	o = opts{
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
		validate: func(set *appsv1.StatefulSet) error {
			if len(set.Spec.Template.Spec.Containers) != 1 {
				return fmt.Errorf("unexpected count of container, got: %d, want: %d", len(set.Spec.Template.Spec.Containers), 2)
			}
			container := set.Spec.Template.Spec.Containers[0]
			if container.Name != "vmanomaly" {
				return fmt.Errorf("unexpected container name, got: %s, want: %s", container.Name, "vmanomaly")
			}
			if container.LivenessProbe.TimeoutSeconds != 20 {
				return fmt.Errorf("unexpected liveness probe config, want timeout: %d, got: %d", container.LivenessProbe.TimeoutSeconds, 20)
			}
			if container.LivenessProbe.HTTPGet.Path != "/health" {
				return fmt.Errorf("unexpected path for probe, got: %s, want: %s", container.LivenessProbe.HTTPGet.Path, "/health")
			}
			if container.ReadinessProbe.HTTPGet.Path != "/health" {
				return fmt.Errorf("unexpected path for probe, got: %s, want: %s", container.ReadinessProbe.HTTPGet.Path, "/health")
			}
			return nil
		},
	}
	f(o)
}

func Test_createDefaultConfig(t *testing.T) {
	type opts struct {
		cr                *vmv1.VMAnomaly
		wantErr           bool
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		cfg := map[build.ResourceKind]*build.ResourceCfg{
			build.TLSAssetsResourceKind: {
				MountDir:   tlsAssetsDir,
				SecretName: build.ResourceName(build.TLSAssetsResourceKind, opts.cr),
			},
		}
		ctx := context.TODO()
		ac := build.NewAssetsCache(ctx, fclient, cfg)
		if _, err := createOrUpdateConfig(ctx, fclient, opts.cr, nil, ac); (err != nil) != opts.wantErr {
			t.Fatalf("createOrUpdateConfig() error = %v, wantErr %v", err, opts.wantErr)
		}
		if opts.wantErr {
			return
		}
		var createdSecret corev1.Secret
		secretName := build.ResourceName(build.SecretConfigResourceKind, opts.cr)

		err := fclient.Get(ctx, types.NamespacedName{Namespace: opts.cr.Namespace, Name: secretName}, &createdSecret)
		if err != nil {
			t.Fatalf("config for vmanomaly not exist, err: %v", err)
		}
	}

	// create vmanomaly config
	o := opts{
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
	}
	f(o)

	// with raw config
	o = opts{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-anomaly",
			},
			Spec: vmv1.VMAnomalySpec{
				ConfigRawYaml: "some-bad-yaml",
			},
		},
		wantErr: true,
	}
	f(o)
}
