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
		actual := ConvertVMSingle("test-name", "test-ns", values)
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
