package v1beta1

import (
	"testing"
)

func TestVMBackup_SnapshotDeletePathWithFlags(t *testing.T) {
	type opts struct {
		port      string
		want      string
		extraArgs map[string]string
	}
	f := func(opts opts) {
		t.Helper()
		cr := VMBackup{}
		if got := cr.SnapshotDeletePathWithFlags(opts.port, opts.extraArgs); got != opts.want {
			t.Errorf("SnapshotDeletePathWithFlags() = %v, want %v", got, opts.want)
		}
	}

	// default delete path
	o := opts{
		port: "8428",
		want: "http://localhost:8428/snapshot/delete",
	}
	f(o)

	// delete path with prefix
	o = opts{
		port: "8428",
		want: "http://localhost:8428/pref-1/snapshot/delete",
		extraArgs: map[string]string{
			vmPathPrefixFlagName: "/pref-1",
			"other-flag":         "other-value",
		},
	}
	f(o)
}

func TestVMBackup_SnapshotCreatePathWithFlags(t *testing.T) {
	type opts struct {
		port      string
		want      string
		extraArgs map[string]string
	}
	f := func(opts opts) {
		cr := VMBackup{}
		if got := cr.SnapshotCreatePathWithFlags(opts.port, opts.extraArgs); got != opts.want {
			t.Errorf("SnapshotDeletePathWithFlags() = %v, want %v", got, opts.want)
		}
	}

	// base ok
	o := opts{
		port: "8429",
		want: "http://localhost:8429/snapshot/create",
	}
	f(o)

	// with prefix
	o = opts{
		port: "8429",
		want: "http://localhost:8429/prefix/custom/snapshot/create",
		extraArgs: map[string]string{
			"http.pathPrefix": "/prefix/custom",
		},
	}
	f(o)

	// with prefix and auth key
	o = opts{
		port: "8429",
		want: "http://localhost:8429/prefix/custom/snapshot/create?authKey=some-auth-key",
		extraArgs: map[string]string{
			"http.pathPrefix": "/prefix/custom",
			"snapshotAuthKey": "some-auth-key",
		},
	}
	f(o)
}
