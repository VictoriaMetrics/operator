package vmsingle

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
		synctest.Test(t, func(t *testing.T) {
			assert.NoError(t, CreateOrUpdate(ctx, o.cr, fclient))
			if o.validate != nil {
				o.validate(ctx, fclient, o.cr)
			}
		})
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
			assert.Equal(t, got.Name, "vmsingle-vmsingle-base")
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
			assert.Equal(t, got.Name, "vmsingle-vmsingle-base")
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
			assert.Equal(t, got.Labels, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vmsingle",
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
			assert.Equal(t, got.Annotations, map[string]string{"controller": "true"})
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
			Spec: vmv1beta1.VMSingleSpec{
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
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			nsn := types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, nsn, &got))
			assert.Equal(t, got.Labels, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vmsingle",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			})
			assert.Equal(t, got.Annotations, map[string]string{
				"env": "base",
			})
			var pvc corev1.PersistentVolumeClaim
			assert.NoError(t, rclient.Get(ctx, nsn, &pvc))
			assert.Equal(t, pvc.Labels, map[string]string{
				"env":                         "test",
				"app.kubernetes.io/name":      "vmsingle",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			})
			assert.Equal(t, pvc.Annotations, map[string]string{
				"env": "test",
			})
		}})

	// openshift service CA volume — mode enabled mounts regardless of cluster
	f(opts{
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.OpenshiftCompatibilityMode = "enabled"
		},
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got))
			var caVol *corev1.Volume
			for i := range got.Spec.Template.Spec.Volumes {
				if got.Spec.Template.Spec.Volumes[i].Name == "openshift-service-ca" {
					caVol = &got.Spec.Template.Spec.Volumes[i]
					break
				}
			}
			if assert.NotNil(t, caVol, "expected openshift-service-ca volume") {
				assert.Equal(t, "openshift-service-ca.crt", caVol.ConfigMap.Name)
			}
			for _, c := range got.Spec.Template.Spec.Containers {
				if c.Name != "vmsingle" {
					continue
				}
				var caMount *corev1.VolumeMount
				for i := range c.VolumeMounts {
					if c.VolumeMounts[i].Name == "openshift-service-ca" {
						caMount = &c.VolumeMounts[i]
						break
					}
				}
				if assert.NotNil(t, caMount, "expected openshift-service-ca volumeMount in vmsingle container") {
					assert.Equal(t, "/etc/ssl/certs/openshift-service-ca", caMount.MountPath)
					assert.True(t, caMount.ReadOnly)
				}
			}
		},
	})

	// openshift service CA volume — mode disabled skips regardless of config
	f(opts{
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.OpenshiftCompatibilityMode = "disabled"
		},
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) {
			var got appsv1.Deployment
			assert.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got))
			for _, v := range got.Spec.Template.Spec.Volumes {
				assert.NotEqual(t, "openshift-service-ca", v.Name)
			}
		},
	})
}

func TestMakeSpecForVMSingleOk(t *testing.T) {
	type opts struct {
		cr                     *vmv1beta1.VMSingle
		extraConfigSecretCount int
		wantYaml               string
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(nil)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(o.cr)
		// Compare only the PodSpec, not the ObjectMeta, same as the vmagent test.
		var wantSpec corev1.PodSpec
		assert.NoError(t, yaml.Unmarshal([]byte(o.wantYaml), &wantSpec))
		wantYAMLForCompare, err := yaml.Marshal(wantSpec)
		assert.NoError(t, err)
		got, err := newPodSpec(ctx, o.cr, o.extraConfigSecretCount)
		assert.NoError(t, err)
		gotYAML, err := yaml.Marshal(got.Spec)
		assert.NoError(t, err)
		assert.Equal(t, string(wantYAMLForCompare), string(gotYAML))
	}

	// scrape config split across 2 extra secrets — verifies volumes, mounts, and reloader flags
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{Name: "single", Namespace: "default"},
			Spec: vmv1beta1.VMSingleSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(false),
				},
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					Image: vmv1beta1.Image{
						Tag: "v1.97.1",
					},
					UseDefaultResources: ptr.To(false),
					Port:                "8428",
				},
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImage: "vmcustomer:v1",
				},
			},
		},
		extraConfigSecretCount: 2,
		wantYaml: `
volumes:
    - name: data
      volumesource:
        emptydir: {}
    - name: tls-assets
      volumesource:
        secret:
            secretname: tls-assets-vmsingle-single
    - name: config-out
      volumesource:
        emptydir: {}
    - name: config
      volumesource:
        secret:
            secretname: vmsingle-single
    - name: sc-files-out
      volumesource:
        emptydir: {}
    - name: sc-raw-1
      volumesource:
        secret:
            secretname: vmsingle-single-sc-1
    - name: sc-raw-2
      volumesource:
        secret:
            secretname: vmsingle-single-sc-2
initcontainers:
    - name: config-init
      image: vmcustomer:v1
      args:
        - --config-envsubst-file=/etc/vm/config_out/scrape.yaml
        - --config-secret-key=scrape.yaml.gz
        - --config-secret-name=default/vmsingle-single
        - --only-init-config
        - --reload-url=http://127.0.0.1:8428/-/reload
        - --webhook-method=POST
      volumemounts:
        - name: config-out
          mountpath: /etc/vm/config_out
containers:
    - name: vmsingle
      image: victoriametrics/victoria-metrics:v1.97.1
      args:
        - -httpListenAddr=:8428
        - -promscrape.config=/etc/vm/config_out/scrape.yaml
        - -storageDataPath=/victoria-metrics-data
      ports:
        - name: http
          containerport: 8428
          protocol: TCP
      volumemounts:
        - name: data
          mountpath: /victoria-metrics-data
        - name: config-out
          readonly: true
          mountpath: /etc/vm/config_out
        - name: tls-assets
          readonly: true
          mountpath: /etc/vm-tls/certs
        - name: config
          readonly: true
          mountpath: /etc/vm/config
        - name: sc-files-out
          readonly: true
          mountpath: /etc/vm/sc-files
      readinessprobe:
        probehandler:
          httpget:
            path: /health
            port:
              intval: 8428
            scheme: HTTP
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
      terminationmessagepolicy: FallbackToLogsOnError
      imagepullpolicy: IfNotPresent
    - name: config-reloader
      image: vmcustomer:v1
      args:
        - --config-envsubst-file=/etc/vm/config_out/scrape.yaml
        - --config-secret-key=scrape.yaml.gz
        - --config-secret-name=default/vmsingle-single
        - --reload-url=http://127.0.0.1:8428/-/reload
        - --webhook-method=POST
        - --watched-dir=/etc/vm/sc-raw-1
        - --target-dir=/etc/vm/sc-files/sc-raw-1
        - --watched-dir=/etc/vm/sc-raw-2
        - --target-dir=/etc/vm/sc-files/sc-raw-2
      ports:
        - name: reloader-http
          containerport: 8435
          protocol: TCP
      volumemounts:
        - name: config-out
          mountpath: /etc/vm/config_out
        - name: sc-files-out
          mountpath: /etc/vm/sc-files
        - name: sc-raw-1
          readonly: true
          mountpath: /etc/vm/sc-raw-1
        - name: sc-raw-2
          readonly: true
          mountpath: /etc/vm/sc-raw-2
      livenessprobe:
        probehandler:
          httpget:
            path: /health
            port:
              intval: 8435
            scheme: HTTP
        timeoutseconds: 1
        periodseconds: 10
        successthreshold: 1
        failurethreshold: 3
      readinessprobe:
        probehandler:
          httpget:
            path: /health
            port:
              intval: 8435
            scheme: HTTP
        initialdelayseconds: 5
        timeoutseconds: 1
        periodseconds: 10
        successthreshold: 1
        failurethreshold: 3
      terminationmessagepolicy: FallbackToLogsOnError
serviceaccountname: vmsingle-single
`,
	})
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
			assert.Equal(t, svc.Name, "vmsingle-single-1")
			assert.Equal(t, svc.Namespace, "default")

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
			assert.Equal(t, svc.Spec.Ports, expectedPorts)
			assert.Equal(t, svc.Labels, map[string]string{
				"app.kubernetes.io/name":      "vmsingle",
				"app.kubernetes.io/instance":  "single-1",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			})
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
			assert.Equal(t, svc.Name, "vmsingle-single-1")
			assert.Equal(t, svc.Namespace, "default")
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
			assert.Equal(t, svc.Labels, map[string]string{
				"app.kubernetes.io/name":      "vmsingle",
				"app.kubernetes.io/instance":  "single-1",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			})
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
			assert.Equal(t, svc.Name, "vmsingle-single-1")
			assert.Equal(t, svc.Namespace, "default")
			assert.Len(t, svc.Spec.Ports, 2)
			assert.Equal(t, svc.Labels, map[string]string{
				"app.kubernetes.io/name":      "vmsingle",
				"app.kubernetes.io/instance":  "single-1",
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
