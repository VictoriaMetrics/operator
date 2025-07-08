package v1beta1

import (
	"testing"
)

func TestVMBackup_SnapshotDeletePathWithFlags(t *testing.T) {
	f := func(port, want string, extraArgs map[string]string) {
		t.Helper()
		cr := VMBackup{}
		if got := cr.SnapshotDeletePathWithFlags(port, extraArgs); got != want {
			t.Errorf("SnapshotDeletePathWithFlags() = %v, want %v", got, want)
		}
	}

	// default delete path
	f("8428", "http://localhost:8428/snapshot/delete", nil)

	// delete path with prefix
	f("8428", "http://localhost:8428/pref-1/snapshot/delete", map[string]string{
		vmPathPrefixFlagName: "/pref-1",
		"other-flag":         "other-value",
	})
}

func TestVMBackup_SnapshotCreatePathWithFlags(t *testing.T) {
	f := func(port, want string, extraArgs map[string]string) {
		cr := VMBackup{}
		if got := cr.SnapshotCreatePathWithFlags(port, extraArgs); got != want {
			t.Errorf("SnapshotDeletePathWithFlags() = %v, want %v", got, want)
		}
	}

	// base ok
	f("8429", "http://localhost:8429/snapshot/create", nil)

	// with prefix
	f("8429", "http://localhost:8429/prefix/custom/snapshot/create", map[string]string{
		"http.pathPrefix": "/prefix/custom",
	})

	// with prefix and auth key
	f("8429", "http://localhost:8429/prefix/custom/snapshot/create?authKey=some-auth-key", map[string]string{
		"http.pathPrefix": "/prefix/custom",
		"snapshotAuthKey": "some-auth-key",
	})
}
