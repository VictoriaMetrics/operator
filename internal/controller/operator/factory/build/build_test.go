package build

import (
	"reflect"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestLicenseAddArgsTo(t *testing.T) {
	type opts struct {
		license        *vmv1beta1.License
		args           []string
		secretMountDir string
		want           []string
	}
	f := func(opts opts) {
		got := LicenseArgsTo(opts.args, opts.license, opts.secretMountDir)
		slices.Sort(got)
		slices.Sort(opts.want)
		if !reflect.DeepEqual(got, opts.want) {
			t.Errorf("vmv1beta1.License.MaybeAddToArgs() = %v, want %v", got, opts.want)
		}
	}

	// license key provided
	o := opts{
		license: &vmv1beta1.License{
			Key: ptr.To("test-key"),
		},
		secretMountDir: "/etc/secrets",
		want:           []string{"-license=test-key"},
	}
	f(o)

	// license key provided with force offline
	o = opts{
		license: &vmv1beta1.License{
			Key:          ptr.To("test-key"),
			ForceOffline: ptr.To(true),
		},
		secretMountDir: "/etc/secrets",
		want:           []string{"-license=test-key", "-license.forceOffline=true"},
	}
	f(o)

	// license key provided with reload interval
	o = opts{
		license: &vmv1beta1.License{
			KeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "license-secret"},
				Key:                  "license-key",
			},
			ReloadInterval: ptr.To("30s"),
		},
		secretMountDir: "/etc/secrets",
		want:           []string{"-licenseFile=/etc/secrets/license-secret/license-key", "-licenseFile.reloadInterval=30s"},
	}
	f(o)

	// license key provided via secret with force offline
	o = opts{
		license: &vmv1beta1.License{
			KeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "license-secret"},
				Key:                  "license-key",
			},
			ForceOffline: ptr.To(true),
		},
		secretMountDir: "/etc/secrets",
		want:           []string{"-licenseFile=/etc/secrets/license-secret/license-key", "-license.forceOffline=true"},
	}
	f(o)
}
