package vtagent

import (
	"context"
	"sort"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1.VTAgent
		cfgMutator        func(c *config.BaseOperatorConf)
		validate          func(set *appsv1.StatefulSet)
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		cfg := config.MustGetBaseConfig()
		if o.cfgMutator != nil {
			defaultCfg := *cfg
			defer func() {
				*config.MustGetBaseConfig() = defaultCfg
			}()
			o.cfgMutator(cfg)
		}
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(o.cr)
		synctest.Test(t, func(t *testing.T) {
			assert.NoError(t, CreateOrUpdate(ctx, o.cr, fclient))
			if o.validate != nil {
				var got appsv1.StatefulSet
				assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Namespace: o.cr.Namespace, Name: o.cr.PrefixedName()}, &got))
				o.validate(&got)
			}
		})
	}

	// generate vtagent statefulset with storage
	f(opts{
		cr: &vmv1.VTAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent",
				Namespace: "default",
			},
			Spec: vmv1.VTAgentSpec{
				RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(0)),
				},
				Storage: &vmv1beta1.StorageSpec{
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
		validate: func(got *appsv1.StatefulSet) {
			assert.Len(t, got.Spec.Template.Spec.Containers, 1)
			assert.Len(t, got.Spec.VolumeClaimTemplates, 2)
			assert.Equal(t, *got.Spec.VolumeClaimTemplates[0].Spec.StorageClassName, "embed-sc")
			assert.Equal(t, got.Spec.VolumeClaimTemplates[0].Spec.Resources, corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			})
			assert.Equal(t, *got.Spec.VolumeClaimTemplates[1].Spec.StorageClassName, "default")
			assert.Equal(t, got.Spec.VolumeClaimTemplates[1].Spec.Resources, corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("2Gi"),
				},
			})
		},
	})

	// generate vtagent with tls-secret
	f(opts{
		cr: &vmv1.VTAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent-tls",
				Namespace: "default",
			},
			Spec: vmv1.VTAgentSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(0)),
				},
				RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
					{
						URL: "http://remote-write2",
						TLSConfig: &vmv1.TLSConfig{
							CASecret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "remote2-secret",
								},
								Key: "ca",
							},
							CertSecret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "remote2-secret",
								},
								Key: "ca",
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
						TLSConfig: &vmv1.TLSConfig{
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
						TLSConfig: &vmv1.TLSConfig{CertFile: "/tmp/cert1", KeyFile: "/tmp/key1", CAFile: "/tmp/ca"},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "default"},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "remote2-secret", Namespace: "default"},
				Data:       map[string][]byte{"cert": []byte(`cert-data`), "ca": []byte(`ca-data`), "key": []byte(`key-data`)},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "remote3-secret", Namespace: "default"},
				Data:       map[string][]byte{"key": []byte(`key-data`)},
			},
		},
		validate: func(got *appsv1.StatefulSet) {
			assert.Len(t, got.Spec.Template.Spec.Containers, 1)
			cnt := got.Spec.Template.Spec.Containers[0]
			assert.Contains(t, cnt.Args, "-remoteWrite.tlsCAFile=,/etc/vt/remote-write-assets/remote2-secret/ca,,/tmp/ca")
			assert.Contains(t, cnt.Args, "-remoteWrite.tlsCertFile=,/etc/vt/remote-write-assets/remote2-secret/ca,,/tmp/cert1")
			assert.Contains(t, cnt.Args, "-remoteWrite.tlsKeyFile=,/etc/vt/remote-write-assets/remote2-secret/key,/etc/vt/remote-write-assets/remote3-secret/key,/tmp/key1")

			var volumeNames []string
			for _, v := range got.Spec.Template.Spec.Volumes {
				volumeNames = append(volumeNames, v.Name)
			}
			assert.Contains(t, volumeNames, remoteWriteAssetVolumeName("remote2-secret"))
			assert.Contains(t, volumeNames, remoteWriteAssetVolumeName("remote3-secret"))

			var mountNames []string
			for _, vm := range cnt.VolumeMounts {
				mountNames = append(mountNames, vm.Name)
			}
			assert.Contains(t, mountNames, remoteWriteAssetVolumeName("remote2-secret"))
			assert.Contains(t, mountNames, remoteWriteAssetVolumeName("remote3-secret"))
		},
	})

	// generate vtagent with prevSpec
	{
		fclient := k8stools.GetTestClientWithObjects(nil)
		ctx := context.TODO()
		build.AddDefaults(fclient.Scheme())

		prevSpec := vmv1.VTAgentSpec{
			RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
				{URL: "http://remote-write"},
			},
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To(int32(0)),
			},
			PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{
				MinAvailable: ptr.To(intstr.FromInt32(1)),
			},
		}
		cr := &vmv1.VTAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent-prevspec",
				Namespace: "default",
			},
			Spec: *prevSpec.DeepCopy(),
		}
		fclient.Scheme().Default(cr)

		synctest.Test(t, func(t *testing.T) {
			assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

			var pdb policyv1.PodDisruptionBudget
			pdbNSN := types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}
			assert.NoError(t, fclient.Get(ctx, pdbNSN, &pdb))

			// simulate the controller applying a spec update that drops the PodDisruptionBudget
			cr.Status.LastAppliedSpec = prevSpec.DeepCopy()
			cr.Spec.PodDisruptionBudget = nil
			assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

			err := fclient.Get(ctx, pdbNSN, &pdb)
			assert.Error(t, err)
			assert.True(t, k8serrors.IsNotFound(err))
		})
	}

	// generate vtagent with storage and additional claim templates
	f(opts{
		cr: &vmv1.VTAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent-storage",
				Namespace: "default",
			},
			Spec: vmv1.VTAgentSpec{
				RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
				Storage: &vmv1beta1.StorageSpec{
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
		validate: func(got *appsv1.StatefulSet) {
			assert.Len(t, got.Spec.Template.Spec.Containers, 1)
			assert.Len(t, got.Spec.VolumeClaimTemplates, 2)
			assert.Equal(t, *got.Spec.VolumeClaimTemplates[0].Spec.StorageClassName, "embed-sc")
			assert.Equal(t, got.Spec.VolumeClaimTemplates[0].Spec.Resources, corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				},
			})
			assert.Equal(t, *got.Spec.VolumeClaimTemplates[1].Spec.StorageClassName, "default")
			assert.Equal(t, got.Spec.VolumeClaimTemplates[1].Spec.Resources, corev1.VolumeResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("2Gi"),
				},
			})
		},
	})

	// with oauth2 rw
	f(opts{
		cr: &vmv1.VTAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oauth2",
				Namespace: "default",
			},
			Spec: vmv1.VTAgentSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(0)),
				},
				RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
					{
						URL: "http://some-url",
						OAuth2: &vmv1.OAuth2{
							TokenURL: "http://oauth2-svc/auth",
							ClientIDSecret: &corev1.SecretKeySelector{
								Key: "client-id",
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "oauth2-access",
								},
							},
							ClientSecret: &corev1.SecretKeySelector{
								Key: "client-secret",
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "oauth2-access",
								},
							},
						},
						TLSConfig: &vmv1.TLSConfig{},
					},
				},
			},
		},
		validate: func(set *appsv1.StatefulSet) {
			assert.Len(t, set.Spec.Template.Spec.Containers, 1)
			cnt := set.Spec.Template.Spec.Containers[0]
			assert.Equal(t, cnt.Name, "vtagent")
			assert.Contains(t, cnt.Args, "-remoteWrite.oauth2.clientSecretFile=/etc/vt/remote-write-assets/oauth2-access/client-secret")
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
	})

	// with basicAuth rw
	f(opts{
		cr: &vmv1.VTAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "basic-auth",
				Namespace: "default",
			},
			Spec: vmv1.VTAgentSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(0)),
				},
				RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
					{
						URL: "http://some-url",
						BasicAuth: &vmv1beta1.BasicAuth{
							Username: corev1.SecretKeySelector{
								Key: "username",
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "basic-auth-access",
								},
							},
							Password: corev1.SecretKeySelector{
								Key: "password",
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "basic-auth-access",
								},
							},
						},
					},
				},
			},
		},
		validate: func(set *appsv1.StatefulSet) {
			assert.Len(t, set.Spec.Template.Spec.Containers, 1)
			cnt := set.Spec.Template.Spec.Containers[0]
			assert.Equal(t, cnt.Name, "vtagent")
			assert.Contains(t, cnt.Args, "-remoteWrite.basicAuth.usernameFile=/etc/vt/remote-write-assets/basic-auth-access/username")
			assert.Contains(t, cnt.Args, "-remoteWrite.basicAuth.passwordFile=/etc/vt/remote-write-assets/basic-auth-access/password")
			var volumeNames []string
			for _, v := range set.Spec.Template.Spec.Volumes {
				volumeNames = append(volumeNames, v.Name)
			}
			assert.Contains(t, volumeNames, remoteWriteAssetVolumeName("basic-auth-access"))
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "basic-auth-access",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"username": []byte(`some-username`),
					"password": []byte(`some-password`),
				},
			},
		},
	})

	// a secret referenced both via spec.secrets and via a remote-write credential must get
	// two distinct mounts, since they're mounted at different paths for different purposes
	f(opts{
		cr: &vmv1.VTAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "shared-secret",
				Namespace: "default",
			},
			Spec: vmv1.VTAgentSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(0)),
					Secrets:      []string{"shared-secret"},
				},
				RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
					{
						URL: "http://some-url",
						BearerTokenSecret: &corev1.SecretKeySelector{
							Key: "token",
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "shared-secret",
							},
						},
					},
				},
			},
		},
		validate: func(set *appsv1.StatefulSet) {
			assert.Len(t, set.Spec.Template.Spec.Containers, 1)
			cnt := set.Spec.Template.Spec.Containers[0]
			assert.Contains(t, cnt.Args, "-remoteWrite.bearerTokenFile=/etc/vt/remote-write-assets/shared-secret/token")

			var volumeNames []string
			for _, v := range set.Spec.Template.Spec.Volumes {
				volumeNames = append(volumeNames, v.Name)
			}
			assert.Contains(t, volumeNames, "secret-shared-secret")
			assert.Contains(t, volumeNames, remoteWriteAssetVolumeName("shared-secret"))

			var genericMount, rwMount *corev1.VolumeMount
			for i, vm := range cnt.VolumeMounts {
				switch vm.Name {
				case "secret-shared-secret":
					genericMount = &cnt.VolumeMounts[i]
				case remoteWriteAssetVolumeName("shared-secret"):
					rwMount = &cnt.VolumeMounts[i]
				}
			}
			require.NotNil(t, genericMount, "spec.secrets mount must exist")
			require.NotNil(t, rwMount, "remote-write asset mount must exist")
			assert.Equal(t, "/etc/vm/secrets/shared-secret", genericMount.MountPath)
			assert.Equal(t, "/etc/vt/remote-write-assets/shared-secret", rwMount.MountPath)
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shared-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"token": []byte(`some-token`),
				},
			},
		},
	})

	// two distinct secret names that sanitize to the same DNS1123 label (differ only by a
	// character SanitizeVolumeName normalizes away) must not collide into a single volume
	f(opts{
		cr: &vmv1.VTAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "collision",
				Namespace: "default",
			},
			Spec: vmv1.VTAgentSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(0)),
				},
				RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
					{
						URL: "http://some-url",
						BearerTokenSecret: &corev1.SecretKeySelector{
							Key:                  "token",
							LocalObjectReference: corev1.LocalObjectReference{Name: "my.secret"},
						},
					},
					{
						URL: "http://some-url-2",
						BearerTokenSecret: &corev1.SecretKeySelector{
							Key:                  "token",
							LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
						},
					},
				},
			},
		},
		validate: func(set *appsv1.StatefulSet) {
			assert.Len(t, set.Spec.Template.Spec.Containers, 1)
			cnt := set.Spec.Template.Spec.Containers[0]
			assert.Contains(t, cnt.Args, "-remoteWrite.bearerTokenFile=/etc/vt/remote-write-assets/my.secret/token,/etc/vt/remote-write-assets/my-secret/token")

			nameA := remoteWriteAssetVolumeName("my.secret")
			nameB := remoteWriteAssetVolumeName("my-secret")
			require.NotEqual(t, nameA, nameB, "distinct secret names must not collide onto the same volume name")

			var volumeNames []string
			for _, v := range set.Spec.Template.Spec.Volumes {
				volumeNames = append(volumeNames, v.Name)
			}
			assert.Contains(t, volumeNames, nameA)
			assert.Contains(t, volumeNames, nameB)

			var mountNames []string
			for _, vm := range cnt.VolumeMounts {
				mountNames = append(mountNames, vm.Name)
			}
			assert.Contains(t, mountNames, nameA)
			assert.Contains(t, mountNames, nameB)
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "my.secret", Namespace: "default"},
				Data:       map[string][]byte{"token": []byte(`token-a`)},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
				Data:       map[string][]byte{"token": []byte(`token-b`)},
			},
		},
	})

	// with format field
	f(opts{
		cr: &vmv1.VTAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "with-format",
				Namespace: "default",
			},
			Spec: vmv1.VTAgentSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(0)),
				},
				RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
					{
						URL:    "http://some-url",
						Format: ptr.To("jsonline"),
					},
				},
			},
		},
		validate: func(set *appsv1.StatefulSet) {
			assert.Len(t, set.Spec.Template.Spec.Containers, 1)
			cnt := set.Spec.Template.Spec.Containers[0]
			assert.Contains(t, cnt.Args, "-remoteWrite.format=jsonline")
		},
	})

	// managed metadata
	f(opts{
		cr: &vmv1.VTAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent",
				Namespace: "default",
			},
			Spec: vmv1.VTAgentSpec{
				ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{
					Labels: map[string]string{
						"env": "prod",
					},
					Annotations: map[string]string{
						"controller": "true",
					},
				},
				RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
			},
		},
		validate: func(set *appsv1.StatefulSet) {
			assert.Equal(t, set.Labels, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vtagent",
				"app.kubernetes.io/instance":  "example-agent",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			})
			assert.Equal(t, set.Annotations, map[string]string{
				"controller": "true",
			})
		},
	})

	// common labels
	f(opts{
		cr: &vmv1.VTAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent",
				Namespace: "default",
			},
		},
		cfgMutator: func(c *config.BaseOperatorConf) {
			c.CommonLabels = map[string]string{
				"env": "prod",
			}
			c.CommonAnnotations = map[string]string{
				"controller": "true",
			}
		},
		validate: func(set *appsv1.StatefulSet) {
			assert.Equal(t, set.Labels, map[string]string{
				"env":                         "prod",
				"app.kubernetes.io/name":      "vtagent",
				"app.kubernetes.io/instance":  "example-agent",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			})
			assert.Equal(t, set.Annotations, map[string]string{
				"controller": "true",
			})
		},
	})

	// test custom terminationGracePeriodSeconds is propagated to pod spec
	f(opts{
		cr: &vmv1.VTAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent-grace",
				Namespace: "default",
			},
			Spec: vmv1.VTAgentSpec{
				RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount:                  ptr.To(int32(1)),
					TerminationGracePeriodSeconds: ptr.To[int64](60),
				},
			},
		},
		validate: func(got *appsv1.StatefulSet) {
			assert.NotNil(t, got.Spec.Template.Spec.TerminationGracePeriodSeconds)
			assert.Equal(t, int64(60), *got.Spec.Template.Spec.TerminationGracePeriodSeconds)
			cnt := got.Spec.Template.Spec.Containers[0]
			require.NotNil(t, cnt.Lifecycle)
			require.NotNil(t, cnt.Lifecycle.PreStop)
			assert.Equal(t, int64(15), cnt.Lifecycle.PreStop.Sleep.Seconds)
		},
	})
}

func TestBuildRemoteWriteArgs(t *testing.T) {
	f := func(cr *vmv1.VTAgent, want []string) {
		t.Helper()
		sort.Strings(want)
		got, err := buildRemoteWriteArgs(cr)
		assert.NoError(t, err)
		sort.Strings(got)
		assert.Equal(t, want, got)
	}

	// test with tls config full
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{
			RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
				{
					URL: "localhost:10429",
					TLSConfig: &vmv1.TLSConfig{
						CASecret: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "tls-secret",
							},
							Key: "ca",
						},
					},
				},
				{
					URL: "localhost:10429",
					TLSConfig: &vmv1.TLSConfig{
						CAFile: "/path/to_ca",
						CertSecret: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "tls-secret",
							},
							Key: "cert",
						},
					},
				},
			},
		},
	}, []string{
		`-remoteWrite.tlsCAFile=/etc/vt/remote-write-assets/tls-secret/ca,/path/to_ca`,
		`-remoteWrite.tlsCertFile=,/etc/vt/remote-write-assets/tls-secret/cert`,
		`-remoteWrite.url=localhost:10429,localhost:10429`,
	},
	)

	// test certFile/keyFile take precedence over certSecret/keySecret
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-files",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{
			RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
				{
					URL: "localhost:10429",
					TLSConfig: &vmv1.TLSConfig{
						CertFile: "/path/to_cert",
						KeyFile:  "/path/to_key",
					},
				},
				{
					URL: "localhost:10430",
					TLSConfig: &vmv1.TLSConfig{
						CertSecret: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "tls-secret",
							},
							Key: "cert",
						},
						KeySecret: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "tls-secret",
							},
							Key: "key",
						},
					},
				},
			},
		},
	}, []string{
		`-remoteWrite.tlsCertFile=/path/to_cert,/etc/vt/remote-write-assets/tls-secret/cert`,
		`-remoteWrite.tlsKeyFile=/path/to_key,/etc/vt/remote-write-assets/tls-secret/key`,
		`-remoteWrite.url=localhost:10429,localhost:10430`,
	})

	// test insecure with key only
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{
			RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
				{
					URL: "localhost:10429",
					TLSConfig: &vmv1.TLSConfig{
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
	}, []string{
		`-remoteWrite.url=localhost:10429`,
		`-remoteWrite.tlsInsecureSkipVerify=true`,
		`-remoteWrite.tlsKeyFile=/etc/vt/remote-write-assets/tls-secret/key`,
	})

	// test format field
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
			{
				URL:    "localhost:10429",
				Format: ptr.To("native"),
			},
			{
				URL:    "localhost:10430",
				Format: ptr.To("jsonline"),
			},
		}},
	}, []string{
		`-remoteWrite.url=localhost:10429,localhost:10430`,
		`-remoteWrite.format=native,jsonline`,
	})

	// test basicAuth
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
			{
				URL: "localhost:10429",
				BasicAuth: &vmv1beta1.BasicAuth{
					Username: corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "basic-auth-secret",
						},
						Key: "username",
					},
					Password: corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "basic-auth-secret",
						},
						Key: "password",
					},
				},
			},
			{
				URL: "localhost:10431",
				BasicAuth: &vmv1beta1.BasicAuth{
					Username: corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "basic-auth-secret-2",
						},
						Key: "username",
					},
					PasswordFile: "/path/to_password",
				},
			},
		}},
	}, []string{
		`-remoteWrite.url=localhost:10429,localhost:10431`,
		`-remoteWrite.basicAuth.usernameFile=/etc/vt/remote-write-assets/basic-auth-secret/username,/etc/vt/remote-write-assets/basic-auth-secret-2/username`,
		`-remoteWrite.basicAuth.passwordFile=/etc/vt/remote-write-assets/basic-auth-secret/password,/path/to_password`,
	})

	// test sendTimeout
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
			{
				URL: "localhost:10429",

				SendTimeout: ptr.To("10s"),
			},
			{
				URL:         "localhost:10431",
				SendTimeout: ptr.To("15s"),
			},
		}},
	}, []string{
		`-remoteWrite.url=localhost:10429,localhost:10431`,
		`-remoteWrite.sendTimeout=10s,15s`,
	})

	// test maxDiskUsage
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
			{
				URL:          "localhost:10429",
				MaxDiskUsage: ptr.To(vmv1beta1.BytesString("1500MB")),
			},
			{
				URL:          "localhost:10431",
				MaxDiskUsage: ptr.To(vmv1beta1.BytesString("500MB")),
			},
			{
				URL: "localhost:10432",
			},
		}},
	}, []string{
		`-remoteWrite.url=localhost:10429,localhost:10431,localhost:10432`,
		`-remoteWrite.maxDiskUsagePerURL=1500MB,500MB,`,
	})

	// test automatic maxDiskUsage
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{
			Storage: &vmv1beta1.StorageSpec{
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
			RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
				{
					URL: "localhost:10429",
				},
				{
					URL: "localhost:10431",
				},
				{
					URL: "localhost:10432",
				},
			},
		},
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=3579139413`,
		`-remoteWrite.url=localhost:10429,localhost:10431,localhost:10432`,
	})

	// test oauth2
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
			{
				URL:         "localhost:10429",
				SendTimeout: ptr.To("10s"),
			},
			{
				URL:         "localhost:10431",
				SendTimeout: ptr.To("15s"),
				OAuth2: &vmv1.OAuth2{
					Scopes:   []string{"scope-1", "scope-2"},
					TokenURL: "http://some-url",
					ClientSecret: &corev1.SecretKeySelector{
						Key: "some-client-secret",
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "some-secret",
						},
					},
					ClientIDSecret: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
						Key:                  "some-id",
					},
					EndpointParams: map[string]string{"query": "value1", "timeout": "30s"},
				},
			},
		}},
	}, []string{
		`-remoteWrite.oauth2.clientID=,/etc/vt/remote-write-assets/some-secret/some-id`,
		`-remoteWrite.oauth2.clientSecretFile=,/etc/vt/remote-write-assets/some-secret/some-client-secret`,
		`-remoteWrite.oauth2.scopes=,scope-1;scope-2`,
		`-remoteWrite.oauth2.tokenUrl=,http://some-url`,
		`-remoteWrite.oauth2.endpointParams=,'{"query":"value1","timeout":"30s"}'`,
		`-remoteWrite.url=localhost:10429,localhost:10431`,
		`-remoteWrite.sendTimeout=10s,15s`,
	})

	// test oauth2 endpointParams containing a single quote is escaped so it can't
	// break out of the enclosing quotes used to delimit the flag value
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
			{
				URL: "localhost:10429",
				OAuth2: &vmv1.OAuth2{
					TokenURL: "http://some-url",
					ClientSecret: &corev1.SecretKeySelector{
						Key:                  "some-client-secret",
						LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
					},
					ClientIDSecret: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
						Key:                  "some-id",
					},
					EndpointParams: map[string]string{"note": "it's a test"},
				},
			},
		}},
	}, []string{
		`-remoteWrite.oauth2.clientID=/etc/vt/remote-write-assets/some-secret/some-id`,
		`-remoteWrite.oauth2.clientSecretFile=/etc/vt/remote-write-assets/some-secret/some-client-secret`,
		`-remoteWrite.oauth2.tokenUrl=http://some-url`,
		`-remoteWrite.oauth2.endpointParams='{"note":"it\'s a test"}'`,
		`-remoteWrite.url=localhost:10429`,
	})

	// test bearer token
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
			{
				URL:         "localhost:10429",
				SendTimeout: ptr.To("10s"),
			},
			{
				URL:         "localhost:10431",
				SendTimeout: ptr.To("15s"),
				BearerTokenSecret: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
					Key:                  "some-key",
				},
			},
		}},
	}, []string{
		`-remoteWrite.bearerTokenFile=,/etc/vt/remote-write-assets/some-secret/some-key`,
		`-remoteWrite.url=localhost:10429,localhost:10431`,
		`-remoteWrite.sendTimeout=10s,15s`,
	})

	// test with headers
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
			{
				URL:         "localhost:10429",
				SendTimeout: ptr.To("10s"),
			},
			{
				URL:         "localhost:10431",
				SendTimeout: ptr.To("15s"),
				BearerTokenSecret: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
					Key:                  "some-key",
				},
				Headers: []string{"key: value", "second-key: value2"},
			},
		}},
	}, []string{
		`-remoteWrite.bearerTokenFile=,/etc/vt/remote-write-assets/some-secret/some-key`,
		`-remoteWrite.headers='','key: value^^second-key: value2'`,
		`-remoteWrite.url=localhost:10429,localhost:10431`,
		`-remoteWrite.sendTimeout=10s,15s`,
	})

	// test with proxyURL (one remote write with defaults)
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{
			RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
				{
					URL: "http://localhost:10431",
				},
				{
					URL:      "http://localhost:10432",
					ProxyURL: ptr.To("http://proxy.example.com"),
				},
				{
					URL: "http://localhost:10433",
				},
			},
		},
	}, []string{
		`-remoteWrite.proxyURL=,http://proxy.example.com,`,
		`-remoteWrite.url=http://localhost:10431,http://localhost:10432,http://localhost:10433`,
	})

	// test simple ok
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
	}, nil)

	// with remoteWriteSettings
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{
			RemoteWriteSettings: &vmv1.VTAgentRemoteWriteSettings{
				ShowURL:            ptr.To(true),
				TmpDataPath:        ptr.To("/tmp/my-path"),
				MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("1000")),
			},
		},
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=1000`,
		`-remoteWrite.tmpDataPath=/tmp/my-path`,
		`-remoteWrite.showURL=true`,
	})

	// maxDiskUsage already set in RemoteWriteSpec
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{
			RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
				{
					URL:          "localhost:10431",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("500MB")),
				},
			},
			RemoteWriteSettings: &vmv1.VTAgentRemoteWriteSettings{
				MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("1000")),
			},
		},
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=500MB`,
		`-remoteWrite.url=localhost:10431`,
	})

	// remoteWriteSettings with storage-based auto disk usage
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{
			RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
				{
					URL: "localhost:10429",
				},
				{
					URL: "localhost:10431",
				},
				{
					URL: "localhost:10432",
				},
			},
			RemoteWriteSettings: &vmv1.VTAgentRemoteWriteSettings{
				MaxBlockSize: ptr.To(vmv1beta1.BytesString(`1000`)),
			},
			Storage: &vmv1beta1.StorageSpec{
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
	}, []string{
		`-remoteWrite.url=localhost:10429,localhost:10431,localhost:10432`,
		`-remoteWrite.maxBlockSize=1000`,
		`-remoteWrite.maxDiskUsagePerURL=3579139413`,
	})

	// storage-based auto disk usage must be skipped when tmpDataPath is set, since the
	// persistent-queue buffer is then no longer backed by the storage PVC
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{
			TmpDataPath: ptr.To("/custom-path"),
			Storage: &vmv1beta1.StorageSpec{
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
			RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
				{URL: "localhost:10429"},
				{URL: "localhost:10431"},
			},
		},
	}, []string{
		`-remoteWrite.url=localhost:10429,localhost:10431`,
	})

	// RemoteWriteSettings.MaxDiskUsagePerURL must take priority over storage-based sizing
	// for targets without their own per-URL MaxDiskUsage, even when at least one other
	// target does set one (which forces the per-index flag rendering path)
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vtagent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{
			Storage: &vmv1beta1.StorageSpec{
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
			RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
				{
					URL:          "localhost:10429",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("500MB")),
				},
				{
					URL: "localhost:10431",
				},
			},
			RemoteWriteSettings: &vmv1.VTAgentRemoteWriteSettings{
				MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("1234")),
			},
		},
	}, []string{
		`-remoteWrite.url=localhost:10429,localhost:10431`,
		`-remoteWrite.maxDiskUsagePerURL=500MB,1234`,
	})
}

func TestMakeSpecForAgentOk(t *testing.T) {
	f := func(cr *vmv1.VTAgent, predefinedObjects []runtime.Object, wantYaml string) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		scheme := fclient.Scheme()
		build.AddDefaults(scheme)
		scheme.Default(cr)
		// this trick allows to omit empty fields for yaml
		var wantSpec corev1.PodSpec
		assert.NoError(t, yaml.Unmarshal([]byte(wantYaml), &wantSpec))
		wantYAMLForCompare, err := yaml.Marshal(wantSpec)
		assert.NoError(t, err)
		got, err := newPodSpec(cr)
		assert.NoError(t, err)
		gotYAML, err := yaml.Marshal(got)
		assert.NoError(t, err)
		assert.Equal(t, string(wantYAMLForCompare), string(gotYAML))
	}
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: vmv1.VTAgentSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				Image: vmv1beta1.Image{
					Repository: "vt-repo",
					Tag:        "v0.11.0",
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
				Port: "10429",
			},
		},
	}, []runtime.Object{}, `
containers:
  - name: vtagent
    image: vt-repo:v0.11.0
    args:
      - -httpListenAddr=:10429
      - -tmpDataPath=/vtagent-data
    ports:
      - name: http
        containerport: 10429
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
      - name: tmp-data
        mountpath: /vtagent-data
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 10429
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
            intval: 10429
          scheme: HTTP
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    lifecycle:
      prestop:
        sleep:
          seconds: 15
    terminationmessagepolicy: FallbackToLogsOnError
    imagepullpolicy: IfNotPresent
serviceaccountname: vtagent-agent

    `)
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: vmv1.VTAgentSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				Image: vmv1beta1.Image{
					Tag: "v0.11.0",
				},
				UseDefaultResources: ptr.To(false),
				Port:                "10429",
			},
		},
	}, []runtime.Object{}, `
containers:
  - name: vtagent
    image: victoriametrics/vtagent:v0.11.0
    args:
      - -httpListenAddr=:10429
      - -tmpDataPath=/vtagent-data
    ports:
      - name: http
        containerport: 10429
        protocol: TCP
    volumemounts:
      - name: tmp-data
        mountpath: /vtagent-data
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 10429
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
            intval: 10429
          scheme: HTTP
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    lifecycle:
      prestop:
        sleep:
          seconds: 15
    terminationmessagepolicy: FallbackToLogsOnError
    imagepullpolicy: IfNotPresent
serviceaccountname: vtagent-agent
`)

	// test maxDiskUsage and empty remoteWriteSettings
	f(&vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: vmv1.VTAgentSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				Image: vmv1beta1.Image{
					Tag: "v0.11.0",
				},
				UseDefaultResources: ptr.To(false),
				Port:                "10429",
			},
			RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
				{
					URL:          "http://some-url/insert/native",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
				},
				{
					URL:          "http://some-url-2/insert/native",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("10GB")),
				},
				{
					URL: "http://some-url-3/insert/native",
				},
			},
		},
	}, []runtime.Object{}, `
containers:
  - name: vtagent
    image: victoriametrics/vtagent:v0.11.0
    args:
      - -httpListenAddr=:10429
      - -remoteWrite.maxDiskUsagePerURL=10GB,10GB,
      - -remoteWrite.url=http://some-url/insert/native,http://some-url-2/insert/native,http://some-url-3/insert/native
      - -tmpDataPath=/vtagent-data
    ports:
      - name: http
        containerport: 10429
        protocol: TCP
    volumemounts:
      - name: tmp-data
        mountpath: /vtagent-data
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 10429
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
            intval: 10429
          scheme: HTTP
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    lifecycle:
      prestop:
        sleep:
          seconds: 15
    terminationmessagepolicy: FallbackToLogsOnError
    imagepullpolicy: IfNotPresent
serviceaccountname: vtagent-agent

    `)
}

func TestCreateOrUpdate_Paused(t *testing.T) {
	// Create a paused VTAgent CR and test that it is not reconciled
	cr := &vmv1.VTAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-agent",
			Namespace: "default",
		},
		Spec: vmv1.VTAgentSpec{
			RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
				{URL: "http://remote-write"},
			},
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To(int32(1)),
				Paused:       true,
			},
		},
	}
	nsn := types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr})
	ctx := context.TODO()
	build.AddDefaults(fclient.Scheme())
	fclient.Scheme().Default(cr)

	synctest.Test(t, func(t *testing.T) {
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

		var sts appsv1.StatefulSet
		err := fclient.Get(ctx, nsn, &sts)
		assert.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))

		// unpause and verify reconciliation
		cr.Spec.Paused = false
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
		err = fclient.Get(ctx, nsn, &sts)
		assert.NoError(t, err)

		// pause and update replica count
		cr.Spec.Paused = true
		cr.Spec.ReplicaCount = ptr.To(int32(2))
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

		// check that replicas count is not updated
		err = fclient.Get(ctx, nsn, &sts)
		assert.NoError(t, err)
		assert.Equal(t, int32(1), *sts.Spec.Replicas)
	})
}
