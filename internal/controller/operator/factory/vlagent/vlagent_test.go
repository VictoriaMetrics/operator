package vlagent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-test/deep"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	f := func(cr *vmv1.VLAgent, mustAddPrevSpec, statefulSetMode, wantErr bool, validate func(set *appsv1.StatefulSet) error, predefinedObjects ...runtime.Object) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		ctx := context.TODO()
		if mustAddPrevSpec {
			jsonSpec, err := json.Marshal(cr.Spec)
			if err != nil {
				t.Fatalf("cannot set last applied spec: %s", err)
			}
			if cr.Annotations == nil {
				cr.Annotations = make(map[string]string)
			}
			cr.Annotations["operator.victoriametrics/last-applied-spec"] = string(jsonSpec)
		}
		errC := make(chan error, 1)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(cr)
		go func() {
			err := CreateOrUpdate(ctx, cr, fclient)
			errC <- err
		}()

		if statefulSetMode {
			err := wait.PollUntilContextTimeout(context.Background(), 20*time.Millisecond, time.Second, false, func(ctx context.Context) (done bool, err error) {
				var sts appsv1.StatefulSet
				if err := fclient.Get(ctx, types.NamespacedName{Namespace: "default", Name: fmt.Sprintf("vlagent-%s", cr.Name)}, &sts); err != nil {
					return false, nil
				}
				sts.Status.ReadyReplicas = ptr.Deref(cr.Spec.ReplicaCount, 0)
				sts.Status.UpdatedReplicas = ptr.Deref(cr.Spec.ReplicaCount, 0)
				sts.Status.CurrentReplicas = ptr.Deref(cr.Spec.ReplicaCount, 0)
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

		err := <-errC
		if (err != nil) != wantErr {
			t.Errorf("CreateOrUpdate() error = %v, wantErr %v", err, wantErr)
			return
		}
		if statefulSetMode {
			var got appsv1.StatefulSet
			if err := fclient.Get(context.Background(), types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, &got); (err != nil) != wantErr {
				t.Fatalf("CreateOrUpdate() error = %v, wantErr %v", err, wantErr)
			}
			if err := validate(&got); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	}

	// generate vlagent statefulset with storage
	f(&vmv1.VLAgent{
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
			CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{},
			Mode:                    vmv1.StatefulSetMode,
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
	}, false, true, false, func(got *appsv1.StatefulSet) error {
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
	}, k8stools.NewReadyDeployment("vlagent-example-agent", "default"))

	// generate vlagent with bauth-secret
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-agent-bauth",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{
					URL: "http://remote-write",
					BasicAuth: &vmv1beta1.BasicAuth{
						Username: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "bauth-secret",
							},
							Key: "user",
						},
						Password: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "bauth-secret",
							},
							Key: "password",
						},
					},
				},
			},
		},
	}, false, false, false, nil, k8stools.NewReadyDeployment("vlagent-example-agent-bauth", "default"), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "bauth-secret", Namespace: "default"},
		Data:       map[string][]byte{"user": []byte(`user-name`), "password": []byte(`user-password`)},
	})

	// fail if bearer token secret is missing, without basic auth
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-agent-bearer-missing",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{
					URL:               "http://remote-write",
					BearerTokenSecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "bearer-secret"}, Key: "token"},
				},
			},
		},
	}, false, false, true, nil)

	// generate vlagent with tls-secret
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-agent-tls",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
		},
	}, false, false, false, nil, k8stools.NewReadyDeployment("vlagent-example-agent-tls", "default"), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "default"},
	}, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tls-scrape", Namespace: "default"},
		Data:       map[string][]byte{"cert": []byte(`cert-data`), "ca": []byte(`ca-data`), "key": []byte(`key-data`)},
	}, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "remote2-secret", Namespace: "default"},
		Data:       map[string][]byte{"cert": []byte(`cert-data`), "ca": []byte(`ca-data`), "key": []byte(`key-data`)},
	}, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "remote3-secret", Namespace: "default"},
		Data:       map[string][]byte{"key": []byte(`key-data`)},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "remote3-cm", Namespace: "default"},
		Data:       map[string]string{"ca": "ca-data", "cert": "cert-data"},
	})

	// generate vlagent statefulset with serviceName when additional service is headless
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-agent-with-headless-service",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{URL: "http://remote-write"},
			},
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptr.To(int32(1)),
			},
			Mode: vmv1.StatefulSetMode,
			ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
				EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
					Name: "my-headless-additional-service",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: corev1.ClusterIPNone,
				},
			},
		},
	}, false, true, false, func(got *appsv1.StatefulSet) error {
		if got.Spec.ServiceName != "my-headless-additional-service" {
			return fmt.Errorf("unexpected serviceName, got: %s, want: %s", got.Spec.ServiceName, "my-headless-additional-service")
		}
		return nil
	}, k8stools.NewReadyDeployment("vlagent-example-agent", "default"))

	// generate vlagent statefulset with prevSpec
	f(&vmv1.VLAgent{
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
			Mode: vmv1.StatefulSetMode,
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
	}, true, true, false, func(got *appsv1.StatefulSet) error {
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
	})

	// with oauth2 rw
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "oauth2",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptr.To(int32(0)),
			},
			Mode: vmv1.StatefulSetMode,
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
	}, false, true, false, func(set *appsv1.StatefulSet) error {
		cnt := set.Spec.Template.Spec.Containers[0]
		if cnt.Name != "vlagent" {
			return fmt.Errorf("unexpected container name: %q, want: vlagent", cnt.Name)
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
	}, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "oauth2-access",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"client-secret": []byte(`some-secret-value`),
			"client-id":     []byte(`some-id-value`),
		},
	})
}

func TestBuildRemoteWriteArgs(t *testing.T) {
	f := func(cr *vmv1.VLAgent, want []string, predefinedObjects ...runtime.Object) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		ac := getAssetsCache(ctx, fclient, cr)
		sort.Strings(want)
		got, err := buildRemoteWriteArgs(cr, ac)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		sort.Strings(got)
		assert.Equal(t, want, got)
	}

	// test with tls config full
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "oauth2",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=1073741824`,
		`-remoteWrite.tlsCAFile=/etc/vlagent-tls/certs/default_tls-secret_ca,/path/to_ca`,
		`-remoteWrite.tlsCertFile=,/etc/vlagent-tls/certs/default_tls-secret_cert`,
		`-remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data`,
		`-remoteWrite.url=localhost:8429,localhost:8429`,
	}, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"ca":   []byte("ca-value"),
			"cert": []byte("cert-value"),
		},
	})

	// test insecure with key only
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=1073741824`,
		`-remoteWrite.url=localhost:8429`,
		`-remoteWrite.tlsInsecureSkipVerify=true`,
		`-remoteWrite.tlsKeyFile=/etc/vlagent-tls/certs/default_tls-secret_key`,
		`-remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data`,
	}, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"key": []byte("key-value"),
		},
	})

	// test insecure
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
			{
				URL: "localhost:8429",
				TLSConfig: &vmv1beta1.TLSConfig{
					InsecureSkipVerify: true,
				},
			},
		}},
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=1073741824`,
		`-remoteWrite.url=localhost:8429`,
		`-remoteWrite.tlsInsecureSkipVerify=true`,
		`-remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data`,
	})

	// test sendTimeout
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
			{
				URL: "localhost:8429",

				SendTimeout: ptr.To("10s"),
			},
			{
				URL:         "localhost:8431",
				SendTimeout: ptr.To("15s"),
			},
		}},
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=1073741824`,
		`-remoteWrite.url=localhost:8429,localhost:8431`,
		`-remoteWrite.sendTimeout=10s,15s`,
		`-remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data`,
	})

	// test maxDiskUsage
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
	}, []string{
		`-remoteWrite.url=localhost:8429,localhost:8431,localhost:8432`,
		`-remoteWrite.maxDiskUsagePerURL=1500MB,500MB,1073741824`,
		`-remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data`,
	})

	// test automatic maxDiskUsage
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			Mode: vmv1.StatefulSetMode,
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
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=3579139413`,
		`-remoteWrite.url=localhost:8429,localhost:8431,localhost:8432`,
		`-remoteWrite.tmpDataPath=/vlagent_pq/vlagent-remotewrite-data`,
	})

	// test automatic maxDiskUsage with at least one defined
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{
			Mode: vmv1.StatefulSetMode,
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
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
	}, []string{
		`-remoteWrite.url=localhost:8429,localhost:8431,localhost:8432`,
		`-remoteWrite.maxDiskUsagePerURL=5000MB,3579139413,3579139413`,
		`-remoteWrite.tmpDataPath=/vlagent_pq/vlagent-remotewrite-data`,
	})

	// test oauth2
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=1073741824`,
		`-remoteWrite.oauth2.clientID=,some-id`,
		`-remoteWrite.oauth2.clientSecretFile=,/etc/vlagent/config/default_some-cm_some-secret`,
		`-remoteWrite.oauth2.scopes=,scope-1`,
		`-remoteWrite.oauth2.tokenUrl=,http://some-url`,
		`-remoteWrite.url=localhost:8429,localhost:8431`,
		`-remoteWrite.sendTimeout=10s,15s`,
		`-remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data`,
	}, &corev1.Secret{
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
		})

	// test bearer token
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=1073741824`,
		`-remoteWrite.bearerTokenFile="","/etc/vlagent/config/default_some-secret_some-key"`,
		`-remoteWrite.url=localhost:8429,localhost:8431`,
		`-remoteWrite.sendTimeout=10s,15s`,
		`-remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data`,
	}, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"some-key": []byte("token"),
		},
	})

	// test with headers
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
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
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=1073741824`,
		`-remoteWrite.bearerTokenFile="","/etc/vlagent/config/default_some-secret_some-key"`,
		`-remoteWrite.headers=,key: value^^second-key: value2`,
		`-remoteWrite.url=localhost:8429,localhost:8431`,
		`-remoteWrite.sendTimeout=10s,15s`,
		`-remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data`,
	}, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"some-key": []byte("token"),
		},
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
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=1073741824`,
		`-remoteWrite.proxyURL=,http://proxy.example.com,`,
		`-remoteWrite.url=http://localhost:8431,http://localhost:8432,http://localhost:8433`,
		`-remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data`,
	})

	// test with StatefulMode
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
		Spec: vmv1.VLAgentSpec{Mode: vmv1.StatefulSetMode},
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=1073741824`,
		`-remoteWrite.tmpDataPath=/vlagent_pq/vlagent-remotewrite-data`,
	})

	// test simple ok
	f(&vmv1.VLAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vlagent",
			Namespace: "default",
		},
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=1073741824`,
		`-remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data`,
	})

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
					URL:          "localhost:8431",
					MaxDiskUsage: ptr.To(vmv1beta1.BytesString("500MB")),
				},
			},
			RemoteWriteSettings: &vmv1.VLAgentRemoteWriteSettings{
				MaxDiskUsagePerURL: ptr.To(vmv1beta1.BytesString("1000")),
			},
		},
	}, []string{
		`-remoteWrite.maxDiskUsagePerURL=500MB`,
		`-remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data`,
		`-remoteWrite.url=localhost:8431`,
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
					URL: "localhost:8429",
				},
				{
					URL: "localhost:8431",
				},
				{
					URL: "localhost:8432",
				},
			},
			RemoteWriteSettings: &vmv1.VLAgentRemoteWriteSettings{
				MaxBlockSize: ptr.To(int32(1000)),
			},
			Mode: vmv1.StatefulSetMode,
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
	}, []string{
		`-remoteWrite.url=localhost:8429,localhost:8431,localhost:8432`,
		`-remoteWrite.maxBlockSize=1000`,
		`-remoteWrite.maxDiskUsagePerURL=3579139413`,
		`-remoteWrite.tmpDataPath=/vlagent_pq/vlagent-remotewrite-data`,
	})
}

func TestCreateOrUpdateService(t *testing.T) {
	tests := []struct {
		name                  string
		cr                    *vmv1.VLAgent
		want                  func(svc *corev1.Service) error
		wantAdditionalService func(svc *corev1.Service) error
		wantErr               bool
		predefinedObjects     []runtime.Object
	}{
		{
			name: "base case",
			cr: &vmv1.VLAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "base",
					Namespace: "default",
				},
			},
			want: func(svc *corev1.Service) error {
				if svc.Name != "vlagent-base" {
					return fmt.Errorf("unexpected name for service: %v", svc.Name)
				}
				if len(svc.Spec.Ports) != 1 {
					return fmt.Errorf("unexpected count for service ports: %v", len(svc.Spec.Ports))
				}
				return nil
			},
		},
		{
			name: "base case with extra service",
			cr: &vmv1.VLAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "base",
					Namespace: "default",
				},
				Spec: vmv1.VLAgentSpec{
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
							"app.kubernetes.io/name":      "vlagent",
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
				if svc.Name != "vlagent-base" {
					return fmt.Errorf("unexpected name for service: %v", svc.Name)
				}
				if len(svc.Spec.Ports) != 1 {
					return fmt.Errorf("unexpected count for ports, want 3, got: %v", len(svc.Spec.Ports))
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			ctx := context.TODO()
			got, err := createOrUpdateService(ctx, cl, tt.cr, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateService() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err := tt.want(got); err != nil {
				t.Errorf("CreateOrUpdateService() unexpected error: %v", err)
			}
			if tt.wantAdditionalService != nil {
				var additionalSvc corev1.Service
				if err := cl.Get(ctx, types.NamespacedName{Namespace: tt.cr.Namespace, Name: tt.cr.Spec.ServiceSpec.NameOrDefault(tt.cr.Name)}, &additionalSvc); err != nil {
					t.Fatalf("unexpected error: %s", err)
				}
				if err := tt.wantAdditionalService(&additionalSvc); err != nil {
					t.Fatalf("CreateOrUpdateService validation failed for additional service: %s", err)
				}
			}
		})
	}
}

func TestMakeSpecForAgentOk(t *testing.T) {
	f := func(cr *vmv1.VLAgent, predefinedObjects []runtime.Object, wantYaml string) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		ac := getAssetsCache(ctx, fclient, cr)
		scheme := fclient.Scheme()
		build.AddDefaults(scheme)
		scheme.Default(cr)
		// this trick allows to omit empty fields for yaml
		var wantSpec corev1.PodSpec
		if err := yaml.Unmarshal([]byte(wantYaml), &wantSpec); err != nil {
			t.Fatalf("not expected wantYaml: %q: \n%q", wantYaml, err)
		}
		wantYAMLForCompare, err := yaml.Marshal(wantSpec)
		if err != nil {
			t.Fatalf("BUG: cannot parse as yaml: %q", err)
		}
		got, err := makeSpec(cr, ac)
		if err != nil {
			t.Fatalf("not expected error=%q", err)
		}
		gotYAML, err := yaml.Marshal(got)
		if err != nil {
			t.Fatalf("cannot parse got as yaml: %q", err)
		}

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
volumes:
  - name: config
    volumesource:
      secret:
        secretname: vlagent-agent
  - name: tls-assets
    volumesource:
      secret: 
        secretname: tls-assets-vlagent-agent
  - name: persistent-queue-data
    volumesource:
      emptydir:
        medium: ""
        sizelimit: null
containers:
  - name: vlagent
    image: vm-repo:v1.97.1
    args:
      - -httpListenAddr=:9425
      - -remoteWrite.maxDiskUsagePerURL=1073741824
      - -remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data
    ports:
      - name: http
        hostport: 0
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
      - name: config
        readonly: true
        mountpath: /etc/vlagent/config
      - name: tls-assets
        readonly: true
        mountpath: /etc/vlagent-tls/certs
      - name: persistent-queue-data
        readonly: false
        mountpath: /tmp/vlagent-remotewrite-data
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
      initialdelayseconds: 0
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
volumes:
  - name: config
    volumesource:
      secret: 
        secretname: vlagent-agent
  - name: tls-assets
    volumesource:
      secret:
        secretname: tls-assets-vlagent-agent
  - name: persistent-queue-data
    volumesource:
      emptydir: {}
containers:
  - name: vlagent
    image: victoriametrics/vlagent:v1.97.1
    args:
      - -httpListenAddr=:9429
      - -remoteWrite.maxDiskUsagePerURL=1073741824
      - -remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data
    ports:
      - name: http
        containerport: 9429
        protocol: TCP
    volumemounts:
      - name: config
        readonly: true
        mountpath: /etc/vlagent/config
      - name: tls-assets
        readonly: true
        mountpath: /etc/vlagent-tls/certs
      - name: persistent-queue-data
        readonly: false
        mountpath: /tmp/vlagent-remotewrite-data
        subpath: ""
        mountpropagation: null
        subpathexpr: ""
    livenessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 9429
          scheme: HTTP
      initialdelayseconds: 0
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
      terminationgraceperiodseconds: null
    readinessprobe:
      probehandler:
        httpget:
          path: /health
          port:
            intval: 9429
          scheme: HTTP
      initialdelayseconds: 0
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
      terminationgraceperiodseconds: null
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
volumes:
  - name: config
    volumesource:
      secret:
        secretname: vlagent-agent
  - name: tls-assets
    volumesource:
      secret:
        secretname: tls-assets-vlagent-agent
  - name: persistent-queue-data
    volumesource:
      emptydir:
        medium: ""
        sizelimit: null
containers:
  - name: vlagent
    image: victoriametrics/vlagent:v1.97.1
    args:
      - -httpListenAddr=:9425
      - -remoteWrite.maxDiskUsagePerURL=10GB,10GB,1073741824
      - -remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data
      - -remoteWrite.url=http://some-url/api/v1/write,http://some-url-2/api/v1/write,http://some-url-3/api/v1/write
    ports:
      - name: http
        containerport: 9425
        protocol: TCP
    resources:
      limits: {}
      requests: {}
      claims: []
    volumemounts:
      - name: config
        readonly: true
        mountpath: /etc/vlagent/config
      - name: tls-assets
        readonly: true
        mountpath: /etc/vlagent-tls/certs
      - name: persistent-queue-data
        readonly: false
        mountpath: /tmp/vlagent-remotewrite-data
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
      initialdelayseconds: 0
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    terminationmessagepolicy: FallbackToLogsOnError
    imagepullpolicy: IfNotPresent
serviceaccountname: vlagent-agent

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
volumes:
  - name: config
    volumesource:
      secret:
        secretname: vlagent-agent
  - name: tls-assets
    volumesource:
      secret:
        secretname: tls-assets-vlagent-agent
  - name: persistent-queue-data
    volumesource:
      emptydir:
        medium: ""
        sizelimit: null
containers:
  - name: vlagent
    image: victoriametrics/vlagent:v1.97.1
    args:
      - -httpListenAddr=:9425
      - -remoteWrite.maxDiskUsagePerURL=10GB,20MB,10GB
      - -remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data
      - -remoteWrite.url=http://some-url/api/v1/write,http://some-url-2/api/v1/write,http://some-url-3/api/v1/write
    ports:
      - name: http
        containerport: 9425
        protocol: TCP
    resources:
      limits: {}
      requests: {}
      claims: []
    volumemounts:
      - name: config
        readonly: true
        mountpath: /etc/vlagent/config
      - name: tls-assets
        readonly: true
        mountpath: /etc/vlagent-tls/certs
      - name: persistent-queue-data
        readonly: false
        mountpath: /tmp/vlagent-remotewrite-data
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
      initialdelayseconds: 0
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
volumes:
  - name: config
    volumesource:
      secret:
        secretname: vlagent-agent
  - name: tls-assets
    volumesource:
      secret:
        secretname: tls-assets-vlagent-agent
  - name: persistent-queue-data
    volumesource:
      emptydir:
        medium: ""
        sizelimit: null
containers:
  - name: vlagent
    image: victoriametrics/vlagent:v0.0.1
    args:
      - -httpListenAddr=:9425
      - -remoteWrite.maxDiskUsagePerURL=35GiB
      - -remoteWrite.tmpDataPath=/tmp/vlagent-remotewrite-data
      - -remoteWrite.url=http://some-url/api/v1/write,http://some-url-2/api/v1/write,http://some-url-3/api/v1/write
    ports:
      - name: http
        containerport: 9425
        protocol: TCP
    resources:
      limits: {}
      requests: {}
      claims: []
    volumemounts:
      - name: config
        readonly: true
        mountpath: /etc/vlagent/config
      - name: tls-assets
        readonly: true
        mountpath: /etc/vlagent-tls/certs
      - name: persistent-queue-data
        readonly: false
        mountpath: /tmp/vlagent-remotewrite-data
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
      initialdelayseconds: 0
      timeoutseconds: 5
      periodseconds: 5
      successthreshold: 1
      failurethreshold: 10
    terminationmessagepolicy: FallbackToLogsOnError
    imagepullpolicy: IfNotPresent
serviceaccountname: vlagent-agent

    `)

}
