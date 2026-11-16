package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestSyslogHTTPListenerPortCollision(t *testing.T) {
	syslog := &SyslogServerSpec{TCPListeners: []*SyslogTCPListener{{ListenPort: 9430}}}
	listeners := []vmv1beta1.HTTPListener{{Name: "extra", Port: 9430}}
	for _, tc := range []struct {
		name     string
		validate func() error
	}{
		{"VLAgent", func() error {
			return (&VLAgent{Spec: VLAgentSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{HTTPListeners: listeners},
				RemoteWrite:      []VLAgentRemoteWriteSpec{{URL: "http://example.com"}},
				SyslogSpec:       syslog,
			}}).Validate()
		}},
		{"VLSingle", func() error {
			return (&VLSingle{Spec: VLSingleSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{HTTPListeners: listeners},
				SyslogSpec:       syslog,
			}}).Validate()
		}},
		{"VLCluster insert", func() error {
			return (&VLCluster{Spec: VLClusterSpec{VLInsert: &VLInsert{
				CommonAppsParams: vmv1beta1.CommonAppsParams{HTTPListeners: listeners},
				SyslogSpec:       syslog,
			}}}).Validate()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorContains(t, tc.validate(), "httpListeners name=\"extra\" port=9430 conflicts with syslogSpec.tcpListeners[0].listenPort")
			listeners[0].Port = 9431
			assert.NoError(t, tc.validate())
			listeners[0].Port = 9430
		})
	}
}

func TestSyslogHTTPPortCollision(t *testing.T) {
	f := func(httpPort string, syslogPort int32, wantErr bool) {
		t.Helper()
		syslog := &SyslogServerSpec{TCPListeners: []*SyslogTCPListener{{ListenPort: syslogPort}}}
		err := syslog.Validate(httpPort, &vmv1beta1.CommonAppsParams{})
		if wantErr {
			assert.ErrorContains(t, err, "must not be equal to the HTTP listen port")
			return
		}
		assert.NoError(t, err)
	}

	f("9428", 9428, true)
	f("9428", 9430, false)

	// legacy extraArgs.httpListenAddr listener
	p := &vmv1beta1.CommonAppsParams{ExtraArgs: map[string]string{"httpListenAddr": ":9428,:9430"}}
	syslog := &SyslogServerSpec{TCPListeners: []*SyslogTCPListener{{ListenPort: 9430}}}
	assert.ErrorContains(t, syslog.Validate("9428", p), "extraArgs.httpListenAddr port=9430 conflicts")

	// defaulted primary port
	assert.Error(t, (&VLSingle{Spec: VLSingleSpec{SyslogSpec: &SyslogServerSpec{TCPListeners: []*SyslogTCPListener{{ListenPort: 9428}}}}}).Validate())
	assert.ErrorContains(t, (&VLAgent{Spec: VLAgentSpec{
		RemoteWrite: []VLAgentRemoteWriteSpec{{URL: "http://example.com"}},
		SyslogSpec:  &SyslogServerSpec{TCPListeners: []*SyslogTCPListener{{ListenPort: 9429}}},
	}}).Validate(), "must not be equal to the HTTP listen port=9429")
	assert.Error(t, (&VLCluster{Spec: VLClusterSpec{VLInsert: &VLInsert{SyslogSpec: &SyslogServerSpec{TCPListeners: []*SyslogTCPListener{{ListenPort: 9481}}}}}}).Validate())
}
