package build

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestLicenseAddArgsTo(t *testing.T) {
	type opts struct {
		args           []string
		secretMountDir string
		license        vmv1beta1.License
		want           []string
	}

	f := func(o opts) {
		t.Helper()
		got := LicenseArgsTo(o.args, &o.license, o.secretMountDir)
		slices.Sort(got)
		slices.Sort(o.want)
		assert.Equal(t, o.want, got)
	}

	// license key provided
	f(opts{
		license: vmv1beta1.License{
			Key: ptr.To("test-key"),
		},
		args:           []string{},
		secretMountDir: "/etc/secrets",
		want:           []string{"-license=test-key"},
	})

	// license key provided with force offline
	f(opts{
		license: vmv1beta1.License{
			Key:          ptr.To("test-key"),
			ForceOffline: ptr.To(true),
		},
		args:           []string{},
		secretMountDir: "/etc/secrets",
		want:           []string{"-license=test-key", "-license.forceOffline=true"},
	})

	// license key provided with reload interval
	f(opts{
		license: vmv1beta1.License{
			KeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "license-secret"},
				Key:                  "license-key",
			},
			ReloadInterval: ptr.To("30s"),
		},
		args:           []string{},
		secretMountDir: "/etc/secrets",
		want:           []string{"-licenseFile=/etc/secrets/license-secret/license-key", "-licenseFile.reloadInterval=30s"},
	})

	// license key provided via secret with force offline
	f(opts{
		license: vmv1beta1.License{
			KeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "license-secret"},
				Key:                  "license-key",
			},
			ForceOffline: ptr.To(true),
		},
		args:           []string{},
		secretMountDir: "/etc/secrets",
		want:           []string{"-licenseFile=/etc/secrets/license-secret/license-key", "-license.forceOffline=true"},
	})
}

func TestGzipGunzipConfig(t *testing.T) {
	f := func(data any) {
		var b bytes.Buffer
		var err error
		t.Helper()
		switch d := data.(type) {
		case string:
			_, err = b.Write([]byte(d))
		default:
			err = binary.Write(&b, binary.BigEndian, data)
		}
		assert.NoError(t, err, "failed to write data to buffer")
		compressed, err := GzipConfig(b.Bytes())
		assert.NoError(t, err, "failed to compress data")
		uncompressed, err := GunzipConfig(compressed)
		assert.NoError(t, err, "failed to uncompress data")
		assert.True(t, bytes.Equal(b.Bytes(), uncompressed))
	}

	// test compression-decompression
	f("test data")

	// empty data
	f("")

	// binary data
	f([]int32{1, 2, 3, 4})
}

func TestMergeMapsRecursive(t *testing.T) {
	base := map[string]any{
		"a": map[string]any{
			"b": "keep",
			"c": "override-me",
		},
		"d": "root-keep",
	}
	override := map[string]any{
		"a": map[string]any{
			"c": "new",
			"z": "added",
		},
		"e": "root-added",
	}
	// initial merge
	mergeMapsRecursive(base, override)
	assert.Equal(t, "keep", base["a"].(map[string]any)["b"])
	assert.Equal(t, "new", base["a"].(map[string]any)["c"])
	assert.Equal(t, "added", base["a"].(map[string]any)["z"])
	assert.Equal(t, "root-keep", base["d"])
	assert.Equal(t, "root-added", base["e"])
}

func TestDeepMerge(t *testing.T) {
	type opts struct {
		override *vmv1beta1.VMClusterSpec
		validate func(base, merged *vmv1beta1.VMClusterSpec, err error)
	}
	f := func(oss ...opts) {
		t.Helper()
		base := &vmv1beta1.VMClusterSpec{
			ClusterVersion:     "v1.0.0",
			ServiceAccountName: "base",
			RetentionPeriod:    "30d",
			VMSelect: &vmv1beta1.VMSelect{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
					ExtraArgs:    map[string]string{"keep": "x", "override": "old"},
				},
			},
			VMInsert: &vmv1beta1.VMInsert{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
					ExtraArgs:    map[string]string{"insert-arg": "1"},
				},
			},
		}
		merged := base.DeepCopy()
		for _, o := range oss {
			o.validate(base, merged, MergeDeep(merged, o.override))
		}
	}

	// with extra args override
	f(opts{
		override: &vmv1beta1.VMClusterSpec{
			ClusterVersion: "v1.2.3",
			VMSelect: &vmv1beta1.VMSelect{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(3)),
					ExtraArgs:    map[string]string{"override": "new", "add": "y"},
				},
			},
			ServiceAccountName: "zone-sa",
		},
		validate: func(base, merged *vmv1beta1.VMClusterSpec, err error) {
			assert.NoError(t, err)

			// top-level
			assert.Equal(t, "v1.2.3", merged.ClusterVersion)
			assert.Equal(t, "zone-sa", merged.ServiceAccountName)

			// nested merge
			if !assert.NotNil(t, merged.VMSelect) || !assert.NotNil(t, merged.VMSelect.ReplicaCount) {
				return
			}
			assert.Equal(t, int32(3), *merged.VMSelect.ReplicaCount)
			assert.Equal(t, "x", merged.VMSelect.ExtraArgs["keep"])
			assert.Equal(t, "new", merged.VMSelect.ExtraArgs["override"])
			assert.Equal(t, "y", merged.VMSelect.ExtraArgs["add"])

			// untouched subtree
			if !assert.NotNil(t, merged.VMInsert) || !assert.NotNil(t, merged.VMInsert.ReplicaCount) {
				return
			}
			assert.Equal(t, int32(1), *merged.VMInsert.ReplicaCount)
			assert.Equal(t, "1", merged.VMInsert.ExtraArgs["insert-arg"])
		},
	})

	// with nil override spec
	f(opts{
		validate: func(base, merged *vmv1beta1.VMClusterSpec, err error) {
			assert.NoError(t, err)
			assert.Equal(t, base, merged)
		},
	})

	// with empty override spec
	f(opts{
		override: &vmv1beta1.VMClusterSpec{},
		validate: func(base, merged *vmv1beta1.VMClusterSpec, err error) {
			assert.NoError(t, err)
			assert.Equal(t, base, merged)
		},
	})

	// with override spec that modifies top-level fields
	f(opts{
		override: &vmv1beta1.VMClusterSpec{
			ClusterVersion:     "v2.0.0",
			ServiceAccountName: "global-sa",
		},
		validate: func(_, merged *vmv1beta1.VMClusterSpec, err error) {
			assert.Equal(t, "v2.0.0", merged.ClusterVersion)
			assert.Equal(t, "global-sa", merged.ServiceAccountName)
			assert.Equal(t, "30d", merged.RetentionPeriod)
		},
	})

	// multiple overrides
	f(opts{
		override: &vmv1beta1.VMClusterSpec{
			ClusterVersion:     "v2.0.0",
			ServiceAccountName: "global-sa",
		},
		validate: func(_, merged *vmv1beta1.VMClusterSpec, err error) {
			assert.NoError(t, err)
			assert.Equal(t, "v2.0.0", merged.ClusterVersion)
			assert.Equal(t, "global-sa", merged.ServiceAccountName)
			assert.Equal(t, "30d", merged.RetentionPeriod)
		},
	}, opts{
		override: &vmv1beta1.VMClusterSpec{
			RetentionPeriod: "10d",
			ClusterVersion:  "v3.0.0",
		},
		validate: func(_, merged *vmv1beta1.VMClusterSpec, err error) {
			assert.NoError(t, err)
			assert.Equal(t, "v3.0.0", merged.ClusterVersion)        // Cluster-specific override should take precedence
			assert.Equal(t, "global-sa", merged.ServiceAccountName) // From global override, unchanged by cluster override
			assert.Equal(t, "10d", merged.RetentionPeriod)          // From cluster override
		},
	})
}
