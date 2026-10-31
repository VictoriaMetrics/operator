package config

import (
	"maps"
	"reflect"
	"testing"
)

func FuzzLabelsSet(f *testing.F) {
	for _, seed := range []string{
		"", "env=prod", "a=b,c=d", "a=b=c", "a=", "=value", "a=1,a=2",
		"invalid", "a=b,c", "a=b,", ",a=b", "a=b\nline", "да=нет", "a=one\\,two",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		labels := Labels{}
		if err := labels.Set("existing=value"); err != nil {
			t.Fatal(err)
		}
		before := maps.Clone(labels.LabelsMap)
		firstErr := labels.Set(input)
		firstString, firstMap := labels.String(), maps.Clone(labels.LabelsMap)
		secondErr := labels.Set(input)
		if (firstErr == nil) != (secondErr == nil) || firstErr != nil && firstErr.Error() != secondErr.Error() {
			t.Fatalf("non-deterministic parse errors: %v != %v", firstErr, secondErr)
		}
		if labels.String() != firstString || !reflect.DeepEqual(labels.LabelsMap, firstMap) {
			t.Fatalf("non-deterministic parse: %q %v != %q %v", firstString, firstMap, labels.String(), labels.LabelsMap)
		}
		if firstErr != nil {
			if firstString != "existing=value" || !reflect.DeepEqual(firstMap, before) {
				t.Fatalf("failed parse changed labels: %q %v", firstString, firstMap)
			}
			return
		}
		if firstString != input {
			t.Fatalf("String() = %q, want %q", firstString, input)
		}
	})
}

func FuzzLabelsMerge(f *testing.F) {
	for _, seed := range [][2]string{
		{"", ""},
		{"a=1", "b=2"},
		{"shared=new,a=1", "shared=old,b=2"},
		{"a=1,a=2", "a=3"},
	} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, local, external string) {
		var labels, other Labels
		if labels.Set(local) != nil || other.Set(external) != nil {
			return
		}
		original := maps.Clone(other.LabelsMap)
		merged := labels.Merge(other.LabelsMap)
		for key, value := range labels.LabelsMap {
			if merged[key] != value {
				t.Fatalf("local label %q lost: %q != %q", key, merged[key], value)
			}
		}
		if !maps.Equal(other.LabelsMap, original) || !maps.Equal(merged, labels.Merge(other.LabelsMap)) {
			t.Fatalf("Merge changed inputs or produced unstable output: %v", merged)
		}
		for key := range merged {
			if _, ok := labels.LabelsMap[key]; !ok {
				if merged[key] != original[key] {
					t.Fatalf("external label %q lost", key)
				}
			}
		}
	})
}
