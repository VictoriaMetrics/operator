package alertmanager

import (
	"context"
	"fmt"
	"testing"
	"time"

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

func TestCreateOrUpdateAlertManager(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *vmv1beta1.VMAlertmanager
	}
	tests := []struct {
		name             string
		args             args
		validate         func(set *appsv1.StatefulSet) error
		wantErr          bool
		predefinedObjets []runtime.Object
	}{
		{
			name: "simple alertmanager",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1beta1.VMAlertmanager{
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
			},
			wantErr: false,
			validate: func(set *appsv1.StatefulSet) error {
				if set.Name != "vmalertmanager-test-am" {
					return fmt.Errorf("unexpected name, got: %s, want: %s", set.Name, "vmalertmanager-test-am")
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
					"app.kubernetes.io/instance":  "test-am",
					"app.kubernetes.io/name":      "vmalertmanager",
					"managed-by":                  "vm-operator",
					"main":                        "system",
				}); len(diff) > 0 {
					return fmt.Errorf("unexpected diff with labels: %v", diff)
				}
				return nil
			},
		},
		{
			name: "alertmanager with embedded probe",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1beta1.VMAlertmanager{
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
				if len(set.Spec.Template.Spec.Containers) != 2 {
					return fmt.Errorf("unexpected count of container, got: %d, want: %d", len(set.Spec.Template.Spec.Containers), 2)
				}
				vmaContainer := set.Spec.Template.Spec.Containers[0]
				if vmaContainer.Name != "alertmanager" {
					return fmt.Errorf("unexpected container name, got: %s, want: %s", vmaContainer.Name, "alertmanager")
				}
				if vmaContainer.LivenessProbe.TimeoutSeconds != 20 {
					return fmt.Errorf("unexpected liveness probe config, want timeout: %d, got: %d", vmaContainer.LivenessProbe.TimeoutSeconds, 20)
				}
				if vmaContainer.LivenessProbe.HTTPGet.Path != "/-/healthy" {
					return fmt.Errorf("unexpected path for probe, got: %s, want: %s", vmaContainer.LivenessProbe.HTTPGet.Path, "/-/healthy")
				}
				if vmaContainer.ReadinessProbe.HTTPGet.Path != "/-/healthy" {
					return fmt.Errorf("unexpected path for probe, got: %s, want: %s", vmaContainer.ReadinessProbe.HTTPGet.Path, "/-/healthy")
				}

				return nil
			},
		},
		{
			name: "alertmanager with templates",
			predefinedObjets: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-am",
						Namespace: "monitoring",
					},
					Data: map[string]string{
						"test_1.tmpl": "test_1",
						"test_2.tmpl": "test_2",
					},
				},
			},
			args: args{
				ctx: context.TODO(),
				cr: &vmv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "test-am",
						Namespace:   "monitoring",
						Annotations: map[string]string{"not": "touch"},
						Labels:      map[string]string{"main": "system"},
					},
					Spec: vmv1beta1.VMAlertmanagerSpec{
						Templates: []vmv1beta1.ConfigMapKeyReference{
							{LocalObjectReference: corev1.LocalObjectReference{Name: "test-am"}, Key: "test_1.tmpl"},
							{LocalObjectReference: corev1.LocalObjectReference{Name: "test-am"}, Key: "test_2.tmpl"},
						},
					},
				},
			},
			wantErr: false,
			validate: func(set *appsv1.StatefulSet) error {
				if set.Name != "vmalertmanager-test-am" {
					return fmt.Errorf("unexpected name, got: %s, want: %s", set.Name, "vmalertmanager-test-am")
				}
				if len(set.Spec.Template.Spec.Volumes) != 4 {
					return fmt.Errorf("unexpected count of volumes, got: %d, want: %d", len(set.Spec.Template.Spec.Volumes), 4)
				}
				templatesVolume := set.Spec.Template.Spec.Volumes[2]
				if templatesVolume.Name != "templates-test-am" {
					return fmt.Errorf("unexpected volume name, got: %s, want: %s", templatesVolume.Name, "templates-test-am")
				}
				if templatesVolume.ConfigMap.Name != "test-am" {
					return fmt.Errorf("unexpected configmap name, got: %s, want: %s", templatesVolume.ConfigMap.Name, "test-am")
				}

				vmaContainer := set.Spec.Template.Spec.Containers[0]
				if vmaContainer.Name != "alertmanager" {
					return fmt.Errorf("unexpected container name, got: %s, want: %s", vmaContainer.Name, "alertmanager")
				}

				if len(vmaContainer.VolumeMounts) != 4 {
					return fmt.Errorf("unexpected count of volume mounts, got: %d, want: %d", len(vmaContainer.VolumeMounts), 4)
				}
				templatesVolumeMount := vmaContainer.VolumeMounts[3]
				if templatesVolumeMount.Name != "templates-test-am" {
					return fmt.Errorf("unexpected volume name, got: %s, want: %s", templatesVolumeMount.Name, "templates-test-am")
				}
				if templatesVolumeMount.MountPath != "/etc/vm/templates/test-am" {
					return fmt.Errorf("unexpected volume mount path, got: %s, want: %s", templatesVolumeMount.MountPath, "/etc/vm/templates/test-am")
				}
				if !templatesVolumeMount.ReadOnly {
					return fmt.Errorf("unexpected volume mount read only, got: %t, want: %t", templatesVolumeMount.ReadOnly, true)
				}

				foundTemplatesDir := false
				for _, arg := range set.Spec.Template.Spec.Containers[1].Args {
					if arg == "-volume-dir=/etc/vm/templates/test-am" {
						foundTemplatesDir = true
					}
				}
				if !foundTemplatesDir {
					return fmt.Errorf("templates dir not found in args of config-reloader container")
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
								t.Errorf("cannot get statefulset for alertmanager: %s", err)
								return
							}
							continue
						}
						got.Status.ReadyReplicas = *tt.args.cr.Spec.ReplicaCount
						got.Status.UpdatedReplicas = *tt.args.cr.Spec.ReplicaCount

						if err := fclient.Status().Update(ctx, &got); err != nil {
							t.Errorf("cannot update status statefulset for alertmanager: %s", err)
						}
						return
					}
				}
			}()
			err := CreateOrUpdateAlertManager(ctx, tt.args.cr, fclient)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CreateOrUpdateAlertManager() error = %v, wantErr %v", err, tt.wantErr)
			}
			// TODO add client.Default
			var got appsv1.StatefulSet
			if err := fclient.Get(ctx, types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.PrefixedName()}, &got); (err != nil) != tt.wantErr {
				t.Fatalf("CreateOrUpdateAlertManager() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err := tt.validate(&got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func Test_createDefaultAMConfig(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *vmv1beta1.VMAlertmanager
	}
	tests := []struct {
		name                string
		args                args
		wantErr             bool
		predefinedObjects   []runtime.Object
		secretMustBeMissing bool
	}{
		{
			name: "create alertmanager config",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-am",
					},
					Spec: vmv1beta1.VMAlertmanagerSpec{},
				},
			},
			predefinedObjects: []runtime.Object{},
		},
		{
			name: "with exist config",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-am",
					},
					Spec: vmv1beta1.VMAlertmanagerSpec{
						ConfigSecret: "some-secret-name",
					},
				},
			},
			secretMustBeMissing: true,
			predefinedObjects:   []runtime.Object{},
		},
		{
			name: "with raw config",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-am",
					},
					Spec: vmv1beta1.VMAlertmanagerSpec{
						ConfigRawYaml: "some-bad-yaml",
					},
				},
			},
			predefinedObjects: []runtime.Object{},
		},
		{
			name: "with alertmanager config support",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-am",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAlertmanagerSpec{
						ConfigSecret:            "some-name",
						ConfigRawYaml:           "global: {}",
						ConfigSelector:          &metav1.LabelSelector{},
						ConfigNamespaceSelector: &metav1.LabelSelector{},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMAlertmanagerConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "some",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAlertmanagerConfigSpec{
						Route: &vmv1beta1.Route{Receiver: "base"},
						Receivers: []vmv1beta1.Receiver{
							{
								Name: "base",
								WebhookConfigs: []vmv1beta1.WebhookConfig{
									{URL: ptr.To("http://some-url")},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			if err := CreateAMConfig(tt.args.ctx, tt.args.cr, fclient); (err != nil) != tt.wantErr {
				t.Fatalf("createDefaultAMConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			var createdSecret corev1.Secret
			secretName := tt.args.cr.ConfigSecretName()

			err := fclient.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: secretName}, &createdSecret)
			if err != nil {
				if errors.IsNotFound(err) && tt.secretMustBeMissing {
					return
				}
				t.Fatalf("config for alertmanager not exist, err: %v", err)
			}
		})
	}
}
