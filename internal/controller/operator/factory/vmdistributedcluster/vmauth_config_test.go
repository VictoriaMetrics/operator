package vmdistributedcluster

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func Test_buildVMAuthVMSelectURLMaps(t *testing.T) {
	t.Run("single default vmcluster", func(t *testing.T) {
		vmClusters := []*vmv1beta1.VMCluster{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmc-test",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMClusterSpec{},
			},
		}
		want := URLMap{
			SrcPaths:           []string{"/.*"},
			URLPrefix:          []string{"http://srv+vmselect-vmc-test.default.svc:8481"},
			DiscoverBackendIPs: true,
		}
		got := buildVMAuthVMSelectURLMaps(vmClusters)
		assert.Equal(t, want, got)
	})

	t.Run("multiple vmclusters", func(t *testing.T) {
		vmClusters := []*vmv1beta1.VMCluster{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "vmc-1", Namespace: "ns1"},
				Spec:       vmv1beta1.VMClusterSpec{},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "vmc-2", Namespace: "ns2"},
				Spec: vmv1beta1.VMClusterSpec{
					VMSelect: &vmv1beta1.VMSelect{},
				},
			},
		}
		want := URLMap{
			SrcPaths: []string{"/.*"},
			URLPrefix: []string{
				"http://srv+vmselect-vmc-1.ns1.svc:8481",
				"http://srv+vmselect-vmc-2.ns2.svc:8481",
			},
			DiscoverBackendIPs: true,
		}
		got := buildVMAuthVMSelectURLMaps(vmClusters)
		assert.Equal(t, want, got)
	})

	t.Run("no vmclusters", func(t *testing.T) {
		vmClusters := []*vmv1beta1.VMCluster{}
		want := URLMap{
			SrcPaths:           []string{"/.*"},
			URLPrefix:          []string{},
			DiscoverBackendIPs: true,
		}
		got := buildVMAuthVMSelectURLMaps(vmClusters)
		assert.Equal(t, want, got)
	})
}

func Test_buildVMAuthLBSecret(t *testing.T) {
	cr := &vmv1alpha1.VMDistributedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmdist",
			Namespace: "prod",
		},
		Spec: vmv1alpha1.VMDistributedClusterSpec{
			VMAuth: vmv1alpha1.VMAuthNameAndSpec{
				Name: "vmauth",
				Spec: &vmv1beta1.VMAuthLoadBalancerSpec{},
			},
		},
	}
	vmClusters := []*vmv1beta1.VMCluster{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmc-test",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMClusterSpec{},
		},
	}

	secret, err := buildVMAuthLBSecret(cr, vmClusters)
	assert.NoError(t, err)
	assert.NotNil(t, secret)

	assert.Equal(t, "vmdclusterlb-vmdist", secret.Name)
	assert.Equal(t, "prod", secret.Namespace)

	configGZData, ok := secret.Data[configGZName]
	assert.True(t, ok)
	assert.NotEmpty(t, configGZData)

	gr, err := gzip.NewReader(bytes.NewReader(configGZData))
	assert.NoError(t, err)
	defer gr.Close()
	decompressed, err := io.ReadAll(gr)
	assert.NoError(t, err)

	expectedYAML := `unauthorized_user:
  url_map:
  - src_paths:
    - /.*
    url_prefix:
    - http://srv+vmselect-vmc-test.default.svc:8481
    discover_backend_ips: true
`
	assert.Equal(t, []byte(expectedYAML), decompressed)

	t.Run("multiple vmclusters", func(t *testing.T) {
		vmClusters := []*vmv1beta1.VMCluster{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmc-test-1",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMClusterSpec{},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmc-test-2",
					Namespace: "monitoring",
				},
				Spec: vmv1beta1.VMClusterSpec{},
			},
		}
		secret, err := buildVMAuthLBSecret(cr, vmClusters)
		assert.NoError(t, err)
		assert.NotNil(t, secret)

		configGZData, ok := secret.Data[configGZName]
		assert.True(t, ok)

		gr, err := gzip.NewReader(bytes.NewReader(configGZData))
		assert.NoError(t, err)
		defer gr.Close()
		decompressed, err := io.ReadAll(gr)
		assert.NoError(t, err)

		expectedYAML := `unauthorized_user:
  url_map:
  - src_paths:
    - /.*
    url_prefix:
    - http://srv+vmselect-vmc-test-1.default.svc:8481
    - http://srv+vmselect-vmc-test-2.monitoring.svc:8481
    discover_backend_ips: true
`
		assert.Equal(t, []byte(expectedYAML), decompressed)
	})

	// Test with no vmclusters
	t.Run("no vmclusters", func(t *testing.T) {
		secret, err := buildVMAuthLBSecret(cr, []*vmv1beta1.VMCluster{})
		assert.NoError(t, err)
		assert.NotNil(t, secret)

		configGZData, ok := secret.Data[configGZName]
		assert.True(t, ok)

		gr, err := gzip.NewReader(bytes.NewReader(configGZData))
		assert.NoError(t, err)
		defer gr.Close()
		decompressed, err := io.ReadAll(gr)
		assert.NoError(t, err)

		expectedYAML := `unauthorized_user:
  url_map:
  - src_paths:
    - /.*
    url_prefix: []
    discover_backend_ips: true
`
		assert.Equal(t, []byte(expectedYAML), decompressed)
	})
}
