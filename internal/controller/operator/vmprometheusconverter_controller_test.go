package operator

import (
	"reflect"
	"testing"
)

func Test_mergeLabelsWithStrategy(t *testing.T) {
	type opts struct {
		old           map[string]string
		new           map[string]string
		mergeStrategy string
		want          map[string]string
	}
	f := func(opts opts) {
		if got := mergeLabelsWithStrategy(opts.old, opts.new, opts.mergeStrategy); !reflect.DeepEqual(got, opts.want) {
			t.Errorf("mergeLabelsWithStrategy() = %v, want %v", got, opts.want)
		}
	}

	// delete not existing label
	o := opts{
		old:           map[string]string{"label1": "value1", "label2": "value2", "missinglabel": "value3"},
		new:           map[string]string{"label1": "value1", "label2": "value4"},
		mergeStrategy: MetaPreferProm,
		want:          map[string]string{"label1": "value1", "label2": "value4"},
	}
	f(o)

	// add new label
	o = opts{
		old:           map[string]string{"label1": "value1", "label2": "value2", "missinglabel": "value3"},
		new:           map[string]string{"label1": "value1", "label2": "value4", "label5": "value10"},
		mergeStrategy: MetaPreferProm,
		want:          map[string]string{"label1": "value1", "label2": "value4", "label5": "value10"},
	}
	f(o)

	// add new label with VM priority
	o = opts{
		old:           map[string]string{"label1": "value1", "label2": "value2", "label5": "value3"},
		new:           map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
		mergeStrategy: MetaPreferVM,
		want:          map[string]string{"label1": "value1", "label2": "value2", "label5": "value3"},
	}
	f(o)

	// remove all labels
	o = opts{
		old:           nil,
		new:           map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
		mergeStrategy: MetaPreferVM,
		want:          nil,
	}
	f(o)

	// remove keep old labels
	o = opts{
		old:           map[string]string{"label1": "value1", "label2": "value4"},
		new:           nil,
		mergeStrategy: MetaPreferVM,
		want:          map[string]string{"label1": "value1", "label2": "value4"},
	}
	f(o)

	// merge all labels with VMPriority
	o = opts{
		old:           map[string]string{"label1": "value1", "label2": "value4"},
		new:           map[string]string{"label1": "value2", "label2": "value4", "missinglabel": "value10"},
		mergeStrategy: MetaMergeLabelsVMPriority,
		want:          map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
	}
	f(o)
}
