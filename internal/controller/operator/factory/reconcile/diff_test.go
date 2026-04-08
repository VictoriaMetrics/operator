package reconcile

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffDeepOk(t *testing.T) {
	type CmpDeeplyNestedStruct struct {
		DeeplyNested string `json:"field,omitempty"`
	}
	type CmpNestedStruct struct {
		CmpDeeplyNestedStruct `json:",inline,omitempty"`
	}
	type cmpStruct struct {
		Field1          []string       `json:"field1,omitempty"`
		Field2          map[string]int `json:"field2,omitempty"`
		Field3          int32
		Field4          map[int]int `json:"field4,omitempty"`
		Field5          string
		CmpNestedStruct `json:",inline"`
	}
	type opts struct {
		desired, current             *cmpStruct
		expected, expectedDerivative string
	}
	f := func(o opts) {
		t.Helper()
		got := diffDeepInternal(o.desired, o.current, false, "spec")
		data, err := json.Marshal(got)
		assert.NoError(t, err)
		assert.Equal(t, string(data), o.expected)

		// derivative
		got = diffDeepInternal(o.desired, o.current, true, "spec")
		data, err = json.Marshal(got)
		assert.NoError(t, err)
		assert.Equal(t, string(data), o.expectedDerivative)
	}

	// nil to empty slice
	f(opts{
		desired: &cmpStruct{
			Field1: make([]string, 0),
		},
		current:            &cmpStruct{},
		expected:           `{}`,
		expectedDerivative: `{}`,
	})

	// nil to empty map
	f(opts{
		desired: &cmpStruct{
			Field4: make(map[int]int),
		},
		current:            &cmpStruct{},
		expected:           `{}`,
		expectedDerivative: `{}`,
	})

	// desired slice is empty
	f(opts{
		desired: &cmpStruct{},
		current: &cmpStruct{
			Field1: []string{"1"},
		},
		expected:           `{"spec.field1":{"--":["1"]}}`,
		expectedDerivative: `{}`,
	})

	// desired struct is nil
	f(opts{
		current: &cmpStruct{
			Field1: []string{"1"},
		},
		expected:           `{"spec":{"--":{"field1":["1"],"Field3":0,"Field5":""}}}`,
		expectedDerivative: `{}`,
	})

	// equal numbers
	f(opts{
		desired: &cmpStruct{
			Field3: 5,
		},
		current: &cmpStruct{
			Field3: 5,
		},
		expected:           `{}`,
		expectedDerivative: `{}`,
	})

	// different numbers
	f(opts{
		desired: &cmpStruct{
			Field3: 5,
		},
		current: &cmpStruct{
			Field3: 6,
		},
		expected:           `{"spec.Field3":{"--":6,"++":5}}`,
		expectedDerivative: `{"spec.Field3":{"--":6,"++":5}}`,
	})

	// different strings
	f(opts{
		desired: &cmpStruct{
			Field5: "new",
		},
		current: &cmpStruct{
			Field5: "line",
		},
		expected:           `{"spec.Field5":{"--":"line","++":"new"}}`,
		expectedDerivative: `{"spec.Field5":{"--":"line","++":"new"}}`,
	})

	// slice and number
	f(opts{
		desired: &cmpStruct{},
		current: &cmpStruct{
			Field1: make([]string, 0),
			Field3: 5,
		},
		expected:           `{"spec.Field3":{"--":5,"++":0}}`,
		expectedDerivative: `{"spec.Field3":{"--":5,"++":0}}`,
	})

	// map and number
	f(opts{
		desired: &cmpStruct{
			Field2: make(map[string]int),
			Field3: 10,
		},
		current:            &cmpStruct{},
		expected:           `{"spec.Field3":{"--":0,"++":10}}`,
		expectedDerivative: `{"spec.Field3":{"--":0,"++":10}}`,
	})

	// nil slice with 1 element slice
	f(opts{
		desired: &cmpStruct{
			Field1: []string{"v1"},
		},
		current:            &cmpStruct{},
		expected:           `{"spec.field1":{"++":["v1"]}}`,
		expectedDerivative: `{"spec.field1":{"++":["v1"]}}`,
	})

	// nil slice with 2 element slice
	f(opts{
		desired: &cmpStruct{
			Field1: []string{"1", "2"},
		},
		current:            &cmpStruct{},
		expected:           `{"spec.field1":{"++":["1","2"]}}`,
		expectedDerivative: `{"spec.field1":{"++":["1","2"]}}`,
	})

	// different non-empty slices, desired slice is smaller
	f(opts{
		desired: &cmpStruct{
			Field1: []string{"1"},
		},
		current: &cmpStruct{
			Field1: []string{"1", "2"},
		},
		expected:           `{"spec.field1[1]":{"--":"2"}}`,
		expectedDerivative: `{"spec.field1[1]":{"--":"2"}}`,
	})

	// different non-empty slices with same length
	f(opts{
		desired: &cmpStruct{
			Field1: []string{"1"},
		},
		current: &cmpStruct{
			Field1: []string{"2"},
		},
		expected:           `{"spec.field1[0]":{"--":"2","++":"1"}}`,
		expectedDerivative: `{"spec.field1[0]":{"--":"2","++":"1"}}`,
	})

	// different non-empty slices, desired slice is bigger
	f(opts{
		desired: &cmpStruct{
			Field1: []string{"1", "2"},
		},
		current: &cmpStruct{
			Field1: []string{"2"},
		},
		expected:           `{"spec.field1[0]":{"++":"1"}}`,
		expectedDerivative: `{"spec.field1[0]":{"++":"1"}}`,
	})

	// swapped int keys of map
	f(opts{
		desired: &cmpStruct{
			Field2: map[string]int{
				"1": 1,
				"2": 2,
			},
		},
		current: &cmpStruct{
			Field2: map[string]int{
				"1": 2,
				"2": 1,
			},
		},
		expected:           `{"spec.field2['1']":{"--":2,"++":1},"spec.field2['2']":{"--":1,"++":2}}`,
		expectedDerivative: `{"spec.field2['1']":{"--":2,"++":1},"spec.field2['2']":{"--":1,"++":2}}`,
	})

	// swapped string keys of map
	f(opts{
		desired: &cmpStruct{
			Field4: map[int]int{
				1: 1,
				2: 2,
			},
		},
		current: &cmpStruct{
			Field4: map[int]int{
				1: 2,
				2: 1,
			},
			CmpNestedStruct: CmpNestedStruct{
				CmpDeeplyNestedStruct: CmpDeeplyNestedStruct{
					DeeplyNested: "value",
				},
			},
		},
		expected:           `{"spec.field":{"--":"value","++":""},"spec.field4[1]":{"--":2,"++":1},"spec.field4[2]":{"--":1,"++":2}}`,
		expectedDerivative: `{"spec.field4[1]":{"--":2,"++":1},"spec.field4[2]":{"--":1,"++":2}}`,
	})
}
