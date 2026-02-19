package operator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_mergeLabelsWithStrategy(t *testing.T) {
	type opts struct {
		old           map[string]string
		new           map[string]string
		mergeStrategy string
		want          map[string]string
	}
	f := func(o opts) {
		t.Helper()
		got := mergeLabelsWithStrategy(o.old, o.new, o.mergeStrategy)
		assert.Equal(t, got, o.want)
	}

	// delete not existing label
	f(opts{
		old:           map[string]string{"label1": "value1", "label2": "value2", "missinglabel": "value3"},
		new:           map[string]string{"label1": "value1", "label2": "value4"},
		mergeStrategy: MetaPreferProm,
		want:          map[string]string{"label1": "value1", "label2": "value4"},
	})

	// add new label
	f(opts{
		old:           map[string]string{"label1": "value1", "label2": "value2", "missinglabel": "value3"},
		new:           map[string]string{"label1": "value1", "label2": "value4", "label5": "value10"},
		mergeStrategy: MetaPreferProm,
		want:          map[string]string{"label1": "value1", "label2": "value4", "label5": "value10"},
	})

	// add new label with VM priority
	f(opts{
		old:           map[string]string{"label1": "value1", "label2": "value2", "label5": "value3"},
		new:           map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
		mergeStrategy: MetaPreferVM,
		want:          map[string]string{"label1": "value1", "label2": "value2", "label5": "value3"},
	})

	// remove all labels
	f(opts{
		old:           nil,
		new:           map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
		mergeStrategy: MetaPreferVM,
		want:          nil,
	})

	// remove keep old labels
	f(opts{
		old:           map[string]string{"label1": "value1", "label2": "value4"},
		new:           nil,
		mergeStrategy: MetaPreferVM,
		want:          map[string]string{"label1": "value1", "label2": "value4"},
	})

	// merge all labels with VMPriority
	f(opts{
		old:           map[string]string{"label1": "value1", "label2": "value4"},
		new:           map[string]string{"label1": "value2", "label2": "value4", "missinglabel": "value10"},
		mergeStrategy: MetaMergeLabelsVMPriority,
		want:          map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
	})
}
