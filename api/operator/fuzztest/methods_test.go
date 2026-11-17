package fuzztest

import "testing"

type callTarget struct{ calls int }

func (t *callTarget) Touch() { t.calls++ }

func TestCallMethods(t *testing.T) {
	c := New(nil)

	// non-pointer and nil inputs are skipped
	CallMethods(c, nil, nil)
	CallMethods(c, callTarget{}, nil)
	CallMethods(c, (*callTarget)(nil), nil)

	v := &callTarget{}
	CallMethods(c, v, nil)
	if v.calls != 1 {
		t.Fatalf("want 1 call, got %d", v.calls)
	}
}
