package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVMBackup_SnapshotDeletePathWithFlags(t *testing.T) {
	type opts struct {
		host      string
		port      string
		extraArgs map[string]string
		want      string
	}
	f := func(o opts) {
		t.Helper()
		cr := VMBackup{}
		if got := cr.SnapshotDeletePathWithFlags(o.host, o.port, o.extraArgs); got != o.want {
			t.Errorf("SnapshotDeletePathWithFlags() = %v, want %v", got, o.want)
		}
	}

	// default delete path
	f(opts{
		host: "localhost",
		port: "8428",
		want: "http://localhost:8428/snapshot/delete",
	})

	// delete path with prefix
	f(opts{
		host:      "127.0.0.1",
		port:      "8428",
		extraArgs: map[string]string{httpPathPrefixFlag: "/pref-1", "other-flag": "other-value"},
		want:      "http://127.0.0.1:8428/pref-1/snapshot/delete",
	})

	// delete path with auth key
	f(opts{
		host:      "127.0.0.1",
		port:      "8428",
		extraArgs: map[string]string{httpPathPrefixFlag: "/pref-1", "other-flag": "other-value", snapshotAuthKeyFlag: "test"},
		want:      "http://127.0.0.1:8428/pref-1/snapshot/delete?authKey=test",
	})
}

func TestVMBackup_SnapshotCreatePathWithFlags(t *testing.T) {
	type opts struct {
		host      string
		port      string
		extraArgs map[string]string
		want      string
	}
	f := func(o opts) {
		t.Helper()
		cr := VMBackup{}
		got := cr.SnapshotCreatePathWithFlags(o.host, o.port, o.extraArgs)
		assert.Equal(t, o.want, got)
	}

	// base ok
	f(opts{
		host: "localhost",
		port: "8429",
		want: "http://localhost:8429/snapshot/create",
	})

	// with prefix
	f(opts{
		host: "127.0.0.1",
		port: "8429",
		extraArgs: map[string]string{
			"http.pathPrefix": "/prefix/custom",
		},
		want: "http://127.0.0.1:8429/prefix/custom/snapshot/create",
	})

	// with prefix and auth key
	f(opts{
		host: "localhost",
		port: "8429",
		extraArgs: map[string]string{
			"http.pathPrefix": "/prefix/custom",
			"snapshotAuthKey": "some-auth-key",
		},
		want: "http://localhost:8429/prefix/custom/snapshot/create?authKey=some-auth-key",
	})
}
