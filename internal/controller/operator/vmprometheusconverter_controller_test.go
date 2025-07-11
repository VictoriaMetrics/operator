package operator

import (
	"reflect"
	"testing"
)

func Test_mergeLabelsWithStrategy(t *testing.T) {
	f := func(old, new, want map[string]string, mergeStrategy string) {
		t.Helper()
		if got := mergeLabelsWithStrategy(old, new, mergeStrategy); !reflect.DeepEqual(got, want) {
			t.Errorf("mergeLabelsWithStrategy() = %v, want %v", got, want)
		}
	}

	// delete not existing label
	f(map[string]string{
		"label1":       "value1",
		"label2":       "value2",
		"missinglabel": "value3",
	}, map[string]string{
		"label1": "value1",
		"label2": "value4",
	}, map[string]string{
		"label1": "value1",
		"label2": "value4",
	}, MetaPreferProm)

	// add new label
	f(map[string]string{
		"label1":       "value1",
		"label2":       "value2",
		"missinglabel": "value3",
	}, map[string]string{
		"label1": "value1",
		"label2": "value4",
		"label5": "value10",
	}, map[string]string{
		"label1": "value1",
		"label2": "value4",
		"label5": "value10",
	}, MetaPreferProm)

	// add new label with VM priority
	f(map[string]string{
		"label1": "value1",
		"label2": "value2",
		"label5": "value3",
	}, map[string]string{
		"label1":       "value1",
		"label2":       "value4",
		"missinglabel": "value10",
	}, map[string]string{
		"label1": "value1",
		"label2": "value2",
		"label5": "value3",
	}, MetaPreferVM)

	// remove all labels
	f(nil, map[string]string{
		"label1":       "value1",
		"label2":       "value4",
		"missinglabel": "value10",
	}, nil, MetaPreferVM)

	// remove keep old labels
	f(map[string]string{
		"label1": "value1",
		"label2": "value4",
	}, nil, map[string]string{
		"label1": "value1",
		"label2": "value4",
	}, MetaPreferVM)

	// merge all labels with VMPriority
	f(map[string]string{
		"label1": "value1",
		"label2": "value4",
	}, map[string]string{
		"label1":       "value2",
		"label2":       "value4",
		"missinglabel": "value10",
	}, map[string]string{
		"label1":       "value1",
		"label2":       "value4",
		"missinglabel": "value10",
	}, MetaMergeLabelsVMPriority)
}
