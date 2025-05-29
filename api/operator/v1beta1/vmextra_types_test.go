package v1beta1

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	"k8s.io/utils/ptr"

	corev1 "k8s.io/api/core/v1"
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

func TestLicense_MaybeAddToArgs(t *testing.T) {
	type args struct {
		args           []string
		secretMountDir string
	}
	tests := []struct {
		name    string
		license License
		args    args
		want    []string
	}{
		{
			name: "license key provided",
			license: License{
				Key: ptr.To("test-key"),
			},
			args: args{
				args:           []string{},
				secretMountDir: "/etc/secrets",
			},
			want: []string{"-license=test-key"},
		},
		{
			name: "license key provided with force offline",
			license: License{
				Key:          ptr.To("test-key"),
				ForceOffline: ptr.To(true),
			},
			args: args{
				args:           []string{},
				secretMountDir: "/etc/secrets",
			},
			want: []string{"-license=test-key", "-license.forceOffline=true"},
		},
		{
			name: "license key provided with reload interval",
			license: License{
				KeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "license-secret"},
					Key:                  "license-key",
				},
				ReloadInterval: ptr.To("30s"),
			},
			args: args{
				args:           []string{},
				secretMountDir: "/etc/secrets",
			},
			want: []string{"-licenseFile=/etc/secrets/license-secret/license-key", "-licenseFile.reloadInterval=30s"},
		},
		{
			name: "license key provided via secret with force offline",
			license: License{
				KeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "license-secret"},
					Key:                  "license-key",
				},
				ForceOffline: ptr.To(true),
			},
			args: args{
				args:           []string{},
				secretMountDir: "/etc/secrets",
			},
			want: []string{"-licenseFile=/etc/secrets/license-secret/license-key", "-license.forceOffline=true"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.license.MaybeAddToArgs(tt.args.args, tt.args.secretMountDir)
			slices.Sort(got)
			slices.Sort(tt.want)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("License.MaybeAddToArgs() = %v, want %v", got, tt.want)
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
