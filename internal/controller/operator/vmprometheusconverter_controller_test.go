package operator

import (
	"reflect"
	"testing"
)

func Test_mergeLabelsWithStrategy(t *testing.T) {
	tests := []struct {
		name          string
		old           map[string]string
		new           map[string]string
		mergeStrategy string
		want          map[string]string
	}{
		{
			name:          "delete not existing label",
			old:           map[string]string{"label1": "value1", "label2": "value2", "missinglabel": "value3"},
			new:           map[string]string{"label1": "value1", "label2": "value4"},
			mergeStrategy: MetaPreferProm,
			want:          map[string]string{"label1": "value1", "label2": "value4"},
		},
		{
			name:          "add new label",
			old:           map[string]string{"label1": "value1", "label2": "value2", "missinglabel": "value3"},
			new:           map[string]string{"label1": "value1", "label2": "value4", "label5": "value10"},
			mergeStrategy: MetaPreferProm,
			want:          map[string]string{"label1": "value1", "label2": "value4", "label5": "value10"},
		},
		{
			name:          "add new label with VM priority",
			old:           map[string]string{"label1": "value1", "label2": "value2", "label5": "value3"},
			new:           map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
			mergeStrategy: MetaPreferVM,
			want:          map[string]string{"label1": "value1", "label2": "value2", "label5": "value3"},
		},
		{
			name:          "remove all labels",
			old:           nil,
			new:           map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
			mergeStrategy: MetaPreferVM,
			want:          nil,
		},
		{
			name:          "remove keep old labels",
			old:           map[string]string{"label1": "value1", "label2": "value4"},
			new:           nil,
			mergeStrategy: MetaPreferVM,
			want:          map[string]string{"label1": "value1", "label2": "value4"},
		},
		{
			name:          "merge all labels with VMPriority",
			old:           map[string]string{"label1": "value1", "label2": "value4"},
			new:           map[string]string{"label1": "value2", "label2": "value4", "missinglabel": "value10"},
			mergeStrategy: MetaMergeLabelsVMPriority,
			want:          map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeLabelsWithStrategy(tt.old, tt.new, tt.mergeStrategy); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mergeLabelsWithStrategy() = %v, want %v", got, tt.want)
			}
		})
	}
}
