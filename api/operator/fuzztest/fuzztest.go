// Package fuzztest provides callbacks for fuzzing operator API structs.
package fuzztest

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/randfill"
)

// New returns a fuzzer with Kubernetes quantity, duration, raw extension and int-or-string values.
func New(data []byte) *randfill.Filler {
	return randfill.NewFromGoFuzz(data).
		// Fuzzing defaults: up to 4 levels deep, 20% chance for nil values, 0-3 elements in the slice/maps
		MaxDepth(4).NilChance(0.2).NumElements(0, 3).
		Funcs(
			// Function to fuzz resource.Quantity
			func(quantity *resource.Quantity, c randfill.Continue) {
				size := int64(c.Intn(1 << 20))
				if c.Bool() {
					size = -size
				}
				if c.Intn(8) == 0 {
					*quantity = *resource.NewQuantity(0, resource.BinarySI)
					return
				}
				if c.Bool() {
					raw := []string{"10Gi", "1e3", "100m", "10GiB", "1..5", fmt.Sprintf("%dm", size)}[c.Intn(6)]
					if parsed, err := resource.ParseQuantity(raw); err == nil {
						*quantity = parsed
						return
					}
				}
				*quantity = *resource.NewQuantity(size, resource.BinarySI)
			},
			// Function to fuzz metav1.Duration
			func(value *metav1.Duration, c randfill.Continue) {
				if c.Intn(8) == 0 {
					value.Duration = 0
					return
				}
				value.Duration = time.Duration(1+c.Intn(3600)) * time.Second
				if c.Bool() {
					value.Duration = -value.Duration
				}
			},
			func(value *runtime.RawExtension, c randfill.Continue) {
				switch c.Intn(4) {
				case 0:
					value.Raw = nil
				case 1:
					value.Raw = []byte(`{}`)
				case 2:
					value.Raw = []byte(fmt.Sprintf(`{"matches":[{"path":{"type":"Exact","value":"/%d"}}]}`, c.Uint64()))
				default:
					value.Raw = []byte("{")
				}
				value.Object = nil
			},
			// Function to fuzz intstr.IntOrString
			func(value *intstr.IntOrString, c randfill.Continue) {
				if c.Bool() {
					*value = intstr.FromInt32(c.Int31())
				} else {
					*value = intstr.FromString(fmt.Sprintf("port-%d", c.Int31()))
				}
			},
		)
}
