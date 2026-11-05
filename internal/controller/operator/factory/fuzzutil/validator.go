package fuzzutil

import (
	"encoding/json"
	"slices"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	"github.com/VictoriaMetrics/operator/internal/config"
)

// validatorSeeds hit the parsing error branches shared by webhook validators.
var validatorSeeds = []string{
	`{}`,
	`{"spec":[]}`,
	`{"spec":{"unknownField":1}}`,
	`{"metadata":{"annotations":{"operator.victoriametrics.com/skip-validation":"true"}}}`,
}

// ValidatorTarget fuzzes a single webhook validator.
type ValidatorTarget struct {
	seeds []string
	run   func(t *testing.T, data []byte)
}

// NewValidator wraps v, so create, update and delete admission run on fuzzed objects.
func NewValidator[T client.Object](newObj func() T, v admission.Validator[T], seeds ...string) ValidatorTarget {
	return ValidatorTarget{
		seeds: slices.Concat(validatorSeeds, seeds),
		run: func(t *testing.T, data []byte) {
			obj, old := newObj(), newObj()
			if err := json.Unmarshal(data, obj); err != nil {
				fuzztest.New(data).MaxDepth(8).Fill(obj)
			}

			// old object differs from new one, so update compares real specs
			fuzztest.New(data).MaxDepth(8).Fill(old)

			ctx := t.Context()
			_, _ = v.ValidateCreate(ctx, obj)
			_, _ = v.ValidateUpdate(ctx, old, obj)
			_, _ = v.ValidateDelete(ctx, obj)
		},
	}
}

// Validators fuzzes targets, picking one by kind.
func Validators(f *testing.F, targets ...ValidatorTarget) {
	for kind, target := range targets {
		for _, seed := range target.seeds {
			f.Add(uint8(kind), false, []byte(seed))
			f.Add(uint8(kind), true, []byte(seed))
		}
	}
	f.Fuzz(func(t *testing.T, kind uint8, openShift bool, data []byte) {
		// toggle OpenShift warnings
		cfg := config.MustGetBaseConfig()
		mode := cfg.OpenshiftCompatibilityMode
		t.Cleanup(func() { cfg.OpenshiftCompatibilityMode = mode })
		cfg.OpenshiftCompatibilityMode = "disabled"
		if openShift {
			cfg.OpenshiftCompatibilityMode = "enabled"
		}

		targets[int(kind)%len(targets)].run(t, data)
	})
}
