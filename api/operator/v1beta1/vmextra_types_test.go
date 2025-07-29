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
	type opts struct {
		flags       map[string]string
		defaultPath string
		want        string
	}
	f := func(opts opts) {
		t.Helper()
		if got := BuildPathWithPrefixFlag(opts.flags, opts.defaultPath); got != opts.want {
			t.Errorf("buildPathWithPrefixFlag() = %v, want %v", got, opts.want)
		}
	}

	// default path
	o := opts{
		defaultPath: healthPath,
		want:        healthPath,
	}
	f(o)

	// with some prefix
	o = opts{
		flags: map[string]string{
			"some.flag":          "some-value",
			vmPathPrefixFlagName: "/prefix/path/",
		},
		defaultPath: healthPath,
		want:        fmt.Sprintf("/prefix/path%s", healthPath),
	}
	f(o)

	// with bad path
	o = opts{
		flags: map[string]string{
			"some.flag":          "some-value",
			vmPathPrefixFlagName: "badpath/badvalue",
		},
		defaultPath: healthPath,
		want:        fmt.Sprintf("badpath/badvalue%s", healthPath),
	}
	f(o)
}

func TestParsingMatch(t *testing.T) {
	f := func(data string, match StringOrArray, wantErr bool) {
		t.Helper()
		var newMatch StringOrArray
		err := yaml.Unmarshal([]byte(data), &newMatch)
		if err != nil {
			if !wantErr {
				t.Errorf("Match.UnmarshalYAML() error = %v, wantErr %v", err, wantErr)
			} else {
				return
			}
		}
		if !reflect.DeepEqual(newMatch, match) {
			t.Fatalf("Match.UnmarshalYAML() got wrong result: %v, want: %v", match, newMatch)
		}
	}

	// old string match
	f(`http_requests_total`, StringOrArray{"http_requests_total"}, false)

	// new list match
	f(`
- \{__name__=~"count1"\}
- \{__name__=~"count2"\}
`, StringOrArray{"\\{__name__=~\"count1\"\\}", "\\{__name__=~\"count2\"\\}"}, false)

	// wrong type of match
	f(`{__name__=~"count1"}`, StringOrArray{}, true)
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
