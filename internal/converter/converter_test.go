package converter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestConvertVMSingle(t *testing.T) {
	f := func(values *VMSingleHelmValues, expected func() *vmv1beta1.VMSingle) {
		t.Helper()
		actual := Convert("test-name", "test-ns", values)
		assert.Equal(t, expected(), actual)
	}

	// Basic conversion
	f(
		&VMSingleHelmValues{
			Server: ServerValues{
				Image: ImageValues{
					Repository: "victoriametrics/victoria-metrics",
					Tag:        "v1.93.0",
					PullPolicy: "Always",
				},
				ReplicaCount:    ptr.To(int32(2)),
				RetentionPeriod: "14d",
			},
		},
		func() *vmv1beta1.VMSingle {
			cr := &vmv1beta1.VMSingle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operator.victoriametrics.com/v1beta1",
					Kind:       "VMSingle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-ns",
				},
			}
			cr.Spec.ReplicaCount = ptr.To(int32(2))
			cr.Spec.Image.Repository = "victoriametrics/victoria-metrics"
			cr.Spec.Image.Tag = "v1.93.0"
			cr.Spec.Image.PullPolicy = corev1.PullAlways
			cr.Spec.RetentionPeriod = "14d"
			return cr
		},
	)

	// Complex conversion with storage, extraArgs, image registry and variant
	f(
		&VMSingleHelmValues{
			Global: GlobalValues{
				Image: ImageValues{
					Registry: "quay.io",
				},
			},
			Server: ServerValues{
				Image: ImageValues{
					Repository: "victoriametrics/victoria-metrics",
					Tag:        "v1.93.0",
					Variant:    "cluster",
					PullPolicy: "IfNotPresent",
				},
				ExtraArgs: map[string]interface{}{
					"envflag.enable": true,
					"loggerFormat":   "json",
				},
				PersistentVolume: &PersistentVolumeValues{
					Enabled:      true,
					StorageClass: "fast-storage",
					Size:         "10Gi",
				},
				PodAnnotations: map[string]string{
					"prometheus.io/scrape": "true",
				},
			},
		},
		func() *vmv1beta1.VMSingle {
			cr := &vmv1beta1.VMSingle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operator.victoriametrics.com/v1beta1",
					Kind:       "VMSingle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-ns",
				},
			}
			cr.Spec.Image.Repository = "quay.io/victoriametrics/victoria-metrics"
			cr.Spec.Image.Tag = "v1.93.0-cluster"
			cr.Spec.Image.PullPolicy = corev1.PullIfNotPresent
			cr.Spec.ExtraArgs = map[string]string{
				"envflag.enable": "true",
				"loggerFormat":   "json",
			}
			cr.Spec.Storage = &corev1.PersistentVolumeClaimSpec{
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				StorageClassName: ptr.To("fast-storage"),
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("10Gi"),
					},
				},
			}
			cr.Spec.PodMetadata = &vmv1beta1.EmbeddedObjectMetadata{
				Annotations: map[string]string{
					"prometheus.io/scrape": "true",
				},
			}
			return cr
		},
	)
}

func TestConvertVMCluster(t *testing.T) {
	f := func(values *VMClusterHelmValues, expected func() *vmv1beta1.VMCluster) {
		t.Helper()
		actual := Convert("test-cluster", "test-ns", values)
		assert.Equal(t, expected(), actual)
	}

	f(
		&VMClusterHelmValues{
			VMSelect: ServerValues{
				ReplicaCount: ptr.To(int32(2)),
				Image: ImageValues{
					Repository: "victoriametrics/vmselect",
					Tag:        "v1.93.0",
					Variant:    "cluster",
				},
			},
			VMInsert: ServerValues{
				ReplicaCount: ptr.To(int32(2)),
				Image: ImageValues{
					Repository: "victoriametrics/vminsert",
					Tag:        "v1.93.0",
					Variant:    "cluster",
				},
			},
			VMStorage: ServerValues{
				ReplicaCount:    ptr.To(int32(2)),
				RetentionPeriod: "14d",
				Image: ImageValues{
					Repository: "victoriametrics/vmstorage",
					Tag:        "v1.93.0",
					Variant:    "cluster",
				},
			},
		},
		func() *vmv1beta1.VMCluster {
			cr := &vmv1beta1.VMCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operator.victoriametrics.com/v1beta1",
					Kind:       "VMCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-ns",
				},
			}
			cr.Spec.RetentionPeriod = "14d"
			cr.Spec.VMSelect = &vmv1beta1.VMSelect{}
			cr.Spec.VMSelect.ReplicaCount = ptr.To(int32(2))
			cr.Spec.VMSelect.Image = vmv1beta1.Image{
				Repository: "victoriametrics/vmselect",
				Tag:        "v1.93.0-cluster",
			}
			cr.Spec.VMInsert = &vmv1beta1.VMInsert{}
			cr.Spec.VMInsert.ReplicaCount = ptr.To(int32(2))
			cr.Spec.VMInsert.Image = vmv1beta1.Image{
				Repository: "victoriametrics/vminsert",
				Tag:        "v1.93.0-cluster",
			}
			cr.Spec.VMStorage = &vmv1beta1.VMStorage{}
			cr.Spec.VMStorage.ReplicaCount = ptr.To(int32(2))
			cr.Spec.VMStorage.Image = vmv1beta1.Image{
				Repository: "victoriametrics/vmstorage",
				Tag:        "v1.93.0-cluster",
			}
			return cr
		},
	)
}

func TestConvertVMAgent(t *testing.T) {
	f := func(values *VMAgentHelmValues, expected func() *vmv1beta1.VMAgent) {
		t.Helper()
		actual := Convert("test-agent", "test-ns", values)
		assert.Equal(t, expected(), actual)
	}

	f(
		&VMAgentHelmValues{
			ReplicaCount: ptr.To(int32(2)),
			Image: ImageValues{
				Repository: "victoriametrics/vmagent",
				Tag:        "v1.93.0",
			},
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL: "http://vminsert:8480/insert/0/prometheus",
				},
			},
		},
		func() *vmv1beta1.VMAgent {
			cr := &vmv1beta1.VMAgent{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operator.victoriametrics.com/v1beta1",
					Kind:       "VMAgent",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "test-ns",
				},
			}
			cr.Spec.ReplicaCount = ptr.To(int32(2))
			cr.Spec.Image.Repository = "victoriametrics/vmagent"
			cr.Spec.Image.Tag = "v1.93.0"
			cr.Spec.RemoteWrite = []vmv1beta1.VMAgentRemoteWriteSpec{
				{
					URL: "http://vminsert:8480/insert/0/prometheus",
				},
			}
			return cr
		},
	)
}
