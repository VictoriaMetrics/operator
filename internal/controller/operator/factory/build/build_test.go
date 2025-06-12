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
	type args struct {
		args           []string
		secretMountDir string
	}
	tests := []struct {
		name    string
		license vmv1beta1.License
		args    args
		want    []string
	}{
		{
			name: "license key provided",
			license: vmv1beta1.License{
				Key: ptr.To("test-key"),
			},
			args: args{
				args:           []string{},
				secretMountDir: "/etc/secrets",
			},
			want: []string{"-license=test-key"},
		},
		{
			name: "license key provided with force offline",
			license: vmv1beta1.License{
				Key:          ptr.To("test-key"),
				ForceOffline: ptr.To(true),
			},
			args: args{
				args:           []string{},
				secretMountDir: "/etc/secrets",
			},
			want: []string{"-license=test-key", "-license.forceOffline=true"},
		},
		{
			name: "license key provided with reload interval",
			license: vmv1beta1.License{
				KeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "license-secret"},
					Key:                  "license-key",
				},
				ReloadInterval: ptr.To("30s"),
			},
			args: args{
				args:           []string{},
				secretMountDir: "/etc/secrets",
			},
			want: []string{"-licenseFile=/etc/secrets/license-secret/license-key", "-licenseFile.reloadInterval=30s"},
		},
		{
			name: "license key provided via secret with force offline",
			license: vmv1beta1.License{
				KeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "license-secret"},
					Key:                  "license-key",
				},
				ForceOffline: ptr.To(true),
			},
			args: args{
				args:           []string{},
				secretMountDir: "/etc/secrets",
			},
			want: []string{"-licenseFile=/etc/secrets/license-secret/license-key", "-license.forceOffline=true"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LicenseArgsTo(tt.args.args, &tt.license, tt.args.secretMountDir)
			slices.Sort(got)
			slices.Sort(tt.want)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("vmv1beta1.License.MaybeAddToArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}
