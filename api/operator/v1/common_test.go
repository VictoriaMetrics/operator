package v1

import (
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestOTLPGRPCSpec_Validate(t *testing.T) {
	f := func(g *OTLPGRPCSpec, httpPort string, listeners []vmv1beta1.HTTPListener, wantErr bool) {
		t.Helper()
		err := g.Validate(httpPort, listeners)
		if wantErr && err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !wantErr && err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	}

	// nil spec is always valid
	f(nil, "10429", nil, false)

	// distinct ports
	f(&OTLPGRPCSpec{ListenPort: 4317}, "10429", nil, false)

	// colliding with the HTTP port
	f(&OTLPGRPCSpec{ListenPort: 10429}, "10429", nil, true)

	// a listener named like the generated OTLP gRPC port collides
	f(&OTLPGRPCSpec{ListenPort: 4317}, "10429", []vmv1beta1.HTTPListener{
		{Name: "otlp-grpc", Addr: ":10429"},
	}, true)

	// a listener with a different name is fine
	f(&OTLPGRPCSpec{ListenPort: 4317}, "10429", []vmv1beta1.HTTPListener{
		{Name: "http", Addr: ":10429"},
	}, false)

	// a listener's own port colliding with the gRPC port is invalid, even with a different name
	f(&OTLPGRPCSpec{ListenPort: 4317}, "10429", []vmv1beta1.HTTPListener{
		{Name: "http", Addr: ":4317"},
	}, true)

	// nil grpcSpec is always valid, regardless of listener names
	f(nil, "10429", []vmv1beta1.HTTPListener{
		{Name: "otlp-grpc", Addr: ":4317"},
	}, false)
}
