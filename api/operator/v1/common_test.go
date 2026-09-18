package v1

import "testing"

func TestOTLPGRPCSpec_Validate(t *testing.T) {
	f := func(g *OTLPGRPCSpec, httpPort string, wantErr bool) {
		t.Helper()
		err := g.Validate(httpPort)
		if wantErr && err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !wantErr && err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	}

	// nil spec is always valid
	f(nil, "10429", false)

	// distinct ports
	f(&OTLPGRPCSpec{ListenPort: 4317}, "10429", false)

	// colliding with the HTTP port
	f(&OTLPGRPCSpec{ListenPort: 10429}, "10429", true)
}

func TestTLSServerConfig_Validate(t *testing.T) {
	f := func(tc *TLSServerConfig, wantErr bool) {
		t.Helper()
		err := tc.Validate()
		if wantErr && err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !wantErr && err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	}

	// nil config and empty cipher suites are always valid
	f(nil, false)
	f(&TLSServerConfig{}, false)

	// supported cipher suite names, case is ignored
	f(&TLSServerConfig{CipherSuites: []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_AES_128_GCM_SHA256"}}, false)
	f(&TLSServerConfig{CipherSuites: []string{"tls_aes_128_gcm_sha256"}}, false)

	// cipher suite defined by its ID
	f(&TLSServerConfig{CipherSuites: []string{"0x1301"}}, false)

	// typo in the cipher suite name
	f(&TLSServerConfig{CipherSuites: []string{"TLS_AES_128_GCM_SHA257"}}, true)

	// cipher suite considered insecure by the application
	f(&TLSServerConfig{CipherSuites: []string{"TLS_RSA_WITH_AES_128_CBC_SHA"}}, true)

	// unknown cipher suite ID
	f(&TLSServerConfig{CipherSuites: []string{"0x0000"}}, true)

	// a single unsupported name is enough to reject the whole list
	f(&TLSServerConfig{CipherSuites: []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_128_GCM_SHA257"}}, true)
}

func TestSyslogServerSpec_Validate(t *testing.T) {
	f := func(s *SyslogServerSpec, wantErr bool) {
		t.Helper()
		err := s.Validate()
		if wantErr && err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !wantErr && err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	}

	// nil spec is always valid
	f(nil, false)

	// listener without tlsConfig
	f(&SyslogServerSpec{TCPListeners: []*SyslogTCPListener{{ListenPort: 3001}}}, false)

	// unsupported cipher suite at the second listener
	f(&SyslogServerSpec{TCPListeners: []*SyslogTCPListener{
		{ListenPort: 3001},
		{ListenPort: 3002, TLSConfig: &TLSServerConfig{CipherSuites: []string{"TLS_AES_128_GCM_SHA257"}}},
	}}, true)

	// equal tls settings at both listeners, order and case of cipher suites is ignored
	f(&SyslogServerSpec{TCPListeners: []*SyslogTCPListener{
		{ListenPort: 3001, TLSConfig: &TLSServerConfig{
			MinVersion:   "TLS12",
			CipherSuites: []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_AES_128_GCM_SHA256"},
		}},
		{ListenPort: 3002, TLSConfig: &TLSServerConfig{
			MinVersion:   "TLS12",
			CipherSuites: []string{"tls_aes_128_gcm_sha256", "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"},
		}},
	}}, false)

	// listener without tlsConfig doesn't take part in the comparison
	f(&SyslogServerSpec{TCPListeners: []*SyslogTCPListener{
		{ListenPort: 3001},
		{ListenPort: 3002, TLSConfig: &TLSServerConfig{MinVersion: "TLS12"}},
	}}, false)

	// minVersion is defined only at the second listener, it would silently
	// change the effective minimum TLS version of the first one
	f(&SyslogServerSpec{TCPListeners: []*SyslogTCPListener{
		{ListenPort: 3001, TLSConfig: &TLSServerConfig{}},
		{ListenPort: 3002, TLSConfig: &TLSServerConfig{MinVersion: "TLS12"}},
	}}, true)

	// diverging minVersion
	f(&SyslogServerSpec{TCPListeners: []*SyslogTCPListener{
		{ListenPort: 3001, TLSConfig: &TLSServerConfig{MinVersion: "TLS13"}},
		{ListenPort: 3002, TLSConfig: &TLSServerConfig{MinVersion: "TLS12"}},
	}}, true)

	// diverging cipher suites
	f(&SyslogServerSpec{TCPListeners: []*SyslogTCPListener{
		{ListenPort: 3001, TLSConfig: &TLSServerConfig{CipherSuites: []string{"TLS_AES_128_GCM_SHA256"}}},
		{ListenPort: 3002, TLSConfig: &TLSServerConfig{CipherSuites: []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"}}},
	}}, true)
}
