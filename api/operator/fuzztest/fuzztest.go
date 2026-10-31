// Package fuzztest provides callbacks for fuzzing operator API structs.
package fuzztest

import (
	"fmt"

	gofuzz "github.com/google/gofuzz"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// New returns a fuzzer with valid Kubernetes quantity and int-or-string values.
func New(data []byte) *gofuzz.Fuzzer {
	return gofuzz.NewFromGoFuzz(data).
		MaxDepth(4).NilChance(0.2).NumElements(0, 3).
		Funcs(
			func(quantity *resource.Quantity, c gofuzz.Continue) {
				size := int64(c.Intn(1 << 20))
				if c.RandBool() {
					size = -size
				}
				if c.Intn(8) == 0 {
					*quantity = *resource.NewQuantity(0, resource.BinarySI)
					return
				}
				if c.RandBool() {
					raw := []string{"10Gi", "1e3", "100m", "10GiB", "1..5", fmt.Sprintf("%dm", size)}[c.Intn(6)]
					if parsed, err := resource.ParseQuantity(raw); err == nil {
						*quantity = parsed
						return
					}
				}
				*quantity = *resource.NewQuantity(size, resource.BinarySI)
			},
			func(value *intstr.IntOrString, c gofuzz.Continue) {
				if c.RandBool() {
					*value = intstr.FromInt32(c.Int31())
				} else {
					*value = intstr.FromString(fmt.Sprintf("port-%d", c.Int31()))
				}
			},
		)
}
