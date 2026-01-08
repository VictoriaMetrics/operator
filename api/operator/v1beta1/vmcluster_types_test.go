package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVMBackup_SnapshotDeletePathWithFlags(t *testing.T) {
	type opts struct {
		port      string
		extraArgs map[string]ArgValue
		want      string
	}
	f := func(o opts) {
		t.Helper()
		cr := VMBackup{}
		if got := cr.SnapshotDeletePathWithFlags(o.port, o.extraArgs); got != o.want {
			t.Errorf("SnapshotDeletePathWithFlags() = %v, want %v", got, o.want)
		}
	}

	// default delete path
	f(opts{
		port: "8428",
		want: "http://localhost:8428/snapshot/delete",
	})

	// delete path with prefix
	f(opts{
		port: "8428",
		extraArgs: map[string]ArgValue{
			vmPathPrefixFlagName: []string{"/pref-1"},
			"other-flag":         []string{"other-value"},
		},
		want: "http://localhost:8428/pref-1/snapshot/delete",
	})
}

func TestVMBackup_SnapshotCreatePathWithFlags(t *testing.T) {
	type opts struct {
		port      string
		extraArgs map[string]ArgValue
		want      string
	}
	f := func(o opts) {
		t.Helper()
		cr := VMBackup{}
		got := cr.SnapshotCreatePathWithFlags(o.port, o.extraArgs)
		assert.Equal(t, o.want, got)
	}

	// base ok
	f(opts{
		port: "8429",
		want: "http://localhost:8429/snapshot/create",
	})

	// with prefix
	f(opts{
		port: "8429",
		extraArgs: map[string]ArgValue{
			"http.pathPrefix": []string{"/prefix/custom"},
		},
		want: "http://localhost:8429/prefix/custom/snapshot/create",
	})

	// with prefix and auth key
	f(opts{
		port: "8429",
		extraArgs: map[string]ArgValue{
			"http.pathPrefix": []string{"/prefix/custom"},
			"snapshotAuthKey": []string{"some-auth-key"},
		},
		want: "http://localhost:8429/prefix/custom/snapshot/create?authKey=some-auth-key",
	})
}
