package vmanomaly

import (
	"context"
	"fmt"
	"testing"
	"time"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/go-test/deep"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func TestCreateOrUpdateDeploy(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *vmv1.VMAnomaly
	}
	tests := []struct {
		name             string
		args             args
		validate         func(set *appsv1.StatefulSet) error
		wantErr          bool
		predefinedObjets []runtime.Object
	}{
		{
			name: "simple vmanomaly",
			args: args{
				ctx: context.TODO(),
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
						ConfigRawYaml: `
reader:
  datasource_url: "http://test.com"
writer:
  class: vm
  datasource_url: "http://test.com"
models:
  model_univariate_1:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['query_alias2']
schedulers:
  scheduler_periodic_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: "1m"
    fit_every: "2m"
    fit_window: "3h"
`,
					},
				},
			},
			wantErr: false,
			validate: func(set *appsv1.StatefulSet) error {
				if set.Name != "vmanomaly-test-anomaly" {
					return fmt.Errorf("unexpected name, got: %s, want: %s", set.Name, "vmanomaly-test-anomaly")
				}
				if diff := deep.Equal(set.Spec.Template.Spec.Containers[0].Resources, corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("30m"),
						corev1.ResourceMemory: resource.MustParse("56Mi"),
					},
				}); len(diff) > 0 {
					return fmt.Errorf("unexpected diff with resources: %v", diff)
				}
				if diff := deep.Equal(set.Labels, map[string]string{
					"app.kubernetes.io/component": "monitoring",
					"app.kubernetes.io/instance":  "test-anomaly",
					"app.kubernetes.io/name":      "vmanomaly",
					"managed-by":                  "vm-operator",
					"main":                        "system",
				}); len(diff) > 0 {
					return fmt.Errorf("unexpected diff with labels: %v", diff)
				}
				return nil
			},
		},
		{
			name: "vmanomaly with embedded probe",
			args: args{
				ctx: context.TODO(),
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
						ConfigRawYaml: `
reader:
  datasource_url: "http://test.com"
writer:
  class: vm
  datasource_url: "http://test.com"
models:
  model_univariate_1:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['query_alias2']
schedulers:
  scheduler_periodic_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: "1m"                           
    fit_every: "2m"
    fit_window: "3h"
`,
						EmbeddedProbes: &vmv1beta1.EmbeddedProbes{
							LivenessProbe: &corev1.Probe{
								TimeoutSeconds: 20,
							},
						},
					},
				},
			},
			wantErr: false,
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjets)
			build.AddDefaults(fclient.Scheme())
			fclient.Scheme().Default(tt.args.cr)
			ctx, cancel := context.WithTimeout(tt.args.ctx, time.Second*20)
			defer cancel()

			go func() {
				tc := time.NewTicker(time.Millisecond * 100)
				for {
					select {
					case <-ctx.Done():
						return
					case <-tc.C:
						var got appsv1.StatefulSet
						if err := fclient.Get(ctx, types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.PrefixedName()}, &got); err != nil {
							if !errors.IsNotFound(err) {
								t.Errorf("cannot get statefulset for vmanomaly: %s", err)
								return
							}
							continue
						}
						got.Status.ReadyReplicas = *tt.args.cr.Spec.ReplicaCount
						got.Status.UpdatedReplicas = *tt.args.cr.Spec.ReplicaCount

						if err := fclient.Status().Update(ctx, &got); err != nil {
							t.Errorf("cannot update status statefulset for vmanomaly: %s", err)
						}
						return
					}
				}
			}()
			err := CreateOrUpdateDeploy(ctx, tt.args.cr, fclient)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CreateOrUpdateDeploy() error = %v, wantErr %v", err, tt.wantErr)
			}
			// TODO add client.Default
			var got appsv1.StatefulSet
			if err := fclient.Get(ctx, types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.PrefixedName()}, &got); (err != nil) != tt.wantErr {
				t.Fatalf("CreateOrUpdateDeploy() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err := tt.validate(&got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func Test_createDefaultConfig(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *vmv1.VMAnomaly
	}
	tests := []struct {
		name                string
		args                args
		wantErr             bool
		predefinedObjects   []runtime.Object
		secretMustBeMissing bool
	}{
		{
			name: "create vmanomaly config",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1.VMAnomaly{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-anomaly",
					},
					Spec: vmv1.VMAnomalySpec{
						ConfigRawYaml: `
reader:
  datasource_url: "http://test.com"
writer:
  class: vm
  datasource_url: "http://test.com"
models:
  model_univariate_1:
    class: 'zscore'
    z_threshold: 2.5
    queries: ['query_alias2']
schedulers:
  scheduler_periodic_1m:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: "1m"
    fit_every: "2m"
    fit_window: "3h"
`,
					},
				},
			},
			predefinedObjects: []runtime.Object{},
		},
		{
			name: "with raw config",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1.VMAnomaly{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-anomaly",
					},
					Spec: vmv1.VMAnomalySpec{
						ConfigRawYaml: "some-bad-yaml",
					},
				},
			},
			predefinedObjects: []runtime.Object{},
			wantErr:           true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			if err := CreateOrUpdateConfig(tt.args.ctx, fclient, tt.args.cr); (err != nil) != tt.wantErr {
				t.Fatalf("createDefaultConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			var createdSecret corev1.Secret
			secretName := tt.args.cr.ConfigSecretName()

			err := fclient.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: secretName}, &createdSecret)
			if err != nil {
				if errors.IsNotFound(err) && tt.secretMustBeMissing {
					return
				}
				t.Fatalf("config for vmanomaly not exist, err: %v", err)
			}
		})
	}
}
