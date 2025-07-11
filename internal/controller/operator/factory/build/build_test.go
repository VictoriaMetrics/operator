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
	f := func(license vmv1beta1.License, args []string, secretMountDir string, want []string) {
		got := LicenseArgsTo(args, &license, secretMountDir)
		slices.Sort(got)
		slices.Sort(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("vmv1beta1.License.MaybeAddToArgs() = %v, want %v", got, want)
		}
	}

	// license key provided
	f(vmv1beta1.License{
		Key: ptr.To("test-key"),
	}, []string{}, "/etc/secrets", []string{"-license=test-key"})

	// license key provided with force offline
	f(vmv1beta1.License{
		Key:          ptr.To("test-key"),
		ForceOffline: ptr.To(true),
	}, []string{}, "/etc/secrets", []string{"-license=test-key", "-license.forceOffline=true"})

	// license key provided with reload interval
	f(vmv1beta1.License{
		KeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "license-secret"},
			Key:                  "license-key",
		},
		ReloadInterval: ptr.To("30s"),
	}, []string{}, "/etc/secrets", []string{"-licenseFile=/etc/secrets/license-secret/license-key", "-licenseFile.reloadInterval=30s"})

	// license key provided via secret with force offline
	f(vmv1beta1.License{
		KeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "license-secret"},
			Key:                  "license-key",
		},
		ForceOffline: ptr.To(true),
	}, []string{}, "/etc/secrets", []string{"-licenseFile=/etc/secrets/license-secret/license-key", "-license.forceOffline=true"})
}
