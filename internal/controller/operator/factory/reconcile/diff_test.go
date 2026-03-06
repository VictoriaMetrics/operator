package reconcile

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffDeepOk(t *testing.T) {
	f := func(desiredObj, currentObj any, expected string) {
		t.Helper()
		got := diffDeep(desiredObj, currentObj, "spec")
		data, err := json.Marshal(got)
		assert.NoError(t, err)
		assert.Equal(t, string(data), expected)
	}
	var a1Slice []string
	a2SliceNonNil := make([]string, 0)
	f(a1Slice, a2SliceNonNil, `{}`)
	a1MapNonNil := make(map[int]int, 0)
	var a2MapNil map[int]int
	f(a1MapNonNil, a2MapNil, `{}`)
	f(5, 5, `{}`)
	f(5, 6, `{"spec":{"--":6,"++":5}}`)
	f("new", "line", `{"spec":{"--":"line","++":"new"}}`)

	type cmpStruct struct {
		Field1 []string    `json:"field1,omitempty"`
		Field2 map[int]int `json:"field2,omitempty"`
		Field3 int32
	}
	var a1StructEmpty cmpStruct
	a2StructFilled := cmpStruct{
		Field1: make([]string, 0),
		Field3: 5,
	}
	f(a1StructEmpty, a2StructFilled, `{"spec.Field3":{"--":5,"++":0}}`)

	var a2StructEmptyPtr *cmpStruct
	a1StructFilledPtr := &cmpStruct{
		Field2: make(map[int]int),
		Field3: 10,
	}
	f(a1StructFilledPtr, a2StructEmptyPtr, `{"spec":{"++":{"Field3":10}}}`)
}

func TestDiffDeepDerivativeOk(t *testing.T) {
	f := func(desiredObj, currentObj any, expected string) {
		t.Helper()
		got := diffDeepDerivative(desiredObj, currentObj, "spec")
		data, err := json.Marshal(got)
		assert.NoError(t, err)
		assert.Equal(t, string(data), expected)
	}
	var oldS []string
	newS := make([]string, 0)
	f(oldS, newS, `{}`)
	var newM map[int]int
	oldM := make(map[int]int, 0)
	f(oldM, newM, `{}`)
	f(5, 5, `{}`)
	f(5, 6, `{"spec":{"--":6,"++":5}}`)
	f("new", "line", `{"spec":{"--":"line","++":"new"}}`)

	type cmpStruct struct {
		Field1 []string       `json:"field1,omitempty"`
		Field2 map[string]int `json:"field2,omitempty"`
		Field3 int32          `json:"field3"`
		Field4 map[int]int    `json:"field4,omitempty"`
	}
	var a2Empty cmpStruct
	a1Filled := cmpStruct{
		Field1: []string{"v1"},
	}
	f(a1Filled, a2Empty, `{"spec.field1":{"++":["v1"]}}`)

	a1EmptyPtr := &cmpStruct{}
	a2FilledPtr := &cmpStruct{
		Field1: []string{"1", "2"},
		Field2: make(map[string]int),
	}
	f(a2FilledPtr, a1EmptyPtr, `{"spec.field1":{"++":["1","2"]}}`)

	a3PartialPtr := &cmpStruct{
		Field1: []string{"1"},
	}
	a3FilledPtr := &cmpStruct{
		Field1: []string{"1", "2"},
	}
	f(a3PartialPtr, a3FilledPtr, `{"spec.field1[1]":{"--":"2"}}`)

	a4PartialPtr := &cmpStruct{
		Field1: []string{"1"},
	}
	a4FilledPtr := &cmpStruct{
		Field1: []string{"2"},
	}
	f(a4PartialPtr, a4FilledPtr, `{"spec.field1[0]":{"--":"2","++":"1"}}`)

	a5PartialPtr := &cmpStruct{
		Field1: []string{"1", "2"},
	}
	a5FilledPtr := &cmpStruct{
		Field1: []string{"2"},
	}
	f(a5PartialPtr, a5FilledPtr, `{"spec.field1[0]":{"++":"1"}}`)

	a6PartialPtr := &cmpStruct{
		Field1: []string{"1", "2"},
	}
	a6FilledPtr := &cmpStruct{
		Field1: []string{"1"},
	}
	f(a6PartialPtr, a6FilledPtr, `{"spec.field1[1]":{"++":"2"}}`)

	a7PartialPtr := &cmpStruct{
		Field2: map[string]int{
			"1": 1,
			"2": 2,
		},
	}
	a7FilledPtr := &cmpStruct{
		Field2: map[string]int{
			"1": 2,
			"2": 1,
		},
	}
	f(a7PartialPtr, a7FilledPtr, `{"spec.field2['1']":{"--":2,"++":1},"spec.field2['2']":{"--":1,"++":2}}`)

	a8PartialPtr := &cmpStruct{
		Field4: map[int]int{
			1: 1,
			2: 2,
		},
	}
	a8FilledPtr := &cmpStruct{
		Field4: map[int]int{
			1: 2,
			2: 1,
		},
	}
	f(a8PartialPtr, a8FilledPtr, `{"spec.field4[1]":{"--":2,"++":1},"spec.field4[2]":{"--":1,"++":2}}`)
}
