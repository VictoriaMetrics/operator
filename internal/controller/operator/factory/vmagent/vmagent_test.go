package vmagent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-test/deep"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAgent
		mustAddPrevSpec   bool
		validate          func(set *appsv1.StatefulSet) error
		statefulsetMode   bool
		wantErr           bool
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		if o.mustAddPrevSpec {
			jsonSpec, err := json.Marshal(o.cr.Spec)
			if err != nil {
				t.Fatalf("cannot set last applied spec: %s", err)
			}
			if o.cr.Annotations == nil {
				o.cr.Annotations = make(map[string]string)
			}
			o.cr.Annotations["operator.victoriametrics/last-applied-spec"] = string(jsonSpec)
		}
		errC := make(chan error, 1)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(o.cr)
		go func() {
			err := CreateOrUpdate(ctx, o.cr, fclient)
			errC <- err
		}()

		if o.statefulsetMode {
			if o.cr.Spec.ShardCount != nil {
				for i := 0; i < *o.cr.Spec.ShardCount; i++ {
					err := wait.PollUntilContextTimeout(context.Background(), 20*time.Millisecond, time.Second, true, func(ctx context.Context) (done bool, err error) {
						var sts appsv1.StatefulSet
						if err := fclient.Get(ctx, types.NamespacedName{
							Namespace: "default",
							Name:      fmt.Sprintf("vmagent-%s-%d", o.cr.Name, i),
						}, &sts); err != nil {

							return false, nil
						}
						sts.Status.ObservedGeneration = sts.Generation
						sts.Status.ReadyReplicas = ptr.Deref(o.cr.Spec.ReplicaCount, 0)
						sts.Status.UpdatedReplicas = ptr.Deref(o.cr.Spec.ReplicaCount, 0)
						sts.Status.CurrentReplicas = ptr.Deref(o.cr.Spec.ReplicaCount, 0)
						if err := fclient.Status().Update(ctx, &sts); err != nil {
							return false, err
						}

						return true, nil
					})
					if err != nil {
						t.Errorf("cannot wait sts ready: %s", err)
					}
					if o.cr.Spec.PodDisruptionBudget != nil {
						err = wait.PollUntilContextTimeout(context.Background(), 20*time.Millisecond, time.Second, true, func(ctx context.Context) (done bool, err error) {
							var pdb policyv1.PodDisruptionBudget
							if err := fclient.Get(ctx, types.NamespacedName{
								Namespace: "default",
								Name:      fmt.Sprintf("vmagent-%s-%d", o.cr.Name, i),
							}, &pdb); err != nil {

								return false, nil
							}
							return true, nil
						})
						if err != nil {
							t.Errorf("pdb vmagent-%s-%d didn't get created: %s", o.cr.Name, i, err)
						}
					}
				}
			} else {
				err := wait.PollUntilContextTimeout(context.Background(), 20*time.Millisecond, time.Second, true, func(ctx context.Context) (done bool, err error) {
					var sts appsv1.StatefulSet
					if err := fclient.Get(ctx, types.NamespacedName{Namespace: "default", Name: fmt.Sprintf("vmagent-%s", o.cr.Name)}, &sts); err != nil {
						return false, nil
					}
					sts.Status.ReadyReplicas = ptr.Deref(o.cr.Spec.ReplicaCount, 0)
					sts.Status.UpdatedReplicas = ptr.Deref(o.cr.Spec.ReplicaCount, 0)
					sts.Status.CurrentReplicas = ptr.Deref(o.cr.Spec.ReplicaCount, 0)
					err = fclient.Status().Update(ctx, &sts)
					if err != nil {
						return false, err
					}
					return true, nil
				})
				if err != nil {
					t.Errorf("cannot wait sts ready: %s", err)
				}
			}
		}
		err := <-errC
		if (err != nil) != o.wantErr {
			t.Errorf("CreateOrUpdate() error: %s", cmp.Diff(err, o.wantErr))
			return
		}
		if o.statefulsetMode && o.cr.Spec.ShardCount == nil {
			var got appsv1.StatefulSet
			if err := fclient.Get(context.Background(), types.NamespacedName{Namespace: o.cr.Namespace, Name: o.cr.PrefixedName()}, &got); (err != nil) != o.wantErr {
				t.Fatalf("CreateOrUpdate() statefulMode error: %s", cmp.Diff(err, o.wantErr))
			}
			if err := o.validate(&got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	}

	// generate vmagent statefulset with storage
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{},
				StatefulMode:            true,
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
				StatefulStorage: &vmv1beta1.StorageSpec{
					VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: ptr.To("embed-sc"),
							Resources: corev1.VolumeResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceStorage: resource.MustParse("10Gi"),
								},
							},
						},
					},
				},
				ClaimTemplates: []corev1.PersistentVolumeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "extraTemplate",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: ptr.To("default"),
							Resources: corev1.VolumeResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceStorage: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
			},
		},
		validate: func(got *appsv1.StatefulSet) error {
			assert.Equal(t, 1, len(got.Spec.Template.Spec.Containers))
			assert.Equal(t, 2, len(got.Spec.VolumeClaimTemplates))
			assert.Equal(t, "embed-sc", *got.Spec.VolumeClaimTemplates[0].Spec.StorageClassName)
			if diff := deep.Equal(got.Spec.VolumeClaimTemplates[0].Spec.Resources, corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			}); len(diff) != 0 {
				return fmt.Errorf("unexpected embed VolumeClaimTemplates resources, diff: %v", diff)
			}
			assert.Equal(t, "default", *got.Spec.VolumeClaimTemplates[1].Spec.StorageClassName)
			if diff := deep.Equal(got.Spec.VolumeClaimTemplates[1].Spec.Resources, corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("2Gi"),
				},
			}); len(diff) != 0 {
				return fmt.Errorf("unexpected extra VolumeClaimTemplates resources, diff: %v", diff)
			}
			return nil
		},
		statefulsetMode: true,
		predefinedObjects: []runtime.Object{
			k8stools.NewReadyDeployment("vmagent-example-agent", "default"),
		},
	})

	// generate with shards vmagent
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(0)),
				},
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				ShardCount: func() *int { i := 2; return &i }(),
			},
		},
		predefinedObjects: []runtime.Object{
			k8stools.NewReadyDeployment("vmagent-example-agent-0", "default"),
			k8stools.NewReadyDeployment("vmagent-example-agent-1", "default"),
		},
	})

	// generate vmagent with bauth-secret
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent-bauth",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeSelector: &metav1.LabelSelector{},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			k8stools.NewReadyDeployment("vmagent-example-agent-bauth", "default"),
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "bauth-secret", Namespace: "default"},
				Data:       map[string][]byte{"user": []byte(`user-name`), "password": []byte(`user-password`)},
			},
			&vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmsingle-monitor",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMServiceScrapeSpec{
					Selector: metav1.LabelSelector{},
					Endpoints: []vmv1beta1.Endpoint{
						{
							EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
								Interval: "30s",
								Scheme:   "http",
							},
							EndpointAuth: vmv1beta1.EndpointAuth{
								BasicAuth: &vmv1beta1.BasicAuth{
									Password: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "bauth-secret"}, Key: "password"},
									Username: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "bauth-secret"}, Key: "user"},
								},
							},
						},
					},
				},
			},
		},
	})

	// fail if bearer token secret is missing, without basic auth
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent-bearer-missing",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL:               "http://remote-write",
						BearerTokenSecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "bearer-secret"}, Key: "token"},
					},
				},
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeSelector: &metav1.LabelSelector{},
				},
			},
		},
		wantErr: true,
	})

	// generate vmagent with tls-secret
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent-tls",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
					{
						URL: "http://remote-write2",
						TLSConfig: &vmv1beta1.TLSConfig{
							CA: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "remote2-secret",
									},
									Key: "ca",
								},
							},
							Cert: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "remote2-secret",
									},
									Key: "ca",
								},
							},
							KeySecret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "remote2-secret",
								},
								Key: "key",
							},
						},
					},
					{
						URL: "http://remote-write3",
						TLSConfig: &vmv1beta1.TLSConfig{
							CA: vmv1beta1.SecretOrConfigMap{
								ConfigMap: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "remote3-cm",
									},
									Key: "ca",
								},
							},
							Cert: vmv1beta1.SecretOrConfigMap{
								ConfigMap: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "remote3-cm",
									},
									Key: "ca",
								},
							},
							KeySecret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "remote3-secret",
								},
								Key: "key",
							},
						},
					},
					{
						URL:       "http://remote-write4",
						TLSConfig: &vmv1beta1.TLSConfig{CertFile: "/tmp/cert1", KeyFile: "/tmp/key1", CAFile: "/tmp/ca"},
					},
				},
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					ServiceScrapeSelector: &metav1.LabelSelector{},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			k8stools.NewReadyDeployment("vmagent-example-agent-tls", "default"),
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "default"},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "tls-scrape", Namespace: "default"},
				Data:       map[string][]byte{"cert": []byte(`cert-data`), "ca": []byte(`ca-data`), "key": []byte(`key-data`)},
			},

			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "remote2-secret", Namespace: "default"},
				Data:       map[string][]byte{"cert": []byte(`cert-data`), "ca": []byte(`ca-data`), "key": []byte(`key-data`)},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "remote3-secret", Namespace: "default"},
				Data:       map[string][]byte{"key": []byte(`key-data`)},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "remote3-cm", Namespace: "default"},
				Data:       map[string]string{"ca": "ca-data", "cert": "cert-data"},
			},
			&vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmalert-monitor",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMServiceScrapeSpec{
					Selector: metav1.LabelSelector{},
					Endpoints: []vmv1beta1.Endpoint{
						{
							EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
								Interval: "30s",
								Scheme:   "https",
							},
							EndpointAuth: vmv1beta1.EndpointAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									CA: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tls-scrape"}, Key: "ca"},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tls-scrape"}, Key: "ca"},
									},
									KeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tls-scrape"}, Key: "key"},
								},
							},
						},
					},
				},
			},
		},
	})

	// generate vmagent with inline scrape config
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					InlineScrapeConfig: strings.TrimSpace(`
- job_name: "prometheus"
  static_configs:
  - targets: ["localhost:9090"]
`),
				},
			},
		},
		predefinedObjects: []runtime.Object{
			k8stools.NewReadyDeployment("vmagent-example-agent", "default"),
		},
	})

	// generate vmagent with inline scrape config and secret scrape config
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					AdditionalScrapeConfigs: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "add-cfg"},
						Key:                  "agent.yaml",
					},
					InlineScrapeConfig: strings.TrimSpace(`
- job_name: "prometheus"
  static_configs:
  - targets: ["localhost:9090"]
`),
				},
			},
		},
		predefinedObjects: []runtime.Object{
			k8stools.NewReadyDeployment("vmagent-example-agent", "default"),
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "add-cfg", Namespace: "default"},
				Data: map[string][]byte{"agent.yaml": []byte(strings.TrimSpace(`
- job_name: "alertmanager"
  static_configs:
  - targets: ["localhost:9093"]
`))},
			},
		},
	})

	// generate vmagent statefulset with serviceName when additional service is headless
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent-with-headless-service",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
				StatefulMode: true,
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
						Name: "my-headless-additional-service",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: corev1.ClusterIPNone,
					},
				},
			},
		},
		validate: func(got *appsv1.StatefulSet) error {
			if got.Spec.ServiceName != "my-headless-additional-service" {
				return fmt.Errorf("unexpected serviceName, got: %s, want: %s", got.Spec.ServiceName, "my-headless-additional-service")
			}
			return nil
		},
		statefulsetMode: true,
		predefinedObjects: []runtime.Object{
			k8stools.NewReadyDeployment("vmagent-example-agent", "default"),
		},
	})

	// generate vmagent sharded statefulset with prevSpec
	f(opts{
		mustAddPrevSpec: true,
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				StatefulRollingUpdateStrategy: appsv1.RollingUpdateStatefulSetStrategyType,
				StatefulMode:                  true,
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To[int32](2),
				},
				ShardCount: ptr.To(3),
				PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{
					MinAvailable: ptr.To(intstr.FromInt(1)),
				},
				StatefulStorage: &vmv1beta1.StorageSpec{
					VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: ptr.To("embed-sc"),
							Resources: corev1.VolumeResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceStorage: resource.MustParse("10Gi"),
								},
							},
						},
					},
				},
				ClaimTemplates: []corev1.PersistentVolumeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "extraTemplate",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: ptr.To("default"),
							Resources: corev1.VolumeResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceStorage: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
			},
		},
		validate: func(got *appsv1.StatefulSet) error {
			if len(got.Spec.Template.Spec.Containers) != 1 {
				return fmt.Errorf("unexpected count of container, got: %d, want: %d", len(got.Spec.Template.Spec.Containers), 1)
			}
			if len(got.Spec.VolumeClaimTemplates) != 2 {
				return fmt.Errorf("unexpected count of VolumeClaimTemplates, got: %d, want: %d", len(got.Spec.VolumeClaimTemplates), 2)
			}
			if *got.Spec.VolumeClaimTemplates[0].Spec.StorageClassName != "embed-sc" {
				return fmt.Errorf("unexpected embed VolumeClaimTemplates name, got: %s, want: %s", *got.Spec.VolumeClaimTemplates[0].Spec.StorageClassName, "embed-sc")
			}
			if diff := deep.Equal(got.Spec.VolumeClaimTemplates[0].Spec.Resources, corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			}); len(diff) != 0 {
				return fmt.Errorf("unexpected embed VolumeClaimTemplates resources, diff: %v", diff)
			}
			if *got.Spec.VolumeClaimTemplates[1].Spec.StorageClassName != "default" {
				return fmt.Errorf("unexpected extra VolumeClaimTemplates, got: %s, want: %s", *got.Spec.VolumeClaimTemplates[1].Spec.StorageClassName, "default")
			}
			if diff := deep.Equal(got.Spec.VolumeClaimTemplates[1].Spec.Resources, corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("2Gi"),
				},
			}); len(diff) != 0 {
				return fmt.Errorf("unexpected extra VolumeClaimTemplates resources, diff: %v", diff)
			}
			return nil
		},
		statefulsetMode: true,
	})

	// generate vmagent statefulset with prevSpec
	f(opts{
		mustAddPrevSpec: true,
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
				StatefulMode: true,
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
				StatefulStorage: &vmv1beta1.StorageSpec{
					VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: ptr.To("embed-sc"),
							Resources: corev1.VolumeResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceStorage: resource.MustParse("10Gi"),
								},
							},
						},
					},
				},
				ClaimTemplates: []corev1.PersistentVolumeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "extraTemplate",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: ptr.To("default"),
							Resources: corev1.VolumeResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceStorage: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
			},
		},
		validate: func(got *appsv1.StatefulSet) error {
			if len(got.Spec.Template.Spec.Containers) != 1 {
				return fmt.Errorf("unexpected count of container, got: %d, want: %d", len(got.Spec.Template.Spec.Containers), 1)
			}
			if len(got.Spec.VolumeClaimTemplates) != 2 {
				return fmt.Errorf("unexpected count of VolumeClaimTemplates, got: %d, want: %d", len(got.Spec.VolumeClaimTemplates), 2)
			}
			if *got.Spec.VolumeClaimTemplates[0].Spec.StorageClassName != "embed-sc" {
				return fmt.Errorf("unexpected embed VolumeClaimTemplates name, got: %s, want: %s", *got.Spec.VolumeClaimTemplates[0].Spec.StorageClassName, "embed-sc")
			}
			if diff := deep.Equal(got.Spec.VolumeClaimTemplates[0].Spec.Resources, corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			}); len(diff) != 0 {
				return fmt.Errorf("unexpected embed VolumeClaimTemplates resources, diff: %v", diff)
			}
			if *got.Spec.VolumeClaimTemplates[1].Spec.StorageClassName != "default" {
				return fmt.Errorf("unexpected extra VolumeClaimTemplates, got: %s, want: %s", *got.Spec.VolumeClaimTemplates[1].Spec.StorageClassName, "default")
			}
			if diff := deep.Equal(got.Spec.VolumeClaimTemplates[1].Spec.Resources, corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("2Gi"),
				},
			}); len(diff) != 0 {
				return fmt.Errorf("unexpected extra VolumeClaimTemplates resources, diff: %v", diff)
			}
			return nil
		},
		statefulsetMode: true,
	})

	// with oauth2 rw
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oauth2",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(0)),
				},
				StatefulMode: true,
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL: "http://some-url",
						OAuth2: &vmv1beta1.OAuth2{
							TokenURL: "http://oauth2-svc/auth",
							ClientID: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									Key: "client-id",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "oauth2-access",
									},
								},
							},
							ClientSecret: &corev1.SecretKeySelector{
								Key: "client-secret",
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "oauth2-access",
								},
							},
							TLSConfig: &vmv1beta1.TLSConfig{},
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "oauth2-access",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"client-secret": []byte(`some-secret-value`),
					"client-id":     []byte(`some-id-value`),
				},
			},
		},
		statefulsetMode: true,
		validate: func(set *appsv1.StatefulSet) error {
			cnt := set.Spec.Template.Spec.Containers[0]
			if cnt.Name != "vmagent" {
				return fmt.Errorf("unexpected container name: %q, want: vmagent", cnt.Name)
			}
			hasClientSecretArg := false
			for _, arg := range cnt.Args {
				if strings.Contains(arg, "remoteWrite.oauth2.clientSecretFile") {
					hasClientSecretArg = true
					break
				}
			}
			if !hasClientSecretArg {
				return fmt.Errorf("container must have remoteWrite.oauth2.clientSecretFile flag, has only: %s", strings.Join(cnt.Args, ":,:"))
			}
			return nil
		},
	})
}

func TestBuildRemoteWriteArgs(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAgent
		predefinedObjects []runtime.Object
		want              []string
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ac := getAssetsCache(ctx, fclient, o.cr)
		sort.Strings(o.want)
		got, err := buildRemoteWriteArgs(o.cr, ac)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		sort.Strings(got)
		assert.Equal(t, o.want, got)
	}

	// test with tls config full
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oauth2",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL: "localhost:8429",
						TLSConfig: &vmv1beta1.TLSConfig{
							CA: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
									Key: "ca",
								},
							},
						},
					},
					{
						URL: "localhost:8429",
						TLSConfig: &vmv1beta1.TLSConfig{
							CAFile: "/path/to_ca",
							Cert: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
									Key: "cert",
								},
							},
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"ca":   []byte("ca-value"),
					"cert": []byte("cert-value"),
				},
			},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.tlsCAFile=/etc/vmagent-tls/certs/default_tls-secret_ca,/path/to_ca`,
			`-remoteWrite.tlsCertFile=,/etc/vmagent-tls/certs/default_tls-secret_cert`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
			`-remoteWrite.url=localhost:8429,localhost:8429`,
		},
	})

	// test insecure with key only
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL: "localhost:8429",
						TLSConfig: &vmv1beta1.TLSConfig{
							KeySecret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "tls-secret",
								},
								Key: "key",
							},
							InsecureSkipVerify: true,
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"key": []byte("key-value"),
				},
			},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.url=localhost:8429`,
			`-remoteWrite.tlsInsecureSkipVerify=true`,
			`-remoteWrite.tlsKeyFile=/etc/vmagent-tls/certs/default_tls-secret_key`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test insecure
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL: "localhost:8429",

					TLSConfig: &vmv1beta1.TLSConfig{
						InsecureSkipVerify: true,
					},
				},
			}},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.url=localhost:8429`,
			`-remoteWrite.tlsInsecureSkipVerify=true`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test inline relabeling
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL: "localhost:8429",
						TLSConfig: &vmv1beta1.TLSConfig{
							InsecureSkipVerify: true,
						},
						InlineUrlRelabelConfig: []*vmv1beta1.RelabelConfig{
							{TargetLabel: "rw-1", Replacement: ptr.To("present")},
						},
					},
					{
						URL: "remote-1:8429",

						TLSConfig: &vmv1beta1.TLSConfig{
							InsecureSkipVerify: true,
						},
					},
					{
						URL: "remote-1:8429",
						TLSConfig: &vmv1beta1.TLSConfig{
							InsecureSkipVerify: true,
						},
						InlineUrlRelabelConfig: []*vmv1beta1.RelabelConfig{
							{TargetLabel: "rw-2", Replacement: ptr.To("present")},
						},
					},
				},
				CommonRelabelParams: vmv1beta1.CommonRelabelParams{
					InlineRelabelConfig: []*vmv1beta1.RelabelConfig{
						{TargetLabel: "dst", Replacement: ptr.To("ok")},
					},
				},
			},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.url=localhost:8429,remote-1:8429,remote-1:8429`,
			`-remoteWrite.tlsInsecureSkipVerify=true,true,true`,
			`-remoteWrite.urlRelabelConfig=/etc/vm/relabeling/url_relabeling-0.yaml,,/etc/vm/relabeling/url_relabeling-2.yaml`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test sendTimeout
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL: "localhost:8429",

					SendTimeout: ptr.To("10s"),
				},
				{
					URL:         "localhost:8431",
					SendTimeout: ptr.To("15s"),
				},
			}},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.url=localhost:8429,localhost:8431`,
			`-remoteWrite.sendTimeout=10s,15s`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test multi-tenant
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL: "http://vminsert-cluster-1:8480/insert/multitenant/prometheus/api/v1/write",

						SendTimeout: ptr.To("10s"),
					},
					{
						URL:         "http://vmagent-aggregation:8429",
						SendTimeout: ptr.To("15s"),
					},
				},
				RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
					UseMultiTenantMode: true,
				},
			},
		},
		want: []string{
			`-enableMultitenantHandlers=true`,
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.url=http://vminsert-cluster-1:8480/insert/multitenant/prometheus/api/v1/write,http://vmagent-aggregation:8429`,
			`-remoteWrite.sendTimeout=10s,15s`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test maxDiskUsage
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL:          "localhost:8429",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("1500MB")),
				},
				{
					URL:          "localhost:8431",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("500MB")),
				},
				{
					URL: "localhost:8432",
				},
			}},
		},
		want: []string{
			`-remoteWrite.url=localhost:8429,localhost:8431,localhost:8432`,
			`-remoteWrite.maxDiskUsagePerURL=1500MB,500MB,1073741824`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test automatic maxDiskUsage
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				StatefulMode: true,
				StatefulStorage: &vmv1beta1.StorageSpec{
					VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: ptr.To("embed-sc"),
							Resources: corev1.VolumeResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceStorage: resource.MustParse("10Gi"),
								},
							},
						},
					},
				},
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL: "localhost:8429",
					},
					{
						URL: "localhost:8431",
					},
					{
						URL: "localhost:8432",
					},
				},
			},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=3579139413`,
			`-remoteWrite.url=localhost:8429,localhost:8431,localhost:8432`,
			`-remoteWrite.tmpDataPath=/vmagent_pq/vmagent-remotewrite-data`,
		},
	})

	// test automatic maxDiskUsage with at least one defined
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				StatefulMode: true,
				StatefulStorage: &vmv1beta1.StorageSpec{
					VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: ptr.To("embed-sc"),
							Resources: corev1.VolumeResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceStorage: resource.MustParse("10Gi"),
								},
							},
						},
					},
				},
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL:          "localhost:8429",
						MaxDiskUsage: ptr.To(vmv1beta1.BytesString("5000MB")),
					},
					{
						URL: "localhost:8431",
					},
					{
						URL: "localhost:8432",
					},
				},
			},
		},
		want: []string{
			`-remoteWrite.url=localhost:8429,localhost:8431,localhost:8432`,
			`-remoteWrite.maxDiskUsagePerURL=5000MB,3579139413,3579139413`,
			`-remoteWrite.tmpDataPath=/vmagent_pq/vmagent-remotewrite-data`,
		},
	})

	// test forceVMProto
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL:          "localhost:8429",
					ForceVMProto: true,
				},
				{
					URL: "localhost:8431",
				},
				{
					URL:          "localhost:8432",
					ForceVMProto: true,
				},
			}},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.url=localhost:8429,localhost:8431,localhost:8432`,
			`-remoteWrite.forceVMProto=true,false,true`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test oauth2
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL:         "localhost:8429",
					SendTimeout: ptr.To("10s"),
				},
				{
					URL:         "localhost:8431",
					SendTimeout: ptr.To("15s"),
					OAuth2: &vmv1beta1.OAuth2{
						Scopes:   []string{"scope-1"},
						TokenURL: "http://some-url",
						ClientSecret: &corev1.SecretKeySelector{
							Key: "some-secret",
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "some-cm",
							},
						},
						ClientID: vmv1beta1.SecretOrConfigMap{ConfigMap: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "some-cm"},
							Key:                  "some-key",
						}},
					},
				},
			}},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-cm",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"some-secret": []byte("some-secret"),
				},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-cm",
					Namespace: "default",
				},
				Data: map[string]string{
					"some-key": "some-id",
				},
			},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.oauth2.clientID=,some-id`,
			`-remoteWrite.oauth2.clientSecretFile=,/etc/vmagent/config/default_some-cm_some-secret`,
			`-remoteWrite.oauth2.scopes=,scope-1`,
			`-remoteWrite.oauth2.tokenUrl=,http://some-url`,
			`-remoteWrite.url=localhost:8429,localhost:8431`,
			`-remoteWrite.sendTimeout=10s,15s`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test bearer token
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL:         "localhost:8429",
					SendTimeout: ptr.To("10s"),
				},
				{
					URL:         "localhost:8431",
					SendTimeout: ptr.To("15s"),
					BearerTokenSecret: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
						Key:                  "some-key",
					},
				},
			}},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"some-key": []byte("token"),
				},
			},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.bearerTokenFile="","/etc/vmagent/config/default_some-secret_some-key"`,
			`-remoteWrite.url=localhost:8429,localhost:8431`,
			`-remoteWrite.sendTimeout=10s,15s`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test with headers
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL:         "localhost:8429",
					SendTimeout: ptr.To("10s"),
				},
				{
					URL:         "localhost:8431",
					SendTimeout: ptr.To("15s"),
					BearerTokenSecret: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
						Key:                  "some-key",
					},
					Headers: []string{"key: value", "second-key: value2"},
				},
			}},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"some-key": []byte("token"),
				},
			},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.bearerTokenFile="","/etc/vmagent/config/default_some-secret_some-key"`,
			`-remoteWrite.headers=,key: value^^second-key: value2`,
			`-remoteWrite.url=localhost:8429,localhost:8431`,
			`-remoteWrite.sendTimeout=10s,15s`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test with stream aggr
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL: "localhost:8429",
					StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
						Rules: []vmv1beta1.StreamAggrRule{
							{
								Outputs: []string{"total", "avg"},
							},
						},
						DedupInterval:             "10s",
						IgnoreFirstSampleInterval: "10m",
					},
				},
				{
					URL: "localhost:8431",
					StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
						Rules: []vmv1beta1.StreamAggrRule{
							{
								IgnoreFirstSampleInterval: "20m",
								Outputs:                   []string{"histogram_bucket"},
							},
						},
						KeepInput: true,
					},
				},
			}},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.streamAggr.config=/etc/vm/stream-aggr/RWS_0-CM-STREAM-AGGR-CONF,/etc/vm/stream-aggr/RWS_1-CM-STREAM-AGGR-CONF`,
			`-remoteWrite.streamAggr.dedupInterval=10s,`,
			`-remoteWrite.streamAggr.ignoreFirstSampleInterval=10m,`,
			`-remoteWrite.streamAggr.keepInput=false,true`,
			`-remoteWrite.url=localhost:8429,localhost:8431`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test with stream aggr (one remote write)
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL: "localhost:8431",
					StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
						Rules: []vmv1beta1.StreamAggrRule{
							{
								Outputs: []string{"histogram_bucket"},
							},
						},
						KeepInput:     true,
						DedupInterval: "10s",
					},
				},
			}},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.streamAggr.config=/etc/vm/stream-aggr/RWS_0-CM-STREAM-AGGR-CONF`,
			`-remoteWrite.streamAggr.dedupInterval=10s`,
			`-remoteWrite.streamAggr.keepInput=true`,
			`-remoteWrite.url=localhost:8431`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test with stream aggr (one remote write with defaults)
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL: "localhost:8431",
					StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
						Rules: []vmv1beta1.StreamAggrRule{
							{
								Outputs: []string{"histogram_bucket"},
							},
						},
					},
				},
			}},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.streamAggr.config=/etc/vm/stream-aggr/RWS_0-CM-STREAM-AGGR-CONF`,
			`-remoteWrite.url=localhost:8431`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test with stream aggr (many remote writes)
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL: "localhost:8428",
				},
				{
					URL: "localhost:8429",
					StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
						Rules: []vmv1beta1.StreamAggrRule{
							{
								Interval: "1m",
								Outputs:  []string{"total", "avg"},
							},
						},
						DedupInterval: "10s",
						KeepInput:     true,
					},
				},
				{
					URL: "localhost:8430",
				},
				{
					URL: "localhost:8431",
					StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
						Rules: []vmv1beta1.StreamAggrRule{
							{
								Interval: "1m",
								Outputs:  []string{"histogram_bucket"},
							},
						},
					},
				},
				{
					URL: "localhost:8432",
				},
			}},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.streamAggr.config=,/etc/vm/stream-aggr/RWS_1-CM-STREAM-AGGR-CONF,,/etc/vm/stream-aggr/RWS_3-CM-STREAM-AGGR-CONF,`,
			`-remoteWrite.streamAggr.dedupInterval=,10s,,,`,
			`-remoteWrite.streamAggr.keepInput=false,true,false,false,false`,
			`-remoteWrite.url=localhost:8428,localhost:8429,localhost:8430,localhost:8431,localhost:8432`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test with aws
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL: "localhost:8429",
					AWS: &vmv1beta1.AWS{
						RoleARN: "arn:aws:iam::account:role/role-1",
					},
				},
				{
					URL: "localhost:8431",
				},
				{
					URL: "localhost:8431",
					AWS: &vmv1beta1.AWS{
						UseSigv4: true,
					},
				},
			}},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "remote2-aws-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"accesskey": []byte("accesskeytoken"),
				},
			},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.aws.roleARN=arn:aws:iam::account:role/role-1,,`,
			`-remoteWrite.aws.useSigv4=false,false,true`,
			`-remoteWrite.url=localhost:8429,localhost:8431,localhost:8431`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test with proxyURL (one remote write with defaults)
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL: "http://localhost:8431",
					},
					{
						URL:      "http://localhost:8432",
						ProxyURL: ptr.To("http://proxy.example.com"),
					},
					{
						URL: "http://localhost:8433",
					},
				},
			},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.proxyURL=,http://proxy.example.com,`,
			`-remoteWrite.url=http://localhost:8431,http://localhost:8432,http://localhost:8433`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test with StatefulMode
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{StatefulMode: true},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.tmpDataPath=/vmagent_pq/vmagent-remotewrite-data`,
		},
	})

	// test simple ok
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test labels
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
					Labels: map[string]string{
						"label-1": "value1",
						"label-2": "value2",
					},
				},
			},
		},
		want: []string{
			`-remoteWrite.label=label-1=value1,label-2=value2`,
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// test label
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
					ShowURL: ptr.To(true),
					Labels: map[string]string{
						"label-1": "value1",
					},
				},
			},
		},
		want: []string{
			`-remoteWrite.label=label-1=value1`,
			`-remoteWrite.showURL=true`,
			`-remoteWrite.maxDiskUsagePerURL=1073741824`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
		},
	})

	// with remoteWriteSettings
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
					ShowURL:            ptr.To(true),
					TmpDataPath:        ptr.To("/tmp/my-path"),
					MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("1000")),
					UseMultiTenantMode: true,
				},
			},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=1000`,
			`-remoteWrite.tmpDataPath=/tmp/my-path`,
			`-remoteWrite.showURL=true`,
			`-enableMultitenantHandlers=true`,
		},
	})

	// maxDiskUsage already set in RemoteWriteSpec
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL:          "localhost:8431",
						MaxDiskUsage: ptr.To(vmv1beta1.BytesString("500MB")),
					},
				},
				RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
					MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("1000")),
				},
			},
		},
		want: []string{
			`-remoteWrite.maxDiskUsagePerURL=500MB`,
			`-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data`,
			`-remoteWrite.url=localhost:8431`,
		},
	})

	// maxDiskUsage already set in RemoteWriteSpec
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL: "localhost:8429",
					},
					{
						URL: "localhost:8431",
					},
					{
						URL: "localhost:8432",
					},
				},
				RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
					MaxBlockSize: ptr.To(int32(1000)),
				},
				StatefulMode: true,
				StatefulStorage: &vmv1beta1.StorageSpec{
					VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: ptr.To("embed-sc"),
							Resources: corev1.VolumeResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceStorage: resource.MustParse("10Gi"),
								},
							},
						},
					},
				},
			},
		},
		want: []string{
			`-remoteWrite.url=localhost:8429,localhost:8431,localhost:8432`,
			`-remoteWrite.maxBlockSize=1000`,
			`-remoteWrite.maxDiskUsagePerURL=3579139413`,
			`-remoteWrite.tmpDataPath=/vmagent_pq/vmagent-remotewrite-data`,
		},
	})
}

func TestCreateOrUpdateService(t *testing.T) {
	type opts struct {
		cr                    *vmv1beta1.VMAgent
		want                  func(svc *corev1.Service) error
		wantAdditionalService func(svc *corev1.Service) error
		predefinedObjects     []runtime.Object
	}

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		assert.NoError(t, createOrUpdateService(ctx, cl, o.cr, nil))
		svc := build.Service(o.cr, o.cr.Spec.Port, nil)
		var got corev1.Service
		nsn := types.NamespacedName{
			Name:      svc.Name,
			Namespace: svc.Namespace,
		}
		assert.NoError(t, cl.Get(ctx, nsn, &got))
		if err := o.want(&got); err != nil {
			t.Errorf("CreateOrUpdateService() unexpected error: %v", err)
		}
		if o.wantAdditionalService != nil {
			var additionalSvc corev1.Service
			if err := cl.Get(ctx, types.NamespacedName{Namespace: o.cr.Namespace, Name: o.cr.Spec.ServiceSpec.NameOrDefault(o.cr.Name)}, &additionalSvc); err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if err := o.wantAdditionalService(&additionalSvc); err != nil {
				t.Fatalf("CreateOrUpdateService validation failed for additional service: %s", err)
			}
		}
	}

	// base case
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
		},
		want: func(svc *corev1.Service) error {
			if svc.Name != "vmagent-base" {
				return fmt.Errorf("unexpected name for service: %v", svc.Name)
			}
			if len(svc.Spec.Ports) != 1 {
				return fmt.Errorf("unexpected count for service ports: %v", len(svc.Spec.Ports))
			}
			return nil
		},
	})

	// base case with ingestPorts and extra service
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				InsertPorts: &vmv1beta1.InsertPorts{
					InfluxPort: "8011",
				},
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "extra-svc"},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeNodePort,
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "extra-svc",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmagent",
						"app.kubernetes.io/instance":  "base",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
				},
			},
		},
		want: func(svc *corev1.Service) error {
			if svc.Name != "vmagent-base" {
				return fmt.Errorf("unexpected name for service: %v", svc.Name)
			}
			if len(svc.Spec.Ports) != 3 {
				return fmt.Errorf("unexpected count for ports, want 3, got: %v", len(svc.Spec.Ports))
			}
			return nil
		},
	})

	// base case with ingestPorts and extra service
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				InsertPorts: &vmv1beta1.InsertPorts{
					InfluxPort: "8011",
				},
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "extra-svc"},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeNodePort,
						Ports: []corev1.ServicePort{
							{
								Name:     "influx-udp",
								NodePort: 8085,
								Protocol: corev1.ProtocolUDP,
							},
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "extra-svc",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmagent",
						"app.kubernetes.io/instance":  "base",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
				},
			},
		},
		want: func(svc *corev1.Service) error {
			if svc.Name != "vmagent-base" {
				return fmt.Errorf("unexpected name for service: %v", svc.Name)
			}
			if len(svc.Spec.Ports) != 3 {
				return fmt.Errorf("unexpected count for ports, want 3, got: %v", len(svc.Spec.Ports))
			}
			return nil
		},
		wantAdditionalService: func(svc *corev1.Service) error {
			if len(svc.Spec.Ports) != 1 {
				return fmt.Errorf("unexpected count for ports, want 1, got: %v", len(svc.Spec.Ports))
			}
			if svc.Spec.Ports[0].NodePort != 8085 {
				return fmt.Errorf("unexpected port %v, want 8085", svc.Spec.Ports[0])
			}
			if svc.Spec.Ports[0].Protocol != corev1.ProtocolUDP {
				return fmt.Errorf("unexpected protocol want udp, got: %v", svc.Spec.Ports[0].Protocol)
			}
			return nil
		},
	})
}

func TestCreateOrUpdateRelabelConfigsAssets(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAgent
		predefinedObjects []runtime.Object
		validate          func(cm *corev1.ConfigMap) error
	}
	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		ac := build.NewAssetsCache(ctx, cl, nil)
		if err := createOrUpdateRelabelConfigsAssets(ctx, cl, o.cr, nil, ac); err != nil {
			t.Fatalf("CreateOrUpdateRelabelConfigsAssets() error = %v", err)
		}
		var createdCM corev1.ConfigMap
		if err := cl.Get(ctx, types.NamespacedName{
			Namespace: o.cr.Namespace,
			Name:      build.ResourceName(build.RelabelConfigResourceKind, o.cr),
		}, &createdCM); err != nil {
			t.Fatalf("cannot fetch created cm: %v", err)
		}
		if err := o.validate(&createdCM); err != nil {
			t.Fatalf("cannot validate created cm: %v", err)
		}
	}

	// simple relabelcfg
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonRelabelParams: vmv1beta1.CommonRelabelParams{
					InlineRelabelConfig: []*vmv1beta1.RelabelConfig{
						{
							Regex:        []string{".*"},
							Action:       "DROP",
							SourceLabels: []string{"pod"},
						},
						{},
					},
				},
			},
		},
		validate: func(cm *corev1.ConfigMap) error {
			data, ok := cm.Data[globalRelabelingName]
			if !ok {
				return fmt.Errorf("key: %s, not exists at map: %v", "global_relabeling.yaml", cm.BinaryData)
			}
			wantGlobal := `- source_labels:
  - pod
  regex: .*
  action: DROP
`
			assert.Equal(t, wantGlobal, data)
			return nil
		},
	})

	// combined relabel configs
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmag",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonRelabelParams: vmv1beta1.CommonRelabelParams{
					InlineRelabelConfig: []*vmv1beta1.RelabelConfig{
						{
							Regex:        []string{".*"},
							Action:       "DROP",
							SourceLabels: []string{"pod"},
						},
					},
					RelabelConfig: &corev1.ConfigMapKeySelector{
						Key:                  "global.yaml",
						LocalObjectReference: corev1.LocalObjectReference{Name: "relabels"},
					},
				},
			},
		},
		validate: func(cm *corev1.ConfigMap) error {
			data, ok := cm.Data[globalRelabelingName]
			if !ok {
				return fmt.Errorf("key: %s, not exists at map: %v", "global_relabeling.yaml", cm.BinaryData)
			}
			wantGlobal := strings.TrimSpace(`
- source_labels:
  - pod
  regex: .*
  action: DROP
- action: DROP
  source_labels: ["pod-1"]`)
			assert.Equal(t, wantGlobal, data)
			return nil
		},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "relabels", Namespace: "default"},
				Data: map[string]string{
					"global.yaml": strings.TrimSpace(`
- action: DROP
  source_labels: ["pod-1"]`),
				},
			},
		},
	})
}

func TestCreateOrUpdateStreamAggrConfig(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAgent
		predefinedObjects []runtime.Object
		validate          func(cm *corev1.ConfigMap) error
	}

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		ac := build.NewAssetsCache(ctx, cl, nil)
		if err := createOrUpdateStreamAggrConfig(ctx, cl, o.cr, nil, ac); err != nil {
			t.Fatalf("CreateOrUpdateStreamAggrConfig() error = %v", err)
		}
		var createdCM corev1.ConfigMap
		if err := cl.Get(ctx,
			types.NamespacedName{
				Namespace: o.cr.Namespace,
				Name:      build.ResourceName(build.StreamAggrConfigResourceKind, o.cr),
			}, &createdCM,
		); err != nil {
			t.Fatalf("cannot fetch created cm: %v", err)
		}
		if err := o.validate(&createdCM); err != nil {
			t.Fatalf("cannot validate created cm: %v", err)
		}
	}

	// simple stream aggr config
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL: "localhost:8429",
						StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
							Rules: []vmv1beta1.StreamAggrRule{{
								Interval: "1m",
								Outputs:  []string{"total", "avg"},
							}},
						},
					},
				},
			},
		},
		validate: func(cm *corev1.ConfigMap) error {
			data, ok := cm.Data["RWS_0-CM-STREAM-AGGR-CONF"]
			if !ok {
				return fmt.Errorf("key: %s, not exists at map: %v", "RWS_0-CM-STREAM-AGGR-CONFl", cm.BinaryData)
			}
			wantGlobal := `- interval: 1m
  outputs:
  - total
  - avg
`
			assert.Equal(t, wantGlobal, data)
			return nil
		},
	})

	// simple global and remoteWrite stream aggr config
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
					Rules: []vmv1beta1.StreamAggrRule{{
						Match:    []string{`test`},
						Interval: "30s",
						Outputs:  []string{"total"},
						By:       []string{"job", "instance"},
						Without:  []string{"pod"},
					}},
				},
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL: "localhost:8429",
						StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
							Rules: []vmv1beta1.StreamAggrRule{{
								Match:             []string{`{__name__="count1"}`, `{__name__="count2"}`},
								Interval:          "1m",
								StalenessInterval: "2m",
								Outputs:           []string{"total", "avg"},
								By:                []string{"job", "instance"},
								Without:           []string{"pod"},
								OutputRelabelConfigs: []vmv1beta1.RelabelConfig{{
									SourceLabels: []string{"__name__"},
									TargetLabel:  "metric",
									Regex:        []string{"(.+):.+"},
								}},
							}},
						},
					},
				},
			},
		},
		validate: func(cm *corev1.ConfigMap) error {
			globalData, ok := cm.Data["global_aggregation.yaml"]
			if !ok {
				return fmt.Errorf("key: %s, not exists at map: %v", "global_aggregation.yaml", cm.BinaryData)
			}
			wantGlobal := `- match: test
  interval: 30s
  outputs:
  - total
  by:
  - job
  - instance
  without:
  - pod
`
			assert.Equal(t, wantGlobal, globalData)
			remoteData, ok := cm.Data["RWS_0-CM-STREAM-AGGR-CONF"]
			if !ok {
				return fmt.Errorf("key: %s, not exists at map: %v", "RWS_0-CM-STREAM-AGGR-CONFl", cm.BinaryData)
			}
			wantRemote := `- match:
  - '{__name__="count1"}'
  - '{__name__="count2"}'
  interval: 1m
  staleness_interval: 2m
  outputs:
  - total
  - avg
  by:
  - job
  - instance
  without:
  - pod
  output_relabel_configs:
  - regex: (.+):.+
`
			assert.Equal(t, wantRemote, remoteData)
			return nil
		},
	})

	// stream aggr config with multie regex
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL: "localhost:8429",
						StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
							Rules: []vmv1beta1.StreamAggrRule{{
								Match:                     []string{`{__name__="count1"}`, `{__name__="count2"}`},
								Interval:                  "1m",
								StalenessInterval:         "2m",
								IgnoreFirstSampleInterval: "20m",
								Outputs:                   []string{"total", "avg"},
								By:                        []string{"job", "instance"},
								Without:                   []string{"pod"},
								OutputRelabelConfigs: []vmv1beta1.RelabelConfig{{
									SourceLabels: []string{"__name__"},
									TargetLabel:  "metric",
									Regex:        []string{"vmagent", "vmalert", "vmauth"},
								}},
							}},
						},
					},
				},
			},
		},
		validate: func(cm *corev1.ConfigMap) error {
			data, ok := cm.Data["RWS_0-CM-STREAM-AGGR-CONF"]
			if !ok {
				return fmt.Errorf("key: %s, not exists at map: %v", "RWS_0-CM-STREAM-AGGR-CONFl", cm.BinaryData)
			}
			wantGlobal := `- match:
  - '{__name__="count1"}'
  - '{__name__="count2"}'
  interval: 1m
  staleness_interval: 2m
  outputs:
  - total
  - avg
  by:
  - job
  - instance
  without:
  - pod
  ignore_first_sample_interval: 20m
  output_relabel_configs:
  - regex:
    - vmagent
    - vmalert
    - vmauth
`
			assert.Equal(t, wantGlobal, data)
			return nil
		},
	})
}

func TestMakeSpecForAgentOk(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAgent
		predefinedObjects []runtime.Object
		wantYaml          string
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ac := getAssetsCache(ctx, fclient, o.cr)
		scheme := fclient.Scheme()
		build.AddDefaults(scheme)
		scheme.Default(o.cr)
		// this trick allows to omit empty fields for yaml
		var wantSpec corev1.PodSpec
		if err := yaml.Unmarshal([]byte(o.wantYaml), &wantSpec); err != nil {
			t.Fatalf("unexpected error: %e", err)
		}
		wantYAMLForCompare, err := yaml.Marshal(wantSpec)
		if err != nil {
			t.Fatalf("BUG: cannot parse as yaml: %q", err)
		}
		got, err := newPodSpec(o.cr, ac)
		if err != nil {
			t.Fatalf("not expected error=%q", err)
		}
		gotYAML, err := yaml.Marshal(got)
		if err != nil {
			t.Fatalf("cannot parse got as yaml: %q", err)
		}

		assert.Equal(t, string(wantYAMLForCompare), string(gotYAML))
	}
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
					Image: vmv1beta1.Image{
						Repository: "vm-repo",
						Tag:        "v1.97.1",
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10m"),
							corev1.ResourceMemory: resource.MustParse("10Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10m"),
							corev1.ResourceMemory: resource.MustParse("10Mi"),
						},
					},
					Port: "8425",
				},
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImage: "vmcustom:config-reloader-v0.35.0",
				},
			},
		},
		wantYaml: `
volumes:
    - name: persistent-queue-data
      volumesource:
        emptydir: {}
initcontainers: []
containers:
    - name: vmagent
      image: vm-repo:v1.97.1
      args:
        - -httpListenAddr=:8425
        - -remoteWrite.maxDiskUsagePerURL=1073741824
        - -remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data
      ports:
        - name: http
          containerport: 8425
          protocol: TCP
      resources:
        limits:
            cpu:
                format: DecimalSI
            memory:
                format: BinarySI
        requests:
            cpu:
                format: DecimalSI
            memory:
                format: BinarySI
        claims: []
      volumemounts:
        - name: persistent-queue-data
          mountpath: /tmp/vmagent-remotewrite-data
      livenessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8425
                scheme: HTTP
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
      readinessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8425
                scheme: HTTP
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
      terminationmessagepolicy: FallbackToLogsOnError
      imagepullpolicy: IfNotPresent
serviceaccountname: vmagent-agent

    `})
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(false),
				},
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
					Image: vmv1beta1.Image{
						Tag: "v1.97.1",
					},
					UseDefaultResources: ptr.To(false),
					Port:                "8429",
				},
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImage: "vmcustomer:v1",
				},
			},
		},
		wantYaml: `
volumes:
    - name: persistent-queue-data
      volumesource:
        emptydir: {}
    - name: tls-assets
      volumesource:
        secret:
            secretname: tls-assets-vmagent-agent
    - name: config-out
      volumesource:
        emptydir: {}
    - name: config
      volumesource:
        secret:
            secretname: vmagent-agent
initcontainers:
    - name: config-init
      image: vmcustomer:v1
      args:
        - --config-envsubst-file=/etc/vmagent/config_out/vmagent.yaml
        - --config-secret-key=vmagent.yaml.gz
        - --config-secret-name=default/vmagent-agent
        - --only-init-config
        - --reload-url=http://127.0.0.1:8429/-/reload
        - --webhook-method=POST
      volumemounts:
        - name: config-out
          mountpath: /etc/vmagent/config_out
containers:
    - name: vmagent
      image: victoriametrics/vmagent:v1.97.1
      args:
        - -httpListenAddr=:8429
        - -promscrape.config=/etc/vmagent/config_out/vmagent.yaml
        - -remoteWrite.maxDiskUsagePerURL=1073741824
        - -remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data
      ports:
        - name: http
          containerport: 8429
          protocol: TCP
      volumemounts:
        - name: persistent-queue-data
          mountpath: /tmp/vmagent-remotewrite-data
        - name: config-out
          readonly: true
          mountpath: /etc/vmagent/config_out
        - name: tls-assets
          readonly: true
          mountpath: /etc/vmagent-tls/certs
        - name: config
          readonly: true
          mountpath: /etc/vmagent/config
      livenessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8429
                scheme: HTTP
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
      readinessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8429
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
        - --config-envsubst-file=/etc/vmagent/config_out/vmagent.yaml
        - --config-secret-key=vmagent.yaml.gz
        - --config-secret-name=default/vmagent-agent
        - --reload-url=http://127.0.0.1:8429/-/reload
        - --webhook-method=POST
      ports:
        - name: reloader-http
          containerport: 8435
          protocol: TCP
      volumemounts:
        - name: config-out
          mountpath: /etc/vmagent/config_out
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

serviceaccountname: vmagent-agent
`,
	})

	// test maxDiskUsage and empty remoteWriteSettings
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
					Image: vmv1beta1.Image{
						Tag: "v1.97.1",
					},
					UseDefaultResources: ptr.To(false),
					Port:                "8425",
				},
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImage: "vmcustom:config-reloader-v0.35.0",
				},
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL:          "http://some-url/api/v1/write",
						MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
					},
					{
						URL:          "http://some-url-2/api/v1/write",
						MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
					},
					{
						URL: "http://some-url-3/api/v1/write",
					},
				},
			},
		},
		wantYaml: `
volumes:
    - name: persistent-queue-data
      volumesource:
        emptydir: {}
initcontainers: []
containers:
    - name: vmagent
      image: victoriametrics/vmagent:v1.97.1
      args:
        - -httpListenAddr=:8425
        - -remoteWrite.maxDiskUsagePerURL=10GB,10GB,1073741824
        - -remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data
        - -remoteWrite.url=http://some-url/api/v1/write,http://some-url-2/api/v1/write,http://some-url-3/api/v1/write
      ports:
        - name: http
          containerport: 8425
          protocol: TCP
      volumemounts:
        - name: persistent-queue-data
          mountpath: /tmp/vmagent-remotewrite-data
      livenessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8425
                scheme: HTTP
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
      readinessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8425
                scheme: HTTP
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
      terminationmessagepolicy: FallbackToLogsOnError
      imagepullpolicy: IfNotPresent
serviceaccountname: vmagent-agent

    `,
	})

	// test MaxDiskUsage with RemoteWriteSettings
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
					Image: vmv1beta1.Image{
						Tag: "v1.97.1",
					},
					UseDefaultResources: ptr.To(false),
					Port:                "8425",
				},
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImage: "vmcustom:config-reloader-v0.35.0",
				},
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL:          "http://some-url/api/v1/write",
						MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
					},
					{
						URL: "http://some-url-2/api/v1/write",
					},
					{
						URL:          "http://some-url-3/api/v1/write",
						MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
					},
				},
				RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
					MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("20MB")),
				},
			},
		},
		wantYaml: `
volumes:
    - name: persistent-queue-data
      volumesource:
        emptydir: {}
initcontainers: []
containers:
    - name: vmagent
      image: victoriametrics/vmagent:v1.97.1
      args:
        - -httpListenAddr=:8425
        - -remoteWrite.maxDiskUsagePerURL=10GB,20MB,10GB
        - -remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data
        - -remoteWrite.url=http://some-url/api/v1/write,http://some-url-2/api/v1/write,http://some-url-3/api/v1/write
      ports:
        - name: http
          containerport: 8425
          protocol: TCP
      volumemounts:
        - name: persistent-queue-data
          mountpath: /tmp/vmagent-remotewrite-data
      livenessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8425
                scheme: HTTP
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
      readinessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8425
                scheme: HTTP
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
      terminationmessagepolicy: FallbackToLogsOnError
      imagepullpolicy: IfNotPresent
serviceaccountname: vmagent-agent

    `,
	})

	// test MaxDiskUsage with RemoteWriteSettings and extraArgs overwrite
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					IngestOnlyMode: ptr.To(true),
				},
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
					Image: vmv1beta1.Image{
						Tag: "v1.97.1",
					},
					UseDefaultResources: ptr.To(false),
					Port:                "8425",
				},
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImage: "vmcustom:config-reloader-v0.35.0",
				},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ExtraArgs: map[string]string{
						"remoteWrite.maxDiskUsagePerURL": "35GiB",
						"remoteWrite.forceVMProto":       "false",
					},
				},
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{
						URL:          "http://some-url/api/v1/write",
						MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
					},
					{
						URL:          "http://some-url-2/api/v1/write",
						ForceVMProto: true,
					},
					{
						URL:          "http://some-url-3/api/v1/write",
						MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
					},
				},
				RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{
					MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("20MB")),
				},
			},
		},
		wantYaml: `
volumes:
    - name: persistent-queue-data
      volumesource:
        emptydir: {}
initcontainers: []
containers:
    - name: vmagent
      image: victoriametrics/vmagent:v1.97.1
      args:
        - -httpListenAddr=:8425
        - -remoteWrite.forceVMProto=false
        - -remoteWrite.maxDiskUsagePerURL=35GiB
        - -remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data
        - -remoteWrite.url=http://some-url/api/v1/write,http://some-url-2/api/v1/write,http://some-url-3/api/v1/write
      ports:
        - name: http
          containerport: 8425
          protocol: TCP
      volumemounts:
        - name: persistent-queue-data
          mountpath: /tmp/vmagent-remotewrite-data
      livenessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8425
                scheme: HTTP
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
      readinessprobe:
        probehandler:
            httpget:
                path: /health
                port:
                    intval: 8425
                scheme: HTTP
        timeoutseconds: 5
        periodseconds: 5
        successthreshold: 1
        failurethreshold: 10
      terminationmessagepolicy: FallbackToLogsOnError
      imagepullpolicy: IfNotPresent
serviceaccountname: vmagent-agent

    `,
	})

}
