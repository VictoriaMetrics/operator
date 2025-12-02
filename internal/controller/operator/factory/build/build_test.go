package build

import (
	"reflect"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
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

		if !reflect.DeepEqual(got, o.want) {
			t.Errorf("vmv1beta1.License.MaybeAddToArgs(): %s", cmp.Diff(got, o.want))
		}
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
