package v1beta1

import (
	"strings"
	"testing"
)

func TestVMStaticScrapeValidateNilTargetEndpoint(t *testing.T) {
	cr := &VMStaticScrape{
		Spec: VMStaticScrapeSpec{TargetEndpoints: []*TargetEndpoint{nil}},
	}
	if err := cr.Validate(); err == nil || !strings.Contains(err.Error(), "targetEndpoints[0]") {
		t.Fatalf("expected targetEndpoints[0] validation error, got %v", err)
	}
}
