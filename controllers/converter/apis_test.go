package converter

import (
	v1beta1vm "github.com/VictoriaMetrics/operator/api/v1beta1"
	v1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"testing"
)

func TestConvertTlsConfig(t *testing.T) {
	type args struct {
		tlsConf *v1.TLSConfig
	}
	tests := []struct {
		name string
		args args
		want *v1beta1vm.TLSConfig
	}{
		{
			name: "replace prom secret path",
			args: args{
				tlsConf: &v1.TLSConfig{
					CAFile:   "/etc/prom_add/ca",
					CertFile: "/etc/prometheus/secrets/cert.crt",
					KeyFile:  "/etc/prometheus/configmaps/key.pem",
				},
			},
			want: &v1beta1vm.TLSConfig{
				CAFile:   "/etc/prom_add/ca",
				CertFile: "/etc/vm/secrets/cert.crt",
				KeyFile:  "/etc/vm/configs/key.pem",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertTlsConfig(tt.args.tlsConf)
			if got.KeyFile != tt.want.KeyFile || got.CertFile != tt.want.CertFile || got.CAFile != tt.want.CAFile {
				t.Errorf("ConvertTlsConfig() = \n%v, \nwant \n%v", got, tt.want)
			}
		})
	}
}
