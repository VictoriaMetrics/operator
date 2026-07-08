package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestSyslogServerSpec_ValidateNoListenerNameCollision(t *testing.T) {
	f := func(s *SyslogServerSpec, name string, wantErr bool) {
		t.Helper()
		err := s.ValidateNoListenerNameCollision([]vmv1beta1.HTTPListener{{Name: name, Addr: ":9428"}})
		if wantErr {
			assert.ErrorContains(t, err, "collides")
			return
		}
		assert.NoError(t, err)
	}
	spec := &SyslogServerSpec{
		TCPListeners: []*SyslogTCPListener{{ListenPort: 3001}},
		UDPListeners: []*SyslogUDPListener{{ListenPort: 3002}},
	}

	// nil spec reserves nothing
	f(nil, "syslog-tcp-0", false)

	// generated tcp and udp port names
	f(spec, "syslog-tcp-0", true)
	f(spec, "syslog-udp-0", true)

	// index without a configured listener, and unrelated name
	f(spec, "syslog-tcp-1", false)
	f(spec, "http", false)
}
