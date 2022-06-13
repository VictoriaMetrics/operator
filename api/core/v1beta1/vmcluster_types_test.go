package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVMBackup_SnapshotDeletePathWithFlags(t *testing.T) {
	type fields struct {
	}
	type args struct {
		port      string
		extraArgs map[string]string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   string
	}{
		{
			name: "default delete path",
			args: args{
				port:      "8428",
				extraArgs: nil,
			},
			want: "http://localhost:8428/snapshot/delete",
		},
		{
			name: "delete path with prefix",
			args: args{
				port:      "8428",
				extraArgs: map[string]string{vmPathPrefixFlagName: "/pref-1", "other-flag": "other-value"},
			},
			want: "http://localhost:8428/pref-1/snapshot/delete",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := VMBackup{}
			if got := cr.SnapshotDeletePathWithFlags(tt.args.port, tt.args.extraArgs); got != tt.want {
				t.Errorf("SnapshotDeletePathWithFlags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVMBackup_SnapshotCreatePathWithFlags(t *testing.T) {

	type args struct {
		port      string
		extraArgs map[string]string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "base ok",
			args: args{
				port: "8429",
			},
			want: "http://localhost:8429/snapshot/create",
		},
		{
			name: "with prefix",
			args: args{
				port: "8429",
				extraArgs: map[string]string{
					"http.pathPrefix": "/prefix/custom",
				},
			},
			want: "http://localhost:8429/prefix/custom/snapshot/create",
		},
		{
			name: "with prefix and auth key",
			args: args{
				port: "8429",
				extraArgs: map[string]string{
					"http.pathPrefix": "/prefix/custom",
					"snapshotAuthKey": "some-auth-key",
				},
			},
			want: "http://localhost:8429/prefix/custom/snapshot/create?authKey=some-auth-key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := VMBackup{}
			got := cr.SnapshotCreatePathWithFlags(tt.args.port, tt.args.extraArgs)
			assert.Equal(t, tt.want, got)
		})
	}
}
