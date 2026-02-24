package vmalertmanager

import (
	"context"
	"strings"
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

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdateAlertManager(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlertmanager
		validate          func(set *appsv1.StatefulSet)
		wantErr           bool
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(o.cr)
		ctx := context.TODO()
		err := CreateOrUpdateAlertManager(ctx, o.cr, fclient)
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

	// simple alertmanager
	f(opts{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-am",
				Namespace: "monitoring",
			},
			Spec: vmv1beta1.VMAlertmanagerSpec{
				ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{
					Annotations: map[string]string{"not": "touch"},
					Labels:      map[string]string{"main": "system"},
				},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		validate: func(set *appsv1.StatefulSet) {
			assert.Equal(t, set.Name, "vmalertmanager-test-am")
			assert.Equal(t, set.Spec.Template.Spec.Containers[0].Resources, corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("30m"),
					corev1.ResourceMemory: resource.MustParse("56Mi"),
				},
			})
			assert.Equal(t, set.Labels, map[string]string{
				"app.kubernetes.io/component": "monitoring",
				"app.kubernetes.io/instance":  "test-am",
				"app.kubernetes.io/name":      "vmalertmanager",
				"managed-by":                  "vm-operator",
				"main":                        "system",
			})
		},
	})

	// alertmanager with embedded probe
	f(opts{
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
		validate: func(set *appsv1.StatefulSet) {
			assert.Len(t, set.Spec.Template.Spec.Containers, 2)
			vmaContainer := set.Spec.Template.Spec.Containers[0]
			assert.Equal(t, vmaContainer.Name, "alertmanager")
			assert.Equal(t, vmaContainer.LivenessProbe.TimeoutSeconds, int32(20))
			assert.Equal(t, vmaContainer.LivenessProbe.HTTPGet.Path, "/-/healthy")
			assert.Equal(t, vmaContainer.ReadinessProbe.HTTPGet.Path, "/-/healthy")
		},
	})

	// alertmanager with templates
	f(opts{
		predefinedObjects: []runtime.Object{
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
		validate: func(set *appsv1.StatefulSet) {
			assert.Equal(t, set.Name, "vmalertmanager-test-am")
			assert.Len(t, set.Spec.Template.Spec.Volumes, 4)
			templatesVolume := set.Spec.Template.Spec.Volumes[2]
			assert.Equal(t, templatesVolume.Name, "templates-test-am")
			assert.Equal(t, templatesVolume.ConfigMap.Name, "test-am")
			vmaContainer := set.Spec.Template.Spec.Containers[0]
			assert.Equal(t, vmaContainer.Name, "alertmanager")
			assert.Len(t, vmaContainer.VolumeMounts, 4)
			templatesVolumeMount := vmaContainer.VolumeMounts[3]
			assert.Equal(t, templatesVolumeMount.Name, "templates-test-am")
			assert.Equal(t, templatesVolumeMount.MountPath, "/etc/vm/templates/test-am")
			assert.True(t, templatesVolumeMount.ReadOnly)
			foundTemplatesDir := false
			for _, arg := range set.Spec.Template.Spec.Containers[1].Args {
				if arg == "--watched-dir=/etc/vm/templates/test-am" {
					foundTemplatesDir = true
				}
			}
			assert.True(t, foundTemplatesDir)
		},
	})

	// alertmanager in cluster mode with undefined clusterDomainName
	f(opts{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-am",
				Namespace:   "monitoring",
				Annotations: map[string]string{"not": "touch"},
				Labels:      map[string]string{"main": "system"},
			},
			Spec: vmv1beta1.VMAlertmanagerSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(3)),
				},
			},
		},
		validate: func(set *appsv1.StatefulSet) {
			assert.Len(t, set.Spec.Template.Spec.Containers, 2)
			vmaContainer := set.Spec.Template.Spec.Containers[0]

			clusterPeers := make([]string, 0, 3)
			for _, arg := range vmaContainer.Args {
				if strings.HasPrefix(arg, "--cluster.peer=") {
					clusterPeers = append(clusterPeers, arg)
				}
			}

			assert.ElementsMatch(t, clusterPeers, []string{
				"--cluster.peer=vmalertmanager-test-am-0.vmalertmanager-test-am.monitoring:9094",
				"--cluster.peer=vmalertmanager-test-am-1.vmalertmanager-test-am.monitoring:9094",
				"--cluster.peer=vmalertmanager-test-am-2.vmalertmanager-test-am.monitoring:9094",
			}, "unexpected cluster peer arguments found")
		},
	})

	// alertmanager in cluster mode with clusterDomainName
	f(opts{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-am",
				Namespace:   "monitoring",
				Annotations: map[string]string{"not": "touch"},
				Labels:      map[string]string{"main": "system"},
			},
			Spec: vmv1beta1.VMAlertmanagerSpec{
				ClusterDomainName: "example.com",
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(3)),
				},
			},
		},
		validate: func(set *appsv1.StatefulSet) {
			assert.Len(t, set.Spec.Template.Spec.Containers, 2)
			vmaContainer := set.Spec.Template.Spec.Containers[0]

			clusterPeers := make([]string, 0, 3)
			for _, arg := range vmaContainer.Args {
				if strings.HasPrefix(arg, "--cluster.peer=") {
					clusterPeers = append(clusterPeers, arg)
				}
			}

			assert.ElementsMatch(t, clusterPeers, []string{
				"--cluster.peer=vmalertmanager-test-am-0.vmalertmanager-test-am.monitoring.svc.example.com.:9094",
				"--cluster.peer=vmalertmanager-test-am-1.vmalertmanager-test-am.monitoring.svc.example.com.:9094",
				"--cluster.peer=vmalertmanager-test-am-2.vmalertmanager-test-am.monitoring.svc.example.com.:9094",
			}, "unexpected cluster peer arguments found")
		},
	})
}

func Test_createDefaultAMConfig(t *testing.T) {
	type opts struct {
		cr                  *vmv1beta1.VMAlertmanager
		wantErr             bool
		predefinedObjects   []runtime.Object
		secretMustBeMissing bool
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.TODO()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		err := CreateOrUpdateConfig(ctx, fclient, o.cr, nil)
		if o.wantErr {
			assert.Error(t, err)
			return
		}
		assert.NoError(t, err)
		var createdSecret corev1.Secret
		secretName := o.cr.ConfigSecretName()

		err = fclient.Get(ctx, types.NamespacedName{Namespace: o.cr.Namespace, Name: secretName}, &createdSecret)
		if err != nil {
			if k8serrors.IsNotFound(err) && o.secretMustBeMissing {
				return
			}
			assert.NoError(t, err, "config for alertmanager not exist")
		}

		var amcfgs vmv1beta1.VMAlertmanagerConfigList
		assert.Nil(t, fclient.List(ctx, &amcfgs))
		for _, amcfg := range amcfgs.Items {
			assert.Equal(t, amcfg.Status.UpdateStatus, vmv1beta1.UpdateStatusOperational)
		}
	}

	// create alertmanager config
	f(opts{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-am",
			},
			Spec: vmv1beta1.VMAlertmanagerSpec{},
		},
		predefinedObjects: []runtime.Object{},
	})

	// with exist config
	f(opts{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-am",
			},
			Spec: vmv1beta1.VMAlertmanagerSpec{
				ConfigSecret: "some-secret-name",
			},
		},
		secretMustBeMissing: true,
		predefinedObjects:   []runtime.Object{},
	})

	// with raw config
	f(opts{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-am",
			},
			Spec: vmv1beta1.VMAlertmanagerSpec{
				ConfigRawYaml: "some-bad-yaml",
			},
		},
		predefinedObjects: []runtime.Object{},
		wantErr:           true,
	})

	// with alertmanager config support
	f(opts{
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
	})

	// with utf-8
	f(opts{
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
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAlertmanagerConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertmanagerConfigSpec{
					Route: &vmv1beta1.Route{Receiver: "base", Matchers: []string{`"baf"="daf"`}},
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
	})
}
