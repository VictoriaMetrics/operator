package config

import (
	"reflect"
	"testing"
)

func TestLabelsSet(t *testing.T) {
	f := func(value string, want map[string]string, wantErr bool) {
		t.Helper()
		var l Labels
		err := l.Set(value)
		if wantErr {
			if err == nil {
				t.Fatalf("expecting error for value %q, got nil", value)
			}
			return
		}
		if err != nil {
			t.Fatalf("unexpected error for value %q: %s", value, err)
		}
		if !reflect.DeepEqual(l.LabelsMap, want) {
			t.Fatalf("unexpected labels for value %q: got %v, want %v", value, l.LabelsMap, want)
		}
	}

	// comma-separated key=value pairs
	f("a=b,c=d", map[string]string{"a": "b", "c": "d"}, false)
	// single pair
	f("env=prod", map[string]string{"env": "prod"}, false)
	// empty value yields no labels
	f("", map[string]string{}, false)
	// value without '=' must error instead of panicking on index out of range
	f("invalid", nil, true)
	f("a=b,c", nil, true)
}

func TestLabelsMerge(t *testing.T) {
	var l Labels
	if err := l.Set("common=yes"); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	got := l.Merge(map[string]string{"extra": "1"})
	want := map[string]string{"common": "yes", "extra": "1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected merge result: got %v, want %v", got, want)
	}
}
