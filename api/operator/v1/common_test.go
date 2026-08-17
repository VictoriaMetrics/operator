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
