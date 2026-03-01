package reconcile

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffDeepOk(t *testing.T) {
	f := func(oldObj, newObj any, expected string) {
		t.Helper()
		got := diffDeep(oldObj, newObj, "spec")
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
	f(5, 6, `{"spec":{"--":5,"++":6}}`)
	f("new", "line", `{"spec":{"--":"new","++":"line"}}`)

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
	f(a1StructEmpty, a2StructFilled, `{"spec.Field3":{"--":0,"++":5}}`)

	var a2StructEmptyPtr *cmpStruct
	a1StructFilledPtr := &cmpStruct{
		Field2: make(map[int]int),
		Field3: 10,
	}
	f(a1StructFilledPtr, a2StructEmptyPtr, `{"spec":{"--":{"Field3":10}}}`)
}

func TestDiffDeepDerivativeOk(t *testing.T) {
	f := func(oldObj, newObj any, expected string) {
		t.Helper()
		got := diffDeepDerivative(oldObj, newObj, "spec")
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
	f(5, 6, `{"spec":{"--":5,"++":6}}`)
	f("new", "line", `{"spec":{"--":"new","++":"line"}}`)

	type cmpStruct struct {
		Field1 []string    `json:"field1,omitempty"`
		Field2 map[int]int `json:"field2,omitempty"`
		Field3 int32       `json:"field3"`
	}
	var a2Empty cmpStruct
	a1Filled := cmpStruct{
		Field1: []string{"v1"},
	}
	f(a2Empty, a1Filled, `{"spec.field1":{"++":["v1"]}}`)

	a1EmptyPtr := &cmpStruct{}
	a2FilledPtr := &cmpStruct{
		Field1: []string{"1", "2"},
		Field2: make(map[int]int),
	}
	f(a1EmptyPtr, a2FilledPtr, `{"spec.field1":{"++":["1","2"]}}`)
}
