package reconcile

import (
	"encoding/json"
	"maps"
	"reflect"
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// strategy kinds, used as strategies index and in seeds
const (
	preferVM uint8 = iota
	preferProm
	mergeVMPriority
	mergePromPriority
)

type mergeSeed struct {
	strategy uint8
	maps     string
}

func FuzzMergeMapsWithStrategy(f *testing.F) {
	for _, seed := range []mergeSeed{
		{preferVM, `{}`},
		{preferVM, `{"existing":{"shared":"vm"},"new":{"shared":"prom"}}`},
		{preferProm, `{"existing":{},"new":{},"prev":{}}`},
		{preferProm, `{"existing":{"shared":"vm"},"new":{"shared":"prom"}}`},
		{mergeVMPriority, `{"existing":{"shared":"vm","vm":"only"},"new":{"shared":"prom","prom":"only"},"prev":{"removed":"old"}}`},
		{mergePromPriority, `{"existing":{"shared":"vm","removed":"still-present"},"new":{"shared":"prom","added":"new"},"prev":{"removed":"old","shared":"old"}}`},
		{mergeVMPriority, `{"existing":{"a":"1"},"new":{"b":"2"},"prev":{"a":"old","b":"old"}}`},
		{mergePromPriority, `{"existing":{"a":"1"},"new":{"b":"2"},"prev":{"a":"old","b":"old"}}`},
	} {
		f.Add(seed.strategy, []byte(seed.maps))
	}

	strategies := [...]vmv1beta1.MetadataStrategy{
		preferVM:          vmv1beta1.MetadataStrategyPreferVM,
		preferProm:        vmv1beta1.MetadataStrategyPreferProm,
		mergeVMPriority:   vmv1beta1.MetadataStrategyMergeVMPriority,
		mergePromPriority: vmv1beta1.MetadataStrategyMergePromPriority,
	}
	f.Fuzz(func(t *testing.T, strategy uint8, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		var input struct {
			Existing map[string]string `json:"existing"`
			New      map[string]string `json:"new"`
			Prev     map[string]string `json:"prev"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			return
		}
		existing, next, prev := maps.Clone(input.Existing), maps.Clone(input.New), maps.Clone(input.Prev)
		mode := strategies[int(strategy)%len(strategies)]
		got := mergeMapsWithStrategy(input.Existing, input.New, input.Prev, mode)
		again := mergeMapsWithStrategy(input.Existing, input.New, input.Prev, mode)
		if !reflect.DeepEqual(got, again) {
			t.Fatalf("non-deterministic %s merge: %v != %v", mode, got, again)
		}
		if !reflect.DeepEqual(input.Existing, existing) || !reflect.DeepEqual(input.New, next) || !reflect.DeepEqual(input.Prev, prev) {
			t.Fatalf("%s merge mutated an input map", mode)
		}
		switch mode {
		case vmv1beta1.MetadataStrategyPreferVM:
			if !reflect.DeepEqual(got, existing) {
				t.Fatalf("prefer-vm: got %v, want %v", got, existing)
			}
		case vmv1beta1.MetadataStrategyPreferProm:
			if !reflect.DeepEqual(got, next) {
				t.Fatalf("prefer-prom: got %v, want %v", got, next)
			}
		case vmv1beta1.MetadataStrategyMergeVMPriority, vmv1beta1.MetadataStrategyMergePromPriority:
			primary, secondary := next, existing
			if mode == vmv1beta1.MetadataStrategyMergeVMPriority {
				primary, secondary = existing, next
			}
			for key, value := range primary {
				if actual, ok := got[key]; !ok || actual != value {
					t.Fatalf("%s merge lost primary value for %q: got %q, want %q", mode, key, actual, value)
				}
			}
			for key, value := range secondary {
				if _, ok := primary[key]; ok {
					continue
				}
				if _, wasOwned := prev[key]; wasOwned {
					if _, ok := got[key]; ok {
						t.Fatalf("%s merge retained deleted key %q", mode, key)
					}
				} else if actual, ok := got[key]; !ok || actual != value {
					t.Fatalf("%s merge lost unowned key %q: got %q, want %q", mode, key, actual, value)
				}
			}
		}
	})
}
