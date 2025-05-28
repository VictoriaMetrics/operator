package vlcluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
)

func TestBuildSyslogArgs(t *testing.T) {
	f := func(src *vmv1.SyslogServerSpec, expectedArgs []string) {
		t.Helper()
		got := buildSyslogArgs(nil, src)
		assert.Equal(t, expectedArgs, got, "unexpected syslog args")
	}

	// multiple tcp listeners
	spec := vmv1.SyslogServerSpec{
		TCPListeners: []*vmv1.SyslogTCPListener{
			{
				ListenPort: 3001,
			},
			{
				ListenPort:   3002,
				StreamFields: vmv1.FieldsListString(`["msg_1","msg_2"]`),
			},
		},
	}
	expected := []string{
		"-syslog.listenAddr.tcp=:3001,:3002",
		`-syslog.streamFields.tcp='','["msg_1","msg_2"]'`,
	}
	f(&spec, expected)

	// multiple udp listeners
	spec = vmv1.SyslogServerSpec{
		UDPListeners: []*vmv1.SyslogUDPListener{
			{
				ListenPort:   3001,
				IgnoreFields: vmv1.FieldsListString(`["ignore_1"]`),
			},
			{
				ListenPort:   3002,
				StreamFields: vmv1.FieldsListString(`["msg_1","msg_2"]`),
			},
			{
				ListenPort: 3005,
			},
		},
	}
	expected = []string{
		"-syslog.listenAddr.udp=:3001,:3002,:3005",
		`-syslog.streamFields.udp='','["msg_1","msg_2"]',''`,
		`-syslog.ignoreFields.udp='["ignore_1"]','',''`,
	}
	f(&spec, expected)

	// mixed udp and tcp
	// multiple udp listeners
	spec = vmv1.SyslogServerSpec{
		TCPListeners: []*vmv1.SyslogTCPListener{
			{
				ListenPort: 3001,
			},
			{
				ListenPort:   3002,
				StreamFields: vmv1.FieldsListString(`["msg_1","msg_2"]`),
			},
		},
		UDPListeners: []*vmv1.SyslogUDPListener{
			{
				ListenPort:   3001,
				IgnoreFields: vmv1.FieldsListString(`["ignore_1"]`),
			},
			{
				ListenPort:   3002,
				StreamFields: vmv1.FieldsListString(`["msg_1","msg_2"]`),
			},
			{
				ListenPort: 3005,
			},
		},
	}
	expected = []string{
		"-syslog.listenAddr.tcp=:3001,:3002",
		`-syslog.streamFields.tcp='','["msg_1","msg_2"]'`,
		"-syslog.listenAddr.udp=:3001,:3002,:3005",
		`-syslog.streamFields.udp='','["msg_1","msg_2"]',''`,
		`-syslog.ignoreFields.udp='["ignore_1"]','',''`,
	}
	f(&spec, expected)

	// with tls
	spec = vmv1.SyslogServerSpec{
		TCPListeners: []*vmv1.SyslogTCPListener{
			{
				ListenPort: 3001,
				TenantID:   "10:25",
			},
			{
				ListenPort:   3002,
				StreamFields: vmv1.FieldsListString(`["msg_1","msg_2"]`),
				TLSConfig: &vmv1.TLSServerConfig{
					CertSecret: &corev1.SecretKeySelector{
						Key: "CERT",
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "tls",
						},
					},
					KeyFile: "/etc/vm/secrets/tls/key",
				},
			},
		},
		UDPListeners: []*vmv1.SyslogUDPListener{
			{
				ListenPort:     3001,
				CompressMethod: "zstd",
			},
		},
	}
	expected = []string{
		"-syslog.listenAddr.tcp=:3001,:3002",
		`-syslog.streamFields.tcp='','["msg_1","msg_2"]'`,
		`-syslog.tenantID.tcp='10:25',''`,
		"-syslog.tls=,true",
		"-syslog.tlsCertFile=/etc/vm/tls-server-secrets/tls/CERT",
		"-syslog.tlsKeyFile=/etc/vm/secrets/tls/key",
		"-syslog.listenAddr.udp=:3001",
		"-syslog.compressMethod.udp='zstd'",
	}
	f(&spec, expected)

}
