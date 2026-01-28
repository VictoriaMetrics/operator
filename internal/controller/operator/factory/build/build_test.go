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
		if err != nil {
			t.Errorf("failed to write data to buffer: %v", err)
		}
		compressed, err := GzipConfig(b.Bytes())
		if err != nil {
			t.Errorf("failed to compress data: %v", err)
		}
		uncompressed, err := GunzipConfig(compressed)
		if err != nil {
			t.Errorf("failed to uncompress data: %v", err)
		}
		assert.True(t, bytes.Equal(b.Bytes(), uncompressed))
	}

	// test compression-decompression
	f("test data")

	// empty data
	f("")

	// binary data
	f([]int32{1, 2, 3, 4})
}
