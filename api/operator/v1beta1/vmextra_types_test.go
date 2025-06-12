package v1beta1

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

func Test_buildPathWithPrefixFlag(t *testing.T) {
	type args struct {
		flags       map[string]string
		defaultPath string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "default path",
			args: args{
				defaultPath: healthPath,
				flags:       nil,
			},
			want: healthPath,
		},
		{
			name: "with some prefix",
			args: args{
				defaultPath: healthPath,
				flags:       map[string]string{"some.flag": "some-value", vmPathPrefixFlagName: "/prefix/path/"},
			},
			want: fmt.Sprintf("/prefix/path%s", healthPath),
		},
		{
			name: "with bad path ",
			args: args{
				defaultPath: healthPath,
				flags:       map[string]string{"some.flag": "some-value", vmPathPrefixFlagName: "badpath/badvalue"},
			},
			want: fmt.Sprintf("badpath/badvalue%s", healthPath),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildPathWithPrefixFlag(tt.args.flags, tt.args.defaultPath); got != tt.want {
				t.Errorf("buildPathWithPrefixFlag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParsingMatch(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		match   StringOrArray
		wantErr bool
	}{
		{
			name:  "old string match",
			data:  `http_requests_total`,
			match: StringOrArray{"http_requests_total"},
		},
		{
			name: "new list match",
			data: `
- \{__name__=~"count1"\}
- \{__name__=~"count2"\}
`,
			match: StringOrArray{"\\{__name__=~\"count1\"\\}", "\\{__name__=~\"count2\"\\}"},
		},
		{
			name:    "wrong type of match",
			data:    `{__name__=~"count1"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var match StringOrArray
			err := yaml.Unmarshal([]byte(tt.data), &match)
			if (err != nil) != tt.wantErr {
				t.Errorf("Match.UnmarshalYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(match, tt.match) {
				t.Fatalf("Match.UnmarshalYAML() got wrong result: %v, want: %v", match, tt.match)
			}
		})
	}
}

func TestStringOrArrayMarshal(t *testing.T) {
	f := func(src *StringOrArray, marshalF func(any) ([]byte, error), expected string) {
		t.Helper()
		got, err := marshalF(src)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		assert.Equal(t, expected, string(got))
	}

	f(&StringOrArray{"1", "2", "3"}, json.Marshal, `["1","2","3"]`)
	f(&StringOrArray{"1"}, json.Marshal, `"1"`)
	f(&StringOrArray{}, json.Marshal, `""`)
	f(&StringOrArray{"1", "2", "3"}, yaml.Marshal, `- "1"
- "2"
- "3"
`)
	f(&StringOrArray{"1"}, yaml.Marshal, `"1"
`)
	f(&StringOrArray{}, yaml.Marshal, `""
`)

}

func TestStringOrArrayUnMarshal(t *testing.T) {
	f := func(src string, unmarshalF func([]byte, any) error, expected StringOrArray) {
		t.Helper()
		var got StringOrArray
		if err := unmarshalF([]byte(src), &got); err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		assert.Equal(t, expected, got)
	}
	f(`["1","2","3"]`, json.Unmarshal, StringOrArray{"1", "2", "3"})
	f(`"1"`, json.Unmarshal, StringOrArray{"1"})
	f(`""`, json.Unmarshal, StringOrArray{""})
	f(`- "1"
- "2"
- "3"
`, yaml.Unmarshal, StringOrArray{"1", "2", "3"})

	f(`"1"
`, yaml.Unmarshal, StringOrArray{"1"})
	f(`""
`, yaml.Unmarshal, StringOrArray{""})

}
