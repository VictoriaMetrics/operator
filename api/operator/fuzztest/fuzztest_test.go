package fuzztest

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestNewFillsKubernetesValues(t *testing.T) {
	for _, data := range [][]byte{{}, {0, 1, 2, 3}, {255, 128, 64, 32}} {
		var got struct {
			Quantity resource.Quantity
			Port     intstr.IntOrString
		}
		New(data).Fill(&got)
		if _, err := resource.ParseQuantity(got.Quantity.String()); err != nil {
			t.Fatalf("invalid quantity for %v: %v", data, err)
		}
		if got.Port.Type != intstr.Int && got.Port.Type != intstr.String {
			t.Fatalf("invalid int-or-string type for %v: %v", data, got.Port.Type)
		}
	}
}
