package v1beta1

import (
	"fmt"
	"testing"
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
		{name: "default path",
			args: args{
				defaultPath: healthPath,
				flags:       nil,
			},
			want: healthPath,
		},
		{name: "with some prefix",
			args: args{
				defaultPath: healthPath,
				flags:       map[string]string{"some.flag": "some-value", vmPathPrefixFlagName: "/prefix/path/"},
			},
			want: fmt.Sprintf("/prefix/path%s", healthPath),
		},
		{name: "with bad path ",
			args: args{
				defaultPath: healthPath,
				flags:       map[string]string{"some.flag": "some-value", vmPathPrefixFlagName: "badpath/badvalue"},
			},
			want: fmt.Sprintf("badpath/badvalue%s", healthPath),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildPathWithPrefixFlag(tt.args.flags, tt.args.defaultPath); got != tt.want {
				t.Errorf("buildPathWithPrefixFlag() = %v, want %v", got, tt.want)
			}
		})
	}
}
