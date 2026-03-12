package v1

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestVTCluster_Validate(t *testing.T) {
	type opts struct {
		clusterVersion   string
		componentVersion string
		wantErr          bool
	}
	f := func(o opts) {
		t.Helper()
		cr := &VTCluster{
			Spec: VTClusterSpec{
				ClusterVersion:   o.clusterVersion,
				ComponentVersion: o.componentVersion,
			},
		}
		err := cr.Validate()
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}

	// both componentVersion and clusterVersion present
	f(opts{
		clusterVersion:   "v1.0.0",
		componentVersion: "v1.1.0",
		wantErr:          true,
	})

	// only componentVersion present
	f(opts{
		componentVersion: "v1.1.0",
		wantErr:          false,
	})

	// only clusterVersion present
	f(opts{
		clusterVersion: "v1.0.0",
		wantErr:        false,
	})
}
