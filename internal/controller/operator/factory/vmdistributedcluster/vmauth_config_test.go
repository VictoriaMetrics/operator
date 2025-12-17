package vmdistributedcluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func Test_buildVMAuthVMSelectURLMaps(t *testing.T) {
	type args struct {
		vmClusters []*vmv1beta1.VMCluster
	}
	tests := []struct {
		name string
		args args
		want URLMap
	}{
		{
			name: "single default vmcluster",
			args: args{
				vmClusters: []*vmv1beta1.VMCluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "vmc-test",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMClusterSpec{},
					},
				},
			},
			want: URLMap{
				SrcPaths:           []string{"/.*"},
				URLPrefix:          []string{"http://srv+vmselect-vmc-test.default.svc:8481"},
				DiscoverBackendIPs: true,
			},
		},

		{
			name: "multiple vmclusters",
			args: args{
				vmClusters: []*vmv1beta1.VMCluster{
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
				},
			},
			want: URLMap{
				SrcPaths: []string{"/.*"},
				URLPrefix: []string{
					"http://srv+vmselect-vmc-1.ns1.svc:8481",
					"http://srv+vmselect-vmc-2.ns2.svc:8481",
				},
				DiscoverBackendIPs: true,
			},
		},
		{
			name: "no vmclusters",
			args: args{
				vmClusters: []*vmv1beta1.VMCluster{},
			},
			want: URLMap{
				SrcPaths:           []string{"/.*"},
				URLPrefix:          []string{},
				DiscoverBackendIPs: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildVMAuthVMSelectURLMaps(tt.args.vmClusters)
			assert.Equal(t, tt.want, got)
		})
	}
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

	configData, ok := secret.Data["config.yaml"]
	assert.True(t, ok)
	assert.NotEmpty(t, configData)

	expectedYAML := `unauthorized_user:
  url_map:
  - src_paths:
    - /.*
    url_prefix:
    - http://srv+vmselect-vmc-test.default.svc:8481
    discover_backend_ips: true
`
	assert.Equal(t, []byte(expectedYAML), configData)

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

		configData, ok := secret.Data["config.yaml"]
		assert.True(t, ok)
		expectedYAML := `unauthorized_user:
  url_map:
  - src_paths:
    - /.*
    url_prefix:
    - http://srv+vmselect-vmc-test-1.default.svc:8481
    - http://srv+vmselect-vmc-test-2.monitoring.svc:8481
    discover_backend_ips: true
`
		assert.Equal(t, []byte(expectedYAML), configData)
	})

	// Test with no vmclusters
	t.Run("no vmclusters", func(t *testing.T) {
		secret, err := buildVMAuthLBSecret(cr, []*vmv1beta1.VMCluster{})
		assert.NoError(t, err)
		assert.NotNil(t, secret)

		configData, ok := secret.Data["config.yaml"]
		assert.True(t, ok)
		expectedYAML := `unauthorized_user:
  url_map:
  - src_paths:
    - /.*
    url_prefix: []
    discover_backend_ips: true
`
		assert.Equal(t, []byte(expectedYAML), configData)
	})
}
