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
