package converter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	k8syaml "sigs.k8s.io/yaml"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func unmarshalYAML(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, k8syaml.Unmarshal(data, &m))
	return m
}

func TestUnmarshalValuesSupportsQuotedResourceQuantities(t *testing.T) {
	values, err := UnmarshalValues([]byte(`
server:
  resources:
    requests:
      cpu: "2"
      memory: "4Gi"
    limits:
      cpu: "4"
      memory: "8Gi"
`), "victoria-metrics-single")
	require.NoError(t, err)

	got := values.(*VMSingleHelmValues)
	require.NotNil(t, got.Server.Resources)
	assert.Equal(t, resource.MustParse("2"), got.Server.Resources.Requests[corev1.ResourceCPU])
	assert.Equal(t, resource.MustParse("4Gi"), got.Server.Resources.Requests[corev1.ResourceMemory])
	assert.Equal(t, resource.MustParse("4"), got.Server.Resources.Limits[corev1.ResourceCPU])
	assert.Equal(t, resource.MustParse("8Gi"), got.Server.Resources.Limits[corev1.ResourceMemory])
}

func TestUnmarshalValuesSupportsEnvAliasFields(t *testing.T) {
	values, err := UnmarshalValues([]byte(`
env:
  - name: FOO
    value: bar
remoteWrite:
  - url: http://vminsert:8480/insert/0/prometheus
image:
  repository: victoriametrics/vmagent
  tag: v1.93.0
`), "victoria-metrics-agent")
	require.NoError(t, err)

	got := values.(*VMAgentHelmValues)
	require.Len(t, got.ExtraEnvs, 1)
	assert.Equal(t, "FOO", got.ExtraEnvs[0].Name)
	assert.Equal(t, "bar", got.ExtraEnvs[0].Value)
}

// TestUnmarshalValuesSupportsStorageClassName reproduces #2389: the helm charts use
// persistentVolume.storageClassName (not storageClass), and must go through real YAML ->
// UnmarshalValues -> Convert, not direct struct construction which hides yaml/json tag bugs.
func TestUnmarshalValuesSupportsStorageClassName(t *testing.T) {
	values, err := UnmarshalValues([]byte(`
server:
  persistentVolume:
    enabled: true
    storageClassName: fast-storage
    size: 10Gi
`), "victoria-metrics-single")
	require.NoError(t, err)

	got := values.(*VMSingleHelmValues)
	require.NotNil(t, got.Server.PersistentVolume)
	assert.Equal(t, "fast-storage", got.Server.PersistentVolume.StorageClassName)

	cr, err := Convert("test-name", "test-ns", got)
	require.NoError(t, err)
	single := cr.(*vmv1beta1.VMSingle)
	require.NotNil(t, single.Spec.Storage)
	require.NotNil(t, single.Spec.Storage.StorageClassName)
	assert.Equal(t, "fast-storage", *single.Spec.Storage.StorageClassName)
}

// TestUnmarshalValuesSupportsStorageClassNameForVLCluster covers #2389's exact CR path:
// spec.vlstorage.storage.volumeClaimTemplate.
func TestUnmarshalValuesSupportsStorageClassNameForVLCluster(t *testing.T) {
	values, err := UnmarshalValues([]byte(`
vlstorage:
  persistentVolume:
    enabled: true
    storageClassName: fast-storage
`), "victoria-logs-cluster")
	require.NoError(t, err)

	cr, err := Convert("test-name", "test-ns", values)
	require.NoError(t, err)
	cluster := cr.(*vmv1.VLCluster)
	require.NotNil(t, cluster.Spec.VLStorage)
	require.NotNil(t, cluster.Spec.VLStorage.Storage)
	require.NotNil(t, cluster.Spec.VLStorage.Storage.VolumeClaimTemplate.Spec.StorageClassName)
	assert.Equal(t, "fast-storage", *cluster.Spec.VLStorage.Storage.VolumeClaimTemplate.Spec.StorageClassName)
}

// TestUnmarshalValuesSupportsFullSecurityContext reproduces #2391: the operator's CRD only
// exposes runAsNonRoot/runAsUser/seccompProfile via spec.securityContext.podSecurityContext,
// so the converter must promote them there instead of silently dropping them.
func TestUnmarshalValuesSupportsFullSecurityContext(t *testing.T) {
	values, err := UnmarshalValues([]byte(`
server:
  securityContext:
    allowPrivilegeEscalation: false
    seccompProfile:
      type: RuntimeDefault
    capabilities:
      drop:
        - ALL
    readOnlyRootFilesystem: true
    runAsNonRoot: true
    runAsUser: 1000
`), "victoria-metrics-single")
	require.NoError(t, err)

	cr, err := Convert("test-name", "test-ns", values)
	require.NoError(t, err)
	single := cr.(*vmv1beta1.VMSingle)
	require.NotNil(t, single.Spec.SecurityContext)

	require.NotNil(t, single.Spec.SecurityContext.ContainerSecurityContext)
	assert.Equal(t, ptr.To(false), single.Spec.SecurityContext.AllowPrivilegeEscalation)
	assert.Equal(t, ptr.To(true), single.Spec.SecurityContext.ReadOnlyRootFilesystem)
	require.NotNil(t, single.Spec.SecurityContext.Capabilities)
	assert.Equal(t, []corev1.Capability{"ALL"}, single.Spec.SecurityContext.Capabilities.Drop)

	require.NotNil(t, single.Spec.SecurityContext.PodSecurityContext)
	assert.Equal(t, ptr.To(true), single.Spec.SecurityContext.RunAsNonRoot)
	assert.Equal(t, ptr.To(int64(1000)), single.Spec.SecurityContext.RunAsUser)
	require.NotNil(t, single.Spec.SecurityContext.SeccompProfile)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, single.Spec.SecurityContext.SeccompProfile.Type)
}

// TestUnmarshalValuesSecurityContextPromotesSELinuxAndWindowsOptions guards against
// mergePodSecurityContext silently dropping these two fields, which the operator's
// SecurityContext does support via its embedded PodSecurityContext.
func TestUnmarshalValuesSecurityContextPromotesSELinuxAndWindowsOptions(t *testing.T) {
	values, err := UnmarshalValues([]byte(`
server:
  securityContext:
    seLinuxOptions:
      level: "s0:c123,c456"
    windowsOptions:
      runAsUserName: "ContainerUser"
`), "victoria-metrics-single")
	require.NoError(t, err)

	cr, err := Convert("test-name", "test-ns", values)
	require.NoError(t, err)
	single := cr.(*vmv1beta1.VMSingle)
	require.NotNil(t, single.Spec.SecurityContext)
	require.NotNil(t, single.Spec.SecurityContext.PodSecurityContext)

	require.NotNil(t, single.Spec.SecurityContext.SELinuxOptions)
	assert.Equal(t, "s0:c123,c456", single.Spec.SecurityContext.SELinuxOptions.Level)
	require.NotNil(t, single.Spec.SecurityContext.WindowsOptions)
	assert.Equal(t, ptr.To("ContainerUser"), single.Spec.SecurityContext.WindowsOptions.RunAsUserName)
}

// TestUnmarshalValuesSecurityContextPodLevelTakesPrecedence ensures an explicit
// podSecurityContext value is not clobbered by the promoted container-level one.
func TestUnmarshalValuesSecurityContextPodLevelTakesPrecedence(t *testing.T) {
	values, err := UnmarshalValues([]byte(`
server:
  podSecurityContext:
    runAsUser: 2000
  securityContext:
    runAsUser: 1000
`), "victoria-metrics-single")
	require.NoError(t, err)

	cr, err := Convert("test-name", "test-ns", values)
	require.NoError(t, err)
	single := cr.(*vmv1beta1.VMSingle)
	require.NotNil(t, single.Spec.SecurityContext)
	require.NotNil(t, single.Spec.SecurityContext.PodSecurityContext)
	assert.Equal(t, ptr.To(int64(2000)), single.Spec.SecurityContext.RunAsUser)
}

// TestUnmarshalValuesSupportsRemoteWriteTLS reproduces #2390: the helm charts expose TLS
// settings for remoteWrite entries as flat tlsCAFile/tlsCertFile/etc. keys, not the operator
// CRD's nested tlsConfig object.
func TestUnmarshalValuesSupportsRemoteWriteTLS(t *testing.T) {
	values, err := UnmarshalValues([]byte(`
image:
  repository: victoriametrics/vmagent
  tag: v1.93.0
remoteWrite:
  - url: https://vminsert:8480/insert/0/prometheus
    tlsCAFile: /etc/vmagent-tls/certs/ca.crt
    tlsCertFile: /etc/vmagent-tls/certs/cert.pem
    tlsKeyFile: /etc/vmagent-tls/certs/key.pem
    tlsServerName: vminsert
    tlsInsecureSkipVerify: true
`), "victoria-metrics-agent")
	require.NoError(t, err)

	cr, err := Convert("test-name", "test-ns", values)
	require.NoError(t, err)
	agent := cr.(*vmv1beta1.VMAgent)
	require.Len(t, agent.Spec.RemoteWrite, 1)
	require.NotNil(t, agent.Spec.RemoteWrite[0].TLSConfig)
	assert.Equal(t, "/etc/vmagent-tls/certs/ca.crt", agent.Spec.RemoteWrite[0].TLSConfig.CAFile)
	assert.Equal(t, "/etc/vmagent-tls/certs/cert.pem", agent.Spec.RemoteWrite[0].TLSConfig.CertFile)
	assert.Equal(t, "/etc/vmagent-tls/certs/key.pem", agent.Spec.RemoteWrite[0].TLSConfig.KeyFile)
	assert.Equal(t, "vminsert", agent.Spec.RemoteWrite[0].TLSConfig.ServerName)
	assert.True(t, agent.Spec.RemoteWrite[0].TLSConfig.InsecureSkipVerify)
}

// TestUnmarshalValuesSupportsRemoteWriteTLSForVLAgent covers the vlagent/vlcollector charts,
// which use the v1 API's own TLSConfig type.
func TestUnmarshalValuesSupportsRemoteWriteTLSForVLAgent(t *testing.T) {
	values, err := UnmarshalValues([]byte(`
image:
  repository: victoriametrics/victoria-logs
  tag: v0.3.2
remoteWrite:
  - url: https://victoria-logs:9428
    tlsCAFile: /etc/tls/ca.crt
    tlsInsecureSkipVerify: true
`), "victoria-logs-agent")
	require.NoError(t, err)

	cr, err := Convert("test-name", "test-ns", values)
	require.NoError(t, err)
	agent := cr.(*vmv1.VLAgent)
	require.Len(t, agent.Spec.RemoteWrite, 1)
	require.NotNil(t, agent.Spec.RemoteWrite[0].TLSConfig)
	assert.Equal(t, "/etc/tls/ca.crt", agent.Spec.RemoteWrite[0].TLSConfig.CAFile)
	assert.True(t, agent.Spec.RemoteWrite[0].TLSConfig.InsecureSkipVerify)
}

// TestUnmarshalValuesSupportsRemoteWriteTLSForVMAlert covers vmalert's singular remoteWrite entry.
func TestUnmarshalValuesSupportsRemoteWriteTLSForVMAlert(t *testing.T) {
	values, err := UnmarshalValues([]byte(`
server:
  datasource:
    url: http://vmselect:8481/select/0/prometheus
  notifier:
    url: http://vmalertmanager:9093
  remoteWrite:
    url: https://vminsert:8480/insert/0/prometheus
    tlsCertFile: /etc/vmalert-tls/certs/cert.pem
    tlsKeyFile: /etc/vmalert-tls/certs/key.pem
`), "victoria-metrics-alert")
	require.NoError(t, err)

	cr, err := Convert("test-name", "test-ns", values)
	require.NoError(t, err)
	alert := cr.(*vmv1beta1.VMAlert)
	require.NotNil(t, alert.Spec.RemoteWrite)
	require.NotNil(t, alert.Spec.RemoteWrite.TLSConfig)
	assert.Equal(t, "/etc/vmalert-tls/certs/cert.pem", alert.Spec.RemoteWrite.TLSConfig.CertFile)
	assert.Equal(t, "/etc/vmalert-tls/certs/key.pem", alert.Spec.RemoteWrite.TLSConfig.KeyFile)
}

func TestConvertVMSingle(t *testing.T) {
	f := func(values *VMSingleHelmValues, expected func() *vmv1beta1.VMSingle) {
		t.Helper()
		actual, err := Convert("test-name", "test-ns", values)
		assert.NoError(t, err)
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
					Enabled:          true,
					StorageClassName: "fast-storage",
					Size:             "10Gi",
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

	// extraVolumes/extraVolumeMounts must be propagated. See #2424.
	f(
		&VMSingleHelmValues{
			Server: ServerValues{
				ExtraVolumes: []corev1.Volume{
					{Name: "extra", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				},
				ExtraVolumeMounts: []corev1.VolumeMount{
					{Name: "extra", MountPath: "/extra"},
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
			cr.Spec.Volumes = []corev1.Volume{
				{Name: "extra", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			}
			cr.Spec.VolumeMounts = []corev1.VolumeMount{
				{Name: "extra", MountPath: "/extra"},
			}
			return cr
		},
	)
}

func TestConvertVMCluster(t *testing.T) {
	f := func(values *VMClusterHelmValues, expected func() *vmv1beta1.VMCluster) {
		t.Helper()
		actual, err := Convert("test-cluster", "test-ns", values)
		assert.NoError(t, err)
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
		actual, err := Convert("test-agent", "test-ns", values)
		assert.NoError(t, err)
		assert.Equal(t, expected(), actual)
	}

	f(
		&VMAgentHelmValues{
			ReplicaCount: ptr.To(int32(2)),
			Image: ImageValues{
				Repository: "victoriametrics/vmagent",
				Tag:        "v1.93.0",
			},
			RemoteWrite: []VMAgentRemoteWriteValues{
				{
					VMAgentRemoteWriteSpec: vmv1beta1.VMAgentRemoteWriteSpec{
						URL: "http://vminsert:8480/insert/0/prometheus",
					},
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

	// extraVolumes/extraVolumeMounts must be propagated. See #2424.
	f(
		&VMAgentHelmValues{
			ExtraVolumes: []corev1.Volume{
				{Name: "extra", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
			ExtraVolumeMounts: []corev1.VolumeMount{
				{Name: "extra", MountPath: "/extra"},
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
			cr.Spec.Volumes = []corev1.Volume{
				{Name: "extra", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			}
			cr.Spec.VolumeMounts = []corev1.VolumeMount{
				{Name: "extra", MountPath: "/extra"},
			}
			return cr
		},
	)
}

func TestConvertVMAlert(t *testing.T) {
	f := func(values *VMAlertHelmValues, expected func() *vmv1beta1.VMAlert) {
		t.Helper()
		actual, err := Convert("test-alert", "test-ns", values)
		assert.NoError(t, err)
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

	// extraVolumes/extraVolumeMounts must be propagated. See #2424.
	f(
		&VMAlertHelmValues{
			Server: VMAlertServerValues{
				ExtraVolumes: []corev1.Volume{
					{Name: "extra", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				},
				ExtraVolumeMounts: []corev1.VolumeMount{
					{Name: "extra", MountPath: "/extra"},
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
			cr.Spec.Volumes = []corev1.Volume{
				{Name: "extra", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			}
			cr.Spec.VolumeMounts = []corev1.VolumeMount{
				{Name: "extra", MountPath: "/extra"},
			}
			return cr
		},
	)
}

func TestConvertVMAnomaly(t *testing.T) {
	f := func(values *VMAnomalyHelmValues, expected func() *vmv1.VMAnomaly) {
		t.Helper()
		actual, err := Convert("test-anomaly", "test-ns", values)
		assert.NoError(t, err)
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

	// extraVolumes/extraVolumeMounts must be propagated. See #2424.
	f(
		&VMAnomalyHelmValues{
			Reader: &VMAnomalyReaderValues{
				DatasourceURL:  "http://vmselect:8481/select/0/prometheus",
				SamplingPeriod: "1m",
			},
			Writer: &VMAnomalyWriterValues{
				DatasourceURL: "http://vminsert:8480/insert/0/prometheus",
			},
			ExtraVolumes: []corev1.Volume{
				{Name: "extra", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
			ExtraVolumeMounts: []corev1.VolumeMount{
				{Name: "extra", MountPath: "/extra"},
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
			cr.Spec.Reader = &vmv1.VMAnomalyReadersSpec{
				DatasourceURL:  "http://vmselect:8481/select/0/prometheus",
				SamplingPeriod: "1m",
			}
			cr.Spec.Writer = &vmv1.VMAnomalyWritersSpec{
				DatasourceURL: "http://vminsert:8480/insert/0/prometheus",
			}
			cr.Spec.Volumes = []corev1.Volume{
				{Name: "extra", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			}
			cr.Spec.VolumeMounts = []corev1.VolumeMount{
				{Name: "extra", MountPath: "/extra"},
			}
			return cr
		},
	)
}
func TestConvertVLAgent(t *testing.T) {
	f := func(values *VLAgentHelmValues, expected func() *vmv1.VLAgent) {
		t.Helper()
		actual, err := Convert("test-name", "test-ns", values)
		assert.NoError(t, err)
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
			RemoteWrite: []VLAgentRemoteWriteValues{
				{VLAgentRemoteWriteSpec: vmv1.VLAgentRemoteWriteSpec{URL: "http://victoria-logs:9428"}},
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
		actual, err := Convert("test-name", "test-ns", values)
		assert.NoError(t, err)
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

	// extraVolumes/extraVolumeMounts must be propagated for every cluster role. See #2424.
	f(
		&VLClusterHelmValues{
			VLSelect: ServerValues{
				Enabled: ptr.To(true),
				ExtraVolumes: []corev1.Volume{
					{Name: "extra", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				},
				ExtraVolumeMounts: []corev1.VolumeMount{
					{Name: "extra", MountPath: "/extra"},
				},
			},
			VLInsert: ServerValues{
				Enabled: ptr.To(true),
				ExtraVolumes: []corev1.Volume{
					{Name: "extra", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				},
				ExtraVolumeMounts: []corev1.VolumeMount{
					{Name: "extra", MountPath: "/extra"},
				},
			},
			VLStorage: ServerValues{
				Enabled: ptr.To(true),
				ExtraVolumes: []corev1.Volume{
					{Name: "extra", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				},
				ExtraVolumeMounts: []corev1.VolumeMount{
					{Name: "extra", MountPath: "/extra"},
				},
			},
		},
		func() *vmv1.VLCluster {
			extraVolumes := []corev1.Volume{
				{Name: "extra", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			}
			extraVolumeMounts := []corev1.VolumeMount{
				{Name: "extra", MountPath: "/extra"},
			}
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
							Volumes:      extraVolumes,
							VolumeMounts: extraVolumeMounts,
						},
					},
					VLInsert: &vmv1.VLInsert{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Volumes:      extraVolumes,
							VolumeMounts: extraVolumeMounts,
						},
					},
					VLStorage: &vmv1.VLStorage{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Volumes:      extraVolumes,
							VolumeMounts: extraVolumeMounts,
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
		actual, err := Convert("test-name", "test-ns", values)
		assert.NoError(t, err)
		assert.Equal(t, expected(), actual)
	}

	// Basic conversion
	f(
		&VLCollectorHelmValues{
			Image: ImageValues{
				Repository: "victoriametrics/vlagent",
				Tag:        "v0.3.2",
			},
			RemoteWrite: []VLAgentRemoteWriteValues{
				{VLAgentRemoteWriteSpec: vmv1.VLAgentRemoteWriteSpec{URL: "http://victoria-logs:9428"}},
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
		actual, err := Convert("test-name", "test-ns", values)
		assert.NoError(t, err)
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
		actual, err := Convert("test-name", "test-ns", values)
		assert.NoError(t, err)
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
func TestConvertVTCluster(t *testing.T) {
	f := func(values *VTClusterHelmValues, expected func() *vmv1.VTCluster) {
		t.Helper()
		actual, err := Convert("test-name", "test-ns", values)
		assert.NoError(t, err)
		assert.Equal(t, expected(), actual)
	}

	// Basic conversion
	f(
		&VTClusterHelmValues{
			VTSelect: ServerValues{
				Enabled: ptr.To(true),
				Image: ImageValues{
					Repository: "victoriametrics/victoria-traces",
					Tag:        "v0.3.2",
				},
				ReplicaCount: ptr.To(int32(2)),
			},
			VTInsert: ServerValues{
				Enabled: ptr.To(true),
				Image: ImageValues{
					Repository: "victoriametrics/victoria-traces",
					Tag:        "v0.3.2",
				},
				ReplicaCount: ptr.To(int32(2)),
			},
			VTStorage: ServerValues{
				Enabled: ptr.To(true),
				Image: ImageValues{
					Repository: "victoriametrics/victoria-traces",
					Tag:        "v0.3.2",
				},
				ReplicaCount: ptr.To(int32(2)),
			},
		},
		func() *vmv1.VTCluster {
			return &vmv1.VTCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operator.victoriametrics.com/v1",
					Kind:       "VTCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-ns",
				},
				Spec: vmv1.VTClusterSpec{
					Select: &vmv1.VTSelect{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Repository: "victoriametrics/victoria-traces",
								Tag:        "v0.3.2",
							},
							ReplicaCount: ptr.To(int32(2)),
						},
					},
					Insert: &vmv1.VTInsert{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Repository: "victoriametrics/victoria-traces",
								Tag:        "v0.3.2",
							},
							ReplicaCount: ptr.To(int32(2)),
						},
					},
					Storage: &vmv1.VTStorage{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Repository: "victoriametrics/victoria-traces",
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
func TestConvertVMAuth(t *testing.T) {
	f := func(values *VMAuthHelmValues, expected func() *vmv1beta1.VMAuth) {
		t.Helper()
		actual, err := Convert("test-name", "test-ns", values)
		assert.NoError(t, err)
		assert.Equal(t, expected(), actual)
	}

	// Basic conversion
	f(
		&VMAuthHelmValues{
			ServerValues: ServerValues{
				Image: ImageValues{
					Repository: "victoriametrics/vmauth",
					Tag:        "v1.100.0",
				},
				ReplicaCount: ptr.To(int32(1)),
			},
		},
		func() *vmv1beta1.VMAuth {
			return &vmv1beta1.VMAuth{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "operator.victoriametrics.com/v1beta1",
					Kind:       "VMAuth",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-ns",
				},
				Spec: vmv1beta1.VMAuthSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						Image: vmv1beta1.Image{
							Repository: "victoriametrics/vmauth",
							Tag:        "v1.100.0",
						},
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			}
		},
	)
}

// TestUnmarshalValuesSupportsVMAuthConfig reproduces #2397, end-to-end from raw YAML: the
// chart's `config.users`/`config.unauthorized_user` must become standalone VMUser CRs and
// VMAuth.spec.unauthorizedUserAccessSpec respectively.
func TestUnmarshalValuesSupportsVMAuthConfig(t *testing.T) {
	values, err := UnmarshalValues([]byte(`
config:
  users:
    - username: "cluster-select-account-123"
      password: "secret"
      url_prefix: "http://vmselect:8481/select/123/prometheus"
    - username: "LoadBalanced_User"
      bearer_token: "mytoken"
      url_map:
        - src_paths: ["/api/v1/query.*"]
          url_prefix:
            - "http://vmselect-1:8481/"
            - "http://vmselect-2:8481/"
  unauthorized_user:
    url_prefix: "http://vminsert:8480/insert/0/prometheus"
`), "victoria-metrics-auth")
	require.NoError(t, err)

	authValues, ok := values.(*VMAuthHelmValues)
	require.True(t, ok)

	cr, err := Convert("test-name", "test-ns", authValues)
	require.NoError(t, err)
	auth := cr.(*vmv1beta1.VMAuth)
	require.NotNil(t, auth.Spec.UnauthorizedUserAccessSpec)
	assert.Equal(t, vmv1beta1.StringOrArray{"http://vminsert:8480/insert/0/prometheus"}, auth.Spec.UnauthorizedUserAccessSpec.URLPrefix)
	require.NotNil(t, auth.Spec.UserSelector)
	assert.Equal(t, vmAuthUserSelectorLabels("test-name"), auth.Spec.UserSelector.MatchLabels)
	assert.Nil(t, auth.Spec.UserNamespaceSelector)

	users, err := ConvertVMAuthUsers("test-name", "test-ns", authValues)
	require.NoError(t, err)
	require.Len(t, users, 2)

	first := users[0]
	assert.Equal(t, "cluster-select-account-123", first.Name)
	assert.Equal(t, "test-ns", first.Namespace)
	assert.Equal(t, vmAuthUserSelectorLabels("test-name"), first.Labels)
	require.NotNil(t, first.Spec.Username)
	assert.Equal(t, "cluster-select-account-123", *first.Spec.Username)
	require.NotNil(t, first.Spec.Password)
	assert.Equal(t, "secret", *first.Spec.Password)
	require.Len(t, first.Spec.TargetRefs, 1)
	require.NotNil(t, first.Spec.TargetRefs[0].Static)
	assert.Equal(t, "http://vmselect:8481/select/123/prometheus", first.Spec.TargetRefs[0].Static.URL)

	second := users[1]
	assert.Equal(t, "loadbalanced-user", second.Name)
	require.NotNil(t, second.Spec.BearerToken)
	assert.Equal(t, "mytoken", *second.Spec.BearerToken)
	require.Len(t, second.Spec.TargetRefs, 1)
	assert.Equal(t, []string{"/api/v1/query.*"}, second.Spec.TargetRefs[0].Paths)
	require.NotNil(t, second.Spec.TargetRefs[0].Static)
	assert.Equal(t, []string{"http://vmselect-1:8481/", "http://vmselect-2:8481/"}, second.Spec.TargetRefs[0].Static.URLs)
}

func TestConvertVMAuthUsers_Errors(t *testing.T) {
	_, err := ConvertVMAuthUsers("test-name", "test-ns", &VMAuthHelmValues{
		Config: &VMAuthConfigValues{Users: []VMAuthConfigUser{{Password: "x"}}},
	})
	assert.ErrorContains(t, err, "username is required")

	_, err = ConvertVMAuthUsers("test-name", "test-ns", &VMAuthHelmValues{
		Config: &VMAuthConfigValues{Users: []VMAuthConfigUser{{Username: "foo"}}},
	})
	assert.ErrorContains(t, err, "url_prefix is required")
}

func TestConvertVMAuthUsers_NoConfig(t *testing.T) {
	users, err := ConvertVMAuthUsers("test-name", "test-ns", &VMAuthHelmValues{})
	require.NoError(t, err)
	assert.Empty(t, users)
}

func TestMergeValues(t *testing.T) {
	f := func(base, override string, expected map[string]any) {
		t.Helper()
		got, err := MergeValues([]byte(base), []byte(override))
		require.NoError(t, err)
		assert.Equal(t, expected, unmarshalYAML(t, got))
	}

	// override
	f(
		"retentionPeriod: 1\nreplicaCount: 1\n",
		"retentionPeriod: 30d\n",
		map[string]any{"replicaCount": float64(1), "retentionPeriod": "30d"},
	)

	// deep merge
	f(
		"image:\n  repository: victoriametrics/victoria-metrics\n  tag: v1.0.0\n",
		"image:\n  tag: v2.0.0\n",
		map[string]any{"image": map[string]any{
			"repository": "victoriametrics/victoria-metrics",
			"tag":        "v2.0.0",
		}},
	)

	// add new key
	f(
		"replicaCount: 1\n",
		"replicaCount: 1\nextraArgs:\n  loggerFormat: json\n",
		map[string]any{
			"replicaCount": float64(1),
			"extraArgs":    map[string]any{"loggerFormat": "json"},
		},
	)

	// empty override
	f(
		"retentionPeriod: 14d\n",
		"",
		map[string]any{"retentionPeriod": "14d"},
	)

	// empty base
	f(
		"",
		"retentionPeriod: 14d\n",
		map[string]any{"retentionPeriod": "14d"},
	)

	// a chart-default "headers: {}" map is normalized to []string "key:value" (#2398),
	// regardless of nesting depth or whether it sits inside a list.
	f(
		"datasource:\n  url: \"\"\n  headers: {}\n",
		"datasource:\n  url: http://vmauth:8427\n",
		map[string]any{"datasource": map[string]any{
			"url":     "http://vmauth:8427",
			"headers": []any{},
		}},
	)
	f(
		"notifier:\n  headers: {}\nremoteWrite:\n  headers: {}\n",
		"notifier:\n  headers:\n    X-Scope-OrgID: \"1\"\n    Authorization: \"Bearer xxx\"\n",
		map[string]any{
			"notifier":    map[string]any{"headers": []any{"Authorization:Bearer xxx", "X-Scope-OrgID:1"}},
			"remoteWrite": map[string]any{"headers": []any{}},
		},
	)
	f(
		"remoteWrite:\n- url: a\n  headers: {}\n- url: b\n  headers:\n    X-Foo: bar\n",
		"",
		map[string]any{"remoteWrite": []any{
			map[string]any{"url": "a", "headers": []any{}},
			map[string]any{"url": "b", "headers": []any{"X-Foo:bar"}},
		}},
	)
}

// TestMergeValues_VMAlertDatasourceHeaders reproduces #2398: vmalert's default values.yaml
// ships `server.datasource.headers: {}` (a map), which used to fail to unmarshal into the
// operator's HTTPAuth.Headers []string field once merged with a user's override.
func TestMergeValues_VMAlertDatasourceHeaders(t *testing.T) {
	chartDefaults := "server:\n  datasource:\n    url: \"\"\n    headers: {}\n"
	userOverride := "server:\n  datasource:\n    url: http://vmauth:8427\n"

	merged, err := MergeValues([]byte(chartDefaults), []byte(userOverride))
	require.NoError(t, err)

	values, err := UnmarshalValues(merged, "victoria-metrics-alert")
	require.NoError(t, err)

	vmAlertValues, ok := values.(*VMAlertHelmValues)
	require.True(t, ok)
	assert.Equal(t, "http://vmauth:8427", vmAlertValues.Server.Datasource.URL)
	assert.Empty(t, vmAlertValues.Server.Datasource.Headers)
}

func TestFetchLatestChartVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`
apiVersion: v1
entries:
  victoria-metrics-single:
  - version: "0.14.0"
  - version: "0.13.0"
  victoria-metrics-agent:
  - version: "0.12.0"
`))
	}))
	defer srv.Close()

	orig := helmChartsIndexURL
	helmChartsIndexURL = srv.URL
	defer func() { helmChartsIndexURL = orig }()

	version, err := fetchLatestChartVersion("victoria-metrics-single")
	require.NoError(t, err)
	assert.Equal(t, "0.14.0", version)

	version, err = fetchLatestChartVersion("victoria-metrics-agent")
	require.NoError(t, err)
	assert.Equal(t, "0.12.0", version)

	_, err = fetchLatestChartVersion("unknown-chart")
	assert.ErrorContains(t, err, `"unknown-chart" not found`)
}

func TestFetchLatestChartVersionHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	orig := helmChartsIndexURL
	helmChartsIndexURL = srv.URL
	defer func() { helmChartsIndexURL = orig }()

	_, err := fetchLatestChartVersion("victoria-metrics-single")
	assert.ErrorContains(t, err, "HTTP 500")
}

func TestFetchChartDefaults(t *testing.T) {
	const fakeValues = "retentionPeriod: 1\nreplicaCount: 1\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`
apiVersion: v1
entries:
  victoria-metrics-single:
  - version: "0.14.0"
`))
		case "/victoria-metrics-single-0.14.0/charts/victoria-metrics-single/values.yaml":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fakeValues))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	origIndex := helmChartsIndexURL
	origBase := helmChartsRawBaseURL
	helmChartsIndexURL = srv.URL
	helmChartsRawBaseURL = srv.URL
	defer func() {
		helmChartsIndexURL = origIndex
		helmChartsRawBaseURL = origBase
	}()

	got, err := FetchChartDefaults("victoria-metrics-single")
	require.NoError(t, err)
	assert.Equal(t, fakeValues, string(got))
}

func TestFetchChartDefaultsValuesNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`
apiVersion: v1
entries:
  victoria-metrics-single:
  - version: "0.14.0"
`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	origIndex := helmChartsIndexURL
	origBase := helmChartsRawBaseURL
	helmChartsIndexURL = srv.URL
	helmChartsRawBaseURL = srv.URL
	defer func() {
		helmChartsIndexURL = origIndex
		helmChartsRawBaseURL = origBase
	}()

	_, err := FetchChartDefaults("victoria-metrics-single")
	assert.ErrorContains(t, err, "HTTP 404")
}
