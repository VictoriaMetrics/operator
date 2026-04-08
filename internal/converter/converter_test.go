package converter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
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

func TestConvertVMAlert(t *testing.T) {
	f := func(values *VMAlertHelmValues, expected func() *vmv1beta1.VMAlert) {
		t.Helper()
		actual := Convert("test-alert", "test-ns", values)
		assert.Equal(t, expected(), actual)
	}

	f(
		&VMAlertHelmValues{
			Server: VMAlertServerValues{
				ReplicaCount: ptr.To(int32(2)),
				Image: ImageValues{
					Repository: "victoriametrics/vmalert",
					Tag:        "v1.93.0",
				},
				Notifier: &vmv1beta1.VMAlertNotifierSpec{
					URL: "http://vmalertmanager:9093",
				},
			},
		},
		func() *vmv1beta1.VMAlert {
			cr := &vmv1beta1.VMAlert{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operator.victoriametrics.com/v1beta1",
					Kind:       "VMAlert",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-alert",
					Namespace: "test-ns",
				},
			}
			cr.Spec.ReplicaCount = ptr.To(int32(2))
			cr.Spec.Image.Repository = "victoriametrics/vmalert"
			cr.Spec.Image.Tag = "v1.93.0"
			cr.Spec.Notifier = &vmv1beta1.VMAlertNotifierSpec{
				URL: "http://vmalertmanager:9093",
			}
			return cr
		},
	)
}

func TestConvertVMAnomaly(t *testing.T) {
	f := func(values *VMAnomalyHelmValues, expected func() *vmv1.VMAnomaly) {
		t.Helper()
		actual := Convert("test-anomaly", "test-ns", values)
		assert.Equal(t, expected(), actual)
	}

	f(
		&VMAnomalyHelmValues{
			ReplicaCount: ptr.To(int32(1)),
			Image: ImageValues{
				Repository: "victoriametrics/vmanomaly",
				Tag:        "v1.13.0",
			},
			Reader: &VMAnomalyReaderValues{
				DatasourceURL:  "http://vmselect:8481/select/0/prometheus",
				SamplingPeriod: "1m",
			},
			Writer: &VMAnomalyWriterValues{
				DatasourceURL: "http://vminsert:8480/insert/0/prometheus",
			},
		},
		func() *vmv1.VMAnomaly {
			cr := &vmv1.VMAnomaly{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operator.victoriametrics.com/v1",
					Kind:       "VMAnomaly",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-anomaly",
					Namespace: "test-ns",
				},
			}
			cr.Spec.ReplicaCount = ptr.To(int32(1))
			cr.Spec.Image.Repository = "victoriametrics/vmanomaly"
			cr.Spec.Image.Tag = "v1.13.0"
			cr.Spec.Reader = &vmv1.VMAnomalyReadersSpec{
				DatasourceURL:  "http://vmselect:8481/select/0/prometheus",
				SamplingPeriod: "1m",
			}
			cr.Spec.Writer = &vmv1.VMAnomalyWritersSpec{
				DatasourceURL: "http://vminsert:8480/insert/0/prometheus",
			}
			return cr
		},
	)
}
func TestConvertVLAgent(t *testing.T) {
	f := func(values *VLAgentHelmValues, expected func() *vmv1.VLAgent) {
		t.Helper()
		actual := Convert("test-name", "test-ns", values)
		assert.Equal(t, expected(), actual)
	}

	// Basic conversion
	f(
		&VLAgentHelmValues{
			Image: ImageValues{
				Repository: "victoriametrics/victoria-logs",
				Tag:        "v0.3.2",
			},
			ReplicaCount: ptr.To(int32(1)),
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{URL: "http://victoria-logs:9428"},
			},
			MaxDiskUsagePerURL: "1GiB",
		},
		func() *vmv1.VLAgent {
			return &vmv1.VLAgent{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operator.victoriametrics.com/v1",
					Kind:       "VLAgent",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-ns",
				},
				Spec: vmv1.VLAgentSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						Image: vmv1beta1.Image{
							Repository: "victoriametrics/victoria-logs",
							Tag:        "v0.3.2",
						},
						ReplicaCount: ptr.To(int32(1)),
						ExtraArgs: map[string]string{
							"remoteWrite.maxDiskUsagePerURL": "1GiB",
						},
					},
					RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
						{URL: "http://victoria-logs:9428"},
					},
				},
			}
		},
	)
}
func TestConvertVLCluster(t *testing.T) {
	f := func(values *VLClusterHelmValues, expected func() *vmv1.VLCluster) {
		t.Helper()
		actual := Convert("test-name", "test-ns", values)
		assert.Equal(t, expected(), actual)
	}

	// Basic conversion
	f(
		&VLClusterHelmValues{
			VLSelect: ServerValues{
				Enabled: ptr.To(true),
				Image: ImageValues{
					Repository: "victoriametrics/victoria-logs",
					Tag:        "v0.3.2",
				},
				ReplicaCount: ptr.To(int32(2)),
			},
			VLInsert: ServerValues{
				Enabled: ptr.To(true),
				Image: ImageValues{
					Repository: "victoriametrics/victoria-logs",
					Tag:        "v0.3.2",
				},
				ReplicaCount: ptr.To(int32(2)),
			},
			VLStorage: ServerValues{
				Enabled: ptr.To(true),
				Image: ImageValues{
					Repository: "victoriametrics/victoria-logs",
					Tag:        "v0.3.2",
				},
				ReplicaCount: ptr.To(int32(2)),
			},
		},
		func() *vmv1.VLCluster {
			return &vmv1.VLCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operator.victoriametrics.com/v1",
					Kind:       "VLCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-ns",
				},
				Spec: vmv1.VLClusterSpec{
					VLSelect: &vmv1.VLSelect{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Repository: "victoriametrics/victoria-logs",
								Tag:        "v0.3.2",
							},
							ReplicaCount: ptr.To(int32(2)),
						},
					},
					VLInsert: &vmv1.VLInsert{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Repository: "victoriametrics/victoria-logs",
								Tag:        "v0.3.2",
							},
							ReplicaCount: ptr.To(int32(2)),
						},
					},
					VLStorage: &vmv1.VLStorage{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Repository: "victoriametrics/victoria-logs",
								Tag:        "v0.3.2",
							},
							ReplicaCount: ptr.To(int32(2)),
						},
					},
				},
			}
		},
	)
}
func TestConvertVLCollector(t *testing.T) {
	f := func(values *VLCollectorHelmValues, expected func() *vmv1.VLAgent) {
		t.Helper()
		actual := Convert("test-name", "test-ns", values)
		assert.Equal(t, expected(), actual)
	}

	// Basic conversion
	f(
		&VLCollectorHelmValues{
			Image: ImageValues{
				Repository: "victoriametrics/vlagent",
				Tag:        "v0.3.2",
			},
			RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
				{URL: "http://victoria-logs:9428"},
			},
			Collector: VLCollectorSettings{
				TimeField:        []string{"time"},
				ExcludeFilter:    "kubernetes.pod_name:=%{HOSTNAME}",
				IncludePodLabels: ptr.To(true),
			},
		},
		func() *vmv1.VLAgent {
			return &vmv1.VLAgent{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operator.victoriametrics.com/v1",
					Kind:       "VLAgent",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-ns",
				},
				Spec: vmv1.VLAgentSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						Image: vmv1beta1.Image{
							Repository: "victoriametrics/vlagent",
							Tag:        "v0.3.2",
						},
					},
					RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
						{URL: "http://victoria-logs:9428"},
					},
					K8sCollector: vmv1.VLAgentK8sCollector{
						Enabled:          true,
						TimeFields:       []string{"time"},
						ExcludeFilter:    "kubernetes.pod_name:=%{HOSTNAME}",
						IncludePodLabels: ptr.To(true),
					},
				},
			}
		},
	)
}
func TestConvertVLogs(t *testing.T) {
	f := func(values *VLogsHelmValues, expected func() *vmv1beta1.VLogs) {
		t.Helper()
		actual := Convert("test-name", "test-ns", values)
		assert.Equal(t, expected(), actual)
	}

	// Basic conversion
	f(
		&VLogsHelmValues{
			Server: ServerValues{
				Image: ImageValues{
					Repository: "victoriametrics/victoria-logs",
					Tag:        "v0.3.2",
				},
				ReplicaCount:    ptr.To(int32(1)),
				RetentionPeriod: "14d",
			},
		},
		func() *vmv1beta1.VLogs {
			return &vmv1beta1.VLogs{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operator.victoriametrics.com/v1beta1",
					Kind:       "VLogs",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-ns",
				},
				Spec: vmv1beta1.VLogsSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						Image: vmv1beta1.Image{
							Repository: "victoriametrics/victoria-logs",
							Tag:        "v0.3.2",
						},
						ReplicaCount: ptr.To(int32(1)),
					},
					RetentionPeriod: "14d",
				},
			}
		},
	)
}
func TestConvertVTSingle(t *testing.T) {
	f := func(values *VTSingleHelmValues, expected func() *vmv1.VTSingle) {
		t.Helper()
		actual := Convert("test-name", "test-ns", values)
		assert.Equal(t, expected(), actual)
	}

	// Basic conversion
	f(
		&VTSingleHelmValues{
			Server: ServerValues{
				Image: ImageValues{
					Repository: "victoriametrics/victoria-traces",
					Tag:        "v0.3.2",
				},
				ReplicaCount:    ptr.To(int32(1)),
				RetentionPeriod: "14d",
			},
		},
		func() *vmv1.VTSingle {
			return &vmv1.VTSingle{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operator.victoriametrics.com/v1",
					Kind:       "VTSingle",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-ns",
				},
				Spec: vmv1.VTSingleSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						Image: vmv1beta1.Image{
							Repository: "victoriametrics/victoria-traces",
							Tag:        "v0.3.2",
						},
						ReplicaCount: ptr.To(int32(1)),
					},
					RetentionPeriod: "14d",
				},
			}
		},
	)
}
