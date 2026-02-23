package vlagent

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
		cr                *vmv1.VLAgent
		validate          func(set *appsv1.StatefulSet)
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(o.cr)
		assert.NoError(t, CreateOrUpdate(ctx, o.cr, fclient))
		if o.validate != nil {
			var got appsv1.StatefulSet
			assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Namespace: o.cr.Namespace, Name: o.cr.PrefixedName()}, &got))
			o.validate(&got)
		}
	}

	// generate vlagent statefulset with storage
	f(opts{
		cr: &vmv1.VLAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent",
				Namespace: "default",
			},
			Spec: vmv1.VLAgentSpec{
				RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(0)),
				},
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{},
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

	// generate vlagent with tls-secret
	f(opts{
		cr: &vmv1.VLAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent-tls",
				Namespace: "default",
			},
			Spec: vmv1.VLAgentSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(0)),
				},
				RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
		},
	})

	// generate vlagent with prevSpec
	f(opts{
		cr: &vmv1.VLAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-agent",
				Namespace: "default",
			},
			Spec: vmv1.VLAgentSpec{
				RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
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
		cr: &vmv1.VLAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oauth2",
				Namespace: "default",
			},
			Spec: vmv1.VLAgentSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(0)),
				},
				RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
			assert.Equal(t, cnt.Name, "vlagent")
			assert.Contains(t, cnt.Args, "-remoteWrite.oauth2.clientSecretFile=/etc/vl/remote-write-assets/oauth2-access/client-secret")
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
}

func TestBuildRemoteWriteArgs(t *testing.T) {
	f := func(cr *vmv1.VLAgent, want []string) {
		t.Helper()
		sort.Strings(want)
		got, err := buildRemoteWriteArgs(cr)
		assert.NoError(t, err)
		sort.Strings(got)
		assert.Equal(t, want, got)
	}

	// test with tls config full
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{
					URL: "localhost:9429",
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
					URL: "localhost:9429",
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
		`-remoteWrite.tlsCAFile=/etc/vl/remote-write-assets/tls-secret/ca,/path/to_ca`,
		`-remoteWrite.tlsCertFile=,/etc/vl/remote-write-assets/tls-secret/cert`,
		`-remoteWrite.url=localhost:9429,localhost:9429`,
	},
	)

	// test insecure with key only
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{
					URL: "localhost:9429",
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
		`-remoteWrite.url=localhost:9429`,
		`-remoteWrite.tlsInsecureSkipVerify=true`,
		`-remoteWrite.tlsKeyFile=/etc/vl/remote-write-assets/tls-secret/key`,
	})

	// test insecure
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
			{
				URL: "localhost:9429",
				TLSConfig: &vmv1.TLSConfig{
					InsecureSkipVerify: true,
				},
			},
		}},
	}, []string{
		`-remoteWrite.url=localhost:9429`,
		`-remoteWrite.tlsInsecureSkipVerify=true`,
	})

	// test sendTimeout
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
			{
				URL: "localhost:9429",

				SendTimeout: ptr.To("10s"),
			},
			{
				URL:         "localhost:9431",
				SendTimeout: ptr.To("15s"),
			},
		}},
	}, []string{
		`-remoteWrite.url=localhost:9429,localhost:9431`,
		`-remoteWrite.sendTimeout=10s,15s`,
	})

	// test maxDiskUsage
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
			{
				URL:          "localhost:9429",
				MaxDiskUsage: ptr.To(vmv1beta1.BytesString("1500MB")),
			},
			{
				URL:          "localhost:9431",
				MaxDiskUsage: ptr.To(vmv1beta1.BytesString("500MB")),
			},
			{
				URL: "localhost:9432",
			},
		}},
	}, []string{
		`-remoteWrite.url=localhost:9429,localhost:9431,localhost:9432`,
		`-remoteWrite.maxDiskUsagePerURL=1500MB,500MB,`,
	})

	// test automatic maxDiskUsage
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
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
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{
					URL: "localhost:9429",
				},
				{
					URL: "localhost:9431",
				},
				{
					URL: "localhost:9432",
				},
			},
		},
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=3579139413`,
		`-remoteWrite.url=localhost:9429,localhost:9431,localhost:9432`,
	})

	// test automatic maxDiskUsage with at least one defined
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
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
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{
					URL:          "localhost:9429",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("5000MB")),
				},
				{
					URL: "localhost:9431",
				},
				{
					URL: "localhost:9432",
				},
			},
		},
	}, []string{
		`-remoteWrite.url=localhost:9429,localhost:9431,localhost:9432`,
		`-remoteWrite.maxDiskUsagePerURL=5000MB,3579139413,3579139413`,
	})

	// test oauth2
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
			{
				URL:         "localhost:9429",
				SendTimeout: ptr.To("10s"),
			},
			{
				URL:         "localhost:9431",
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
		`-remoteWrite.oauth2.clientID=,/etc/vl/remote-write-assets/some-secret/some-id`,
		`-remoteWrite.oauth2.clientSecretFile=,/etc/vl/remote-write-assets/some-secret/some-client-secret`,
		`-remoteWrite.oauth2.scopes=,scope-1;scope-2`,
		`-remoteWrite.oauth2.tokenUrl=,http://some-url`,
		`-remoteWrite.oauth2.endpointParams=,'{"query":"value1","timeout":"30s"}'`,
		`-remoteWrite.url=localhost:9429,localhost:9431`,
		`-remoteWrite.sendTimeout=10s,15s`,
	})

	// test bearer token
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
			{
				URL:         "localhost:9429",
				SendTimeout: ptr.To("10s"),
			},
			{
				URL:         "localhost:9431",
				SendTimeout: ptr.To("15s"),
				BearerTokenSecret: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
					Key:                  "some-key",
				},
			},
		}},
	}, []string{
		`-remoteWrite.bearerTokenFile=,/etc/vl/remote-write-assets/some-secret/some-key`,
		`-remoteWrite.url=localhost:9429,localhost:9431`,
		`-remoteWrite.sendTimeout=10s,15s`,
	})

	// test with headers
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
			{
				URL:         "localhost:9429",
				SendTimeout: ptr.To("10s"),
			},
			{
				URL:         "localhost:9431",
				SendTimeout: ptr.To("15s"),
				BearerTokenSecret: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
					Key:                  "some-key",
				},
				Headers: []string{"key: value", "second-key: value2"},
			},
		}},
	}, []string{
		`-remoteWrite.bearerTokenFile=,/etc/vl/remote-write-assets/some-secret/some-key`,
		`-remoteWrite.headers='','key: value^^second-key: value2'`,
		`-remoteWrite.url=localhost:9429,localhost:9431`,
		`-remoteWrite.sendTimeout=10s,15s`,
	})

	// test with proxyURL (one remote write with defaults)
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{
					URL: "http://localhost:9431",
				},
				{
					URL:      "http://localhost:9432",
					ProxyURL: ptr.To("http://proxy.example.com"),
				},
				{
					URL: "http://localhost:9433",
				},
			},
		},
	}, []string{
		`-remoteWrite.proxyURL=,http://proxy.example.com,`,
		`-remoteWrite.url=http://localhost:9431,http://localhost:9432,http://localhost:9433`,
	})

	// test with StatefulMode
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{},
	}, nil)

	// test simple ok
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
	}, nil)

	// with remoteWriteSettings
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			RemoteWriteSettings: &vmv1.VLAgentRemoteWriteSettings{
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
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{
					URL:          "localhost:9431",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("500MB")),
				},
			},
			RemoteWriteSettings: &vmv1.VLAgentRemoteWriteSettings{
				MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("1000")),
			},
		},
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=500MB`,
		`-remoteWrite.url=localhost:9431`,
	})

	// maxDiskUsage already set in RemoteWriteSpec
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{
					URL: "localhost:9429",
				},
				{
					URL: "localhost:9431",
				},
				{
					URL: "localhost:9432",
				},
			},
			RemoteWriteSettings: &vmv1.VLAgentRemoteWriteSettings{
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
		`-remoteWrite.url=localhost:9429,localhost:9431,localhost:9432`,
		`-remoteWrite.maxBlockSize=1000`,
		`-remoteWrite.maxDiskUsagePerURL=3579139413`,
	})
}

func TestMakeSpecForAgentOk(t *testing.T) {
	f := func(cr *vmv1.VLAgent, predefinedObjects []runtime.Object, wantYaml string) {
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
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: vmv1.VLAgentSpec{
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
				Port: "9425",
			},
		},
	}, []runtime.Object{}, `
containers:
  - name: vlagent
    image: vm-repo:v1.97.1
    args:
      - -httpListenAddr=:9425
      - -tmpDataPath=/vlagent-data
    ports:
      - name: http
        containerport: 9425
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
        mountpath: /vlagent-data
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 9425
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
            intval: 9425
          scheme: HTTP
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    terminationmessagepolicy: FallbackToLogsOnError
    imagepullpolicy: IfNotPresent
serviceaccountname: vlagent-agent

    `)
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: vmv1.VLAgentSpec{
			CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
				Image: vmv1beta1.Image{
					Tag: "v1.97.1",
				},
				UseDefaultResources: ptr.To(false),
				Port:                "9429",
			},
		},
	}, []runtime.Object{}, `
containers:
  - name: vlagent
    image: victoriametrics/vlagent:v1.97.1
    args:
      - -httpListenAddr=:9429
      - -tmpDataPath=/vlagent-data
    ports:
      - name: http
        containerport: 9429
        protocol: TCP
    volumemounts:
      - name: tmp-data
        mountpath: /vlagent-data
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 9429
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
            intval: 9429
          scheme: HTTP
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    terminationmessagepolicy: FallbackToLogsOnError
    imagepullpolicy: IfNotPresent
serviceaccountname: vlagent-agent
`)

	// test maxDiskUsage and empty remoteWriteSettings
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: vmv1.VLAgentSpec{
			CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
				Image: vmv1beta1.Image{
					Tag: "v1.97.1",
				},
				UseDefaultResources: ptr.To(false),
				Port:                "9425",
			},
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
	}, []runtime.Object{}, `
containers:
  - name: vlagent
    image: victoriametrics/vlagent:v1.97.1
    args:
      - -httpListenAddr=:9425
      - -remoteWrite.maxDiskUsagePerURL=10GB,10GB,
      - -remoteWrite.url=http://some-url/api/v1/write,http://some-url-2/api/v1/write,http://some-url-3/api/v1/write
      - -tmpDataPath=/vlagent-data
    ports:
      - name: http
        containerport: 9425
        protocol: TCP
    volumemounts:
      - name: tmp-data
        mountpath: /vlagent-data
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 9425
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
            intval: 9425
          scheme: HTTP
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    terminationmessagepolicy: FallbackToLogsOnError
    imagepullpolicy: IfNotPresent
serviceaccountname: vlagent-agent

    `)

	// test k8s collector
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: vmv1.VLAgentSpec{
			CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
				Image: vmv1beta1.Image{
					Tag: "v1.46.0",
				},
				UseDefaultResources: ptr.To(false),
				Port:                "9425",
			},
			K8sCollector: vmv1.VLAgentK8sCollector{
				Enabled:   true,
				MsgFields: []string{"msg", "message"},
			},
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
	}, []runtime.Object{}, `
containers:
  - name: vlagent
    image: victoriametrics/vlagent:v1.46.0
    args:
      - -httpListenAddr=:9425
      - -kubernetesCollector
      - -kubernetesCollector.includePodLabels
      - -kubernetesCollector.msgField=msg,message
      - -remoteWrite.maxDiskUsagePerURL=10GB,10GB,
      - -remoteWrite.url=http://some-url/api/v1/write,http://some-url-2/api/v1/write,http://some-url-3/api/v1/write
      - -tmpDataPath=/var/lib/vlagent-data
    ports:
      - name: http
        containerport: 9425
        protocol: TCP
    volumemounts:
      - name: varlog
        readonly: true
        mountpath: /var/log
      - name: varlib
        readonly: true
        mountpath: /var/lib
      - name: tmp-data
        mountpath: /var/lib/vlagent-data
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 9425
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
            intval: 9425
          scheme: HTTP
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    terminationmessagepolicy: FallbackToLogsOnError
    imagepullpolicy: IfNotPresent
serviceaccountname: vlagent-agent
volumes:
- name: varlog
  volumesource:
    hostpath:
      path: /var/log
- name: varlib
  volumesource:
    hostpath:
      path: /var/lib
- name: tmp-data
  volumesource:
    hostpath:
      path: /var/lib/vlagent-data

    `)

	// test MaxDiskUsage with RemoteWriteSettings
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: vmv1.VLAgentSpec{
			CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
				Image: vmv1beta1.Image{
					Tag: "v1.97.1",
				},
				UseDefaultResources: ptr.To(false),
				Port:                "9425",
			},
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
			RemoteWriteSettings: &vmv1.VLAgentRemoteWriteSettings{
				MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("20MB")),
			},
		},
	}, nil, `
containers:
  - name: vlagent
    image: victoriametrics/vlagent:v1.97.1
    args:
      - -httpListenAddr=:9425
      - -remoteWrite.maxDiskUsagePerURL=10GB,20MB,10GB
      - -remoteWrite.url=http://some-url/api/v1/write,http://some-url-2/api/v1/write,http://some-url-3/api/v1/write
      - -tmpDataPath=/vlagent-data
    ports:
      - name: http
        containerport: 9425
        protocol: TCP
    volumemounts:
      - name: tmp-data
        mountpath: /vlagent-data
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 9425
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
            intval: 9425
          scheme: HTTP
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    terminationmessagepolicy: FallbackToLogsOnError
    imagepullpolicy: IfNotPresent
serviceaccountname: vlagent-agent

    `)
	// test MaxDiskUsage with RemoteWriteSettings and extraArgs overwrite
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Spec: vmv1.VLAgentSpec{
			CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
				Image: vmv1beta1.Image{
					Tag: "v0.0.1",
				},
				UseDefaultResources: ptr.To(false),
				Port:                "9425",
			},
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ExtraArgs: map[string]string{
					"remoteWrite.maxDiskUsagePerURL": "35GiB",
				},
			},
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
			RemoteWriteSettings: &vmv1.VLAgentRemoteWriteSettings{
				MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("20MB")),
			},
		},
	}, nil, `
containers:
  - name: vlagent
    image: victoriametrics/vlagent:v0.0.1
    args:
      - -httpListenAddr=:9425
      - -remoteWrite.maxDiskUsagePerURL=35GiB
      - -remoteWrite.url=http://some-url/api/v1/write,http://some-url-2/api/v1/write,http://some-url-3/api/v1/write
      - -tmpDataPath=/vlagent-data
    ports:
      - name: http
        containerport: 9425
        protocol: TCP
    volumemounts:
      - name: tmp-data
        mountpath: /vlagent-data
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 9425
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
            intval: 9425
          scheme: HTTP
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    terminationmessagepolicy: FallbackToLogsOnError
    imagepullpolicy: IfNotPresent
serviceaccountname: vlagent-agent

    `)

}
