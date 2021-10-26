package factory

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-test/deep"
	"github.com/hashicorp/go-version"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/pointer"

	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func Test_createDefaultAMConfig(t *testing.T) {
	type args struct {
		ctx       context.Context
		cr        *victoriametricsv1beta1.VMAlertmanager
		amVersion *version.Version
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
				cr: &victoriametricsv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-am",
					},
					Spec: victoriametricsv1beta1.VMAlertmanagerSpec{},
				},
			},
			predefinedObjects: []runtime.Object{},
		},
		{
			name: "with exist config",
			args: args{
				ctx: context.TODO(),
				cr: &victoriametricsv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-am",
					},
					Spec: victoriametricsv1beta1.VMAlertmanagerSpec{
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
				cr: &victoriametricsv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-am",
					},
					Spec: victoriametricsv1beta1.VMAlertmanagerSpec{
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
				cr: &victoriametricsv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-am",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAlertmanagerSpec{
						ConfigSecret:            "some-name",
						ConfigRawYaml:           "global: {}",
						ConfigSelector:          &metav1.LabelSelector{},
						ConfigNamespaceSelector: &metav1.LabelSelector{},
					},
				},
				amVersion: alertmanagerConfigMinimumVersion,
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMAlertmanagerConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "some",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAlertmanagerConfigSpec{
						Route: &victoriametricsv1beta1.Route{Receiver: "base"},
						Receivers: []victoriametricsv1beta1.Receiver{
							{
								Name: "base",
								WebhookConfigs: []victoriametricsv1beta1.WebhookConfig{
									{URL: pointer.String("http://some-url")},
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
			if err := createDefaultAMConfig(tt.args.ctx, tt.args.cr, fclient, tt.args.amVersion); (err != nil) != tt.wantErr {
				t.Fatalf("createDefaultAMConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			var createdSecret v1.Secret
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

func TestCreateOrUpdateAlertManager(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *victoriametricsv1beta1.VMAlertmanager
		c   *config.BaseOperatorConf
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
				c:   config.MustGetBaseConfig(),
				cr: &victoriametricsv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "test-am",
						Namespace:   "monitoring",
						Annotations: map[string]string{"not": "touch"},
						Labels:      map[string]string{"main": "system"},
					},
					Spec: victoriametricsv1beta1.VMAlertmanagerSpec{
						ReplicaCount: pointer.Int32Ptr(1),
					},
				},
			},
			wantErr: false,
			validate: func(set *appsv1.StatefulSet) error {
				if set.Name != "vmalertmanager-test-am" {
					return fmt.Errorf("unexpected name, got: %s, want: %s", set.Name, "vmalertmanager-test-am")
				}
				if diff := deep.Equal(set.Labels, map[string]string{
					"app.kubernetes.io/component": "monitoring",
					"app.kubernetes.io/instance":  "test-am",
					"app.kubernetes.io/name":      "vmalertmanager",
					"managed-by":                  "vm-operator",
					"main":                        "system",
				}); len(diff) > 0 {
					return fmt.Errorf("unexpected diff: %v", diff)
				}
				return nil
			},
		},
		{
			name: "alertmanager with embedded probe",
			args: args{
				ctx: context.TODO(),
				c:   config.MustGetBaseConfig(),
				cr: &victoriametricsv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "test-am",
						Namespace:   "monitoring",
						Annotations: map[string]string{"not": "touch"},
						Labels:      map[string]string{"main": "system"},
					},
					Spec: victoriametricsv1beta1.VMAlertmanagerSpec{
						ReplicaCount: pointer.Int32Ptr(1),
						EmbeddedProbes: &victoriametricsv1beta1.EmbeddedProbes{
							LivenessProbe: &v1.Probe{
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjets)
			err := CreateOrUpdateAlertManager(tt.args.ctx, tt.args.cr, fclient, tt.args.c)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CreateOrUpdateAlertManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			var got appsv1.StatefulSet
			if err := fclient.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: tt.args.cr.PrefixedName()}, &got); (err != nil) != tt.wantErr {
				t.Fatalf("CreateOrUpdateAlertManager() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err := tt.validate(&got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
