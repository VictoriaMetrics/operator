package reconcile

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
)

func TestDiffDeepOk(t *testing.T) {
	f := func(oldObj, newObj interface{}, expected string) {
		t.Helper()
		got := diffDeep(oldObj, newObj)
		if got != expected {
			t.Fatalf("unexpected diff, \ngot:\n%s\nwant:\n%s\n", got, expected)
		}
	}
	var a1Slice []string
	a2SliceNonNil := make([]string, 0)
	f(a1Slice, a2SliceNonNil, `{[]string}:-"[]" +"nil"`)
	a1MapNonNil := make(map[int]int, 0)
	var a2MapNil map[int]int
	f(a1MapNonNil, a2MapNil, `{map[int]int}:-"nil" +"map[]"`)
	f(5, 5, "")
	f(5, 6, `{int}:-"6" +"5"`)
	f("new", "line", `{string}:-"line" +"new"`)

	type cmpStruct struct {
		Field1 []string
		Field2 map[int]int
		Field3 int32
	}
	var a1StructEmpty cmpStruct
	a2StructFilled := cmpStruct{
		Field1: make([]string, 0),
		Field3: 5,
	}
	f(a1StructEmpty, a2StructFilled, `{reconcile.cmpStruct}.Field1:-"[]" +"nil",{reconcile.cmpStruct}.Field3:-"5" +"0"`)

	var a2StructEmptyPtr *cmpStruct
	a1StructFilledPtr := &cmpStruct{
		Field2: make(map[int]int),
		Field3: 10,
	}
	f(a1StructFilledPtr, a2StructEmptyPtr, `{*reconcile.cmpStruct}:-"<nil>" +"&{[] map[] 10}"`)
}

func TestDiffDeepDerivativeOk(t *testing.T) {
	f := func(oldObj, newObj interface{}, expected string) {
		t.Helper()
		sym := equality.Semantic.DeepDerivative(oldObj, newObj)

		got := diffDeepDerivative(oldObj, newObj)
		if got != expected {
			t.Fatalf("unexpected diff, \ngot:\n%s\nwant:\n%s,\nsym=%v", got, expected, sym)
		}
	}
	var oldS []string
	newS := make([]string, 0)
	f(oldS, newS, ``)
	var newM map[int]int
	oldM := make(map[int]int, 0)
	f(oldM, newM, ``)
	f(5, 5, "")
	f(5, 6, `{int}:-"6" +"5"`)
	f("new", "line", `{string}:-"line" +"new"`)

	type cmpStruct struct {
		Field1 []string
		Field2 map[int]int
		Field3 int32
	}
	var a2Empty cmpStruct
	a1Filled := cmpStruct{
		Field1: []string{"v1"},
	}
	f(a1Filled, a2Empty, `{reconcile.cmpStruct}.Field1:-"nil" +"[v1]"`)

	a1EmptyPtr := &cmpStruct{}
	a2FilledPtr := &cmpStruct{
		Field1: []string{"1", "2"},
		Field2: make(map[int]int),
	}
	f(a1EmptyPtr, a2FilledPtr, ``)
}
